//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	operatorstore "github.com/Ani-HQ/thirdshift/internal/coordinator/operator"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
)

func TestPublicStatusHostEarningsAndRegionCounts(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()

	runtime := newM4Runtime("hosts: ")
	defer runtime.close()
	node := env.startNode(t, runtime, newScriptedTelemetry(40), nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})
	if err := env.operator.SetNodeRegion(env.ctx, node.nodeID, "in-south", "hosts test", time.Now().UTC()); err != nil {
		t.Fatalf("set node region: %v", err)
	}

	// Two credits inside the window and one that is a day old, so the 24h
	// figure and the lifetime figure have to differ.
	seedHostCredit(t, env, node.nodeID, 1, 4, "available", 0)
	seedHostCredit(t, env, node.nodeID, 2, 6, "pending", 2*time.Hour)
	seedHostCredit(t, env, node.nodeID, 3, 50, "available", 26*time.Hour)
	// Reversed credit was never earned and must not be counted.
	seedHostCredit(t, env, node.nodeID, 4, 999, "reversed", time.Minute)

	status, err := env.operator.PublicStatus(env.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("public status: %v", err)
	}
	if len(status.Hosts) != 1 {
		t.Fatalf("hosts = %#v, want exactly one", status.Hosts)
	}
	host := status.Hosts[0]
	if host.Handle != operatorstore.HostHandle(node.nodeID) {
		t.Fatalf("handle = %q, want the derived handle", host.Handle)
	}
	if host.Region != "in-south" {
		t.Fatalf("host region = %q, want in-south", host.Region)
	}
	if host.State != operatorstore.HostStateIdle && host.State != operatorstore.HostStateServing {
		t.Fatalf("connected host state = %q, want serving or idle", host.State)
	}
	if host.Jobs24h != 2 {
		t.Fatalf("jobs_24h = %d, want 2 (the day-old and reversed credits excluded)", host.Jobs24h)
	}
	if host.CreditedMicrodollars24h != 10 {
		t.Fatalf("credited_microdollars_24h = %d, want 10", host.CreditedMicrodollars24h)
	}
	if host.CreditedMicrodollarsTotal != 60 {
		t.Fatalf("credited_microdollars_total = %d, want 60 (reversed excluded)", host.CreditedMicrodollarsTotal)
	}

	counts := map[string]int{}
	for _, count := range status.RegionNodeCounts {
		counts[count.Region] = count.NodeCount
	}
	if counts["in-south"] != 1 {
		t.Fatalf("region_node_counts = %#v, want one node in in-south", status.RegionNodeCounts)
	}
}

// The public status body is the widest surface we have. No node identity may
// appear anywhere in it, including inside the host ticker we just added.
func TestPublicStatusBodyNeverContainsNodeIdentity(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()

	runtime := newM4Runtime("sentinel: ")
	defer runtime.close()
	node := env.startNode(t, runtime, newScriptedTelemetry(40), nil)
	defer node.stop(t)
	waitForNodePredicate(t, env, node.nodeID, func(node registration.NodeSummary) bool {
		return node.State == "AVAILABLE" && node.SessionStatus == "connected"
	})
	seedHostCredit(t, env, node.nodeID, 9, 7, "available", 0)

	raw := getRawWithBearer(t, env, "/v1/status", "")
	if raw.status != http.StatusOK {
		t.Fatalf("status=%d body=%s", raw.status, string(raw.body))
	}
	body := string(raw.body)

	var fleetID, displayName, fingerprint string
	if err := env.pool.QueryRow(env.ctx, `
SELECT COALESCE(fleet_id, ''), COALESCE(display_name, ''), COALESCE(hardware_fingerprint_hash, '')
FROM nodes WHERE id = $1`, node.nodeID).
		Scan(&fleetID, &displayName, &fingerprint); err != nil {
		t.Fatalf("load node identity: %v", err)
	}
	for _, secret := range []string{node.nodeID, strings.TrimPrefix(node.nodeID, "node_"), fleetID, displayName, fingerprint} {
		if strings.TrimSpace(secret) == "" {
			continue
		}
		if strings.Contains(body, secret) {
			t.Fatalf("public status leaked %q: %s", secret, body)
		}
	}
	// Identity-bearing keys must not appear anywhere. estimated_gpu_hours_reused
	// is deliberately excluded: it is a network-wide aggregate that names no
	// machine, and it predates the ticker.
	scrubbed := strings.ReplaceAll(strings.ToLower(body), "estimated_gpu_hours_reused", "")
	scrubbed = strings.ReplaceAll(scrubbed, "estimated_gpu_hours_reused_24h", "")
	for _, forbidden := range []string{"node_id", "hostname", "fingerprint", "display_name\":\"node"} {
		if strings.Contains(scrubbed, forbidden) {
			t.Fatalf("public status body mentions %q: %s", forbidden, body)
		}
	}

	var decoded operatorstore.PublicStatus
	if err := json.Unmarshal(raw.body, &decoded); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if len(decoded.Hosts) != 1 {
		t.Fatalf("hosts = %#v, want one", decoded.Hosts)
	}
	if decoded.Hosts[0].Handle == "" {
		t.Fatal("host handle is empty")
	}

	// The host entries specifically must carry nothing but the anonymous shape.
	hostsJSON, err := json.Marshal(decoded.Hosts)
	if err != nil {
		t.Fatalf("marshal hosts: %v", err)
	}
	for _, forbidden := range []string{"node", "gpu", "fleet", "host_name", "hostname", "vram", "rtx"} {
		if strings.Contains(strings.ToLower(string(hostsJSON)), forbidden) {
			t.Fatalf("host entries mention %q: %s", forbidden, hostsJSON)
		}
	}
}

func TestPublicStatusHostsEmptyWhenNoRecentSessions(t *testing.T) {
	env := newM4Env(t, ioDiscard{})
	defer env.close()

	status, err := env.operator.PublicStatus(env.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("public status: %v", err)
	}
	if len(status.Hosts) != 0 {
		t.Fatalf("hosts = %#v, want empty with no sessions", status.Hosts)
	}
	if len(status.RegionNodeCounts) != 0 {
		t.Fatalf("region_node_counts = %#v, want empty", status.RegionNodeCounts)
	}
}

// seedHostCredit writes one accepted-attempt credit hold for a node, aged by
// the given amount, without going through a real job.
func seedHostCredit(t *testing.T, env *m4Env, nodeID string, seq int, amount int64, state string, age time.Duration) {
	t.Helper()
	createdAt := time.Now().UTC().Add(-age)
	// Ids must match the ULID-shaped CHECK: 26 Crockford base32 characters.
	suffix := fmt.Sprintf("01K0N%019d%02d", 0, seq)
	holdID := "hcredit_" + suffix
	jobID := "job_" + suffix
	attemptID := "att_" + suffix
	txID := "ltx_" + suffix

	var orgID string
	if err := env.pool.QueryRow(env.ctx, "SELECT id FROM organizations ORDER BY created_at LIMIT 1").Scan(&orgID); err != nil {
		t.Fatalf("load org for credit seed: %v", err)
	}
	if _, err := env.pool.Exec(env.ctx, `
INSERT INTO jobs (id, organization_id, model_id, state, priority, request_metadata, deadline_at, created_at, updated_at, completed_at)
VALUES ($1, $2, 'thirdshift-tiny-chat-v1', 'succeeded', 'standard', '{}'::jsonb, $3, $3, $3, $3)
`, jobID, orgID, createdAt); err != nil {
		t.Fatalf("seed credit job: %v", err)
	}
	if _, err := env.pool.Exec(env.ctx, `
INSERT INTO job_attempts (id, job_id, node_id, attempt_number, lease_nonce, lease_expires_at, deadline_at, status, created_at, accepted_at, started_at, finished_at)
VALUES ($1, $2, $3, 1, $1, $4, $4, 'succeeded', $4, $4, $4, $4)
`, attemptID, jobID, nodeID, createdAt); err != nil {
		t.Fatalf("seed credit attempt: %v", err)
	}
	if _, err := env.pool.Exec(env.ctx, `
INSERT INTO ledger_transactions (id, transaction_type, status, reference_type, reference_id, memo, created_at, posted_at)
VALUES ($1, 'job_acceptance', 'posted', 'job', $2, 'seeded', $3, $3)
`, txID, jobID, createdAt); err != nil {
		t.Fatalf("seed credit transaction: %v", err)
	}
	if _, err := env.pool.Exec(env.ctx, `
INSERT INTO host_credit_holds (id, node_id, job_id, attempt_id, ledger_transaction_id, amount_microdollars, state, available_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $8)
`, holdID, nodeID, jobID, attemptID, txID, amount, state, createdAt); err != nil {
		t.Fatalf("seed credit hold: %v", err)
	}
}
