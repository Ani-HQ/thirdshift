//go:build integration

package integration

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/coordinator/jobs"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/operator"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
	nodeconfig "github.com/Ani-HQ/thirdshift/internal/node/config"
	noderegistration "github.com/Ani-HQ/thirdshift/internal/node/registration"
)

func TestM6InternalOperatorAPIActionsAuditAndNoPromptLeak(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()
	env.jobService.CreditHold = time.Millisecond
	configureM5Model(t, env, 1_000_000, 1_000_000, 500_000, 0)

	runtime := newM4Runtime("M6_COMPLETION_SENTINEL ")
	defer runtime.close()
	node := env.startNode(t, runtime, newScriptedTelemetry(41), nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})

	unauthorized := operatorGetRaw(t, env, "/internal/v1/overview", "")
	if unauthorized.status != http.StatusUnauthorized {
		t.Fatalf("unauthorized overview status = %d body=%s", unauthorized.status, string(unauthorized.body))
	}

	overview := operatorGetRaw(t, env, "/internal/v1/overview", "operator-token")
	if overview.status != http.StatusOK {
		t.Fatalf("overview status = %d body=%s", overview.status, string(overview.body))
	}
	if !bytes.Contains(overview.body, []byte(`"online_nodes":1`)) {
		t.Fatalf("overview missing online node count: %s", string(overview.body))
	}

	const promptSentinel = "M6_PROMPT_SENTINEL_DO_NOT_RENDER"
	body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"`+promptSentinel+`"}],"max_tokens":8,"stream":false}`), "")
	if status != http.StatusOK {
		t.Fatalf("completion status = %d body=%s", status, string(body))
	}
	var completion jobs.OpenAIResponse
	if err := json.Unmarshal(body, &completion); err != nil {
		t.Fatalf("decode completion: %v", err)
	}

	jobsList := operatorGetRaw(t, env, "/internal/v1/jobs", "operator-token")
	if jobsList.status != http.StatusOK {
		t.Fatalf("jobs list status = %d body=%s", jobsList.status, string(jobsList.body))
	}
	assertNoPromptLeak(t, jobsList.body, promptSentinel)
	assertNoPromptLeak(t, jobsList.body, "M6_COMPLETION_SENTINEL")

	jobDetail := operatorGetRaw(t, env, "/internal/v1/jobs/"+completion.Thirdshift.JobID, "operator-token")
	if jobDetail.status != http.StatusOK {
		t.Fatalf("job detail status = %d body=%s", jobDetail.status, string(jobDetail.body))
	}
	assertNoPromptLeak(t, jobDetail.body, promptSentinel)
	assertNoPromptLeak(t, jobDetail.body, "M6_COMPLETION_SENTINEL")

	nodes := operatorGetRaw(t, env, "/internal/v1/nodes", "operator-token")
	if nodes.status != http.StatusOK || !bytes.Contains(nodes.body, []byte(`"gpu"`)) {
		t.Fatalf("nodes response status=%d body=%s", nodes.status, string(nodes.body))
	}
	models := operatorGetRaw(t, env, "/internal/v1/models", "operator-token")
	if models.status != http.StatusOK || !bytes.Contains(models.body, []byte(`"hardware_profiles"`)) {
		t.Fatalf("models response status=%d body=%s", models.status, string(models.body))
	}
	ledgerView := operatorGetRaw(t, env, "/internal/v1/ledger", "operator-token")
	if ledgerView.status != http.StatusOK || !bytes.Contains(ledgerView.body, []byte(`"host_pending_credit_microdollars"`)) {
		t.Fatalf("ledger response status=%d body=%s", ledgerView.status, string(ledgerView.body))
	}
	alerts := operatorGetRaw(t, env, "/internal/v1/alerts", "operator-token")
	if alerts.status != http.StatusOK || !bytes.Contains(alerts.body, []byte(`"alerts"`)) {
		t.Fatalf("alerts response status=%d body=%s", alerts.status, string(alerts.body))
	}

	asyncBody, asyncStatus := postDeveloperRaw(t, env.server, env.apiKey, "/v1/jobs", []byte(`{"model":"thirdshift-tiny-chat-v1","input":{"messages":[{"role":"user","content":"cancel me"}],"max_tokens":8,"stream":false},"deadline_seconds":30}`), "")
	if asyncStatus != http.StatusAccepted {
		t.Fatalf("async status = %d body=%s", asyncStatus, string(asyncBody))
	}
	var asyncJob jobs.JobStatus
	if err := json.Unmarshal(asyncBody, &asyncJob); err != nil {
		t.Fatalf("decode async job: %v", err)
	}
	operatorPostRaw(t, env, "/internal/v1/jobs/"+asyncJob.ID+"/cancel", []byte(`{"reason":"m6 test cancel"}`), "operator-token", http.StatusOK)
	operatorPostRaw(t, env, "/internal/v1/jobs/"+asyncJob.ID+"/retry", []byte(`{"reason":"m6 test retry"}`), "operator-token", http.StatusOK)
	operatorPostRaw(t, env, "/internal/v1/nodes/"+node.nodeID+"/drain", []byte(`{"reason":"m6 test drain"}`), "operator-token", http.StatusOK)
	operatorPostRaw(t, env, "/internal/v1/nodes/"+node.nodeID+"/pause", []byte(`{"reason":"m6 test pause"}`), "operator-token", http.StatusOK)
	operatorPostRaw(t, env, "/internal/v1/nodes/"+node.nodeID+"/quarantine", []byte(`{"reason":"m6 test quarantine"}`), "operator-token", http.StatusOK)

	time.Sleep(5 * time.Millisecond)
	operatorPostRaw(t, env, "/internal/v1/ledger/credits/release", nil, "operator-token", http.StatusOK)
	payoutCreate := operatorPostRaw(t, env, "/internal/v1/payout-batches", []byte(`{}`), "operator-token", http.StatusCreated)
	var batch struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payoutCreate.body, &batch); err != nil || batch.ID == "" {
		t.Fatalf("decode payout batch err=%v body=%s", err, string(payoutCreate.body))
	}
	exported := operatorGetRaw(t, env, "/internal/v1/payout-batches/"+batch.ID+"/export", "operator-token")
	if exported.status != http.StatusOK {
		t.Fatalf("payout export status=%d body=%s", exported.status, string(exported.body))
	}
	operatorPostRaw(t, env, "/internal/v1/payout-batches/"+batch.ID+"/confirm", exported.body, "operator-token", http.StatusOK)

	for _, action := range []string{
		operator.ActionNodeDrain,
		operator.ActionNodePause,
		operator.ActionNodeQuarantine,
		operator.ActionJobRetry,
		operator.ActionJobCancel,
		operator.ActionPayoutCreate,
		operator.ActionPayoutExport,
		operator.ActionPayoutConfirm,
	} {
		assertOperatorAction(t, env, action)
	}
	audit := operatorGetRaw(t, env, "/internal/v1/audit", "operator-token")
	if audit.status != http.StatusOK || !bytes.Contains(audit.body, []byte(operator.ActionCatalogSync)) {
		t.Fatalf("audit response status=%d body=%s", audit.status, string(audit.body))
	}
}

func TestM6FleetDefaultsApplyAndReportCSV(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()
	orgID := createOrg(t, env.server, "M6 Fleet Org")
	createBody := operatorPostRaw(t, env, "/internal/v1/fleets", []byte(`{"org_id":"`+orgID+`","name":"Cafe Alpha","schedule_from":"23:00","schedule_until":"08:00"}`), "operator-token", http.StatusCreated)
	var fleet struct {
		ID            string `json:"id"`
		ScheduleFrom  string `json:"schedule_from"`
		ScheduleUntil string `json:"schedule_until"`
	}
	if err := json.Unmarshal(createBody.body, &fleet); err != nil {
		t.Fatalf("decode fleet: %v", err)
	}
	if fleet.ID == "" || fleet.ScheduleFrom != "23:00" || fleet.ScheduleUntil != "08:00" {
		t.Fatalf("bad fleet response: %s", string(createBody.body))
	}

	inviteToken := createInviteForFleet(t, env, fleet.ID)
	dataDir := t.TempDir()
	if _, err := noderegistration.Login(env.ctx, noderegistration.LoginOptions{
		DataDir:        dataDir,
		CoordinatorURL: env.server.URL,
		InviteToken:    inviteToken,
		HTTPClient:     env.server.Client(),
	}); err != nil {
		t.Fatalf("node login with fleet defaults: %v", err)
	}
	cfg, err := nodeconfig.Load(dataDir)
	if err != nil {
		t.Fatalf("load node config: %v", err)
	}
	if cfg.ScheduleFrom != "23:00" || cfg.ScheduleUntil != "08:00" {
		t.Fatalf("fleet defaults not applied to node config: %#v", cfg)
	}

	report := operatorGetRaw(t, env, "/internal/v1/fleets/"+fleet.ID+"/report?from=2026-01-01&to=2027-01-01", "operator-token")
	if report.status != http.StatusOK {
		t.Fatalf("fleet report status=%d body=%s", report.status, string(report.body))
	}
	records, err := csv.NewReader(bytes.NewReader(report.body)).ReadAll()
	if err != nil {
		t.Fatalf("parse fleet report: %v", err)
	}
	if len(records) < 2 || strings.Join(records[0], ",") != "fleet_id,node_id,jobs_succeeded,jobs_failed,prompt_tokens,completion_tokens,host_credit_microdollars" {
		t.Fatalf("unexpected report records: %#v", records)
	}
}

type operatorHTTPResult struct {
	body   []byte
	status int
}

func operatorGetRaw(t *testing.T, env *m4Env, path, token string) operatorHTTPResult {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new operator GET: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := env.server.Client().Do(req)
	if err != nil {
		t.Fatalf("operator GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	return operatorHTTPResult{body: readAll(resp), status: resp.StatusCode}
}

func operatorPostRaw(t *testing.T, env *m4Env, path string, body []byte, token string, wantStatus int) operatorHTTPResult {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, env.server.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new operator POST: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := env.server.Client().Do(req)
	if err != nil {
		t.Fatalf("operator POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	result := operatorHTTPResult{body: readAll(resp), status: resp.StatusCode}
	if resp.StatusCode != wantStatus {
		t.Fatalf("operator POST %s status=%d want=%d body=%s", path, resp.StatusCode, wantStatus, string(result.body))
	}
	return result
}

func createInviteForFleet(t *testing.T, env *m4Env, fleetID string) string {
	t.Helper()
	var resp struct {
		Token string `json:"token"`
	}
	postAdmin(t, env.server, "/internal/v1/invites", map[string]any{"fleet_id": fleetID, "expires_in_seconds": 60}, &resp)
	if resp.Token == "" {
		t.Fatal("invite token missing")
	}
	return resp.Token
}

func assertNoPromptLeak(t *testing.T, body []byte, sentinel string) {
	t.Helper()
	if bytes.Contains(body, []byte(sentinel)) {
		t.Fatalf("operator response leaked sentinel %q: %s", sentinel, string(body))
	}
	if bytes.Contains(body, []byte("request_metadata")) {
		t.Fatalf("operator response leaked request_metadata: %s", string(body))
	}
}

func assertOperatorAction(t *testing.T, env *m4Env, action string) {
	t.Helper()
	var count int
	if err := env.pool.QueryRow(env.ctx, "SELECT count(*) FROM operator_actions WHERE action = $1", action).Scan(&count); err != nil {
		t.Fatalf("count operator action %s: %v", action, err)
	}
	if count == 0 {
		t.Fatalf("operator action %s was not audited", action)
	}
}
