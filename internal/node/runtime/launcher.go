package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	defaultHealthInterval  = 10 * time.Second
	defaultStartupTimeout  = 5 * time.Minute
	defaultShutdownTimeout = 5 * time.Second
	defaultMaxLogBytes     = int64(1 << 20)
)

type LaunchConfig struct {
	ExecutablePath  string
	ModelPath       string
	WorkDir         string
	LogDir          string
	Args            ApprovedArgs
	HealthInterval  time.Duration
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
	MaxLogBytes     int64
	Redactions      []string
	Env             []string
}

type ApprovedArgs struct {
	ContextSize int
	BatchSize   int
	GPULayers   string
	Device      string
	Parallel    int
	Host        string
	Port        int
}

type RunningRuntime struct {
	BaseURL string
	Port    int

	cmd      *exec.Cmd
	done     chan struct{}
	controls platformControls
	stopOnce sync.Once
	waitMu   sync.Mutex
	waitErr  error

	healthClock healthClock
}

func StartLlamaServer(ctx context.Context, cfg LaunchConfig) (*RunningRuntime, error) {
	if cfg.ExecutablePath == "" {
		return nil, fmt.Errorf("runtime executable path is required")
	}
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("model path is required")
	}
	if cfg.Args.Host != "127.0.0.1" {
		return nil, fmt.Errorf("runtime host %q is rejected; llama-server must bind to 127.0.0.1", cfg.Args.Host)
	}
	if cfg.HealthInterval == 0 {
		cfg.HealthInterval = defaultHealthInterval
	}
	if cfg.StartupTimeout == 0 {
		cfg.StartupTimeout = defaultStartupTimeout
	}
	if cfg.ShutdownTimeout == 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}
	if cfg.MaxLogBytes == 0 {
		cfg.MaxLogBytes = defaultMaxLogBytes
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = filepath.Dir(cfg.ExecutablePath)
	}
	if cfg.LogDir == "" {
		cfg.LogDir = cfg.WorkDir
	}

	port := cfg.Args.Port
	if port == 0 {
		freePort, err := chooseFreeLoopbackPort()
		if err != nil {
			return nil, err
		}
		port = freePort
	}

	logWriter, err := newRotatingLogWriter(filepath.Join(cfg.LogDir, "llama-server.log"), cfg.MaxLogBytes, cfg.Redactions)
	if err != nil {
		return nil, err
	}

	args := buildLlamaArgs(cfg, port)
	cmd := exec.Command(cfg.ExecutablePath, args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(os.Environ(), cfg.Env...)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter

	controls := newPlatformControls(cfg)
	if err := controls.BeforeStart(cmd); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start llama-server: %w", err)
	}
	if err := controls.AfterStart(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}

	running := &RunningRuntime{
		BaseURL:  "http://127.0.0.1:" + strconv.Itoa(port),
		Port:     port,
		cmd:      cmd,
		done:     make(chan struct{}),
		controls: controls,
	}
	go func() {
		running.setWaitErr(cmd.Wait())
		close(running.done)
	}()

	if err := running.waitForHealth(ctx, cfg.StartupTimeout, cfg.HealthInterval); err != nil {
		_ = running.Stop(context.Background(), cfg.ShutdownTimeout)
		return nil, err
	}
	return running, nil
}

func (r *RunningRuntime) Stop(ctx context.Context, timeout time.Duration) error {
	var stopErr error
	r.stopOnce.Do(func() {
		defer r.controls.Cleanup()
		select {
		case <-r.done:
			err := r.getWaitErr()
			if err != nil && !errors.Is(err, os.ErrProcessDone) {
				stopErr = err
			}
			return
		default:
		}

		if timeout == 0 {
			timeout = defaultShutdownTimeout
		}
		_ = r.cmd.Process.Signal(os.Interrupt)
		waitCh := make(chan error, 1)
		go func() {
			<-r.done
			waitCh <- r.getWaitErr()
		}()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case err := <-waitCh:
			if err != nil && !errors.Is(err, os.ErrProcessDone) {
				stopErr = err
			}
		case <-timer.C:
			if err := r.cmd.Process.Kill(); err != nil {
				stopErr = fmt.Errorf("kill llama-server after shutdown timeout: %w", err)
			}
			<-waitCh
		case <-ctx.Done():
			_ = r.cmd.Process.Kill()
			stopErr = ctx.Err()
		}
	})
	return stopErr
}

func (r *RunningRuntime) waitForHealth(ctx context.Context, timeout, interval time.Duration) error {
	clock := r.healthClock
	if clock == nil {
		clock = realHealthClock{}
	}
	deadline, cancel := clock.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := clock.NewTicker(interval)
	defer ticker.Stop()

	go func() {
		select {
		case <-r.done:
			cancel()
		case <-deadline.Done():
		}
	}()

	for {
		select {
		case <-r.done:
			return r.processExitedBeforeHealthErr()
		case <-deadline.Done():
			return r.healthDeadlineErr(deadline)
		default:
		}
		if ok := r.checkHealth(deadline); ok {
			return nil
		}
		select {
		case <-r.done:
			return r.processExitedBeforeHealthErr()
		case <-deadline.Done():
			return r.healthDeadlineErr(deadline)
		case <-ticker.C():
		}
	}
}

func (r *RunningRuntime) processExitedBeforeHealthErr() error {
	if err := r.getWaitErr(); err != nil {
		return fmt.Errorf("llama-server process exited before health check passed: %w", err)
	}
	return fmt.Errorf("llama-server process exited before health check passed")
}

func (r *RunningRuntime) healthDeadlineErr(deadline context.Context) error {
	select {
	case <-r.done:
		return r.processExitedBeforeHealthErr()
	default:
	}
	err := deadline.Err()
	if err == nil {
		err = context.DeadlineExceeded
	}
	return fmt.Errorf("llama-server health check timed out: %w", err)
}

func (r *RunningRuntime) setWaitErr(err error) {
	r.waitMu.Lock()
	defer r.waitMu.Unlock()
	r.waitErr = err
}

func (r *RunningRuntime) getWaitErr() error {
	r.waitMu.Lock()
	defer r.waitMu.Unlock()
	return r.waitErr
}

func (r *RunningRuntime) checkHealth(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.BaseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

type healthClock interface {
	WithTimeout(context.Context, time.Duration) (context.Context, context.CancelFunc)
	NewTicker(time.Duration) healthTicker
}

type healthTicker interface {
	C() <-chan time.Time
	Stop()
}

type realHealthClock struct{}

func (realHealthClock) WithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeout)
}

func (realHealthClock) NewTicker(interval time.Duration) healthTicker {
	return realHealthTicker{Ticker: time.NewTicker(interval)}
}

type realHealthTicker struct {
	*time.Ticker
}

func (t realHealthTicker) C() <-chan time.Time {
	return t.Ticker.C
}

func buildLlamaArgs(cfg LaunchConfig, port int) []string {
	args := []string{
		"--host", cfg.Args.Host,
		"--port", strconv.Itoa(port),
		"--model", cfg.ModelPath,
	}
	if cfg.Args.ContextSize > 0 {
		args = append(args, "--ctx-size", strconv.Itoa(cfg.Args.ContextSize))
	}
	if cfg.Args.BatchSize > 0 {
		args = append(args, "--batch-size", strconv.Itoa(cfg.Args.BatchSize))
	}
	if cfg.Args.GPULayers != "" {
		args = append(args, "--n-gpu-layers", cfg.Args.GPULayers)
	}
	if cfg.Args.Device != "" {
		args = append(args, "--device", cfg.Args.Device)
	}
	if cfg.Args.Parallel > 0 {
		args = append(args, "--parallel", strconv.Itoa(cfg.Args.Parallel))
	}
	return args
}

func chooseFreeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("choose loopback port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
