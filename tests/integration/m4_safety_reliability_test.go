//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/coordinator/auth"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/httpapi"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/jobs"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/ledger"
	operatorstore "github.com/Ani-HQ/thirdshift/internal/coordinator/operator"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
	nodeagent "github.com/Ani-HQ/thirdshift/internal/node/agent"
	"github.com/Ani-HQ/thirdshift/internal/node/control"
	noderegistration "github.com/Ani-HQ/thirdshift/internal/node/registration"
	"github.com/Ani-HQ/thirdshift/internal/shared/logging"
	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestM4ScheduleExcludesOutOfWindowNode(t *testing.T) {
	env := newM4Env(t, io.Discard)
	defer env.close()

	outRuntime := newM4Runtime("out: ")
	defer outRuntime.close()
	inRuntime := newM4Runtime("in: ")
	defer inRuntime.close()

	outNode := env.startNode(t, outRuntime, newScriptedTelemetry(40), func(opts *nodeagent.Options) {
		opts.ScheduleFrom = "23:00"
		opts.ScheduleUntil = "08:00"
		opts.Now = func() time.Time { return time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC) }
	})
	defer outNode.stop(t)
	inNode := env.startNode(t, inRuntime, newScriptedTelemetry(40), nil)
	defer inNode.stop(t)

	waitForNodePredicate(t, env, outNode.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "IDLE" && node.SessionStatus == "connected" && node.ScheduleState == "out_of_window"
	})
	waitForNodePredicate(t, env, inNode.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected" && node.ScheduleState == "in_window"
	})

	body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"schedule route"}],"max_tokens":8,"stream":false}`), "")
	if status != http.StatusOK {
		t.Fatalf("completion status = %d body=%s", status, string(body))
	}
	if outRuntime.callCount() != 0 {
		t.Fatalf("out-of-window runtime calls = %d, want 0", outRuntime.callCount())
	}
	if inRuntime.callCount() != 1 {
		t.Fatalf("in-window runtime calls = %d, want 1", inRuntime.callCount())
	}
}

func TestM4PauseResumeDrainAndThermalGuard(t *testing.T) {
	env := newM4Env(t, io.Discard)
	defer env.close()

	runtime := newM4Runtime("m4: ")
	runtime.holdPrompt = "drain hold"
	defer runtime.close()
	telemetry := newScriptedTelemetry(40)
	node := env.startNode(t, runtime, telemetry, nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})

	if _, err := control.Send(env.ctx, node.dataDir, control.Command{Action: "pause"}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "PAUSED" && node.Paused
	})
	body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"paused route"}],"max_tokens":8,"stream":false}`), "")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("paused completion status = %d body=%s", status, string(body))
	}
	assertAPIErrorCode(t, body, jobs.CodeNoCapacity)

	if _, err := control.Send(env.ctx, node.dataDir, control.Command{Action: "resume"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && !node.Paused
	})
	body, status = postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"after resume"}],"max_tokens":8,"stream":false}`), "")
	if status != http.StatusOK {
		t.Fatalf("resume completion status = %d body=%s", status, string(body))
	}

	holdDone := make(chan completionHTTPResult, 1)
	go func() {
		body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"drain hold"}],"max_tokens":8,"stream":false}`), "")
		holdDone <- completionHTTPResult{body: body, status: status}
	}()
	waitForHoldSignal(t, runtime.holdStarted, holdDone, "drain hold completion")
	if _, err := control.Send(env.ctx, node.dataDir, control.Command{Action: "pause"}); err != nil {
		t.Fatalf("pause during job: %v", err)
	}
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "DRAINING" && node.Draining
	})
	runtime.releaseHold()
	held := waitForHTTPResult(t, holdDone, "drained completion")
	if held.status != http.StatusOK {
		t.Fatalf("drained completion status = %d body=%s", held.status, string(held.body))
	}
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "PAUSED" && node.Paused
	})
	if _, err := control.Send(env.ctx, node.dataDir, control.Command{Action: "resume"}); err != nil {
		t.Fatalf("resume after drain: %v", err)
	}
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && !node.Draining
	})

	telemetry.setTemp(75)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.ThermalState == "warm" && node.State != "AVAILABLE"
	})
	waitForSecurityEvent(t, env, "over_temperature")
	body, status = postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"warm route"}],"max_tokens":8,"stream":false}`), "")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("warm completion status = %d body=%s", status, string(body))
	}
	assertAPIErrorCode(t, body, jobs.CodeNoCapacity)
	telemetry.setTemp(65)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.ThermalState == "normal"
	})
}

func TestM4ThermalHardLimitTerminatesRunningAttempt(t *testing.T) {
	env := newM4Env(t, io.Discard)
	defer env.close()

	runtime := newM4Runtime("hard: ")
	runtime.holdPrompt = "thermal hard"
	defer runtime.close()
	telemetry := newScriptedTelemetry(40)
	node := env.startNode(t, runtime, telemetry, nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})

	done := make(chan completionHTTPResult, 1)
	go func() {
		body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"thermal hard"}],"max_tokens":8,"stream":false}`), "")
		done <- completionHTTPResult{body: body, status: status}
	}()
	waitForHoldSignal(t, runtime.holdStarted, done, "thermal hard completion")
	telemetry.setTemp(90)
	result := waitForHTTPResult(t, done, "thermal hard completion")
	if result.status != http.StatusBadGateway {
		t.Fatalf("hard-limit completion status = %d body=%s", result.status, string(result.body))
	}
	assertAPIErrorCode(t, result.body, jobs.CodeJobFailed)
	waitForSecurityEvent(t, env, "thermal_hard_limit")
	var safetyAttempts int
	if err := env.pool.QueryRow(env.ctx, "SELECT count(*) FROM job_attempts WHERE error_code = 'safety_limit'").Scan(&safetyAttempts); err != nil {
		t.Fatalf("count safety attempts: %v", err)
	}
	if safetyAttempts != 1 {
		t.Fatalf("safety_limit attempts = %d, want 1; attempts=%v", safetyAttempts, attemptErrorCodes(t, env))
	}
}

func TestM4TransientFailureRetriesSecondNodeAndPermanentInvalidRequestDoesNotRetry(t *testing.T) {
	env := newM4Env(t, io.Discard)
	defer env.close()

	firstRuntime := newM4Runtime("first: ")
	firstRuntime.holdPrompt = "retry after disconnect"
	defer firstRuntime.close()
	secondRuntime := newM4Runtime("second: ")
	defer secondRuntime.close()

	firstNode := env.startNode(t, firstRuntime, newScriptedTelemetry(40), nil)
	defer firstNode.stop(t)
	secondNode := env.startNode(t, secondRuntime, newScriptedTelemetry(40), nil)
	defer secondNode.stop(t)
	waitForNodePredicate(t, env, firstNode.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})
	waitForNodePredicate(t, env, secondNode.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})

	done := make(chan completionHTTPResult, 1)
	go func() {
		body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"retry after disconnect"}],"max_tokens":8,"stream":false}`), "m4-retry")
		done <- completionHTTPResult{body: body, status: status}
	}()
	waitForHoldSignal(t, firstRuntime.holdStarted, done, "retry completion first hold")
	firstNode.stop(t)
	firstRuntime.releaseHold()
	result := waitForHTTPResult(t, done, "retry completion")
	if result.status != http.StatusOK {
		t.Fatalf("retry completion status = %d body=%s", result.status, string(result.body))
	}
	var response jobs.OpenAIResponse
	if err := json.Unmarshal(result.body, &response); err != nil {
		t.Fatalf("decode retry response: %v", err)
	}
	if response.Thirdshift.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", response.Thirdshift.Attempts)
	}
	if !strings.Contains(response.Choices[0].Message.Content, "second:") {
		t.Fatalf("completion did not come from second runtime: %#v", response.Choices[0].Message)
	}
	var succeededAttempts int
	if err := env.pool.QueryRow(env.ctx, "SELECT count(*) FROM job_attempts WHERE job_id = $1 AND status = 'succeeded'", response.Thirdshift.JobID).Scan(&succeededAttempts); err != nil {
		t.Fatalf("count succeeded attempts: %v", err)
	}
	if succeededAttempts != 1 {
		t.Fatalf("succeeded attempts = %d, want 1", succeededAttempts)
	}
	attemptsBefore := countAttempts(t, env)
	invalidBody, invalidStatus := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"invalid permanent"}],"max_tokens":8,"stream":true}`), "")
	if invalidStatus != http.StatusBadRequest {
		t.Fatalf("invalid request status = %d body=%s", invalidStatus, string(invalidBody))
	}
	assertAPIErrorCode(t, invalidBody, jobs.CodeInvalidRequest)
	if attemptsAfter := countAttempts(t, env); attemptsAfter != attemptsBefore {
		t.Fatalf("attempts after permanent invalid request = %d, want %d", attemptsAfter, attemptsBefore)
	}
}

func TestM4StructuredLogsRedactPromptCompletionAndSecrets(t *testing.T) {
	var logs bytes.Buffer
	env := newM4Env(t, &logs)
	defer env.close()

	runtime := newM4Runtime("COMPLETION_SENTINEL_VALUE ")
	defer runtime.close()
	node := env.startNode(t, runtime, newScriptedTelemetry(40), nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})

	body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"PROMPT_SENTINEL_VALUE"}],"max_tokens":8,"stream":false}`), "")
	if status != http.StatusOK {
		t.Fatalf("logged completion status = %d body=%s", status, string(body))
	}
	got := logs.String()
	for _, wanted := range []string{"request_id=", "job_id=", "attempt_id="} {
		if !strings.Contains(got, wanted) {
			t.Fatalf("log output missing %q: %s", wanted, got)
		}
	}
	for _, forbidden := range []string{"PROMPT_SENTINEL_VALUE", "COMPLETION_SENTINEL_VALUE", env.apiKey} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("log output contains forbidden value %q: %s", forbidden, got)
		}
	}
}

type m4Env struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	cleanup     func()
	regStore    registration.PGStore
	jobStore    jobs.PGStore
	jobService  *jobs.Service
	operator    operatorstore.Store
	validator   *protocol.Validator
	server      *httptest.Server
	apiKey      string
	modelHash   string
	runtimeHash string
}

func newM4Env(t *testing.T, logOutput io.Writer) *m4Env {
	t.Helper()
	databaseURL := testDatabaseURL(t)
	ctx := context.Background()
	pool, cleanup := migratedPool(t, ctx, databaseURL)
	regStore := registration.PGStore{Pool: pool}
	jobStore := jobs.PGStore{Pool: pool}
	operatorStore := operatorstore.Store{
		Pool:        pool,
		JobStore:    jobStore,
		LedgerStore: ledger.Store{Pool: pool},
		StaleAfter:  2 * time.Second,
	}
	logger := logging.NewTextLogger(logOutput)
	jobService := &jobs.Service{
		Store:       jobStore,
		Scheduler:   jobs.Scheduler{Weights: jobs.DefaultSchedulerWeights()},
		RateLimiter: &jobs.RateLimiter{LimitPerMinute: 1000},
		StaleAfter:  2 * time.Second,
		// A node has to receive the offer over its WebSocket, accept it, and
		// call its runtime before the lease expires. 250ms is inside that
		// round trip once several test packages compete for the machine, and
		// an expired lease fails the request outright rather than merely
		// delaying it.
		LeaseTTL:    5 * time.Second,
		SyncTimeout: 5 * time.Second,
		Logger:      logger,
	}
	validator, err := protocol.NewValidator(filepath.Join("..", "..", "packages", "protocol", "schemas"))
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	server := httptest.NewServer(httpapi.NewMuxWithOptions(httpapi.Options{
		Version:           "integration",
		Registration:      registration.Service{Repository: regStore},
		SessionStore:      regStore,
		TokenSigner:       auth.TokenSigner{Secret: []byte("integration-secret"), TTL: time.Hour},
		ProtocolValidator: validator,
		JobService:        jobService,
		OperatorStore:     &operatorStore,
		CatalogDir:        filepath.Join("..", "..", "models", "catalog"),
		OperatorToken:     "operator-token",
		HeartbeatInterval: 20 * time.Millisecond,
		Logger:            logger,
	}))
	env := &m4Env{
		ctx:        ctx,
		pool:       pool,
		cleanup:    cleanup,
		regStore:   regStore,
		jobStore:   jobStore,
		jobService: jobService,
		operator:   operatorStore,
		validator:  validator,
		server:     server,
	}
	syncCatalog(t, server)
	orgID := createOrg(t, server, "M4 Integration Org")
	env.apiKey = createAPIKey(t, server, orgID, "thirdshift-tiny-chat-v1")
	hashes, err := jobStore.ModelHashes(ctx, "thirdshift-tiny-chat-v1")
	if err != nil {
		t.Fatalf("model hashes: %v", err)
	}
	env.modelHash = hashes.ModelHash
	env.runtimeHash = hashes.RuntimeHash
	return env
}

func (e *m4Env) close() {
	e.server.Close()
	e.cleanup()
}

type m4Node struct {
	nodeID  string
	dataDir string
	cancel  context.CancelFunc
	errCh   chan error
}

func (e *m4Env) startNode(t *testing.T, runtime *m4Runtime, telemetry *scriptedTelemetry, configure func(*nodeagent.Options)) *m4Node {
	t.Helper()
	inviteToken := createInvite(t, e.server.URL, "operator-token")
	dataDir, err := os.MkdirTemp("/tmp", "tsnode-*")
	if err != nil {
		t.Fatalf("create short node data dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dataDir)
	})
	login, err := noderegistration.Login(e.ctx, noderegistration.LoginOptions{
		DataDir:        dataDir,
		CoordinatorURL: e.server.URL,
		InviteToken:    inviteToken,
		HTTPClient:     e.server.Client(),
	})
	if err != nil {
		t.Fatalf("node login: %v", err)
	}
	if telemetry == nil {
		telemetry = newScriptedTelemetry(40)
	}
	agentCtx, stopAgent := context.WithCancel(e.ctx)
	node := &m4Node{
		nodeID:  login.Credentials.NodeID,
		dataDir: dataDir,
		cancel:  stopAgent,
		errCh:   make(chan error, 1),
	}
	opts := nodeagent.Options{
		DataDir:           dataDir,
		CoordinatorURL:    e.server.URL,
		NodeID:            login.Credentials.NodeID,
		AccessToken:       login.Credentials.AccessToken,
		ModelID:           "thirdshift-tiny-chat-v1",
		HeartbeatInterval: 20 * time.Millisecond,
		HTTPClient:        e.server.Client(),
		Validator:         e.validator,
		Runtime: routingRuntime{
			status: nodeagent.RuntimeStatus{
				ModelID:     "thirdshift-tiny-chat-v1",
				RuntimeHash: e.runtimeHash,
				ModelHash:   e.modelHash,
				BaseURL:     runtime.server.URL,
			},
		},
		Telemetry:         telemetry,
		MaxTempC:          70,
		HardTempC:         85,
		ThermalHysteresis: 5,
		ThermalPoll:       10 * time.Millisecond,
	}
	if configure != nil {
		configure(&opts)
	}
	go func() {
		node.errCh <- nodeagent.Run(agentCtx, opts)
	}()
	return node
}

func (n *m4Node) stop(t *testing.T) {
	t.Helper()
	if n.cancel == nil {
		return
	}
	n.cancel()
	select {
	case err := <-n.errCh:
		if err != nil {
			t.Fatalf("agent returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not stop")
	}
	n.cancel = nil
}

type m4Runtime struct {
	server      *httptest.Server
	prefix      string
	holdPrompt  string
	holdStarted chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	mu          sync.Mutex
	calls       int
}

func newM4Runtime(prefix string) *m4Runtime {
	runtime := &m4Runtime{
		prefix:      prefix,
		holdStarted: make(chan struct{}, 1),
		release:     make(chan struct{}),
	}
	runtime.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		prompt := ""
		if len(req.Messages) > 0 {
			prompt = req.Messages[len(req.Messages)-1].Content
		}
		runtime.mu.Lock()
		runtime.calls++
		runtime.mu.Unlock()
		if runtime.holdPrompt != "" && prompt == runtime.holdPrompt {
			runtime.holdStarted <- struct{}{}
			select {
			case <-runtime.release:
			case <-r.Context().Done():
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"role": "assistant", "content": runtime.prefix + prompt},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 4, "completion_tokens": 6, "total_tokens": 10},
		})
	}))
	return runtime
}

func (r *m4Runtime) releaseHold() {
	r.releaseOnce.Do(func() {
		close(r.release)
	})
}

func (r *m4Runtime) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *m4Runtime) close() {
	r.releaseHold()
	r.server.Close()
}

type scriptedTelemetry struct {
	mu  sync.Mutex
	gpu protocol.GPUStatus
}

func newScriptedTelemetry(temp int) *scriptedTelemetry {
	return &scriptedTelemetry{gpu: protocol.GPUStatus{
		Name:               "fake-gpu",
		VRAMTotalMB:        8192,
		VRAMFreeMB:         7000,
		TemperatureC:       temp,
		PowerW:             100,
		PowerLimitW:        200,
		UtilizationPercent: 10,
	}}
}

func (t *scriptedTelemetry) setTemp(temp int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.gpu.TemperatureC = temp
}

func (t *scriptedTelemetry) GPUStatus(context.Context) (protocol.GPUStatus, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.gpu, nil
}

func waitForNodePredicate(t *testing.T, env *m4Env, nodeID string, predicate func(registration.NodeSummary) bool) registration.NodeSummary {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last []registration.NodeSummary
	for time.Now().Before(deadline) {
		nodes, err := env.regStore.ListNodes(env.ctx)
		if err != nil {
			t.Fatalf("list nodes: %v", err)
		}
		last = nodes
		for _, node := range nodes {
			if node.ID == nodeID && predicate(node) {
				return node
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for node %s; last nodes=%#v", nodeID, last)
	return registration.NodeSummary{}
}

func waitForSecurityEvent(t *testing.T, env *m4Env, eventType string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := env.pool.QueryRow(env.ctx, "SELECT count(*) FROM security_events WHERE event_type = $1", eventType).Scan(&count); err != nil {
			t.Fatalf("count security events: %v", err)
		}
		if count > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for security event %s", eventType)
}

// waitForHoldSignal waits for the fake runtime to receive the held request. It
// fails with the real HTTP result if the completion finishes first: a request
// that never reaches the runtime has already failed, and reporting that status
// beats reporting a timeout that hides the cause.
func waitForHoldSignal(t *testing.T, started <-chan struct{}, done <-chan completionHTTPResult, name string) {
	t.Helper()
	select {
	case <-started:
	case result := <-done:
		t.Fatalf("%s finished without reaching the runtime: status=%d body=%s", name, result.status, string(result.body))
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitForSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func waitForHTTPResult(t *testing.T, ch <-chan completionHTTPResult, name string) completionHTTPResult {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
		return completionHTTPResult{}
	}
}

func countAttempts(t *testing.T, env *m4Env) int {
	t.Helper()
	var count int
	if err := env.pool.QueryRow(env.ctx, "SELECT count(*) FROM job_attempts").Scan(&count); err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	return count
}

func attemptErrorCodes(t *testing.T, env *m4Env) []string {
	t.Helper()
	rows, err := env.pool.Query(env.ctx, "SELECT COALESCE(error_code, '') FROM job_attempts ORDER BY created_at")
	if err != nil {
		t.Fatalf("query attempt error codes: %v", err)
	}
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			t.Fatalf("scan attempt error code: %v", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate attempt error codes: %v", err)
	}
	return codes
}
