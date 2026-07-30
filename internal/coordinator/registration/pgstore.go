package registration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/shared/ids"
	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PGStore struct {
	Pool *pgxpool.Pool
}

type NodeSummary struct {
	ID              string     `json:"id"`
	State           string     `json:"state"`
	CurrentModelID  string     `json:"current_model_id,omitempty"`
	LastSeenAt      *time.Time `json:"last_seen_at,omitempty"`
	SessionStatus   string     `json:"session_status"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at,omitempty"`
	ScheduleState   string     `json:"schedule_state,omitempty"`
	ThermalState    string     `json:"thermal_state,omitempty"`
	Paused          bool       `json:"paused"`
	Draining        bool       `json:"draining"`
}

func (s PGStore) CreateInvite(ctx context.Context, invite InviteRecord) error {
	if s.Pool == nil {
		return ErrRepositoryMissing
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin create invite: %w", err)
	}
	defer tx.Rollback(context.Background())

	if _, err := ensureFleet(ctx, tx, invite.FleetID, invite.CreatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO invite_tokens (id, fleet_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5)
`, invite.ID, invite.FleetID, invite.TokenHash, invite.ExpiresAt, invite.CreatedAt); err != nil {
		return fmt.Errorf("insert invite token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create invite: %w", err)
	}
	return nil
}

func (s PGStore) RegisterNode(ctx context.Context, registration NodeRegistration) (RegistrationCreated, error) {
	if s.Pool == nil {
		return RegistrationCreated{}, ErrRepositoryMissing
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RegistrationCreated{}, fmt.Errorf("begin register node: %w", err)
	}
	defer tx.Rollback(context.Background())

	var inviteID, fleetID, status string
	var expiresAt time.Time
	err = tx.QueryRow(ctx, `
SELECT id, fleet_id, status, expires_at
FROM invite_tokens
WHERE token_hash = $1
FOR UPDATE
`, registration.InviteTokenHash).Scan(&inviteID, &fleetID, &status, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return RegistrationCreated{}, ErrInvalidInvite
	}
	if err != nil {
		return RegistrationCreated{}, fmt.Errorf("select invite token: %w", err)
	}
	if status == "used" {
		return RegistrationCreated{}, ErrInviteUsed
	}
	if status != "active" {
		return RegistrationCreated{}, ErrInvalidInvite
	}
	if !registration.Now.Before(expiresAt) {
		if _, err := tx.Exec(ctx, "UPDATE invite_tokens SET status = 'expired' WHERE id = $1", inviteID); err != nil {
			return RegistrationCreated{}, fmt.Errorf("mark invite expired: %w", err)
		}
		return RegistrationCreated{}, ErrInviteExpired
	}

	var organizationID string
	var scheduleFrom, scheduleUntil, scheduleTimezone sql.NullString
	if err := tx.QueryRow(ctx, `
SELECT organization_id, schedule_from, schedule_until, schedule_timezone
FROM fleets
WHERE id = $1
`, fleetID).Scan(&organizationID, &scheduleFrom, &scheduleUntil, &scheduleTimezone); err != nil {
		return RegistrationCreated{}, fmt.Errorf("select invite fleet: %w", err)
	}
	var fleetSchedule *ScheduleDefaults
	if scheduleFrom.Valid && scheduleUntil.Valid {
		timezone := "local"
		if scheduleTimezone.Valid && scheduleTimezone.String != "" {
			timezone = scheduleTimezone.String
		}
		fleetSchedule = &ScheduleDefaults{From: scheduleFrom.String, Until: scheduleUntil.String, Timezone: timezone}
	}
	nodeScheduleSource := "node"
	if fleetSchedule != nil {
		nodeScheduleSource = "fleet"
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO nodes (id, organization_id, fleet_id, state, hardware_fingerprint_hash, registered_at, schedule_from, schedule_until, schedule_source, created_at, updated_at)
VALUES ($1, $2, $3, 'OFFLINE', $4, $5, $6, $7, $8, $5, $5)
`, registration.NodeID, organizationID, fleetID, registration.HardwareFingerprintHash, registration.Now, nullString(scheduleFrom), nullString(scheduleUntil), nodeScheduleSource); err != nil {
		return RegistrationCreated{}, fmt.Errorf("insert node: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO node_keys (id, node_id, key_type, public_key, status, created_at)
VALUES ($1, $2, 'ed25519', $3, 'active', $4)
`, registration.KeyID, registration.NodeID, registration.PublicKey, registration.Now); err != nil {
		return RegistrationCreated{}, fmt.Errorf("insert node key: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO node_bootstrap_tokens (id, node_id, token_hash, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5)
`, registration.BootstrapTokenID, registration.NodeID, registration.BootstrapTokenHash, registration.BootstrapTokenExpiresAt, registration.Now); err != nil {
		return RegistrationCreated{}, fmt.Errorf("insert bootstrap token: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE invite_tokens
SET status = 'used', used_at = $2, used_by_node_id = $3
WHERE id = $1
`, inviteID, registration.Now, registration.NodeID); err != nil {
		return RegistrationCreated{}, fmt.Errorf("mark invite used: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RegistrationCreated{}, fmt.Errorf("commit register node: %w", err)
	}
	return RegistrationCreated{NodeID: registration.NodeID, BootstrapTokenExpiresAt: registration.BootstrapTokenExpiresAt, FleetSchedule: fleetSchedule}, nil
}

func (s PGStore) ConsumeBootstrap(ctx context.Context, nodeID, bootstrapHash string, now time.Time) error {
	if s.Pool == nil {
		return ErrRepositoryMissing
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin consume bootstrap: %w", err)
	}
	defer tx.Rollback(context.Background())

	var id string
	var expiresAt time.Time
	var usedAt *time.Time
	err = tx.QueryRow(ctx, `
SELECT id, expires_at, used_at
FROM node_bootstrap_tokens
WHERE node_id = $1 AND token_hash = $2
FOR UPDATE
`, nodeID, bootstrapHash).Scan(&id, &expiresAt, &usedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidBootstrap
	}
	if err != nil {
		return fmt.Errorf("select bootstrap token: %w", err)
	}
	if usedAt != nil {
		return ErrBootstrapUsed
	}
	if !now.Before(expiresAt) {
		return ErrBootstrapExpired
	}
	if _, err := tx.Exec(ctx, "UPDATE node_bootstrap_tokens SET used_at = $2 WHERE id = $1", id, now); err != nil {
		return fmt.Errorf("mark bootstrap used: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit bootstrap exchange: %w", err)
	}
	return nil
}

func (s PGStore) OpenSession(ctx context.Context, nodeID, protocolVersion, remoteAddr string, now time.Time) (string, error) {
	if s.Pool == nil {
		return "", ErrRepositoryMissing
	}
	sessionID, err := ids.New("sess")
	if err != nil {
		return "", err
	}
	if _, err := s.Pool.Exec(ctx, `
INSERT INTO node_sessions (id, node_id, protocol_version, remote_addr, state, connected_at)
VALUES ($1, $2, $3, $4, 'connected', $5)
`, sessionID, nodeID, protocolVersion, remoteAddr, now); err != nil {
		return "", fmt.Errorf("insert node session: %w", err)
	}
	return sessionID, nil
}

func (s PGStore) RecordHeartbeat(ctx context.Context, sessionID string, heartbeat protocol.NodeHeartbeatPayload, receivedAt time.Time) error {
	if s.Pool == nil {
		return ErrRepositoryMissing
	}
	heartbeatID, err := ids.New("hb")
	if err != nil {
		return err
	}
	gpu, err := json.Marshal(heartbeat.GPU)
	if err != nil {
		return fmt.Errorf("marshal heartbeat gpu: %w", err)
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin record heartbeat: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
INSERT INTO node_heartbeats (id, node_id, session_id, sequence, state, model_id, runtime_hash, model_hash, gpu, active_job_id, schedule_state, thermal_state, paused, draining, uptime_seconds, received_at, sent_at)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9::jsonb, $10, NULLIF($11, ''), NULLIF($12, ''), $13, $14, $15, $16, $17);
`, heartbeatID, heartbeat.NodeID, sessionID, heartbeat.Sequence, heartbeat.State, heartbeat.ModelID, heartbeat.RuntimeHash, heartbeat.ModelHash, string(gpu), heartbeat.ActiveJobID, heartbeat.ScheduleState, heartbeat.ThermalState, heartbeat.Paused, heartbeat.Draining, heartbeat.UptimeSeconds, receivedAt, heartbeat.Timestamp); err != nil {
		return fmt.Errorf("insert heartbeat: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE node_sessions
SET last_heartbeat_at = $2
WHERE id = $1;
`, sessionID, receivedAt); err != nil {
		return fmt.Errorf("update session heartbeat: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE nodes
SET state = $2, current_model_id = NULLIF($3, ''), last_seen_at = $4, updated_at = $4
WHERE id = $1;
`, heartbeat.NodeID, heartbeat.State, heartbeat.ModelID, receivedAt); err != nil {
		return fmt.Errorf("update node heartbeat state: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit record heartbeat: %w", err)
	}
	return nil
}

func (s PGStore) RecordStateChanged(ctx context.Context, stateChanged protocol.NodeStateChangedPayload, receivedAt time.Time) error {
	if s.Pool == nil {
		return ErrRepositoryMissing
	}
	_, err := s.Pool.Exec(ctx, `
UPDATE nodes
SET state = $2, last_seen_at = $3, updated_at = $3
WHERE id = $1
`, stateChanged.NodeID, stateChanged.State, receivedAt)
	if err != nil {
		return fmt.Errorf("record state change: %w", err)
	}
	return nil
}

func (s PGStore) RecordSafetyEvent(ctx context.Context, event protocol.NodeSafetyEventPayload, receivedAt time.Time) error {
	if s.Pool == nil {
		return ErrRepositoryMissing
	}
	eventID, err := ids.New("sec")
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{
		"message":       event.Message,
		"temperature_c": event.TemperatureC,
		"power_w":       event.PowerW,
		"occurred_at":   event.OccurredAt,
		"received_at":   receivedAt,
	})
	if err != nil {
		return fmt.Errorf("marshal safety metadata: %w", err)
	}
	if _, err := s.Pool.Exec(ctx, `
INSERT INTO security_events (id, severity, event_type, node_id, metadata, created_at)
VALUES ($1, $2, $3, $4, $5::jsonb, $6)
`, eventID, event.Severity, event.EventCode, event.NodeID, string(metadata), receivedAt); err != nil {
		return fmt.Errorf("insert safety event: %w", err)
	}
	return nil
}

func (s PGStore) CloseSession(ctx context.Context, sessionID, nodeID string, now time.Time) error {
	if s.Pool == nil {
		return ErrRepositoryMissing
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin close node session: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
UPDATE node_sessions
SET state = 'closed', disconnected_at = $2
WHERE id = $1 AND state = 'connected';
`, sessionID, now); err != nil {
		return fmt.Errorf("close node session: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE nodes
SET state = 'OFFLINE', updated_at = $2
WHERE id = $1;
`, nodeID, now); err != nil {
		return fmt.Errorf("mark node offline: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit close node session: %w", err)
	}
	return nil
}

func (s PGStore) MarkStale(ctx context.Context, cutoff, now time.Time) (int64, error) {
	if s.Pool == nil {
		return 0, ErrRepositoryMissing
	}
	tag, err := s.Pool.Exec(ctx, `
WITH stale_sessions AS (
  UPDATE node_sessions
  SET state = 'stale', disconnected_at = $2
  WHERE state = 'connected'
    AND COALESCE(last_heartbeat_at, connected_at) < $1
  RETURNING node_id
)
UPDATE nodes
SET state = 'OFFLINE', updated_at = $2
WHERE id IN (SELECT node_id FROM stale_sessions)
`, cutoff, now)
	if err != nil {
		return 0, fmt.Errorf("mark stale sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s PGStore) ListNodes(ctx context.Context) ([]NodeSummary, error) {
	if s.Pool == nil {
		return nil, ErrRepositoryMissing
	}
	rows, err := s.Pool.Query(ctx, `
SELECT n.id, n.state, COALESCE(n.current_model_id, ''), n.last_seen_at,
       COALESCE(latest.state, 'none'), latest.last_heartbeat_at,
       COALESCE(hb.schedule_state, ''), COALESCE(hb.thermal_state, ''), COALESCE(hb.paused, false), COALESCE(hb.draining, false)
FROM nodes n
LEFT JOIN LATERAL (
  SELECT state, last_heartbeat_at
  FROM node_sessions
  WHERE node_id = n.id
  ORDER BY connected_at DESC
  LIMIT 1
) latest ON true
LEFT JOIN LATERAL (
  SELECT schedule_state, thermal_state, paused, draining
  FROM node_heartbeats
  WHERE node_id = n.id
  ORDER BY received_at DESC
  LIMIT 1
) hb ON true
ORDER BY n.id
`)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	var nodes []NodeSummary
	for rows.Next() {
		var node NodeSummary
		if err := rows.Scan(&node.ID, &node.State, &node.CurrentModelID, &node.LastSeenAt, &node.SessionStatus, &node.LastHeartbeatAt, &node.ScheduleState, &node.ThermalState, &node.Paused, &node.Draining); err != nil {
			return nil, fmt.Errorf("scan node summary: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node summaries: %w", err)
	}
	return nodes, nil
}

func (s PGStore) PublicKeyForNode(ctx context.Context, nodeID string) (string, error) {
	if s.Pool == nil {
		return "", ErrRepositoryMissing
	}
	var publicKey string
	err := s.Pool.QueryRow(ctx, `
SELECT public_key
FROM node_keys
WHERE node_id = $1 AND status = 'active'
ORDER BY created_at DESC
LIMIT 1
`, nodeID).Scan(&publicKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("node key not found")
	}
	if err != nil {
		return "", fmt.Errorf("select node public key: %w", err)
	}
	return publicKey, nil
}

func ensureFleet(ctx context.Context, tx pgx.Tx, fleetID string, now time.Time) (string, error) {
	var organizationID string
	err := tx.QueryRow(ctx, "SELECT organization_id FROM fleets WHERE id = $1", fleetID).Scan(&organizationID)
	if err == nil {
		return organizationID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("select fleet: %w", err)
	}
	organizationID, err = ids.New("org")
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO organizations (id, name, created_at, updated_at)
VALUES ($1, 'Thirdshift Alpha Organization', $2, $2)
`, organizationID, now); err != nil {
		return "", fmt.Errorf("create default organization for fleet: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO fleets (id, organization_id, name, enrollment_status, created_at, updated_at)
VALUES ($1, $2, 'Thirdshift Alpha Fleet', 'active', $3, $3)
`, fleetID, organizationID, now); err != nil {
		return "", fmt.Errorf("create fleet %s: %w", fleetID, err)
	}
	return organizationID, nil
}

func nullString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
