package agent

import (
	"context"
	"crypto/ed25519"
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

	"github.com/Ani-HQ/thirdshift/internal/node/config"
	"github.com/Ani-HQ/thirdshift/internal/node/control"
	"github.com/Ani-HQ/thirdshift/internal/node/identity"
	"github.com/Ani-HQ/thirdshift/internal/node/models"
	noderuntime "github.com/Ani-HQ/thirdshift/internal/node/runtime"
	nodeschedule "github.com/Ani-HQ/thirdshift/internal/node/schedule"
	"github.com/Ani-HQ/thirdshift/internal/node/session"
	nodestate "github.com/Ani-HQ/thirdshift/internal/node/state"
	"github.com/Ani-HQ/thirdshift/internal/node/telemetry"
	"github.com/Ani-HQ/thirdshift/internal/shared/ids"
	"github.com/Ani-HQ/thirdshift/internal/shared/nodeauth"
	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
	"github.com/Ani-HQ/thirdshift/internal/shared/version"
	"nhooyr.io/websocket"
)

type RuntimeStatus struct {
	ModelID     string
	RuntimeHash string
	ModelHash   string
	BaseURL     string
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
	PrivateKey        ed25519.PrivateKey
	ScheduleFrom      string
	ScheduleUntil     string
	ScheduleLocation  *time.Location
	MaxTempC          int
	HardTempC         int
	ThermalHysteresis int
	PauseIdleTimeout  time.Duration
	ThermalPoll       time.Duration
	// GPUVendor is the detected host GPU vendor. It rides on the heartbeat so
	// the operator can see what backend a host is serving with.
	GPUVendor string
}

type Agent struct {
	opts            Options
	mu              sync.Mutex
	state           nodestate.State
	runtimeStatus   RuntimeStatus
	gpu             protocol.GPUStatus
	scheduleWindow  nodeschedule.Window
	scheduleState   string
	thermalState    string
	sessionID       string
	connected       bool
	sequence        int64
	activeJobID     *string
	activeJobCancel context.CancelFunc
	pauseRequested  bool
	pausedAt        *time.Time
	lastSentState   nodestate.State
	lastHeartbeatAt *time.Time
	lastError       string
	startedAt       time.Time
	writeMu         sync.Mutex
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
	cfg, cfgErr := config.Load(opts.DataDir)
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
		opts.Runtime = &LocalRuntimeProvider{CatalogDir: opts.CatalogDir, DataDir: opts.DataDir, GPUVendor: opts.GPUVendor, HTTPClient: opts.HTTPClient}
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
	if opts.PrivateKey == nil {
		privateKey, _, err := identity.LoadOrCreateKey(opts.DataDir)
		if err != nil {
			return nil, err
		}
		opts.PrivateKey = privateKey
	}
	if cfgErr == nil {
		if opts.ModelID == "" {
			opts.ModelID = cfg.ModelID
		}
		if opts.CoordinatorURL == "" {
			opts.CoordinatorURL = cfg.CoordinatorURL
		}
		if opts.ScheduleFrom == "" {
			opts.ScheduleFrom = cfg.ScheduleFrom
		}
		if opts.ScheduleUntil == "" {
			opts.ScheduleUntil = cfg.ScheduleUntil
		}
		if opts.MaxTempC == 0 {
			opts.MaxTempC = cfg.MaxTempC
		}
		if opts.HardTempC == 0 {
			opts.HardTempC = cfg.HardTempC
		}
		if opts.ThermalHysteresis == 0 {
			opts.ThermalHysteresis = cfg.ThermalHysteresis
		}
		if opts.PauseIdleTimeout == 0 {
			opts.PauseIdleTimeout = cfg.PauseIdleTimeout
		}
	}
	if opts.ModelID == "" {
		opts.ModelID = "thirdshift-tiny-chat-v1"
	}
	if opts.ScheduleFrom == "" {
		opts.ScheduleFrom = "00:00"
	}
	if opts.ScheduleUntil == "" {
		opts.ScheduleUntil = "00:00"
	}
	if opts.MaxTempC == 0 {
		opts.MaxTempC = 78
	}
	if opts.HardTempC == 0 {
		opts.HardTempC = opts.MaxTempC + 10
	}
	if opts.HardTempC <= opts.MaxTempC {
		opts.HardTempC = opts.MaxTempC + 10
	}
	if opts.ThermalHysteresis == 0 {
		opts.ThermalHysteresis = 5
	}
	if opts.PauseIdleTimeout == 0 {
		opts.PauseIdleTimeout = 5 * time.Minute
	}
	if opts.ThermalPoll == 0 {
		opts.ThermalPoll = 10 * time.Second
	}
	window, err := nodeschedule.ParseWindow(opts.ScheduleFrom, opts.ScheduleUntil)
	if err != nil {
		return nil, err
	}
	return &Agent{
		opts:           opts,
		state:          nodestate.Offline,
		scheduleWindow: window,
		scheduleState:  nodeschedule.StateInWindow,
		thermalState:   "normal",
		startedAt:      opts.now(),
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
	readErr := make(chan error, 1)
	go func() {
		readErr <- a.readCoordinator(ctx, conn)
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErr:
			return err
		case <-ticker.C:
			if err := a.sendHeartbeat(ctx, conn); err != nil {
				return err
			}
		}
	}
}

func (a *Agent) readCoordinator(ctx context.Context, conn *websocket.Conn) error {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		envelope, err := a.opts.Validator.ValidateEnvelope(data)
		if err != nil {
			return err
		}
		switch envelope.Type {
		case protocol.TypeJobOffer:
			var offer protocol.JobOfferPayload
			if err := json.Unmarshal(envelope.Payload, &offer); err != nil {
				return fmt.Errorf("decode job.offer: %w", err)
			}
			if err := a.handleJobOffer(ctx, conn, offer); err != nil {
				return err
			}
		case protocol.TypeJobCancel:
			// Cancellation is best-effort in M3. Jobs already running continue to
			// deadline; queued/leased jobs are cancelled coordinator-side.
		case protocol.TypeNodeConfigUpdated, protocol.TypeModelAssign, protocol.TypeModelUnload, protocol.TypeNodeDrain, protocol.TypeRuntimeUpdateAvailable:
			// These messages are part of the protocol but their behavior lands in
			// later milestones.
		default:
			return fmt.Errorf("unsupported coordinator message %s", envelope.Type)
		}
	}
}

func (a *Agent) sendHeartbeat(ctx context.Context, conn *websocket.Conn) error {
	a.mu.Lock()
	state := a.state
	runtimeStatus := a.runtimeStatus
	activeJobID := cloneStringPtr(a.activeJobID)
	a.sequence++
	sequence := a.sequence
	uptime := int64(a.opts.now().Sub(a.startedAt).Seconds())
	a.mu.Unlock()

	gpu, err := a.opts.Telemetry.GPUStatus(ctx)
	if err != nil {
		gpu = protocol.GPUStatus{Name: "telemetry-unavailable"}
	}
	stateChanged, safetyEvent := a.evaluateGuards(gpu, "")
	if stateChanged != nil {
		if err := a.writeEnvelope(ctx, conn, protocol.TypeNodeStateChanged, *stateChanged); err != nil {
			return err
		}
	}
	if safetyEvent != nil {
		if err := a.writeEnvelope(ctx, conn, protocol.TypeNodeSafetyEvent, *safetyEvent); err != nil {
			return err
		}
	}
	a.mu.Lock()
	state = a.state
	runtimeStatus = a.runtimeStatus
	activeJobID = cloneStringPtr(a.activeJobID)
	scheduleState := a.scheduleState
	thermalState := a.thermalState
	paused := a.pauseRequested || a.state == nodestate.Paused
	draining := a.state == nodestate.Draining
	a.mu.Unlock()
	heartbeat := buildHeartbeat(a.opts.NodeID, sequence, state, runtimeStatus, gpu, a.opts.GPUVendor, activeJobID, scheduleState, thermalState, paused, draining, uptime, a.opts.now())
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
	return buildHeartbeat(nodeID, sequence, state, runtimeStatus, gpu, "", nil, nodeschedule.StateInWindow, "normal", false, state == nodestate.Draining, uptimeSeconds, now)
}

func buildHeartbeat(nodeID string, sequence int64, state nodestate.State, runtimeStatus RuntimeStatus, gpu protocol.GPUStatus, gpuVendor string, activeJobID *string, scheduleState, thermalState string, paused, draining bool, uptimeSeconds int64, now time.Time) protocol.NodeHeartbeatPayload {
	return protocol.NodeHeartbeatPayload{
		NodeID:        nodeID,
		Sequence:      sequence,
		State:         string(state),
		ModelID:       runtimeStatus.ModelID,
		RuntimeHash:   runtimeStatus.RuntimeHash,
		ModelHash:     runtimeStatus.ModelHash,
		GPU:           gpu,
		GPUVendor:     gpuVendor,
		ActiveJobID:   activeJobID,
		ScheduleState: scheduleState,
		ThermalState:  thermalState,
		Paused:        paused,
		Draining:      draining,
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
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return conn.Write(ctx, websocket.MessageText, data)
}

func (a *Agent) evaluateGuards(gpu protocol.GPUStatus, reason string) (*protocol.NodeStateChangedPayload, *protocol.NodeSafetyEventPayload) {
	now := a.opts.now()
	a.mu.Lock()
	defer a.mu.Unlock()

	previousState := a.state
	previousThermal := a.thermalState
	if a.scheduleState == "" {
		a.scheduleState = nodeschedule.StateInWindow
	}
	a.scheduleState = a.scheduleWindow.StateAt(now, a.opts.ScheduleLocation)
	a.thermalState = a.nextThermalStateLocked(gpu.TemperatureC)

	active := a.activeJobID != nil
	runtimeReady := a.runtimeStatus.ModelID != "" && a.runtimeStatus.ModelHash != "" && a.runtimeStatus.RuntimeHash != "" && a.runtimeStatus.BaseURL != ""
	switch {
	case a.pauseRequested && active:
		a.forceStateLocked(nodestate.Draining)
	case a.pauseRequested && !active:
		a.forceStateLocked(nodestate.Paused)
	case a.thermalState == "hard_limit":
		if a.activeJobCancel != nil {
			a.activeJobCancel()
		}
		if active || a.state == nodestate.Busy {
			a.forceStateLocked(nodestate.Draining)
		} else if a.state == nodestate.Available || a.state == nodestate.Preparing || a.state == nodestate.Draining {
			a.forceStateLocked(nodestate.Idle)
		}
	case a.thermalState == "warm":
		if active || a.state == nodestate.Busy {
			a.forceStateLocked(nodestate.Draining)
		} else if a.state == nodestate.Available || a.state == nodestate.Preparing || a.state == nodestate.Draining {
			a.forceStateLocked(nodestate.Idle)
		}
	case a.scheduleState == nodeschedule.StateOutOfWindow && !active:
		if a.state == nodestate.Available || a.state == nodestate.Preparing {
			a.forceStateLocked(nodestate.Idle)
		}
	case a.scheduleState == nodeschedule.StateInWindow && a.thermalState == "normal" && !active:
		if (a.state == nodestate.Idle || a.state == nodestate.Draining) && runtimeReady {
			a.forceStateLocked(nodestate.Available)
		}
	}
	if a.state == nodestate.Paused && !active && a.pausedAt != nil && a.opts.PauseIdleTimeout > 0 && !now.Before(a.pausedAt.Add(a.opts.PauseIdleTimeout)) {
		if closer, ok := a.opts.Runtime.(interface{ Close(context.Context) error }); ok && a.runtimeStatus.BaseURL != "" {
			_ = closer.Close(context.Background())
			a.runtimeStatus = RuntimeStatus{}
		}
	}
	if a.state != previousState || a.state != a.lastSentState || a.lastSentState == "" {
		if reason == "" {
			reason = "guard_update"
		}
		from := previousState
		if a.lastSentState != "" && a.lastSentState != a.state {
			from = a.lastSentState
		}
		a.lastSentState = a.state
		a.writeStatusLocked()
		return &protocol.NodeStateChangedPayload{
			NodeID:        a.opts.NodeID,
			PreviousState: string(from),
			State:         string(a.state),
			Reason:        reason,
			Timestamp:     now,
		}, a.safetyEventLocked(previousThermal, gpu, now)
	}
	return nil, a.safetyEventLocked(previousThermal, gpu, now)
}

func (a *Agent) nextThermalStateLocked(tempC int) string {
	if a.thermalState == "" {
		a.thermalState = "normal"
	}
	if tempC <= 0 || a.opts.MaxTempC <= 0 {
		return "normal"
	}
	recoverAt := a.opts.MaxTempC - a.opts.ThermalHysteresis
	if recoverAt < 0 {
		recoverAt = 0
	}
	switch a.thermalState {
	case "warm", "hard_limit":
		if tempC <= recoverAt {
			return "normal"
		}
		if tempC >= a.opts.HardTempC {
			return "hard_limit"
		}
		return "warm"
	default:
		if tempC >= a.opts.HardTempC {
			return "hard_limit"
		}
		if tempC > a.opts.MaxTempC {
			return "warm"
		}
		return "normal"
	}
}

func (a *Agent) safetyEventLocked(previousThermal string, gpu protocol.GPUStatus, now time.Time) *protocol.NodeSafetyEventPayload {
	if a.thermalState == previousThermal || (a.thermalState != "warm" && a.thermalState != "hard_limit") {
		return nil
	}
	severity := "warning"
	eventCode := "over_temperature"
	message := "temperature exceeded configured limit"
	if a.thermalState == "hard_limit" {
		severity = "critical"
		eventCode = "thermal_hard_limit"
		message = "temperature exceeded hard safety limit"
	}
	temp := gpu.TemperatureC
	power := gpu.PowerW
	return &protocol.NodeSafetyEventPayload{
		NodeID:       a.opts.NodeID,
		EventCode:    eventCode,
		Severity:     severity,
		Message:      message,
		TemperatureC: &temp,
		PowerW:       &power,
		OccurredAt:   now,
	}
}

func (a *Agent) forceStateLocked(to nodestate.State) {
	if a.state == to {
		return
	}
	if nodestate.CanTransition(a.state, to) {
		a.state = to
		return
	}
	switch to {
	case nodestate.Paused:
		a.state = nodestate.Paused
	case nodestate.Draining:
		a.state = nodestate.Draining
	case nodestate.Idle:
		a.state = nodestate.Idle
	case nodestate.Available:
		a.state = nodestate.Available
	}
}

func (a *Agent) handleJobOffer(ctx context.Context, conn *websocket.Conn, offer protocol.JobOfferPayload) error {
	runtimeStatus, rejectReason := a.claimJob(offer)
	if rejectReason != "" {
		return a.writeEnvelope(ctx, conn, protocol.TypeJobRejected, protocol.JobRejectedPayload{
			JobID:      offer.JobID,
			AttemptID:  offer.AttemptID,
			ReasonCode: "node_not_eligible",
			Message:    rejectReason,
			Retryable:  true,
			RejectedAt: a.opts.now(),
		})
	}
	defer a.releaseJob()

	acceptedAt := a.opts.now()
	if err := a.writeEnvelope(ctx, conn, protocol.TypeJobAccepted, protocol.JobAcceptedPayload{
		JobID:      offer.JobID,
		AttemptID:  offer.AttemptID,
		AcceptedAt: acceptedAt,
	}); err != nil {
		return err
	}
	startedAt := a.opts.now()
	if err := a.writeEnvelope(ctx, conn, protocol.TypeJobStarted, protocol.JobStartedPayload{
		JobID:     offer.JobID,
		AttemptID: offer.AttemptID,
		StartedAt: startedAt,
	}); err != nil {
		return err
	}

	jobCtx := ctx
	var cancel context.CancelFunc
	if !offer.DeadlineAt.IsZero() {
		jobCtx, cancel = context.WithDeadline(ctx, offer.DeadlineAt)
	} else {
		jobCtx, cancel = context.WithCancel(ctx)
	}
	a.setActiveJobCancel(cancel)
	defer func() {
		cancel()
		a.setActiveJobCancel(nil)
	}()
	safetyCh := a.monitorJobThermal(jobCtx, conn, cancel)
	start := a.opts.now()
	completion, err := noderuntime.ChatCompletion(jobCtx, runtimeStatus.BaseURL, noderuntime.CompletionRequest{
		Model:       offer.ModelID,
		Messages:    runtimeMessages(offer.Request.Messages),
		Temperature: temperatureValue(offer.Request.Temperature),
		MaxTokens:   offer.Request.MaxTokens,
		Stream:      false,
	})
	if err != nil {
		if safetyReason := readSafetyReason(safetyCh); safetyReason != "" {
			return a.writeEnvelope(ctx, conn, protocol.TypeJobFailed, protocol.JobFailedPayload{
				JobID:     offer.JobID,
				AttemptID: offer.AttemptID,
				ErrorCode: "safety_limit",
				Message:   safetyReason,
				Retryable: true,
				FailedAt:  a.opts.now(),
			})
		}
		if a.isHardThermalBlocked() {
			return a.writeEnvelope(ctx, conn, protocol.TypeJobFailed, protocol.JobFailedPayload{
				JobID:     offer.JobID,
				AttemptID: offer.AttemptID,
				ErrorCode: "safety_limit",
				Message:   "temperature exceeded hard safety limit",
				Retryable: true,
				FailedAt:  a.opts.now(),
			})
		}
		return a.writeEnvelope(ctx, conn, protocol.TypeJobFailed, protocol.JobFailedPayload{
			JobID:     offer.JobID,
			AttemptID: offer.AttemptID,
			ErrorCode: "runtime_error",
			Message:   "Local runtime request failed.",
			Retryable: true,
			FailedAt:  a.opts.now(),
		})
	}
	if len(completion.Choices) == 0 {
		return a.writeEnvelope(ctx, conn, protocol.TypeJobFailed, protocol.JobFailedPayload{
			JobID:     offer.JobID,
			AttemptID: offer.AttemptID,
			ErrorCode: "empty_completion",
			Message:   "Local runtime returned no choices.",
			Retryable: false,
			FailedAt:  a.opts.now(),
		})
	}
	finishReason := completion.Choices[0].FinishReason
	if finishReason == "" {
		finishReason = "stop"
	}
	completedAt := a.opts.now()
	payload := protocol.JobCompletedPayload{
		JobID:       offer.JobID,
		AttemptID:   offer.AttemptID,
		ModelID:     offer.ModelID,
		RuntimeHash: runtimeStatus.RuntimeHash,
		ModelHash:   runtimeStatus.ModelHash,
		Message: &protocol.ChatMessage{
			Role:    completion.Choices[0].Message.Role,
			Content: completion.Choices[0].Message.Content,
		},
		Usage: protocol.Usage{
			PromptTokens:     completion.Usage.PromptTokens,
			CompletionTokens: completion.Usage.CompletionTokens,
			TotalTokens:      completion.Usage.TotalTokens,
		},
		DurationMillis: maxInt64(0, completedAt.Sub(start).Milliseconds()),
		FinishReason:   finishReason,
		CompletedAt:    completedAt,
	}
	signature, err := nodeauth.SignJobCompleted(a.opts.PrivateKey, a.opts.NodeID, payload, completedAt)
	if err != nil {
		return a.writeEnvelope(ctx, conn, protocol.TypeJobFailed, protocol.JobFailedPayload{
			JobID:     offer.JobID,
			AttemptID: offer.AttemptID,
			ErrorCode: "signature_failed",
			Message:   "Node could not sign the job result.",
			Retryable: false,
			FailedAt:  a.opts.now(),
		})
	}
	payload.Signature = &signature
	return a.writeEnvelope(ctx, conn, protocol.TypeJobCompleted, payload)
}

func (a *Agent) claimJob(offer protocol.JobOfferPayload) (RuntimeStatus, string) {
	if a.opts.now().After(offer.LeaseExpiresAt) {
		return RuntimeStatus{}, "job lease expired"
	}
	if err := validateOfferRequest(offer.Request); err != nil {
		return RuntimeStatus{}, err.Error()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state != nodestate.Available {
		return RuntimeStatus{}, "node is not available"
	}
	if a.pauseRequested {
		return RuntimeStatus{}, "node is paused or draining"
	}
	if a.scheduleState == nodeschedule.StateOutOfWindow {
		return RuntimeStatus{}, "node is outside its configured work window"
	}
	if a.thermalState != "" && a.thermalState != "normal" {
		return RuntimeStatus{}, "node is thermally blocked"
	}
	runtimeStatus := a.runtimeStatus
	if runtimeStatus.ModelID != offer.ModelID {
		return RuntimeStatus{}, "requested model is not loaded"
	}
	if runtimeStatus.ModelHash == "" || runtimeStatus.RuntimeHash == "" {
		return RuntimeStatus{}, "manifest hashes are not ready"
	}
	if runtimeStatus.BaseURL == "" {
		return RuntimeStatus{}, "local runtime is not reachable"
	}
	if err := nodestate.Transition(a.state, nodestate.Busy); err != nil {
		return RuntimeStatus{}, err.Error()
	}
	activeJobID := offer.JobID
	a.activeJobID = &activeJobID
	a.state = nodestate.Busy
	a.lastError = ""
	a.writeStatusLocked()
	return runtimeStatus, ""
}

func (a *Agent) releaseJob() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.activeJobID = nil
	a.activeJobCancel = nil
	if a.pauseRequested {
		now := a.opts.now()
		a.pausedAt = &now
		a.forceStateLocked(nodestate.Paused)
	} else if a.scheduleState == nodeschedule.StateOutOfWindow || (a.thermalState != "" && a.thermalState != "normal") {
		a.forceStateLocked(nodestate.Idle)
	} else if a.state == nodestate.Busy || a.state == nodestate.Draining {
		a.forceStateLocked(nodestate.Available)
	}
	a.writeStatusLocked()
}

func (a *Agent) setActiveJobCancel(cancel context.CancelFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.activeJobCancel = cancel
}

func (a *Agent) monitorJobThermal(ctx context.Context, conn *websocket.Conn, cancel context.CancelFunc) <-chan string {
	ch := make(chan string, 1)
	poll := a.opts.ThermalPoll
	if poll <= 0 {
		poll = 10 * time.Second
	}
	go func() {
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				gpu, err := a.opts.Telemetry.GPUStatus(ctx)
				if err != nil || gpu.TemperatureC <= 0 || gpu.TemperatureC < a.opts.HardTempC {
					continue
				}
				a.mu.Lock()
				a.thermalState = "hard_limit"
				a.forceStateLocked(nodestate.Draining)
				a.writeStatusLocked()
				a.mu.Unlock()
				temp := gpu.TemperatureC
				power := gpu.PowerW
				event := protocol.NodeSafetyEventPayload{
					NodeID:       a.opts.NodeID,
					EventCode:    "thermal_hard_limit",
					Severity:     "critical",
					Message:      "temperature exceeded hard safety limit",
					TemperatureC: &temp,
					PowerW:       &power,
					OccurredAt:   a.opts.now(),
				}
				_ = a.writeEnvelope(context.Background(), conn, protocol.TypeNodeSafetyEvent, event)
				select {
				case ch <- event.Message:
				default:
				}
				cancel()
				return
			}
		}
	}()
	return ch
}

func readSafetyReason(ch <-chan string) string {
	select {
	case reason := <-ch:
		return reason
	default:
		return ""
	}
}

func (a *Agent) isHardThermalBlocked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.thermalState == "hard_limit"
}

func validateOfferRequest(req protocol.ChatRequest) error {
	if req.Stream {
		return fmt.Errorf("streaming jobs are not supported")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("job request has no messages")
	}
	if req.MaxTokens <= 0 || req.MaxTokens > 1024 {
		return fmt.Errorf("job request max_tokens is outside node limits")
	}
	for _, message := range req.Messages {
		switch message.Role {
		case "system", "user", "assistant":
		default:
			return fmt.Errorf("job request contains unsupported role")
		}
		if strings.TrimSpace(message.Content) == "" {
			return fmt.Errorf("job request contains an empty text message")
		}
	}
	return nil
}

func runtimeMessages(messages []protocol.ChatMessage) []noderuntime.ChatMessage {
	converted := make([]noderuntime.ChatMessage, 0, len(messages))
	for _, message := range messages {
		converted = append(converted, noderuntime.ChatMessage{Role: message.Role, Content: message.Content})
	}
	return converted
}

func temperatureValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	clone := *value
	return &clone
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (a *Agent) handleControl(command control.Command) control.Response {
	switch command.Action {
	case "pause":
		a.mu.Lock()
		a.pauseRequested = true
		now := a.opts.now()
		target := nodestate.Paused
		if a.activeJobID != nil {
			target = nodestate.Draining
		} else {
			a.pausedAt = &now
		}
		if err := nodestate.Transition(a.state, target); err != nil {
			a.forceStateLocked(target)
		} else {
			a.state = target
		}
		a.writeStatusLocked()
		a.mu.Unlock()
		return control.Response{Status: a.status()}
	case "resume":
		if err := a.resume(context.Background()); err != nil {
			return control.Response{Error: err.Error()}
		}
		return control.Response{Status: a.status()}
	case "status":
		return control.Response{Status: a.status()}
	default:
		return control.Response{Error: "unknown control command"}
	}
}

func (a *Agent) resume(ctx context.Context) error {
	a.mu.Lock()
	a.pauseRequested = false
	a.pausedAt = nil
	needsPrepare := a.runtimeStatus.ModelID == "" || a.runtimeStatus.BaseURL == ""
	if a.activeJobID != nil {
		a.mu.Unlock()
		return nil
	}
	if needsPrepare {
		a.forceStateLocked(nodestate.Preparing)
		a.writeStatusLocked()
		a.mu.Unlock()
		status, err := a.opts.Runtime.Prepare(ctx, a.opts.ModelID)
		if err != nil {
			_ = a.transition(nodestate.Error)
			return err
		}
		a.setRuntime(status)
	} else {
		a.mu.Unlock()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scheduleState == nodeschedule.StateOutOfWindow || a.thermalState != "normal" {
		a.forceStateLocked(nodestate.Idle)
	} else {
		a.forceStateLocked(nodestate.Available)
	}
	a.writeStatusLocked()
	return nil
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
		Schedule:          a.scheduleWindow.String(),
		ScheduleState:     a.scheduleState,
		ThermalState:      a.thermalState,
		Paused:            a.pauseRequested || a.state == nodestate.Paused,
		Draining:          a.state == nodestate.Draining,
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
		RuntimeHash: canonicalSHA256(manifest.Runtime.BinarySHA256),
		ModelHash:   canonicalSHA256(manifest.Source.SHA256),
	}, nil
}

type ExistingRuntimeProvider struct {
	CatalogDir string
	BaseURL    string
}

func (p ExistingRuntimeProvider) Prepare(ctx context.Context, modelID string) (RuntimeStatus, error) {
	parsed, err := url.Parse(p.BaseURL)
	if err != nil {
		return RuntimeStatus{}, fmt.Errorf("parse runtime base URL: %w", err)
	}
	if parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" {
		return RuntimeStatus{}, fmt.Errorf("runtime base URL must use http://127.0.0.1:<port>")
	}
	status, err := (CatalogRuntimeStatusProvider{CatalogDir: p.CatalogDir}).Prepare(ctx, modelID)
	if err != nil {
		return RuntimeStatus{}, err
	}
	status.BaseURL = strings.TrimRight(p.BaseURL, "/")
	return status, nil
}

func canonicalSHA256(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}
