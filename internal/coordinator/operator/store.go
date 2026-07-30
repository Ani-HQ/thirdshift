package operator

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/anianroid/thirdshift/internal/coordinator/jobs"
	"github.com/anianroid/thirdshift/internal/coordinator/ledger"
	"github.com/anianroid/thirdshift/internal/shared/ids"
	"github.com/anianroid/thirdshift/internal/shared/protocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ActionNodeDrain       = "node.drain"
	ActionNodePause       = "node.pause"
	ActionNodeQuarantine  = "node.quarantine"
	ActionJobRetry        = "job.retry"
	ActionJobCancel       = "job.cancel"
	ActionPayoutCreate    = "payout.create"
	ActionPayoutExport    = "payout.export"
	ActionPayoutConfirm   = "payout.confirm"
	ActionCatalogSync     = "catalog.sync"
	ActionCreditsRelease  = "credits.release"
	defaultOperatorActor  = "operator-token"
	defaultRecentListSize = 100
)

type Store struct {
	Pool        *pgxpool.Pool
	JobStore    jobs.PGStore
	LedgerStore ledger.Store
	Alerts      AlertConfig
	StaleAfter  time.Duration
	Now         func() time.Time
}

type AlertConfig struct {
	DisconnectSpikeWindow     time.Duration
	DisconnectSpikeThreshold  int
	JobFailureWindow          time.Duration
	JobFailureRateThreshold   float64
	RuntimeCrashWindow        time.Duration
	RuntimeCrashThreshold     int
	OverTempWindow            time.Duration
	NoCapacityWindow          time.Duration
	AuthAnomalyWindow         time.Duration
	AuthAnomalyThreshold      int
	LedgerImbalanceCheck      bool
	NoCapacityForModelEnabled bool
}

func DefaultAlertConfig() AlertConfig {
	return AlertConfig{
		DisconnectSpikeWindow:     15 * time.Minute,
		DisconnectSpikeThreshold:  3,
		JobFailureWindow:          time.Hour,
		JobFailureRateThreshold:   0.25,
		RuntimeCrashWindow:        15 * time.Minute,
		RuntimeCrashThreshold:     3,
		OverTempWindow:            time.Hour,
		NoCapacityWindow:          time.Hour,
		AuthAnomalyWindow:         15 * time.Minute,
		AuthAnomalyThreshold:      5,
		LedgerImbalanceCheck:      true,
		NoCapacityForModelEnabled: true,
	}
}

type Overview struct {
	OnlineNodes                     int                 `json:"online_nodes"`
	AvailableByModel                []ModelAvailability `json:"available_by_model"`
	JobsPerHour                     int                 `json:"jobs_per_hour"`
	SuccessRate                     float64             `json:"success_rate"`
	PendingHostCreditMicrodollars   int64               `json:"pending_host_credit_microdollars"`
	AvailableHostCreditMicrodollars int64               `json:"available_host_credit_microdollars"`
	ActiveAlerts                    []Alert             `json:"active_alerts"`
}

type ModelAvailability struct {
	ModelID        string `json:"model_id"`
	AvailableNodes int    `json:"available_nodes"`
}

type NodeSummary struct {
	ID                      string             `json:"id"`
	OrganizationID          string             `json:"organization_id,omitempty"`
	FleetID                 string             `json:"fleet_id,omitempty"`
	FleetName               string             `json:"fleet_name,omitempty"`
	State                   string             `json:"state"`
	CurrentModelID          string             `json:"current_model_id,omitempty"`
	QuarantinedAt           *time.Time         `json:"quarantined_at,omitempty"`
	LastSeenAt              *time.Time         `json:"last_seen_at,omitempty"`
	SessionStatus           string             `json:"session_status"`
	LastHeartbeatAt         *time.Time         `json:"last_heartbeat_at,omitempty"`
	LastHeartbeatAgeSeconds *int64             `json:"last_heartbeat_age_seconds,omitempty"`
	ScheduleState           string             `json:"schedule_state,omitempty"`
	ThermalState            string             `json:"thermal_state,omitempty"`
	Paused                  bool               `json:"paused"`
	Draining                bool               `json:"draining"`
	GPU                     protocol.GPUStatus `json:"gpu"`
	Reputation              NodeReputation     `json:"reputation"`
	RecentErrors            []string           `json:"recent_errors"`
}

type NodeReputation struct {
	TotalAcceptedJobs         int64   `json:"total_accepted_jobs"`
	RollingSuccessRate        float64 `json:"rolling_success_rate"`
	TimeoutRate               float64 `json:"timeout_rate"`
	HashMismatchCount         int64   `json:"hash_mismatch_count"`
	ChallengePassRate         float64 `json:"challenge_pass_rate"`
	DuplicateDisagreementRate float64 `json:"duplicate_disagreement_rate"`
	SessionStability          float64 `json:"session_stability"`
	LastQuarantineReason      string  `json:"last_quarantine_reason,omitempty"`
}

type ModelSummary struct {
	ID               string            `json:"id"`
	DisplayName      string            `json:"display_name"`
	Status           string            `json:"status"`
	DataClass        string            `json:"data_class"`
	Version          string            `json:"version"`
	CatalogStatus    string            `json:"catalog_status"`
	Capabilities     []string          `json:"capabilities"`
	Pricing          jobs.ModelPricing `json:"pricing"`
	PriceVersion     string            `json:"price_version"`
	AvailableNodes   int               `json:"available_nodes"`
	HardwareProfiles []HardwareProfile `json:"hardware_profiles"`
}

type HardwareProfile struct {
	HardwareClass    string  `json:"hardware_class"`
	MinVRAMMB        int     `json:"min_vram_mb"`
	MinRAMMB         int     `json:"min_ram_mb"`
	TokensPerSecond  float64 `json:"tokens_per_second,omitempty"`
	MaxContextTokens int     `json:"max_context_tokens"`
}

type JobSummary struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organization_id,omitempty"`
	ModelID        string     `json:"model_id"`
	State          string     `json:"state"`
	Priority       string     `json:"priority"`
	Attempts       int        `json:"attempts"`
	ErrorCode      string     `json:"error_code,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	DeadlineAt     *time.Time `json:"deadline_at,omitempty"`
	Timings        JobTimings `json:"timings"`
}

type JobTimings struct {
	QueueMilliseconds *int64 `json:"queue_milliseconds,omitempty"`
	RunMilliseconds   *int64 `json:"run_milliseconds,omitempty"`
	TotalMilliseconds *int64 `json:"total_milliseconds,omitempty"`
}

type JobDetail struct {
	JobSummary
	AttemptsDetail []AttemptSummary `json:"attempts_detail"`
	Usage          *protocol.Usage  `json:"usage,omitempty"`
}

type AttemptSummary struct {
	ID            string     `json:"id"`
	NodeID        string     `json:"node_id,omitempty"`
	AttemptNumber int        `json:"attempt_number"`
	Status        string     `json:"status"`
	ErrorCode     string     `json:"error_code,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	AcceptedAt    *time.Time `json:"accepted_at,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

type LedgerOverview struct {
	CustomerChargesMicrodollars      int64          `json:"customer_charges_microdollars"`
	HostPendingCreditMicrodollars    int64          `json:"host_pending_credit_microdollars"`
	HostAvailableCreditMicrodollars  int64          `json:"host_available_credit_microdollars"`
	VerificationOverheadMicrodollars int64          `json:"verification_overhead_microdollars"`
	PayoutBatches                    []PayoutRecord `json:"payout_batches"`
}

type PayoutRecord struct {
	ID                string     `json:"id"`
	OrganizationID    string     `json:"organization_id,omitempty"`
	Status            string     `json:"status"`
	TotalMicrodollars int64      `json:"total_microdollars"`
	CreatedAt         time.Time  `json:"created_at"`
	ExportedAt        *time.Time `json:"exported_at,omitempty"`
	PaidAt            *time.Time `json:"paid_at,omitempty"`
}

type AuditLog struct {
	OperatorActions []OperatorAction `json:"operator_actions"`
	SecurityEvents  []SecurityEvent  `json:"security_events"`
	ManifestChanges []AuditEvent     `json:"manifest_changes"`
}

type OperatorAction struct {
	ID         string          `json:"id"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	Reason     string          `json:"reason,omitempty"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  time.Time       `json:"created_at"`
}

type SecurityEvent struct {
	ID             string          `json:"id"`
	Severity       string          `json:"severity"`
	EventType      string          `json:"event_type"`
	NodeID         string          `json:"node_id,omitempty"`
	OrganizationID string          `json:"organization_id,omitempty"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedAt      time.Time       `json:"created_at"`
}

type AuditEvent struct {
	ID         string          `json:"id"`
	ActorType  string          `json:"actor_type"`
	ActorID    string          `json:"actor_id,omitempty"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type,omitempty"`
	TargetID   string          `json:"target_id,omitempty"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  time.Time       `json:"created_at"`
}

type Alert struct {
	Code      string         `json:"code"`
	Severity  string         `json:"severity"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

type AlertCounts struct {
	Disconnects      int
	JobsTotal        int
	JobsFailed       int
	HashMismatches   int
	RuntimeCrashes   int
	OverTemps        int
	LedgerImbalances int
	AuthAnomalies    int
	NoCapacityModels []string
}

type Fleet struct {
	ID               string    `json:"id"`
	OrganizationID   string    `json:"organization_id"`
	Name             string    `json:"name"`
	ScheduleFrom     string    `json:"schedule_from,omitempty"`
	ScheduleUntil    string    `json:"schedule_until,omitempty"`
	ScheduleTimezone string    `json:"schedule_timezone"`
	CreatedAt        time.Time `json:"created_at"`
}

type ScheduleDefaults struct {
	From     string
	Until    string
	Timezone string
	Source   string
}

func (s Store) Overview(ctx context.Context) (Overview, error) {
	now := s.now()
	online, err := s.onlineNodeCount(ctx, now)
	if err != nil {
		return Overview{}, err
	}
	jobsPerHour, successRate, err := s.jobStats(ctx, now.Add(-time.Hour))
	if err != nil {
		return Overview{}, err
	}
	pending, available, err := s.creditTotals(ctx)
	if err != nil {
		return Overview{}, err
	}
	models, err := s.ListModels(ctx)
	if err != nil {
		return Overview{}, err
	}
	availability := make([]ModelAvailability, 0, len(models))
	for _, model := range models {
		availability = append(availability, ModelAvailability{ModelID: model.ID, AvailableNodes: model.AvailableNodes})
	}
	alerts, err := s.AlertsList(ctx)
	if err != nil {
		return Overview{}, err
	}
	return Overview{
		OnlineNodes:                     online,
		AvailableByModel:                availability,
		JobsPerHour:                     jobsPerHour,
		SuccessRate:                     successRate,
		PendingHostCreditMicrodollars:   pending,
		AvailableHostCreditMicrodollars: available,
		ActiveAlerts:                    alerts,
	}, nil
}

func (s Store) ListNodes(ctx context.Context) ([]NodeSummary, error) {
	if s.Pool == nil {
		return nil, fmt.Errorf("operator store is not configured")
	}
	rows, err := s.Pool.Query(ctx, `
SELECT n.id, COALESCE(n.organization_id, ''), COALESCE(n.fleet_id, ''), COALESCE(f.name, ''),
       n.state, COALESCE(n.current_model_id, ''), n.quarantined_at, n.last_seen_at,
       COALESCE(ns.state, 'none'), ns.last_heartbeat_at,
       COALESCE(hb.gpu, '{}'::jsonb), COALESCE(hb.schedule_state, ''), COALESCE(hb.thermal_state, ''),
       COALESCE(hb.paused, false), COALESCE(hb.draining, false),
       COALESCE(rep.total_accepted_jobs, 0),
       COALESCE(rep.rolling_success_rate::float8, 0),
       COALESCE(rep.timeout_rate::float8, 0),
       COALESCE(rep.hash_mismatch_count, 0),
       COALESCE(rep.challenge_pass_rate::float8, 0),
       COALESCE(rep.duplicate_disagreement_rate::float8, 0),
       COALESCE(rep.session_stability::float8, 0),
       COALESCE(rep.last_quarantine_reason, '')
FROM nodes n
LEFT JOIN fleets f ON f.id = n.fleet_id
LEFT JOIN LATERAL (
  SELECT state, last_heartbeat_at
  FROM node_sessions
  WHERE node_id = n.id
  ORDER BY connected_at DESC
  LIMIT 1
) ns ON true
LEFT JOIN LATERAL (
  SELECT gpu, schedule_state, thermal_state, paused, draining
  FROM node_heartbeats
  WHERE node_id = n.id
  ORDER BY received_at DESC
  LIMIT 1
) hb ON true
LEFT JOIN node_reputation rep ON rep.node_id = n.id
ORDER BY n.id
`)
	if err != nil {
		return nil, fmt.Errorf("list operator nodes: %w", err)
	}
	defer rows.Close()
	var nodes []NodeSummary
	now := s.now()
	for rows.Next() {
		var node NodeSummary
		var quarantinedAt, lastSeenAt, lastHeartbeatAt sql.NullTime
		var gpuRaw []byte
		if err := rows.Scan(
			&node.ID, &node.OrganizationID, &node.FleetID, &node.FleetName,
			&node.State, &node.CurrentModelID, &quarantinedAt, &lastSeenAt,
			&node.SessionStatus, &lastHeartbeatAt, &gpuRaw, &node.ScheduleState, &node.ThermalState,
			&node.Paused, &node.Draining, &node.Reputation.TotalAcceptedJobs,
			&node.Reputation.RollingSuccessRate, &node.Reputation.TimeoutRate, &node.Reputation.HashMismatchCount,
			&node.Reputation.ChallengePassRate, &node.Reputation.DuplicateDisagreementRate,
			&node.Reputation.SessionStability, &node.Reputation.LastQuarantineReason,
		); err != nil {
			return nil, fmt.Errorf("scan operator node: %w", err)
		}
		node.QuarantinedAt = timePtr(quarantinedAt)
		node.LastSeenAt = timePtr(lastSeenAt)
		node.LastHeartbeatAt = timePtr(lastHeartbeatAt)
		if node.LastHeartbeatAt != nil {
			age := int64(now.Sub(*node.LastHeartbeatAt).Seconds())
			if age < 0 {
				age = 0
			}
			node.LastHeartbeatAgeSeconds = &age
		}
		_ = json.Unmarshal(gpuRaw, &node.GPU)
		node.RecentErrors, _ = s.recentNodeErrors(ctx, node.ID)
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate operator nodes: %w", err)
	}
	return nodes, nil
}

func (s Store) NodeDetail(ctx context.Context, nodeID string) (NodeSummary, error) {
	nodes, err := s.ListNodes(ctx)
	if err != nil {
		return NodeSummary{}, err
	}
	for _, node := range nodes {
		if node.ID == nodeID {
			return node, nil
		}
	}
	return NodeSummary{}, fmt.Errorf("node %s not found", nodeID)
}

func (s Store) DrainNode(ctx context.Context, nodeID, reason string, now time.Time) error {
	if now.IsZero() {
		now = s.now()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin drain node: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "UPDATE nodes SET state = 'DRAINING', updated_at = $2 WHERE id = $1", nodeID, now); err != nil {
		return fmt.Errorf("mark node draining: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE node_sessions
SET state = 'draining'
WHERE id = (
  SELECT id FROM node_sessions WHERE node_id = $1 AND state = 'connected' ORDER BY connected_at DESC LIMIT 1
)
`, nodeID); err != nil {
		return fmt.Errorf("mark session draining: %w", err)
	}
	if err := recordOperatorActionTx(ctx, tx, ActionNodeDrain, "node", nodeID, reason, map[string]any{"source": "internal_api"}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) PauseNode(ctx context.Context, nodeID, reason string, now time.Time) error {
	return s.updateNodeAction(ctx, nodeID, "PAUSED", ActionNodePause, reason, now)
}

func (s Store) QuarantineNode(ctx context.Context, nodeID, reason string, now time.Time) error {
	if now.IsZero() {
		now = s.now()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin quarantine node: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
UPDATE nodes
SET quarantined_at = COALESCE(quarantined_at, $2), updated_at = $2
WHERE id = $1
`, nodeID, now); err != nil {
		return fmt.Errorf("quarantine node: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO node_reputation (node_id, rolling_success_rate, challenge_pass_rate, session_stability, quarantined_at, last_quarantine_reason, updated_at)
VALUES ($1, 0.6000, 1.0000, 0.6000, $2, NULLIF($3, ''), $2)
ON CONFLICT (node_id) DO UPDATE
SET quarantined_at = COALESCE(node_reputation.quarantined_at, EXCLUDED.quarantined_at),
    last_quarantine_reason = COALESCE(NULLIF(EXCLUDED.last_quarantine_reason, ''), node_reputation.last_quarantine_reason),
    updated_at = EXCLUDED.updated_at
`, nodeID, now, reason); err != nil {
		return fmt.Errorf("update quarantine reputation: %w", err)
	}
	if err := recordOperatorActionTx(ctx, tx, ActionNodeQuarantine, "node", nodeID, reason, map[string]any{"source": "internal_api"}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) ListModels(ctx context.Context) ([]ModelSummary, error) {
	if s.JobStore.Pool == nil {
		s.JobStore = jobs.PGStore{Pool: s.Pool}
	}
	modelInfos, err := s.JobStore.ListModels(ctx, s.now().Add(-s.staleAfter()))
	if err != nil {
		return nil, err
	}
	out := make([]ModelSummary, 0, len(modelInfos))
	for _, model := range modelInfos {
		status := ""
		if err := s.Pool.QueryRow(ctx, "SELECT status FROM models WHERE id = $1", model.ID).Scan(&status); err != nil {
			return nil, fmt.Errorf("load model status: %w", err)
		}
		profiles, err := s.hardwareProfiles(ctx, model.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, ModelSummary{
			ID:               model.ID,
			DisplayName:      model.DisplayName,
			Status:           status,
			DataClass:        model.DataClass,
			Version:          model.Version,
			CatalogStatus:    status,
			Capabilities:     model.Capabilities,
			Pricing:          model.Pricing,
			PriceVersion:     model.Verification.PriceVersion,
			AvailableNodes:   model.Availability.AvailableNodes,
			HardwareProfiles: profiles,
		})
	}
	return out, nil
}

func (s Store) ListJobs(ctx context.Context) ([]JobSummary, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT j.id, j.organization_id, j.model_id, j.state, j.priority, j.created_at, j.updated_at,
       j.completed_at, j.deadline_at, count(ja.id), COALESCE(err.error_code, '')
FROM jobs j
LEFT JOIN job_attempts ja ON ja.job_id = j.id
LEFT JOIN LATERAL (
  SELECT error_code
  FROM job_attempts
  WHERE job_id = j.id AND error_code IS NOT NULL
  ORDER BY COALESCE(finished_at, started_at, accepted_at, created_at) DESC
  LIMIT 1
) err ON true
GROUP BY j.id, err.error_code
ORDER BY j.created_at DESC
LIMIT $1
`, defaultRecentListSize)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	var out []JobSummary
	for rows.Next() {
		summary, err := scanJobSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return out, nil
}

func (s Store) JobDetail(ctx context.Context, jobID string) (JobDetail, error) {
	row := s.Pool.QueryRow(ctx, `
SELECT j.id, j.organization_id, j.model_id, j.state, j.priority, j.created_at, j.updated_at,
       j.completed_at, j.deadline_at, count(ja.id), COALESCE(err.error_code, '')
FROM jobs j
LEFT JOIN job_attempts ja ON ja.job_id = j.id
LEFT JOIN LATERAL (
  SELECT error_code
  FROM job_attempts
  WHERE job_id = j.id AND error_code IS NOT NULL
  ORDER BY COALESCE(finished_at, started_at, accepted_at, created_at) DESC
  LIMIT 1
) err ON true
WHERE j.id = $1
GROUP BY j.id, err.error_code
`, jobID)
	summary, err := scanJobSummary(row)
	if err != nil {
		return JobDetail{}, fmt.Errorf("load job detail: %w", err)
	}
	attempts, err := s.jobAttempts(ctx, jobID)
	if err != nil {
		return JobDetail{}, err
	}
	var usage protocol.Usage
	var hasUsage bool
	err = s.Pool.QueryRow(ctx, `
SELECT prompt_tokens, completion_tokens, prompt_tokens + completion_tokens
FROM job_results
WHERE job_id = $1 AND accepted
ORDER BY created_at DESC
LIMIT 1
`, jobID).Scan(&usage.PromptTokens, &usage.CompletionTokens, &usage.TotalTokens)
	if err == nil {
		hasUsage = true
	} else if !errorsIsNoRows(err) {
		return JobDetail{}, fmt.Errorf("load job usage: %w", err)
	}
	detail := JobDetail{JobSummary: summary, AttemptsDetail: attempts}
	if hasUsage {
		detail.Usage = &usage
	}
	return detail, nil
}

func (s Store) CancelJob(ctx context.Context, jobID, reason string, now time.Time) error {
	if now.IsZero() {
		now = s.now()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin operator cancel job: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
UPDATE job_attempts
SET status = 'cancelled', finished_at = COALESCE(finished_at, $2)
WHERE job_id = $1 AND status IN ('offered', 'accepted', 'running')
`, jobID, now); err != nil {
		return fmt.Errorf("cancel active attempts: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE jobs
SET state = 'cancelled', updated_at = $2, completed_at = $2
WHERE id = $1 AND state <> 'succeeded'
`, jobID, now); err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	if err := recordOperatorActionTx(ctx, tx, ActionJobCancel, "job", jobID, reason, map[string]any{"source": "internal_api"}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) RetryJob(ctx context.Context, jobID, reason string, now time.Time) error {
	if now.IsZero() {
		now = s.now()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin operator retry job: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
UPDATE jobs
SET state = 'queued', completed_at = NULL, updated_at = $2
WHERE id = $1 AND state IN ('failed', 'cancelled')
`, jobID, now); err != nil {
		return fmt.Errorf("retry job: %w", err)
	}
	if err := recordOperatorActionTx(ctx, tx, ActionJobRetry, "job", jobID, reason, map[string]any{"source": "internal_api"}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) LedgerOverview(ctx context.Context) (LedgerOverview, error) {
	var overview LedgerOverview
	err := s.Pool.QueryRow(ctx, `
SELECT
  COALESCE(SUM(le.amount_microdollars) FILTER (WHERE la.account_type = $1), 0),
  COALESCE(SUM(le.amount_microdollars) FILTER (WHERE la.account_type = $2), 0)
FROM ledger_entries le
JOIN ledger_transactions lt ON lt.id = le.transaction_id AND lt.status = 'posted'
JOIN ledger_accounts la ON la.id = le.account_id
`, ledger.AccountCustomerUsage, ledger.AccountPlatformVerification).Scan(&overview.CustomerChargesMicrodollars, &overview.VerificationOverheadMicrodollars)
	if err != nil {
		return LedgerOverview{}, fmt.Errorf("ledger aggregates: %w", err)
	}
	pending, available, err := s.creditTotals(ctx)
	if err != nil {
		return LedgerOverview{}, err
	}
	overview.HostPendingCreditMicrodollars = pending
	overview.HostAvailableCreditMicrodollars = available
	batches, err := s.payoutBatches(ctx)
	if err != nil {
		return LedgerOverview{}, err
	}
	overview.PayoutBatches = batches
	return overview, nil
}

func (s Store) PromoteCredits(ctx context.Context, now time.Time) (int64, error) {
	if now.IsZero() {
		now = s.now()
	}
	store := s.ledgerStore()
	released, err := store.PromoteAvailableCredits(ctx, now)
	if err != nil {
		return 0, err
	}
	if err := s.RecordOperatorAction(ctx, ActionCreditsRelease, "host_credit_holds", "available", "", map[string]any{"released": released}, now); err != nil {
		return released, err
	}
	return released, nil
}

func (s Store) CreatePayoutBatch(ctx context.Context, orgID string, now time.Time) (ledger.PayoutBatch, error) {
	if now.IsZero() {
		now = s.now()
	}
	batch, err := s.ledgerStore().CreatePayoutBatch(ctx, orgID, now)
	if err != nil {
		return ledger.PayoutBatch{}, err
	}
	if err := s.RecordOperatorAction(ctx, ActionPayoutCreate, "payout_batch", batch.ID, "", map[string]any{"organization_id": orgID, "total_microdollars": batch.TotalMicrodollars}, now); err != nil {
		return ledger.PayoutBatch{}, err
	}
	return batch, nil
}

func (s Store) ExportPayoutBatch(ctx context.Context, batchID string, now time.Time) ([]byte, ledger.PayoutBatch, error) {
	if now.IsZero() {
		now = s.now()
	}
	body, batch, err := s.ledgerStore().ExportPayoutBatch(ctx, batchID, now)
	if err != nil {
		return nil, ledger.PayoutBatch{}, err
	}
	if err := s.RecordOperatorAction(ctx, ActionPayoutExport, "payout_batch", batch.ID, "", map[string]any{"csv_sha256": batch.ExportedCSVChecksum}, now); err != nil {
		return nil, ledger.PayoutBatch{}, err
	}
	return body, batch, nil
}

func (s Store) ConfirmPayoutBatch(ctx context.Context, batchID string, csvBody []byte, now time.Time) (ledger.PayoutBatch, error) {
	if now.IsZero() {
		now = s.now()
	}
	batch, err := s.ledgerStore().ConfirmPayoutBatch(ctx, batchID, bytes.NewReader(csvBody), now)
	if err != nil {
		return ledger.PayoutBatch{}, err
	}
	if err := s.RecordOperatorAction(ctx, ActionPayoutConfirm, "payout_batch", batch.ID, "", map[string]any{"ledger_transaction_id": batch.TransactionID}, now); err != nil {
		return ledger.PayoutBatch{}, err
	}
	return batch, nil
}

func (s Store) Audit(ctx context.Context) (AuditLog, error) {
	actions, err := s.operatorActions(ctx)
	if err != nil {
		return AuditLog{}, err
	}
	security, err := s.securityEvents(ctx)
	if err != nil {
		return AuditLog{}, err
	}
	manifest, err := s.manifestAuditEvents(ctx)
	if err != nil {
		return AuditLog{}, err
	}
	return AuditLog{OperatorActions: actions, SecurityEvents: security, ManifestChanges: manifest}, nil
}

func (s Store) AlertsList(ctx context.Context) ([]Alert, error) {
	cfg := s.alertConfig()
	now := s.now()
	counts := AlertCounts{}
	var err error
	if counts.Disconnects, err = s.countInt(ctx, "SELECT count(*) FROM node_sessions WHERE state IN ('closed', 'stale') AND disconnected_at >= $1", now.Add(-cfg.DisconnectSpikeWindow)); err != nil {
		return nil, err
	}
	if err := s.Pool.QueryRow(ctx, `
SELECT count(*), count(*) FILTER (WHERE state = 'failed')
FROM jobs
WHERE created_at >= $1
`, now.Add(-cfg.JobFailureWindow)).Scan(&counts.JobsTotal, &counts.JobsFailed); err != nil {
		return nil, fmt.Errorf("count job failures: %w", err)
	}
	if counts.HashMismatches, err = s.countInt(ctx, "SELECT count(*) FROM job_attempts WHERE error_code LIKE '%hash_mismatch%' OR error_code LIKE '%hash mismatch%'"); err != nil {
		return nil, err
	}
	if counts.RuntimeCrashes, err = s.countInt(ctx, "SELECT count(*) FROM security_events WHERE event_type IN ('runtime_crash', 'runtime_crash_loop') AND created_at >= $1", now.Add(-cfg.RuntimeCrashWindow)); err != nil {
		return nil, err
	}
	if counts.OverTemps, err = s.countInt(ctx, "SELECT count(*) FROM security_events WHERE event_type IN ('over_temperature', 'thermal_hard_limit') AND created_at >= $1", now.Add(-cfg.OverTempWindow)); err != nil {
		return nil, err
	}
	if counts.AuthAnomalies, err = s.countInt(ctx, "SELECT count(*) FROM security_events WHERE event_type IN ('auth_anomaly', 'operator_auth_failed') AND created_at >= $1", now.Add(-cfg.AuthAnomalyWindow)); err != nil {
		return nil, err
	}
	if cfg.LedgerImbalanceCheck {
		if counts.LedgerImbalances, err = s.countInt(ctx, `
SELECT count(*)
FROM (
  SELECT lt.id
  FROM ledger_transactions lt
  LEFT JOIN ledger_entries le ON le.transaction_id = lt.id
  WHERE lt.status = 'posted'
  GROUP BY lt.id
  HAVING COALESCE(SUM(le.amount_microdollars), 0) <> 0
) imbalanced
`); err != nil {
			return nil, err
		}
	}
	if cfg.NoCapacityForModelEnabled {
		models, err := s.ListModels(ctx)
		if err != nil {
			return nil, err
		}
		for _, model := range models {
			if model.Status == "alpha" || model.Status == "active" {
				if model.AvailableNodes == 0 {
					counts.NoCapacityModels = append(counts.NoCapacityModels, model.ID)
				}
			}
		}
	}
	return BuildAlerts(counts, cfg, now), nil
}

func BuildAlerts(counts AlertCounts, cfg AlertConfig, now time.Time) []Alert {
	if cfg == (AlertConfig{}) {
		cfg = DefaultAlertConfig()
	}
	var alerts []Alert
	if counts.Disconnects >= cfg.DisconnectSpikeThreshold && cfg.DisconnectSpikeThreshold > 0 {
		alerts = append(alerts, Alert{Code: "node_disconnect_spike", Severity: "warning", Message: "Node disconnect spike detected.", Metadata: map[string]any{"disconnects": counts.Disconnects}, CreatedAt: now})
	}
	if counts.JobsTotal > 0 && cfg.JobFailureRateThreshold > 0 {
		rate := float64(counts.JobsFailed) / float64(counts.JobsTotal)
		if rate >= cfg.JobFailureRateThreshold {
			alerts = append(alerts, Alert{Code: "job_failure_rate", Severity: "warning", Message: "Job failure rate is above threshold.", Metadata: map[string]any{"failed": counts.JobsFailed, "total": counts.JobsTotal, "rate": rate}, CreatedAt: now})
		}
	}
	if counts.HashMismatches > 0 {
		alerts = append(alerts, Alert{Code: "hash_mismatch", Severity: "critical", Message: "Hash mismatch events require review.", Metadata: map[string]any{"count": counts.HashMismatches}, CreatedAt: now})
	}
	if counts.RuntimeCrashes >= cfg.RuntimeCrashThreshold && cfg.RuntimeCrashThreshold > 0 {
		alerts = append(alerts, Alert{Code: "runtime_crash_loop", Severity: "critical", Message: "Runtime crash loop detected.", Metadata: map[string]any{"count": counts.RuntimeCrashes}, CreatedAt: now})
	}
	if counts.OverTemps > 0 {
		alerts = append(alerts, Alert{Code: "gpu_over_temp", Severity: "warning", Message: "GPU over-temperature safety event recorded.", Metadata: map[string]any{"count": counts.OverTemps}, CreatedAt: now})
	}
	if counts.LedgerImbalances > 0 {
		alerts = append(alerts, Alert{Code: "ledger_imbalance", Severity: "critical", Message: "Posted ledger transaction imbalance detected.", Metadata: map[string]any{"count": counts.LedgerImbalances}, CreatedAt: now})
	}
	if counts.AuthAnomalies >= cfg.AuthAnomalyThreshold && cfg.AuthAnomalyThreshold > 0 {
		alerts = append(alerts, Alert{Code: "auth_anomaly", Severity: "warning", Message: "Authentication anomaly threshold exceeded.", Metadata: map[string]any{"count": counts.AuthAnomalies}, CreatedAt: now})
	}
	for _, modelID := range counts.NoCapacityModels {
		alerts = append(alerts, Alert{Code: "no_capacity", Severity: "warning", Message: "Published model has no available nodes.", Metadata: map[string]any{"model_id": modelID}, CreatedAt: now})
	}
	return alerts
}

func (s Store) CreateFleet(ctx context.Context, orgID, name string, schedule ScheduleDefaults, now time.Time) (Fleet, error) {
	if strings.TrimSpace(orgID) == "" {
		return Fleet{}, fmt.Errorf("organization id is required")
	}
	if strings.TrimSpace(name) == "" {
		return Fleet{}, fmt.Errorf("fleet name is required")
	}
	if now.IsZero() {
		now = s.now()
	}
	from, until := strings.TrimSpace(schedule.From), strings.TrimSpace(schedule.Until)
	if (from == "") != (until == "") {
		return Fleet{}, fmt.Errorf("fleet schedule requires both from and until")
	}
	if from != "" {
		if err := validateClock(from); err != nil {
			return Fleet{}, fmt.Errorf("schedule from: %w", err)
		}
		if err := validateClock(until); err != nil {
			return Fleet{}, fmt.Errorf("schedule until: %w", err)
		}
	}
	timezone := schedule.Timezone
	if timezone == "" {
		timezone = "local"
	}
	fleetID, err := ids.New("fleet")
	if err != nil {
		return Fleet{}, err
	}
	var created Fleet
	err = s.Pool.QueryRow(ctx, `
INSERT INTO fleets (id, organization_id, name, enrollment_status, schedule_from, schedule_until, schedule_timezone, schedule_updated_at, created_at, updated_at)
VALUES ($1, $2, $3, 'active', NULLIF($4, ''), NULLIF($5, ''), $6, $7, $7, $7)
RETURNING id, organization_id, name, COALESCE(schedule_from, ''), COALESCE(schedule_until, ''), schedule_timezone, created_at
`, fleetID, orgID, strings.TrimSpace(name), from, until, timezone, now).Scan(&created.ID, &created.OrganizationID, &created.Name, &created.ScheduleFrom, &created.ScheduleUntil, &created.ScheduleTimezone, &created.CreatedAt)
	if err != nil {
		return Fleet{}, fmt.Errorf("create fleet: %w", err)
	}
	return created, nil
}

func (s Store) FleetReportCSV(ctx context.Context, fleetID string, from, until time.Time) ([]byte, error) {
	if strings.TrimSpace(fleetID) == "" {
		return nil, fmt.Errorf("fleet id is required")
	}
	if from.IsZero() {
		from = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if until.IsZero() {
		until = s.now().Add(time.Second)
	}
	rows, err := s.Pool.Query(ctx, `
SELECT n.id,
       count(DISTINCT j.id) FILTER (WHERE j.state = 'succeeded'),
       count(DISTINCT j.id) FILTER (WHERE j.state = 'failed'),
       COALESCE(SUM(jr.prompt_tokens), 0),
       COALESCE(SUM(jr.completion_tokens), 0),
       COALESCE(SUM(h.amount_microdollars), 0)
FROM nodes n
LEFT JOIN job_attempts ja ON ja.node_id = n.id
LEFT JOIN jobs j ON j.id = ja.job_id AND j.created_at >= $2 AND j.created_at < $3
LEFT JOIN job_results jr ON jr.attempt_id = ja.id AND jr.accepted AND j.id IS NOT NULL
LEFT JOIN host_credit_holds h ON h.attempt_id = ja.id AND j.id IS NOT NULL
WHERE n.fleet_id = $1
GROUP BY n.id
ORDER BY n.id
`, fleetID, from, until)
	if err != nil {
		return nil, fmt.Errorf("query fleet report: %w", err)
	}
	defer rows.Close()
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"fleet_id", "node_id", "jobs_succeeded", "jobs_failed", "prompt_tokens", "completion_tokens", "host_credit_microdollars"}); err != nil {
		return nil, err
	}
	for rows.Next() {
		var nodeID string
		var succeeded, failed, promptTokens, completionTokens, credit int64
		if err := rows.Scan(&nodeID, &succeeded, &failed, &promptTokens, &completionTokens, &credit); err != nil {
			return nil, fmt.Errorf("scan fleet report: %w", err)
		}
		if err := writer.Write([]string{
			fleetID,
			nodeID,
			strconv.FormatInt(succeeded, 10),
			strconv.FormatInt(failed, 10),
			strconv.FormatInt(promptTokens, 10),
			strconv.FormatInt(completionTokens, 10),
			strconv.FormatInt(credit, 10),
		}); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fleet report: %w", err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func EffectiveSchedule(local ScheduleDefaults, hasLocalOverride bool, fleet ScheduleDefaults) ScheduleDefaults {
	if hasLocalOverride && local.From != "" && local.Until != "" {
		local.Source = "node"
		return local
	}
	if fleet.From != "" && fleet.Until != "" {
		if fleet.Timezone == "" {
			fleet.Timezone = "local"
		}
		fleet.Source = "fleet"
		return fleet
	}
	if local.From == "" {
		local.From = "00:00"
	}
	if local.Until == "" {
		local.Until = "00:00"
	}
	if local.Timezone == "" {
		local.Timezone = "local"
	}
	local.Source = "node"
	return local
}

func (s Store) RecordManifestSync(ctx context.Context, catalogDir string, synced int, now time.Time) error {
	if now.IsZero() {
		now = s.now()
	}
	eventID, err := ids.New("audit")
	if err != nil {
		return err
	}
	metadata, err := json.Marshal(map[string]any{"catalog_dir": catalogDir, "synced": synced})
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
INSERT INTO audit_events (id, actor_type, actor_id, action, target_type, target_id, metadata, created_at)
VALUES ($1, 'operator', $2, $3, 'catalog', $4, $5::jsonb, $6)
`, eventID, defaultOperatorActor, ActionCatalogSync, catalogDir, string(metadata), now)
	if err != nil {
		return fmt.Errorf("record manifest sync audit: %w", err)
	}
	return s.RecordOperatorAction(ctx, ActionCatalogSync, "catalog", catalogDir, "", map[string]any{"synced": synced}, now)
}

func (s Store) RecordOperatorAction(ctx context.Context, action, targetType, targetID, reason string, metadata map[string]any, now time.Time) error {
	if now.IsZero() {
		now = s.now()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin operator action: %w", err)
	}
	defer tx.Rollback(context.Background())
	if err := recordOperatorActionTx(ctx, tx, action, targetType, targetID, reason, metadata, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) updateNodeAction(ctx context.Context, nodeID, state, action, reason string, now time.Time) error {
	if now.IsZero() {
		now = s.now()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin node action: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "UPDATE nodes SET state = $2, updated_at = $3 WHERE id = $1", nodeID, state, now); err != nil {
		return fmt.Errorf("update node state: %w", err)
	}
	if err := recordOperatorActionTx(ctx, tx, action, "node", nodeID, reason, map[string]any{"state": state, "source": "internal_api"}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func recordOperatorActionTx(ctx context.Context, tx pgx.Tx, action, targetType, targetID, reason string, metadata map[string]any, now time.Time) error {
	actionID, err := ids.New("opact")
	if err != nil {
		return err
	}
	if targetType == "" {
		targetType = "unknown"
	}
	if targetID == "" {
		targetID = "unknown"
	}
	body, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal operator action metadata: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO operator_actions (id, action, target_type, target_id, reason, metadata, created_at)
VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6::jsonb, $7)
`, actionID, action, targetType, targetID, reason, string(body), now)
	if err != nil {
		return fmt.Errorf("insert operator action: %w", err)
	}
	return nil
}

func (s Store) onlineNodeCount(ctx context.Context, now time.Time) (int, error) {
	return s.countInt(ctx, `
SELECT count(*)
FROM nodes n
JOIN LATERAL (
  SELECT state, COALESCE(last_heartbeat_at, connected_at) AS freshness
  FROM node_sessions
  WHERE node_id = n.id
  ORDER BY connected_at DESC
  LIMIT 1
) ns ON true
WHERE ns.state IN ('connected', 'draining') AND ns.freshness >= $1
`, now.Add(-s.staleAfter()))
}

func (s Store) jobStats(ctx context.Context, since time.Time) (int, float64, error) {
	var total, terminal, succeeded int
	if err := s.Pool.QueryRow(ctx, `
SELECT count(*),
       count(*) FILTER (WHERE state IN ('succeeded', 'failed', 'cancelled')),
       count(*) FILTER (WHERE state = 'succeeded')
FROM jobs
WHERE created_at >= $1
`, since).Scan(&total, &terminal, &succeeded); err != nil {
		return 0, 0, fmt.Errorf("query job stats: %w", err)
	}
	rate := 0.0
	if terminal > 0 {
		rate = float64(succeeded) / float64(terminal)
	}
	return total, rate, nil
}

func (s Store) creditTotals(ctx context.Context) (int64, int64, error) {
	var pending, available int64
	err := s.Pool.QueryRow(ctx, `
SELECT COALESCE(SUM(amount_microdollars) FILTER (WHERE state = 'pending'), 0),
       COALESCE(SUM(amount_microdollars) FILTER (WHERE state = 'available'), 0)
FROM host_credit_holds
`).Scan(&pending, &available)
	if err != nil {
		return 0, 0, fmt.Errorf("query credit totals: %w", err)
	}
	return pending, available, nil
}

func (s Store) recentNodeErrors(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT error
FROM (
  SELECT COALESCE(error_code, '') AS error, COALESCE(finished_at, created_at) AS at
  FROM job_attempts
  WHERE node_id = $1 AND error_code IS NOT NULL
  UNION ALL
  SELECT event_type AS error, created_at AS at
  FROM security_events
  WHERE node_id = $1
) recent
WHERE error <> ''
ORDER BY at DESC
LIMIT 5
`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query node errors: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan node error: %w", err)
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (s Store) hardwareProfiles(ctx context.Context, modelID string) ([]HardwareProfile, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT hp.hardware_class, hp.min_vram_mb, hp.min_ram_mb, COALESCE(hp.tokens_per_second::float8, 0), hp.max_context_tokens
FROM model_versions mv
JOIN model_hardware_profiles hp ON hp.model_version_id = mv.id
WHERE mv.model_id = $1
ORDER BY hp.hardware_class
`, modelID)
	if err != nil {
		return nil, fmt.Errorf("query hardware profiles: %w", err)
	}
	defer rows.Close()
	var profiles []HardwareProfile
	for rows.Next() {
		var profile HardwareProfile
		if err := rows.Scan(&profile.HardwareClass, &profile.MinVRAMMB, &profile.MinRAMMB, &profile.TokensPerSecond, &profile.MaxContextTokens); err != nil {
			return nil, fmt.Errorf("scan hardware profile: %w", err)
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanJobSummary(row scanner) (JobSummary, error) {
	var summary JobSummary
	var completedAt, deadlineAt sql.NullTime
	if err := row.Scan(&summary.ID, &summary.OrganizationID, &summary.ModelID, &summary.State, &summary.Priority, &summary.CreatedAt, &summary.UpdatedAt, &completedAt, &deadlineAt, &summary.Attempts, &summary.ErrorCode); err != nil {
		return JobSummary{}, err
	}
	summary.CompletedAt = timePtr(completedAt)
	summary.DeadlineAt = timePtr(deadlineAt)
	summary.Timings = jobTimings(summary.CreatedAt, summary.UpdatedAt, summary.CompletedAt)
	return summary, nil
}

func (s Store) jobAttempts(ctx context.Context, jobID string) ([]AttemptSummary, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, COALESCE(node_id, ''), attempt_number, status, COALESCE(error_code, ''),
       created_at, accepted_at, started_at, finished_at
FROM job_attempts
WHERE job_id = $1
ORDER BY attempt_number
`, jobID)
	if err != nil {
		return nil, fmt.Errorf("query job attempts: %w", err)
	}
	defer rows.Close()
	var attempts []AttemptSummary
	for rows.Next() {
		var attempt AttemptSummary
		var acceptedAt, startedAt, finishedAt sql.NullTime
		if err := rows.Scan(&attempt.ID, &attempt.NodeID, &attempt.AttemptNumber, &attempt.Status, &attempt.ErrorCode, &attempt.CreatedAt, &acceptedAt, &startedAt, &finishedAt); err != nil {
			return nil, fmt.Errorf("scan job attempt: %w", err)
		}
		attempt.AcceptedAt = timePtr(acceptedAt)
		attempt.StartedAt = timePtr(startedAt)
		attempt.FinishedAt = timePtr(finishedAt)
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

func jobTimings(createdAt, updatedAt time.Time, completedAt *time.Time) JobTimings {
	totalEnd := updatedAt
	if completedAt != nil {
		totalEnd = *completedAt
	}
	total := totalEnd.Sub(createdAt).Milliseconds()
	if total < 0 {
		total = 0
	}
	return JobTimings{TotalMilliseconds: &total}
}

func (s Store) payoutBatches(ctx context.Context) ([]PayoutRecord, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, COALESCE(organization_id, ''), status, total_microdollars, created_at, exported_at, paid_at
FROM payout_batches
ORDER BY created_at DESC
LIMIT $1
`, defaultRecentListSize)
	if err != nil {
		return nil, fmt.Errorf("query payout batches: %w", err)
	}
	defer rows.Close()
	var batches []PayoutRecord
	for rows.Next() {
		var batch PayoutRecord
		var exportedAt, paidAt sql.NullTime
		if err := rows.Scan(&batch.ID, &batch.OrganizationID, &batch.Status, &batch.TotalMicrodollars, &batch.CreatedAt, &exportedAt, &paidAt); err != nil {
			return nil, fmt.Errorf("scan payout batch: %w", err)
		}
		batch.ExportedAt = timePtr(exportedAt)
		batch.PaidAt = timePtr(paidAt)
		batches = append(batches, batch)
	}
	return batches, rows.Err()
}

func (s Store) operatorActions(ctx context.Context) ([]OperatorAction, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, action, target_type, target_id, COALESCE(reason, ''), metadata, created_at
FROM operator_actions
ORDER BY created_at DESC
LIMIT $1
`, defaultRecentListSize)
	if err != nil {
		return nil, fmt.Errorf("query operator actions: %w", err)
	}
	defer rows.Close()
	var actions []OperatorAction
	for rows.Next() {
		var action OperatorAction
		if err := rows.Scan(&action.ID, &action.Action, &action.TargetType, &action.TargetID, &action.Reason, &action.Metadata, &action.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan operator action: %w", err)
		}
		actions = append(actions, action)
	}
	return actions, rows.Err()
}

func (s Store) securityEvents(ctx context.Context) ([]SecurityEvent, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, severity, event_type, COALESCE(node_id, ''), COALESCE(organization_id, ''), metadata, created_at
FROM security_events
ORDER BY created_at DESC
LIMIT $1
`, defaultRecentListSize)
	if err != nil {
		return nil, fmt.Errorf("query security events: %w", err)
	}
	defer rows.Close()
	var events []SecurityEvent
	for rows.Next() {
		var event SecurityEvent
		if err := rows.Scan(&event.ID, &event.Severity, &event.EventType, &event.NodeID, &event.OrganizationID, &event.Metadata, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan security event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s Store) manifestAuditEvents(ctx context.Context) ([]AuditEvent, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, actor_type, COALESCE(actor_id, ''), action, COALESCE(target_type, ''), COALESCE(target_id, ''), metadata, created_at
FROM audit_events
WHERE action LIKE 'catalog.%' OR target_type IN ('catalog', 'model_manifest', 'model')
ORDER BY created_at DESC
LIMIT $1
`, defaultRecentListSize)
	if err != nil {
		return nil, fmt.Errorf("query manifest audit events: %w", err)
	}
	defer rows.Close()
	var events []AuditEvent
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(&event.ID, &event.ActorType, &event.ActorID, &event.Action, &event.TargetType, &event.TargetID, &event.Metadata, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s Store) countInt(ctx context.Context, query string, args ...any) (int, error) {
	var count int
	if err := s.Pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count query failed: %w", err)
	}
	return count, nil
}

func (s Store) alertConfig() AlertConfig {
	cfg := s.Alerts
	defaults := DefaultAlertConfig()
	if cfg.DisconnectSpikeWindow <= 0 {
		cfg.DisconnectSpikeWindow = defaults.DisconnectSpikeWindow
	}
	if cfg.DisconnectSpikeThreshold <= 0 {
		cfg.DisconnectSpikeThreshold = defaults.DisconnectSpikeThreshold
	}
	if cfg.JobFailureWindow <= 0 {
		cfg.JobFailureWindow = defaults.JobFailureWindow
	}
	if cfg.JobFailureRateThreshold <= 0 {
		cfg.JobFailureRateThreshold = defaults.JobFailureRateThreshold
	}
	if cfg.RuntimeCrashWindow <= 0 {
		cfg.RuntimeCrashWindow = defaults.RuntimeCrashWindow
	}
	if cfg.RuntimeCrashThreshold <= 0 {
		cfg.RuntimeCrashThreshold = defaults.RuntimeCrashThreshold
	}
	if cfg.OverTempWindow <= 0 {
		cfg.OverTempWindow = defaults.OverTempWindow
	}
	if cfg.NoCapacityWindow <= 0 {
		cfg.NoCapacityWindow = defaults.NoCapacityWindow
	}
	if cfg.AuthAnomalyWindow <= 0 {
		cfg.AuthAnomalyWindow = defaults.AuthAnomalyWindow
	}
	if cfg.AuthAnomalyThreshold <= 0 {
		cfg.AuthAnomalyThreshold = defaults.AuthAnomalyThreshold
	}
	cfg.LedgerImbalanceCheck = cfg.LedgerImbalanceCheck || defaults.LedgerImbalanceCheck
	cfg.NoCapacityForModelEnabled = cfg.NoCapacityForModelEnabled || defaults.NoCapacityForModelEnabled
	return cfg
}

func (s Store) staleAfter() time.Duration {
	if s.StaleAfter > 0 {
		return s.StaleAfter
	}
	return 45 * time.Second
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Store) ledgerStore() ledger.Store {
	if s.LedgerStore.Pool != nil {
		return s.LedgerStore
	}
	return ledger.Store{Pool: s.Pool}
}

func timePtr(value sql.NullTime) *time.Time {
	if value.Valid {
		t := value.Time
		return &t
	}
	return nil
}

func validateClock(value string) error {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return fmt.Errorf("expected HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return fmt.Errorf("hour must be 00 through 23")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return fmt.Errorf("minute must be 00 through 59")
	}
	return nil
}

func errorsIsNoRows(err error) bool {
	return err == pgx.ErrNoRows
}
