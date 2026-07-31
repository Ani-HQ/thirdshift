//go:build integration

package integration

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
)

// TestDrainAndHeartbeatDoNotDeadlock hammers the two transactions that
// previously locked nodes and node_sessions in opposite orders. With the
// inverted order this reproduces "deadlock detected" (SQLSTATE 40P01)
// within a few hundred iterations; with a canonical lock order it must
// stay clean.
func TestDrainAndHeartbeatDoNotDeadlock(t *testing.T) {
	env := newM4Env(t, nil)
	nodeID, sessionID := seedDeadlockNode(t, env)

	heartbeat := protocol.NodeHeartbeatPayload{
		NodeID:        nodeID,
		State:         "AVAILABLE",
		ModelID:       "thirdshift-tiny-chat-v1",
		ScheduleState: "in_window",
		ThermalState:  "normal",
	}

	const workers = 3
	const iterations = 30
	var wg sync.WaitGroup
	errs := make(chan error, workers*iterations*2)
	for w := 0; w < workers; w++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				if err := env.regStore.RecordHeartbeat(env.ctx, sessionID, heartbeat, time.Now()); err != nil {
					errs <- err
				}
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Re-arm: the drain only locks the session row while it is
				// 'connected', which is the state a live fleet is always in.
				if _, err := env.pool.Exec(env.ctx, `UPDATE node_sessions SET state = 'connected' WHERE id = $1`, sessionID); err != nil {
					errs <- err
				}
				if err := env.operator.DrainNode(env.ctx, nodeID, "stress", time.Now()); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if strings.Contains(err.Error(), "deadlock") {
			t.Fatalf("deadlock detected: %v", err)
		}
	}
}

func seedDeadlockNode(t *testing.T, env *m4Env) (string, string) {
	t.Helper()
	nodeID := "node_01J0M00000000000000000DEAD"
	sessionID := "sess_01J0M00000000000000000DEAD"
	if _, err := env.pool.Exec(env.ctx, `INSERT INTO nodes (id, state, current_model_id) VALUES ($1, 'AVAILABLE', 'thirdshift-tiny-chat-v1')`, nodeID); err != nil {
		t.Fatalf("seed deadlock node: %v", err)
	}
	if _, err := env.pool.Exec(env.ctx, `INSERT INTO node_sessions (id, node_id, protocol_version, state, connected_at) VALUES ($1, $2, '1.0', 'connected', now())`, sessionID, nodeID); err != nil {
		t.Fatalf("seed deadlock session: %v", err)
	}
	return nodeID, sessionID
}
