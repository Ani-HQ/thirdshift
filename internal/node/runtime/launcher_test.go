package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/node/runtime/fakellama"
)

func TestMain(m *testing.M) {
	if os.Getenv("THIRDSHIFT_FAKE_LLAMA_SERVER") == "1" {
		os.Exit(fakellama.Main(os.Args[1:]))
	}
	if os.Getenv("THIRDSHIFT_FAKE_LLAMA_CRASH_STARTUP") == "1" {
		time.Sleep(50 * time.Millisecond)
		fmt.Fprintln(os.Stderr, "fake llama-server process crashed during startup")
		os.Exit(42)
	}
	os.Exit(m.Run())
}

func TestLauncherRejectsNonLoopback(t *testing.T) {
	_, err := StartLlamaServer(context.Background(), LaunchConfig{
		ExecutablePath: os.Args[0],
		ModelPath:      filepath.Join(t.TempDir(), "model.gguf"),
		Args: ApprovedArgs{
			Host: "0.0.0.0",
		},
	})
	if err == nil {
		t.Fatal("non-loopback launch succeeded, want error")
	}
}

func TestLauncherHealthShutdownAndLogRedaction(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}

	prompt := "secret prompt body"
	running, err := StartLlamaServer(context.Background(), LaunchConfig{
		ExecutablePath:  os.Args[0],
		ModelPath:       modelPath,
		WorkDir:         dir,
		LogDir:          dir,
		HealthInterval:  20 * time.Millisecond,
		StartupTimeout:  2 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		MaxLogBytes:     1024,
		Redactions:      []string{prompt},
		Env:             []string{"THIRDSHIFT_FAKE_LLAMA_SERVER=1"},
		Args: ApprovedArgs{
			Host:        "127.0.0.1",
			ContextSize: 128,
			BatchSize:   16,
			GPULayers:   "0",
			Parallel:    1,
		},
	})
	if err != nil {
		t.Fatalf("start fake runtime: %v", err)
	}

	completion, err := ChatCompletion(context.Background(), running.BaseURL, CompletionRequest{
		Model:       "thirdshift-test-chat-v1",
		Messages:    []ChatMessage{{Role: "user", Content: prompt}},
		Temperature: 0.2,
		MaxTokens:   16,
		Stream:      false,
	})
	if err != nil {
		t.Fatalf("chat completion: %v", err)
	}
	if len(completion.Choices) != 1 || !strings.Contains(completion.Choices[0].Message.Content, prompt) {
		t.Fatalf("unexpected completion: %#v", completion)
	}
	if err := running.Stop(context.Background(), 2*time.Second); err != nil {
		t.Fatalf("stop runtime: %v", err)
	}

	logData, err := os.ReadFile(filepath.Join(dir, "llama-server.log"))
	if err != nil {
		t.Fatalf("read runtime log: %v", err)
	}
	if strings.Contains(string(logData), prompt) {
		t.Fatalf("runtime log contains prompt body: %s", string(logData))
	}
	if !strings.Contains(string(logData), "[REDACTED]") {
		t.Fatalf("runtime log does not contain redaction marker: %s", string(logData))
	}
}

func TestLauncherDefaultStartupTimeoutAllowsHealthAfterOldDefault(t *testing.T) {
	const oldStartupTimeout = 30 * time.Second
	if defaultStartupTimeout != 5*time.Minute {
		t.Fatalf("default startup timeout = %v, want 5m", defaultStartupTimeout)
	}

	clock := &manualHealthClock{}
	healthCalls := make(chan time.Duration, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elapsed := clock.Elapsed()
		healthCalls <- elapsed
		if elapsed <= oldStartupTimeout {
			http.Error(w, "model loading", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	running := &RunningRuntime{
		BaseURL:     server.URL,
		done:        make(chan struct{}),
		healthClock: clock,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- running.waitForHealth(context.Background(), defaultStartupTimeout, defaultHealthInterval)
	}()

	if elapsed := waitForHealthCall(t, healthCalls); elapsed != 0 {
		t.Fatalf("first health check at %v, want 0", elapsed)
	}
	for _, want := range []time.Duration{10 * time.Second, 20 * time.Second, oldStartupTimeout} {
		clock.Advance(defaultHealthInterval)
		if elapsed := waitForHealthCall(t, healthCalls); elapsed != want {
			t.Fatalf("health check at %v, want %v", elapsed, want)
		}
	}
	select {
	case err := <-errCh:
		t.Fatalf("launcher returned before health passed old 30s default: %v", err)
	default:
	}

	clock.Advance(defaultHealthInterval)
	elapsed := waitForHealthCall(t, healthCalls)
	if elapsed <= oldStartupTimeout {
		t.Fatalf("successful health check happened at %v, want after %v", elapsed, oldStartupTimeout)
	}
	if err := waitForHealthResult(t, errCh); err != nil {
		t.Fatalf("wait for health: %v", err)
	}
}

func TestLauncherFailsFastWhenProcessExitsDuringStartup(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.gguf")
	if err := os.WriteFile(modelPath, []byte("fake model"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}

	started := time.Now()
	_, err := StartLlamaServer(context.Background(), LaunchConfig{
		ExecutablePath:  os.Args[0],
		ModelPath:       modelPath,
		WorkDir:         dir,
		LogDir:          dir,
		HealthInterval:  20 * time.Millisecond,
		ShutdownTimeout: time.Second,
		MaxLogBytes:     1024,
		Env:             []string{"THIRDSHIFT_FAKE_LLAMA_CRASH_STARTUP=1"},
		Args: ApprovedArgs{
			Host: "127.0.0.1",
		},
	})
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("start fake crashed runtime succeeded, want error")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("crashed process failure took %v, want under 2s", elapsed)
	}
	errText := err.Error()
	if !strings.Contains(errText, "process") || !strings.Contains(errText, "exit status") {
		t.Fatalf("error = %q, want process exit error", errText)
	}
	if strings.Contains(errText, "timed out") || strings.Contains(errText, "deadline") {
		t.Fatalf("error = %q, want process exit instead of deadline", errText)
	}
}

func waitForHealthCall(t *testing.T, calls <-chan time.Duration) time.Duration {
	t.Helper()
	select {
	case elapsed := <-calls:
		return elapsed
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for health call")
		return 0
	}
}

func waitForHealthResult(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for health result")
		return nil
	}
}

type manualHealthClock struct {
	mu      sync.Mutex
	elapsed time.Duration
	timeout time.Duration
	cancel  context.CancelFunc
	ticker  *manualHealthTicker
}

func (c *manualHealthClock) WithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	deadline, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.timeout = timeout
	c.cancel = cancel
	c.mu.Unlock()
	return deadline, cancel
}

func (c *manualHealthClock) NewTicker(time.Duration) healthTicker {
	ticker := &manualHealthTicker{ch: make(chan time.Time, 64)}
	c.mu.Lock()
	c.ticker = ticker
	c.mu.Unlock()
	return ticker
}

func (c *manualHealthClock) Advance(delta time.Duration) {
	c.mu.Lock()
	c.elapsed += delta
	elapsed := c.elapsed
	timeout := c.timeout
	cancel := c.cancel
	ticker := c.ticker
	c.mu.Unlock()

	if timeout > 0 && elapsed >= timeout && cancel != nil {
		cancel()
		return
	}
	if ticker != nil {
		ticker.Tick(elapsed)
	}
}

func (c *manualHealthClock) Elapsed() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.elapsed
}

type manualHealthTicker struct {
	ch chan time.Time
}

func (t *manualHealthTicker) C() <-chan time.Time {
	return t.ch
}

func (t *manualHealthTicker) Stop() {}

func (t *manualHealthTicker) Tick(elapsed time.Duration) {
	t.ch <- time.Unix(0, int64(elapsed))
}
