//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/coordinator/auth"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/httpapi"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/jobs"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
	nodeagent "github.com/Ani-HQ/thirdshift/internal/node/agent"
	noderegistration "github.com/Ani-HQ/thirdshift/internal/node/registration"
	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
)

func TestRoutedDeveloperRequestEndToEnd(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	ctx := context.Background()
	pool, cleanup := migratedPool(t, ctx, databaseURL)
	defer cleanup()

	regStore := registration.PGStore{Pool: pool}
	jobStore := jobs.PGStore{Pool: pool}
	jobService := &jobs.Service{
		Store:       jobStore,
		Scheduler:   jobs.Scheduler{Weights: jobs.DefaultSchedulerWeights()},
		RateLimiter: &jobs.RateLimiter{LimitPerMinute: 1000},
		StaleAfter:  2 * time.Second,
		LeaseTTL:    250 * time.Millisecond,
		SyncTimeout: 5 * time.Second,
	}
	validator, err := protocol.NewValidator(filepath.Join("..", "..", "packages", "protocol", "schemas"))
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	signer := auth.TokenSigner{Secret: []byte("integration-secret"), TTL: time.Hour}
	server := httptest.NewServer(httpapi.NewMuxWithOptions(httpapi.Options{
		Version:           "integration",
		Registration:      registration.Service{Repository: regStore},
		SessionStore:      regStore,
		TokenSigner:       signer,
		ProtocolValidator: validator,
		JobService:        jobService,
		CatalogDir:        filepath.Join("..", "..", "models", "catalog"),
		OperatorToken:     "operator-token",
		HeartbeatInterval: 20 * time.Millisecond,
	}))
	defer server.Close()

	syncCatalog(t, server)
	orgID := createOrg(t, server, "Integration Org")
	apiKey := createAPIKey(t, server, orgID, "thirdshift-tiny-chat-v1")

	modelsResp := getDeveloper(t, server, apiKey, "/v1/models")
	if !bytes.Contains(modelsResp, []byte(`"id":"thirdshift-tiny-chat-v1"`)) {
		t.Fatalf("models response missing tiny model: %s", string(modelsResp))
	}

	hashes, err := jobStore.ModelHashes(ctx, "thirdshift-tiny-chat-v1")
	if err != nil {
		t.Fatalf("model hashes: %v", err)
	}
	runtimeControl := newRoutingRuntime()
	defer runtimeControl.server.Close()

	inviteToken := createInvite(t, server.URL, "operator-token")
	dataDir := t.TempDir()
	login, err := noderegistration.Login(ctx, noderegistration.LoginOptions{
		DataDir:        dataDir,
		CoordinatorURL: server.URL,
		InviteToken:    inviteToken,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatalf("node login: %v", err)
	}
	agentCtx, stopAgent := context.WithCancel(ctx)
	defer stopAgent()
	agentErr := make(chan error, 1)
	go func() {
		agentErr <- nodeagent.Run(agentCtx, nodeagent.Options{
			DataDir:           dataDir,
			CoordinatorURL:    server.URL,
			NodeID:            login.Credentials.NodeID,
			AccessToken:       login.Credentials.AccessToken,
			ModelID:           "thirdshift-tiny-chat-v1",
			HeartbeatInterval: 20 * time.Millisecond,
			HTTPClient:        server.Client(),
			Validator:         validator,
			Runtime: routingRuntime{
				status: nodeagent.RuntimeStatus{
					ModelID:     "thirdshift-tiny-chat-v1",
					RuntimeHash: hashes.RuntimeHashes[0],
					ModelHash:   hashes.ModelHash,
					BaseURL:     runtimeControl.server.URL,
				},
			},
			Telemetry: fakeTelemetry{},
		})
	}()
	waitForNodeState(t, ctx, regStore, "AVAILABLE", "connected")

	requestBody := []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"hello route"}],"temperature":0.2,"max_tokens":8,"stream":false}`)
	firstBody, firstStatus := postDeveloperRaw(t, server, apiKey, "/v1/chat/completions", requestBody, "idem-route-1")
	if firstStatus != http.StatusOK {
		t.Fatalf("first completion status = %d body=%s", firstStatus, string(firstBody))
	}
	var first jobs.OpenAIResponse
	if err := json.Unmarshal(firstBody, &first); err != nil {
		t.Fatalf("decode first completion: %v", err)
	}
	if first.Choices[0].Message.Content != "fake completion: hello route" {
		t.Fatalf("completion content = %q", first.Choices[0].Message.Content)
	}
	if first.Usage.TotalTokens == 0 || first.Thirdshift.JobID == "" || first.Thirdshift.DataClass != "public_or_non_sensitive" {
		t.Fatalf("bad completion metadata: %#v", first)
	}

	replayBody, replayStatus := postDeveloperRaw(t, server, apiKey, "/v1/chat/completions", requestBody, "idem-route-1")
	if replayStatus != http.StatusOK {
		t.Fatalf("replay status = %d body=%s", replayStatus, string(replayBody))
	}
	if !bytes.Equal(firstBody, replayBody) {
		t.Fatalf("idempotency replay body differs\nfirst:  %s\nsecond: %s", string(firstBody), string(replayBody))
	}
	var succeededAttempts int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM job_attempts
WHERE job_id = $1 AND status = 'succeeded'
`, first.Thirdshift.JobID).Scan(&succeededAttempts); err != nil {
		t.Fatalf("count succeeded attempts: %v", err)
	}
	if succeededAttempts != 1 {
		t.Fatalf("succeeded attempts = %d, want 1", succeededAttempts)
	}

	asyncBody, asyncStatus := postDeveloperRaw(t, server, apiKey, "/v1/jobs", []byte(`{"model":"thirdshift-tiny-chat-v1","input":{"messages":[{"role":"user","content":"queued only"}],"max_tokens":8,"stream":false},"deadline_seconds":30}`), "")
	if asyncStatus != http.StatusAccepted {
		t.Fatalf("async status = %d body=%s", asyncStatus, string(asyncBody))
	}
	var asyncJob jobs.JobStatus
	if err := json.Unmarshal(asyncBody, &asyncJob); err != nil {
		t.Fatalf("decode async job: %v", err)
	}
	cancelBody, cancelStatus := postDeveloperRaw(t, server, apiKey, "/v1/jobs/"+asyncJob.ID+"/cancel", nil, "")
	if cancelStatus != http.StatusOK {
		t.Fatalf("cancel status = %d body=%s", cancelStatus, string(cancelBody))
	}
	var cancelled jobs.JobStatus
	if err := json.Unmarshal(cancelBody, &cancelled); err != nil {
		t.Fatalf("decode cancelled job: %v", err)
	}
	if cancelled.State != "cancelled" {
		t.Fatalf("cancelled state = %s", cancelled.State)
	}

	oversized := `{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"` + strings.Repeat("x", jobs.MaxPublicRequestBytes) + `"}],"max_tokens":8,"stream":false}`
	oversizedBody, oversizedStatus := postDeveloperRaw(t, server, apiKey, "/v1/chat/completions", []byte(oversized), "")
	if oversizedStatus != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d body=%s", oversizedStatus, string(oversizedBody))
	}
	assertAPIErrorCode(t, oversizedBody, jobs.CodeInvalidRequest)

	holdRequest := []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"hold"}],"max_tokens":8,"stream":false}`)
	holdDone := make(chan completionHTTPResult, 1)
	go func() {
		body, status := postDeveloperRaw(t, server, apiKey, "/v1/chat/completions", holdRequest, "")
		holdDone <- completionHTTPResult{body: body, status: status}
	}()
	select {
	case <-runtimeControl.holdStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for held runtime request")
	}
	var activeAttempts int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM job_attempts WHERE status IN ('offered', 'accepted', 'running')").Scan(&activeAttempts); err != nil {
		t.Fatalf("count active attempts: %v", err)
	}
	if activeAttempts != 1 {
		t.Fatalf("active attempts during held request = %d, want 1", activeAttempts)
	}
	secondBody, secondStatus := postDeveloperRaw(t, server, apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"second while busy"}],"max_tokens":8,"stream":false}`), "")
	if secondStatus != http.StatusServiceUnavailable {
		t.Fatalf("busy second status = %d body=%s", secondStatus, string(secondBody))
	}
	assertAPIErrorCode(t, secondBody, jobs.CodeNoCapacity)
	close(runtimeControl.releaseHold)
	held := <-holdDone
	if held.status != http.StatusOK {
		t.Fatalf("held completion status = %d body=%s", held.status, string(held.body))
	}

	stopAgent()
	select {
	case err := <-agentErr:
		if err != nil {
			t.Fatalf("agent returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not stop")
	}
}

type routingRuntime struct {
	status nodeagent.RuntimeStatus
}

func (r routingRuntime) Prepare(context.Context, string) (nodeagent.RuntimeStatus, error) {
	return r.status, nil
}

type routingRuntimeControl struct {
	server      *httptest.Server
	holdStarted chan struct{}
	releaseHold chan struct{}
}

func newRoutingRuntime() routingRuntimeControl {
	control := routingRuntimeControl{
		holdStarted: make(chan struct{}, 1),
		releaseHold: make(chan struct{}),
	}
	control.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if prompt == "hold" {
			control.holdStarted <- struct{}{}
			<-control.releaseHold
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"role": "assistant", "content": "fake completion: " + prompt},
				"finish_reason": "stop",
			}},
			"usage": map[string]int{"prompt_tokens": 4, "completion_tokens": 6, "total_tokens": 10},
		})
	}))
	return control
}

type completionHTTPResult struct {
	body   []byte
	status int
}

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	return databaseURL
}

func syncCatalog(t *testing.T, server *httptest.Server) {
	t.Helper()
	var resp struct {
		Synced int `json:"synced"`
	}
	postAdmin(t, server, "/internal/v1/catalog/sync", map[string]string{"catalog_dir": filepath.Join("..", "..", "models", "catalog")}, &resp)
	if resp.Synced == 0 {
		t.Fatal("catalog sync reported zero manifests")
	}
}

func createOrg(t *testing.T, server *httptest.Server, name string) string {
	t.Helper()
	var resp struct {
		OrgID string `json:"org_id"`
	}
	postAdmin(t, server, "/internal/v1/orgs", map[string]string{"name": name}, &resp)
	if resp.OrgID == "" {
		t.Fatal("org id missing")
	}
	return resp.OrgID
}

func createAPIKey(t *testing.T, server *httptest.Server, orgID, modelID string) string {
	t.Helper()
	var resp struct {
		Key string `json:"key"`
	}
	postAdmin(t, server, "/internal/v1/api-keys", map[string]any{"org_id": orgID, "models": []string{modelID}}, &resp)
	if resp.Key == "" {
		t.Fatal("api key missing")
	}
	return resp.Key
}

func postAdmin(t *testing.T, server *httptest.Server, path string, body any, target any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal admin request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("new admin request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer operator-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("admin request %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("admin request %s status=%d body=%s", path, resp.StatusCode, readAll(resp))
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode admin response: %v", err)
	}
}

func getDeveloper(t *testing.T, server *httptest.Server, apiKey, path string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatalf("new developer GET: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("developer GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body := readAll(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("developer GET %s status=%d body=%s", path, resp.StatusCode, body)
	}
	return body
}

func postDeveloperRaw(t *testing.T, server *httptest.Server, apiKey, path string, body []byte, idempotencyKey string) ([]byte, int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new developer POST: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("developer POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	return readAll(resp), resp.StatusCode
}

func assertAPIErrorCode(t *testing.T, body []byte, code string) {
	t.Helper()
	var decoded struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode API error: %v body=%s", err, string(body))
	}
	if decoded.Error.Code != code {
		t.Fatalf("error code = %q, want %q body=%s", decoded.Error.Code, code, string(body))
	}
}

func readAll(resp *http.Response) []byte {
	body := new(bytes.Buffer)
	_, _ = body.ReadFrom(resp.Body)
	return body.Bytes()
}
