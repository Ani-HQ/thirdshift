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
	"github.com/Ani-HQ/thirdshift/internal/coordinator/ledger"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
	nodeidentity "github.com/Ani-HQ/thirdshift/internal/node/identity"
	"github.com/Ani-HQ/thirdshift/internal/shared/nodeauth"
	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
)

func TestM5AcceptedJobLedgerHoldReplayAndDuplicateResult(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()
	env.jobService.CreditHold = 50 * time.Millisecond
	configureM5Model(t, env, 1_000_000, 1_000_000, 500_000, 0)

	runtime := newM4Runtime("ledger: ")
	defer runtime.close()
	node := env.startNode(t, runtime, newScriptedTelemetry(40), nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})

	requestBody := []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"ledger accepted"}],"max_tokens":8,"stream":false}`)
	firstBody, firstStatus := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", requestBody, "m5-ledger")
	if firstStatus != http.StatusOK {
		t.Fatalf("completion status = %d body=%s", firstStatus, string(firstBody))
	}
	var first jobs.OpenAIResponse
	if err := json.Unmarshal(firstBody, &first); err != nil {
		t.Fatalf("decode completion: %v", err)
	}
	assertSingleBalancedAcceptance(t, env, first.Thirdshift.JobID)
	holdID, amount, state, availableAt := hostCreditHold(t, env, first.Thirdshift.JobID)
	if state != "pending" {
		t.Fatalf("credit hold state = %s, want pending", state)
	}
	if amount != 3 {
		t.Fatalf("host credit amount = %d, want 3", amount)
	}

	replayBody, replayStatus := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", requestBody, "m5-ledger")
	if replayStatus != http.StatusOK {
		t.Fatalf("replay status = %d body=%s", replayStatus, string(replayBody))
	}
	if !bytes.Equal(firstBody, replayBody) {
		t.Fatalf("idempotent replay changed body\nfirst=%s\nreplay=%s", string(firstBody), string(replayBody))
	}
	assertLedgerAcceptanceCount(t, env, first.Thirdshift.JobID, 1)

	attemptID := succeededAttemptID(t, env, first.Thirdshift.JobID)
	privateKey, _, err := nodeidentity.LoadOrCreateKey(node.dataDir)
	if err != nil {
		t.Fatalf("load node key: %v", err)
	}
	duplicatePayload := protocol.JobCompletedPayload{
		JobID:          first.Thirdshift.JobID,
		AttemptID:      attemptID,
		ModelID:        "thirdshift-tiny-chat-v1",
		RuntimeHash:    env.runtimeHash,
		ModelHash:      env.modelHash,
		Message:        &protocol.ChatMessage{Role: "assistant", Content: first.Choices[0].Message.Content},
		Usage:          first.Usage,
		DurationMillis: 1,
		FinishReason:   "stop",
		CompletedAt:    time.Now().UTC(),
	}
	signature, err := nodeauth.SignJobCompleted(privateKey, node.nodeID, duplicatePayload, time.Now().UTC())
	if err != nil {
		t.Fatalf("sign duplicate payload: %v", err)
	}
	duplicatePayload.Signature = &signature
	envelope, err := protocol.NewEnvelope("msg_01J0M000000000000000000099", protocol.TypeJobCompleted, time.Now().UTC(), duplicatePayload)
	if err != nil {
		t.Fatalf("duplicate envelope: %v", err)
	}
	if err := env.jobService.HandleNodeMessage(env.ctx, node.nodeID, "", envelope); err == nil {
		t.Fatal("duplicate result envelope was accepted, want rejection")
	}
	assertLedgerAcceptanceCount(t, env, first.Thirdshift.JobID, 1)

	released, err := (ledger.Store{Pool: env.pool}).PromoteAvailableCredits(env.ctx, availableAt.Add(time.Millisecond))
	if err != nil {
		t.Fatalf("promote credits: %v", err)
	}
	if released != 1 {
		t.Fatalf("released credits = %d, want 1", released)
	}
	var promotedState string
	if err := env.pool.QueryRow(env.ctx, "SELECT state FROM host_credit_holds WHERE id = $1", holdID).Scan(&promotedState); err != nil {
		t.Fatalf("reload credit hold: %v", err)
	}
	if promotedState != "available" {
		t.Fatalf("promoted state = %s, want available", promotedState)
	}
}

func TestM5ChallengeFailureQuarantinesAfterThreshold(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()
	configureM5Model(t, env, 1_000_000, 1_000_000, 500_000, 0)

	runtime := newM4Runtime("challenge: ")
	defer runtime.close()
	node := env.startNode(t, runtime, newScriptedTelemetry(40), nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})
	jobID, attemptID := createCompletedJobForVerification(t, env, "challenge seed")
	if err := env.jobStore.RecordChallengeOutcome(env.ctx, jobs.ChallengeOutcome{
		JobID:      jobID,
		AttemptID:  attemptID,
		NodeID:     node.nodeID,
		ModelID:    "thirdshift-tiny-chat-v1",
		Passed:     false,
		Reason:     "canary_mismatch",
		OccurredAt: time.Now().UTC(),
	}, 2); err != nil {
		t.Fatalf("record first challenge: %v", err)
	}
	var quarantinedAt *time.Time
	if err := env.pool.QueryRow(env.ctx, "SELECT quarantined_at FROM nodes WHERE id = $1", node.nodeID).Scan(&quarantinedAt); err != nil {
		t.Fatalf("check first quarantine: %v", err)
	}
	if quarantinedAt != nil {
		t.Fatalf("node quarantined after one challenge failure at %v", quarantinedAt)
	}
	if err := env.jobStore.RecordChallengeOutcome(env.ctx, jobs.ChallengeOutcome{
		JobID:      jobID,
		AttemptID:  attemptID,
		NodeID:     node.nodeID,
		ModelID:    "thirdshift-tiny-chat-v1",
		Passed:     false,
		Reason:     "canary_mismatch",
		OccurredAt: time.Now().UTC(),
	}, 2); err != nil {
		t.Fatalf("record second challenge: %v", err)
	}
	if err := env.pool.QueryRow(env.ctx, "SELECT quarantined_at FROM nodes WHERE id = $1", node.nodeID).Scan(&quarantinedAt); err != nil {
		t.Fatalf("check second quarantine: %v", err)
	}
	if quarantinedAt == nil {
		t.Fatal("node was not quarantined after threshold challenge failures")
	}
	body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"quarantine route"}],"max_tokens":8,"stream":false}`), "")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("quarantined route status = %d body=%s", status, string(body))
	}
	assertAPIErrorCode(t, body, jobs.CodeNoCapacity)
}

func TestM5DuplicateSampleSecondNodeRecordsAgreement(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()
	configureM5Model(t, env, 1_000_000, 1_000_000, 500_000, 1)

	firstRuntime := newM4Runtime("dupe: ")
	defer firstRuntime.close()
	secondRuntime := newM4Runtime("dupe: ")
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

	body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"duplicate agreement"}],"max_tokens":8,"stream":false}`), "")
	if status != http.StatusOK {
		t.Fatalf("duplicate-sampled completion status = %d body=%s", status, string(body))
	}
	waitForVerificationEvent(t, env, "duplicate", "accepted")
	if calls := firstRuntime.callCount() + secondRuntime.callCount(); calls != 2 {
		t.Fatalf("runtime calls = %d, want 2 for duplicate sample", calls)
	}
	var overhead int
	if err := env.pool.QueryRow(env.ctx, "SELECT count(*) FROM ledger_transactions WHERE transaction_type = 'verification_overhead' AND status = 'posted'").Scan(&overhead); err != nil {
		t.Fatalf("count verification overhead transactions: %v", err)
	}
	if overhead != 1 {
		t.Fatalf("verification overhead transactions = %d, want 1", overhead)
	}
}

func TestM5PayoutLifecycleAndEconomicsReport(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()
	env.jobService.CreditHold = time.Millisecond
	configureM5Model(t, env, 1_000_000, 1_000_000, 500_000, 0)
	runtime := newM4Runtime("payout: ")
	defer runtime.close()
	node := env.startNode(t, runtime, newScriptedTelemetry(40), nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})

	body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"payout accounting"}],"max_tokens":8,"stream":false}`), "")
	if status != http.StatusOK {
		t.Fatalf("completion status = %d body=%s", status, string(body))
	}
	store := ledger.Store{Pool: env.pool}
	if released, err := store.PromoteAvailableCredits(env.ctx, time.Now().UTC().Add(time.Second)); err != nil || released != 1 {
		t.Fatalf("release credits = %d err=%v, want 1 nil", released, err)
	}
	report, err := store.EconomicsReport(env.ctx, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("economics report: %v", err)
	}
	if report.CustomerRevenueMicrodollars != 10 || report.HostCreditsMicrodollars != 3 || report.ContributionMarginMicrodollars != 7 {
		t.Fatalf("economics report = %#v, want revenue=10 host=3 margin=7", report)
	}

	batch, err := store.CreatePayoutBatch(env.ctx, "", time.Now().UTC())
	if err != nil {
		t.Fatalf("create payout batch: %v", err)
	}
	csvBody, exported, err := store.ExportPayoutBatch(env.ctx, batch.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("export payout batch: %v", err)
	}
	if exported.Status != "exported" {
		t.Fatalf("exported status = %s, want exported", exported.Status)
	}
	records, err := csv.NewReader(bytes.NewReader(csvBody)).ReadAll()
	if err != nil {
		t.Fatalf("parse payout CSV: %v", err)
	}
	if len(records) != 2 || strings.Join(records[0], ",") != "host_id,account_reference,amount_microdollars,memo" {
		t.Fatalf("unexpected payout CSV records: %#v", records)
	}
	if _, err := env.pool.Exec(env.ctx, "UPDATE payout_items SET amount_microdollars = amount_microdollars + 1 WHERE payout_batch_id = $1", batch.ID); err == nil {
		t.Fatal("exported payout item content update succeeded, want immutability error")
	}
	confirmed, err := store.ConfirmPayoutBatch(env.ctx, batch.ID, bytes.NewReader(csvBody), time.Now().UTC())
	if err != nil {
		t.Fatalf("confirm payout batch: %v", err)
	}
	if confirmed.Status != "paid" {
		t.Fatalf("confirmed status = %s, want paid", confirmed.Status)
	}
	var paidCredits int
	if err := env.pool.QueryRow(env.ctx, "SELECT count(*) FROM host_credit_holds WHERE payout_batch_id = $1 AND state = 'paid'", batch.ID).Scan(&paidCredits); err != nil {
		t.Fatalf("count paid credits: %v", err)
	}
	if paidCredits != 1 {
		t.Fatalf("paid credits = %d, want 1", paidCredits)
	}
}

func configureM5Model(t *testing.T, env *m4Env, inputPrice, outputPrice, hostPrice int64, duplicateRate float64) {
	t.Helper()
	if _, err := env.pool.Exec(env.ctx, `
UPDATE model_prices
SET customer_input_per_million_microdollars = $1,
    customer_output_per_million_microdollars = $2,
    host_credit_per_million_output_microdollars = $3
`, inputPrice, outputPrice, hostPrice); err != nil {
		t.Fatalf("configure model prices: %v", err)
	}
	if _, err := env.pool.Exec(env.ctx, "UPDATE model_manifest_limits SET duplicate_sample_rate = $1", duplicateRate); err != nil {
		t.Fatalf("configure duplicate sample rate: %v", err)
	}
}

func assertSingleBalancedAcceptance(t *testing.T, env *m4Env, jobID string) {
	t.Helper()
	assertLedgerAcceptanceCount(t, env, jobID, 1)
	var balance int64
	if err := env.pool.QueryRow(env.ctx, `
SELECT COALESCE(SUM(le.amount_microdollars), 0)
FROM ledger_transactions lt
JOIN ledger_entries le ON le.transaction_id = lt.id
WHERE lt.reference_type = 'job'
  AND lt.reference_id = $1
  AND lt.transaction_type = 'job_acceptance'
  AND lt.status = 'posted'
`, jobID).Scan(&balance); err != nil {
		t.Fatalf("sum ledger entries: %v", err)
	}
	if balance != 0 {
		t.Fatalf("ledger balance = %d, want 0", balance)
	}
}

func assertLedgerAcceptanceCount(t *testing.T, env *m4Env, jobID string, want int) {
	t.Helper()
	var count int
	if err := env.pool.QueryRow(env.ctx, `
SELECT count(*)
FROM ledger_transactions
WHERE reference_type = 'job'
  AND reference_id = $1
  AND transaction_type = 'job_acceptance'
  AND status = 'posted'
`, jobID).Scan(&count); err != nil {
		t.Fatalf("count ledger transactions: %v", err)
	}
	if count != want {
		t.Fatalf("ledger acceptance transaction count = %d, want %d", count, want)
	}
}

func hostCreditHold(t *testing.T, env *m4Env, jobID string) (string, int64, string, time.Time) {
	t.Helper()
	var id string
	var amount int64
	var state string
	var availableAt time.Time
	if err := env.pool.QueryRow(env.ctx, `
SELECT id, amount_microdollars, state, available_at
FROM host_credit_holds
WHERE job_id = $1
`, jobID).Scan(&id, &amount, &state, &availableAt); err != nil {
		t.Fatalf("load host credit hold: %v", err)
	}
	return id, amount, state, availableAt
}

func succeededAttemptID(t *testing.T, env *m4Env, jobID string) string {
	t.Helper()
	var attemptID string
	if err := env.pool.QueryRow(env.ctx, "SELECT id FROM job_attempts WHERE job_id = $1 AND status = 'succeeded'", jobID).Scan(&attemptID); err != nil {
		t.Fatalf("load succeeded attempt: %v", err)
	}
	return attemptID
}

func createCompletedJobForVerification(t *testing.T, env *m4Env, prompt string) (string, string) {
	t.Helper()
	body, status := postDeveloperRaw(t, env.server, env.apiKey, "/v1/chat/completions", []byte(`{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"`+prompt+`"}],"max_tokens":8,"stream":false}`), "")
	if status != http.StatusOK {
		t.Fatalf("seed completion status = %d body=%s", status, string(body))
	}
	var response jobs.OpenAIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode seed completion: %v", err)
	}
	return response.Thirdshift.JobID, succeededAttemptID(t, env, response.Thirdshift.JobID)
}

func waitForVerificationEvent(t *testing.T, env *m4Env, eventType, status string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := env.pool.QueryRow(env.ctx, "SELECT count(*) FROM verification_events WHERE event_type = $1 AND status = $2", eventType, status).Scan(&count); err != nil {
			t.Fatalf("count verification events: %v", err)
		}
		if count > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for verification event %s/%s", eventType, status)
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
