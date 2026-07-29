package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anianroid/thirdshift/internal/node/config"
	"github.com/anianroid/thirdshift/internal/node/control"
	"github.com/anianroid/thirdshift/internal/node/identity"
	"github.com/anianroid/thirdshift/internal/node/models"
	"github.com/anianroid/thirdshift/internal/node/session"
	nodestate "github.com/anianroid/thirdshift/internal/node/state"
	"github.com/anianroid/thirdshift/internal/node/telemetry"
	"github.com/anianroid/thirdshift/internal/shared/ids"
	"github.com/anianroid/thirdshift/internal/shared/protocol"
	"github.com/anianroid/thirdshift/internal/shared/version"
	"nhooyr.io/websocket"
)

type RuntimeStatus struct {
	ModelID     string
	RuntimeHash string
	ModelHash   string
}

type RuntimeStatusProvider interface {
	Prepare(ctx context.Context, modelID string) (RuntimeStatus, error)
}

type Options struct {
	DataDir           string
	CatalogDir        string
	CoordinatorURL    string
	ModelID           string
	AccessToken       string
	NodeID            string
	AgentVersion      string
	HeartbeatInterval time.Duration
	HTTPClient        *http.Client
	Validator         *protocol.Validator
	Telemetry         telemetry.Provider
	Runtime           RuntimeStatusProvider
	Backoff           session.Backoff
	Now               func() time.Time
	Output            io.Writer
}

type Agent struct {
	opts            Options
	mu              sync.Mutex
	state           nodestate.State
	runtimeStatus   RuntimeStatus
	gpu             protocol.GPUStatus
	sessionID       string
	connected       bool
	sequence        int64
	lastHeartbeatAt *time.Time
	lastError       string
	startedAt       time.Time
}

func Run(ctx context.Context, opts Options) error {
	agent, err := New(opts)
	if err != nil {
		return err
	}
	return agent.Run(ctx)
}

func New(opts Options) (*Agent, error) {
	if opts.DataDir == "" {
		opts.DataDir = config.DefaultDataDir()
	}
	if opts.CatalogDir == "" {
		opts.CatalogDir = "models/catalog"
	}
	if opts.AgentVersion == "" {
		opts.AgentVersion = version.Version
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 15 * time.Second
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.Validator == nil {
		validator, err := protocol.NewValidator("")
		if err != nil {
			return nil, err
		}
		opts.Validator = validator
	}
	if opts.Telemetry == nil {
		opts.Telemetry = telemetry.DefaultProvider()
	}
	if opts.Runtime == nil {
		opts.Runtime = &LocalRuntimeProvider{CatalogDir: opts.CatalogDir, DataDir: opts.DataDir, HTTPClient: opts.HTTPClient}
	}
	if opts.Output == nil {
		opts.Output = io.Discard
	}
	if opts.NodeID == "" || opts.AccessToken == "" || opts.CoordinatorURL == "" {
		creds, err := identity.LoadCredentials(opts.DataDir)
		if err != nil {
			return nil, err
		}
		if opts.NodeID == "" {
			opts.NodeID = creds.NodeID
		}
		if opts.AccessToken == "" {
			opts.AccessToken = creds.AccessToken
		}
		if opts.CoordinatorURL == "" {
			opts.CoordinatorURL = creds.CoordinatorURL
		}
	}
	if opts.ModelID == "" {
		cfg, err := config.Load(opts.DataDir)
		if err == nil {
			opts.ModelID = cfg.ModelID
		}
	}
	if opts.ModelID == "" {
		opts.ModelID = "thirdshift-tiny-chat-v1"
	}
	return &Agent{
		opts:      opts,
		state:     nodestate.Offline,
		startedAt: opts.now(),
	}, nil
}

func (a *Agent) Run(ctx context.Context) error {
	if err := os.MkdirAll(a.opts.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	controlCtx, stopControl := context.WithCancel(ctx)
	defer stopControl()
	go func() {
		if err := control.Serve(controlCtx, a.opts.DataDir, a.handleControl); err != nil && !errors.Is(err, context.Canceled) {
			a.setError(err.Error())
		}
	}()

	if err := a.transition(nodestate.Starting); err != nil {
		return err
	}
	if err := a.transition(nodestate.Preparing); err != nil {
		return err
	}
	status, err := a.opts.Runtime.Prepare(ctx, a.opts.ModelID)
	if err != nil {
		_ = a.transition(nodestate.Error)
		a.setError(err.Error())
		return err
	}
	if closer, ok := a.opts.Runtime.(interface{ Close(context.Context) error }); ok {
		defer closer.Close(context.Background())
	}
	a.setRuntime(status)
	if err := a.transition(nodestate.Available); err != nil {
		return err
	}

	attempt := 0
	for {
		if ctx.Err() != nil {
			_ = a.transition(nodestate.Offline)
			a.setConnected("", false)
			return nil
		}
		err := a.runSession(ctx)
		a.setConnected("", false)
		if ctx.Err() != nil {
			_ = a.transition(nodestate.Offline)
			return nil
		}
		if err != nil {
			a.setError(err.Error())
			fmt.Fprintf(a.opts.Output, "session disconnected: %v\n", err)
		}
		delay := a.opts.Backoff.Delay(attempt)
		attempt++
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = a.transition(nodestate.Offline)
			return nil
		case <-timer.C:
		}
	}
}

func (a *Agent) runSession(ctx context.Context) error {
	endpoint, err := sessionURL(a.opts.CoordinatorURL)
	if err != nil {
		return err
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+a.opts.AccessToken)
	conn, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: a.opts.HTTPClient,
		HTTPHeader: header,
	})
	if err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := a.writeEnvelope(ctx, conn, protocol.TypeNodeHello, protocol.NodeHelloPayload{
		NodeID:                    a.opts.NodeID,
		AgentVersion:              a.opts.AgentVersion,
		SupportedProtocolVersions: []string{protocol.Version},
		Capabilities:              []string{"chat_completions"},
		Hostname:                  hostname(),
	}); err != nil {
		return err
	}
	_, acceptedData, err := conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("read session.accepted: %w", err)
	}
	acceptedEnvelope, err := a.opts.Validator.ValidateEnvelope(acceptedData)
	if err != nil {
		return err
	}
	if acceptedEnvelope.Type != protocol.TypeSessionAccepted {
		return fmt.Errorf("expected session.accepted, got %s", acceptedEnvelope.Type)
	}
	var accepted protocol.SessionAcceptedPayload
	if err := json.Unmarshal(acceptedEnvelope.Payload, &accepted); err != nil {
		return fmt.Errorf("decode session.accepted: %w", err)
	}
	if accepted.NodeID != a.opts.NodeID {
		return fmt.Errorf("session accepted for node %s, want %s", accepted.NodeID, a.opts.NodeID)
	}
	a.setConnected(accepted.SessionID, true)
	fmt.Fprintf(a.opts.Output, "connected session %s\n", accepted.SessionID)

	ticker := time.NewTicker(a.opts.HeartbeatInterval)
	defer ticker.Stop()
	if err := a.sendHeartbeat(ctx, conn); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := a.sendHeartbeat(ctx, conn); err != nil {
				return err
			}
		}
	}
}

func (a *Agent) sendHeartbeat(ctx context.Context, conn *websocket.Conn) error {
	a.mu.Lock()
	state := a.state
	runtimeStatus := a.runtimeStatus
	a.sequence++
	sequence := a.sequence
	uptime := int64(a.opts.now().Sub(a.startedAt).Seconds())
	a.mu.Unlock()

	gpu, err := a.opts.Telemetry.GPUStatus(ctx)
	if err != nil {
		gpu = protocol.GPUStatus{Name: "telemetry-unavailable"}
	}
	heartbeat := BuildHeartbeat(a.opts.NodeID, sequence, state, runtimeStatus, gpu, uptime, a.opts.now())
	if err := a.writeEnvelope(ctx, conn, protocol.TypeNodeHeartbeat, heartbeat); err != nil {
		return err
	}
	now := a.opts.now()
	a.mu.Lock()
	a.gpu = gpu
	a.lastHeartbeatAt = &now
	a.mu.Unlock()
	a.writeStatus()
	return nil
}

func BuildHeartbeat(nodeID string, sequence int64, state nodestate.State, runtimeStatus RuntimeStatus, gpu protocol.GPUStatus, uptimeSeconds int64, now time.Time) protocol.NodeHeartbeatPayload {
	return protocol.NodeHeartbeatPayload{
		NodeID:        nodeID,
		Sequence:      sequence,
		State:         string(state),
		ModelID:       runtimeStatus.ModelID,
		RuntimeHash:   runtimeStatus.RuntimeHash,
		ModelHash:     runtimeStatus.ModelHash,
		GPU:           gpu,
		ActiveJobID:   nil,
		UptimeSeconds: uptimeSeconds,
		Timestamp:     now.UTC(),
	}
}

func (a *Agent) writeEnvelope(ctx context.Context, conn *websocket.Conn, typ protocol.MessageType, payload any) error {
	messageID, err := ids.New("msg")
	if err != nil {
		return err
	}
	envelope, err := protocol.NewEnvelope(messageID, typ, a.opts.now(), payload)
	if err != nil {
		return err
	}
	data, err := a.opts.Validator.MarshalAndValidate(envelope)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

func (a *Agent) handleControl(command control.Command) control.Response {
	switch command.Action {
	case "pause":
		if err := a.transition(nodestate.Paused); err != nil {
			return control.Response{Error: err.Error()}
		}
		return control.Response{Status: a.status()}
	case "resume":
		if err := a.transition(nodestate.Available); err != nil {
			if retryErr := a.transition(nodestate.Idle); retryErr != nil {
				return control.Response{Error: err.Error()}
			}
		}
		return control.Response{Status: a.status()}
	case "status":
		return control.Response{Status: a.status()}
	default:
		return control.Response{Error: "unknown control command"}
	}
}

func (a *Agent) transition(to nodestate.State) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := nodestate.Transition(a.state, to); err != nil {
		return err
	}
	a.state = to
	a.lastError = ""
	a.writeStatusLocked()
	return nil
}

func (a *Agent) setRuntime(status RuntimeStatus) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runtimeStatus = status
	a.writeStatusLocked()
}

func (a *Agent) setConnected(sessionID string, connected bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessionID = sessionID
	a.connected = connected
	a.writeStatusLocked()
}

func (a *Agent) setError(message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastError = message
	a.writeStatusLocked()
}

func (a *Agent) status() *control.Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.statusLocked()
}

func (a *Agent) writeStatus() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.writeStatusLocked()
}

func (a *Agent) writeStatusLocked() {
	status := a.statusLocked()
	body, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(StatusPath(a.opts.DataDir), body, 0o600)
}

func (a *Agent) statusLocked() *control.Status {
	var temp *int
	var power *int
	if a.gpu.Name != "" {
		tempValue := a.gpu.TemperatureC
		powerValue := a.gpu.PowerW
		temp = &tempValue
		power = &powerValue
	}
	lastHeartbeat := a.lastHeartbeatAt
	return &control.Status{
		NodeID:            a.opts.NodeID,
		State:             string(a.state),
		GPU:               a.gpu.Name,
		ModelID:           a.runtimeStatus.ModelID,
		RuntimeHash:       a.runtimeStatus.RuntimeHash,
		ModelHash:         a.runtimeStatus.ModelHash,
		Schedule:          "placeholder",
		TemperatureC:      temp,
		PowerW:            power,
		SessionConnected:  a.connected,
		SessionID:         a.sessionID,
		LastHeartbeatAt:   lastHeartbeat,
		CredentialBackend: identity.CredentialBackendDescription(),
		CoordinatorURL:    a.opts.CoordinatorURL,
		HeartbeatInterval: a.opts.HeartbeatInterval.String(),
		LastError:         a.lastError,
	}
}

func StatusPath(dataDir string) string {
	return filepath.Join(dataDir, "status.json")
}

func ReadStatus(dataDir string) (*control.Status, error) {
	data, err := os.ReadFile(StatusPath(dataDir))
	if err != nil {
		return nil, fmt.Errorf("read status file: %w", err)
	}
	var status control.Status
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("decode status file: %w", err)
	}
	return &status, nil
}

func sessionURL(base string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse coordinator URL: %w", err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("coordinator URL must use http, https, ws, or wss")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/node/session"
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "unknown"
	}
	return name
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC()
	}
	return time.Now().UTC()
}

type CatalogRuntimeStatusProvider struct {
	CatalogDir string
}

func (p CatalogRuntimeStatusProvider) Prepare(_ context.Context, modelID string) (RuntimeStatus, error) {
	if p.CatalogDir == "" {
		p.CatalogDir = "models/catalog"
	}
	manifest, _, err := models.LoadCatalogManifest(p.CatalogDir, modelID)
	if err != nil {
		return RuntimeStatus{}, err
	}
	return RuntimeStatus{
		ModelID:     manifest.ModelID,
		RuntimeHash: "sha256:" + manifest.Runtime.BinarySHA256,
		ModelHash:   "sha256:" + manifest.Source.SHA256,
	}, nil
}
