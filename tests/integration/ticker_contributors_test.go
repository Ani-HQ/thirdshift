//go:build integration

package integration

import "testing"

// A node that registered, never served, and went offline is a test artifact.
// It must not appear on the public ticker, while a node that earned credit
// stays visible even once it disconnects.
func TestPublicHostsShowContributorsAndConnectedOnly(t *testing.T) {
	env := newM4Env(t, nil)
	seedIdleGhost(t, env)

	status := publicStatus(t, env, "", "")
	for _, host := range status.Hosts {
		if host.CreditedMicrodollarsTotal == 0 && host.State == "offline" {
			t.Fatalf("offline node with no earnings appeared on the ticker: %+v", host)
		}
	}
}

func seedIdleGhost(t *testing.T, env *m4Env) {
	t.Helper()
	nodeID := "node_01J0M0000000000000000QQQQQ"
	if _, err := env.pool.Exec(env.ctx, `INSERT INTO nodes (id, state) VALUES ($1, 'OFFLINE')`, nodeID); err != nil {
		t.Fatalf("seed ghost node: %v", err)
	}
	if _, err := env.pool.Exec(env.ctx,
		`INSERT INTO node_sessions (id, node_id, protocol_version, state, connected_at, last_heartbeat_at)
		 VALUES ($1, $2, '1.0', 'closed', now() - interval '2 hours', now() - interval '2 hours')`,
		"sess_01J0M0000000000000000QQQQQ", nodeID); err != nil {
		t.Fatalf("seed ghost session: %v", err)
	}
}
