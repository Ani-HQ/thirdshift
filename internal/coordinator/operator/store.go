package operator

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/coordinator/jobs"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/ledger"
	"github.com/Ani-HQ/thirdshift/internal/shared/ids"
	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
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
	ActionFleetSetRegion  = "fleet.set_region"
	ActionNodeSetRegion   = "node.set_region"
	defaultOperatorActor  = "operator-token"
	defaultRecentListSize = 100
)

// Public listing statuses mirror the catalog manifest listing block and the
// models.listing_status column.
const (
	ListingLive     = "live"
	ListingWaitlist = "waitlist"
	ListingHidden   = "hidden"
)

// Public availability states. Only the first three describe measured supply;
// waitlist means the model is offered for applications with no supply yet.
// Public host states. Anything not currently serving or idle reads as offline;
// the page never speculates about why a machine went away.
const (
	HostStateServing = "serving"
	HostStateIdle    = "idle"
	HostStateOffline = "offline"
)

const (
	AvailabilityAvailable = "available"
	AvailabilityLimited   = "limited"
	AvailabilityOffline   = "offline"
	AvailabilityWaitlist  = "waitlist"
)

// Expected monthly output volume bands collected on the access application.
var expectedVolumeBands = map[string]bool{
	"lt_1m":    true,
	"1m_10m":   true,
	"10m_100m": true,
	"gt_100m":  true,
}

var regionPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

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
	Region                  string             `json:"region,omitempty"`
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

type PublicStatus struct {
	ConnectedNodeCount        int                 `json:"connected_node_count"`
	Cities                    []string            `json:"cities"`
	ModelsAvailable           []ModelAvailability `json:"models_available"`
	Models                    []PublicModelStatus `json:"models"`
	RegionsOnline             []string            `json:"regions_online"`
	RegionNodeCounts          []RegionNodeCount   `json:"region_node_counts"`
	Hosts                     []PublicHostStatus  `json:"hosts"`
	RequesterRegion           *string             `json:"requester_region"`
	JobsCompleted24h          int64               `json:"jobs_completed_24h"`
	JobsCompletedTotal        int64               `json:"jobs_completed_total"`
	OutputTokensServed24h     int64               `json:"output_tokens_served_24h"`
	OutputTokensServedTotal   int64               `json:"output_tokens_served_total"`
	EstimatedGPUHoursReused   float64             `json:"estimated_gpu_hours_reused"`
	EstimatedGPUHoursReused24 float64             `json:"estimated_gpu_hours_reused_24h"`
	GeneratedAt               time.Time           `json:"generated_at"`
}

// PublicHostStatus is one contributing machine as the public page may see it.
// Handbook section 2.3 is hard law here: no node id, hostname, GPU, fleet, or
// operator name may appear on this struct, now or later.
type PublicHostStatus struct {
	Handle                    string `json:"handle"`
	Region                    string `json:"region,omitempty"`
	State                     string `json:"state"`
	Jobs24h                   int64  `json:"jobs_24h"`
	CreditedMicrodollars24h   int64  `json:"credited_microdollars_24h"`
	CreditedMicrodollarsTotal int64  `json:"credited_microdollars_total"`
}

// RegionNodeCount is the aggregate the public map draws from. A count is the
// smallest number the map can honestly show; it never carries node detail.
type RegionNodeCount struct {
	Region    string `json:"region"`
	NodeCount int    `json:"node_count"`
}

type PublicModelStatus struct {
	ModelID                       string                       `json:"model_id"`
	DisplayName                   string                       `json:"display_name"`
	Description                   string                       `json:"description"`
	ListingStatus                 string                       `json:"listing_status"`
	Capabilities                  []string                     `json:"capabilities"`
	Price                         PublicModelPrice             `json:"price"`
	MarketComparison              *PublicModelMarketComparison `json:"market_comparison"`
	Attribution                   *PublicModelAttribution      `json:"attribution,omitempty"`
	DataClass                     string                       `json:"data_class"`
	Limits                        PublicModelLimits            `json:"limits"`
	Availability                  PublicModelAvailability      `json:"availability"`
	TypicalOutputTokensPerSecond  *float64                     `json:"typical_output_tokens_per_second"`
	ExpectedOutputTokensPerSecond *float64                     `json:"expected_output_tokens_per_second"`
	Regions                       []string                     `json:"regions"`
	Version                       string                       `json:"version"`
}

// PublicModelAttribution carries license-mandated attribution for public
// surfaces (e.g. "Built with Llama"), present only when the catalog manifest
// declares it.
type PublicModelAttribution struct {
	DisplayText string `json:"display_text"`
	NoticeText  string `json:"notice_text,omitempty"`
	LicenseURL  string `json:"license_url,omitempty"`
	AUPURL      string `json:"aup_url,omitempty"`
}

// PublicModelMarketComparison carries the operator-recorded typical hosted
// price for the same model class. It is only ever present when a catalog
// manifest supplies both numbers and a source note.
type PublicModelMarketComparison struct {
	TypicalInputPerMillionMicrodollars  int64  `json:"typical_input_per_million_microdollars"`
	TypicalOutputPerMillionMicrodollars int64  `json:"typical_output_per_million_microdollars"`
	SourceNote                          string `json:"source_note"`
}

type PublicModelPrice struct {
	InputPerMillionMicrodollars  int64 `json:"input_per_million_microdollars"`
	OutputPerMillionMicrodollars int64 `json:"output_per_million_microdollars"`
}

type PublicModelLimits struct {
	ContextTokens   int `json:"context_tokens"`
	MaxOutputTokens int `json:"max_output_tokens"`
}

type PublicModelAvailability struct {
	AvailableNodes int    `json:"available_nodes"`
	State          string `json:"state"`
}

type ContributionCard struct {
	NodeID                   string    `json:"node_id"`
	NodeName                 string    `json:"node_name"`
	NightsActive             int64     `json:"nights_active"`
	JobsAccepted             int64     `json:"jobs_accepted"`
	TokensServed             int64     `json:"tokens_served"`
	CreditEarnedMicrodollars int64     `json:"credit_earned_microdollars"`
	GeneratedAt              time.Time `json:"generated_at"`
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
	Region           string    `json:"region,omitempty"`
	ScheduleFrom     string    `json:"schedule_from,omitempty"`
	ScheduleUntil    string    `json:"schedule_until,omitempty"`
	ScheduleTimezone string    `json:"schedule_timezone"`
	CreatedAt        time.Time `json:"created_at"`
}

type WaitlistSignup struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	Name           string    `json:"name,omitempty"`
	UseCase        string    `json:"use_case,omitempty"`
	ExpectedVolume string    `json:"expected_volume,omitempty"`
	DataAck        bool      `json:"data_ack"`
	ModelID        string    `json:"model_id,omitempty"`
	Source         string    `json:"source"`
	CreatedAt      time.Time `json:"created_at"`
	LastAppliedAt  time.Time `json:"last_applied_at"`
}

// WaitlistApplication is one manually reviewed request for developer access.
type WaitlistApplication struct {
	Email          string
	Name           string
	UseCase        string
	ExpectedVolume string
	DataAck        bool
	ModelID        string
	Source         string
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
       COALESCE(NULLIF(n.region, ''), NULLIF(f.region, ''), ''),
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
			&node.ID, &node.OrganizationID, &node.FleetID, &node.FleetName, &node.Region,
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

func (s Store) PublicStatus(ctx context.Context, now time.Time) (PublicStatus, error) {
	if now.IsZero() {
		now = s.now()
	}
	connected, err := s.onlineNodeCount(ctx, now)
	if err != nil {
		return PublicStatus{}, err
	}
	publicModels, err := s.publicModels(ctx, now)
	if err != nil {
		return PublicStatus{}, err
	}
	modelsAvailable := make([]ModelAvailability, 0, len(publicModels))
	for _, model := range publicModels {
		modelsAvailable = append(modelsAvailable, ModelAvailability{ModelID: model.ModelID, AvailableNodes: model.Availability.AvailableNodes})
	}
	regionsOnline, err := s.regionsOnline(ctx, now)
	if err != nil {
		return PublicStatus{}, err
	}
	regionNodeCounts, err := s.regionNodeCounts(ctx, now)
	if err != nil {
		return PublicStatus{}, err
	}
	hosts, err := s.publicHosts(ctx, now)
	if err != nil {
		return PublicStatus{}, err
	}
	var completed24h, completedTotal int64
	if err := s.Pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE completed_at >= $1),
       count(*)
FROM jobs
WHERE state = 'succeeded'
`, now.Add(-24*time.Hour)).Scan(&completed24h, &completedTotal); err != nil {
		return PublicStatus{}, fmt.Errorf("query public job counts: %w", err)
	}
	var output24h, outputTotal int64
	var gpuHours24h, gpuHoursTotal float64
	if err := s.Pool.QueryRow(ctx, `
SELECT COALESCE(SUM(jr.completion_tokens) FILTER (WHERE jr.created_at >= $1), 0),
       COALESCE(SUM(jr.completion_tokens), 0),
       COALESCE(SUM(GREATEST(jr.coordinator_duration_millis, jr.duration_millis)) FILTER (WHERE jr.created_at >= $1), 0)::float8 / 3600000.0,
       COALESCE(SUM(GREATEST(jr.coordinator_duration_millis, jr.duration_millis)), 0)::float8 / 3600000.0
FROM job_results jr
WHERE jr.accepted
`, now.Add(-24*time.Hour)).Scan(&output24h, &outputTotal, &gpuHours24h, &gpuHoursTotal); err != nil {
		return PublicStatus{}, fmt.Errorf("query public result totals: %w", err)
	}
	return PublicStatus{
		ConnectedNodeCount:        connected,
		Cities:                    []string{},
		ModelsAvailable:           modelsAvailable,
		Models:                    publicModels,
		RegionsOnline:             regionsOnline,
		RegionNodeCounts:          regionNodeCounts,
		Hosts:                     hosts,
		JobsCompleted24h:          completed24h,
		JobsCompletedTotal:        completedTotal,
		OutputTokensServed24h:     output24h,
		OutputTokensServedTotal:   outputTotal,
		EstimatedGPUHoursReused:   gpuHoursTotal,
		EstimatedGPUHoursReused24: gpuHours24h,
		GeneratedAt:               now,
	}, nil
}

func (s Store) publicModels(ctx context.Context, now time.Time) ([]PublicModelStatus, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT m.id, m.display_name, COALESCE(m.description, ''), m.listing_status, m.data_class, mv.version,
       COALESCE(mp.customer_input_per_million_microdollars, 0),
       COALESCE(mp.customer_output_per_million_microdollars, 0),
       m.market_typical_input_per_million_microdollars,
       m.market_typical_output_per_million_microdollars,
       COALESCE(m.market_comparison_source_note, ''),
       m.expected_output_tokens_per_second::float8,
       COALESCE(ml.max_input_tokens, 4096),
       COALESCE(ml.max_output_tokens, 1024),
       COALESCE(ml.capabilities, '{}'::jsonb),
       COALESCE(m.attribution_display_text, ''),
       COALESCE(m.attribution_notice_text, ''),
       COALESCE(m.attribution_license_url, ''),
       COALESCE(m.attribution_aup_url, '')
FROM models m
JOIN LATERAL (
  SELECT * FROM model_versions WHERE model_id = m.id ORDER BY created_at DESC LIMIT 1
) mv ON true
LEFT JOIN LATERAL (
  SELECT * FROM model_prices WHERE model_version_id = mv.id ORDER BY effective_from DESC LIMIT 1
) mp ON true
LEFT JOIN model_manifest_limits ml ON ml.model_id = m.id
WHERE m.status IN ('alpha', 'active') AND m.listing_status <> 'hidden'
ORDER BY (m.listing_status = 'waitlist'), m.id
`)
	if err != nil {
		return nil, fmt.Errorf("query public models: %w", err)
	}
	defer rows.Close()
	out := []PublicModelStatus{}
	for rows.Next() {
		var model PublicModelStatus
		var capabilitiesRaw []byte
		var marketInput, marketOutput *int64
		var marketSourceNote string
		var expectedSpeed *float64
		var attributionDisplay, attributionNotice, attributionLicenseURL, attributionAUPURL string
		if err := rows.Scan(
			&model.ModelID, &model.DisplayName, &model.Description, &model.ListingStatus, &model.DataClass, &model.Version,
			&model.Price.InputPerMillionMicrodollars, &model.Price.OutputPerMillionMicrodollars,
			&marketInput, &marketOutput, &marketSourceNote, &expectedSpeed,
			&model.Limits.ContextTokens, &model.Limits.MaxOutputTokens, &capabilitiesRaw,
			&attributionDisplay, &attributionNotice, &attributionLicenseURL, &attributionAUPURL,
		); err != nil {
			return nil, fmt.Errorf("scan public model: %w", err)
		}
		if model.Description == "" {
			model.Description = model.DisplayName
		}
		if marketInput != nil && marketOutput != nil {
			model.MarketComparison = &PublicModelMarketComparison{
				TypicalInputPerMillionMicrodollars:  *marketInput,
				TypicalOutputPerMillionMicrodollars: *marketOutput,
				SourceNote:                          marketSourceNote,
			}
		}
		if attributionDisplay != "" {
			model.Attribution = &PublicModelAttribution{
				DisplayText: attributionDisplay,
				NoticeText:  attributionNotice,
				LicenseURL:  attributionLicenseURL,
				AUPURL:      attributionAUPURL,
			}
		}
		model.Capabilities = publicCapabilities(capabilitiesRaw)
		capacity, err := s.publicModelCapacity(ctx, model.ModelID, now.Add(-s.staleAfter()))
		if err != nil {
			return nil, err
		}
		if model.ListingStatus == ListingWaitlist && !capacity.hasSupply() {
			// No node is online with this model, so there is nothing honest
			// to say about nodes, regions, or observed speed. The manifest's
			// expected speed is the only claim, and the public page labels it
			// as expected.
			model.Availability = PublicModelAvailability{State: AvailabilityWaitlist}
			model.Regions = []string{}
			model.ExpectedOutputTokensPerSecond = expectedSpeed
			out = append(out, model)
			continue
		}
		model.Availability = PublicModelAvailability{AvailableNodes: capacity.availableNodes, State: capacity.state()}
		model.Regions = capacity.regions
		speed, err := s.medianOutputTokensPerSecond(ctx, model.ModelID, now.Add(-24*time.Hour))
		if err != nil {
			return nil, err
		}
		model.TypicalOutputTokensPerSecond = speed
		out = append(out, model)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public models: %w", err)
	}
	return out, nil
}

type publicModelCapacity struct {
	availableNodes int
	freshNodes     int
	regions        []string
}

// hasSupply reports whether any node is currently online with this model, in
// any state. It gates the waitlist presentation: real supply always wins.
func (c publicModelCapacity) hasSupply() bool {
	return c.availableNodes > 0 || c.freshNodes > 0
}

func (c publicModelCapacity) state() string {
	if c.availableNodes > 0 {
		return AvailabilityAvailable
	}
	if c.freshNodes > 0 {
		return AvailabilityLimited
	}
	return AvailabilityOffline
}

func (s Store) publicModelCapacity(ctx context.Context, modelID string, freshnessCutoff time.Time) (publicModelCapacity, error) {
	rows, err := s.Pool.Query(ctx, `
WITH latest_model AS (
  SELECT mv.id AS model_version_id, ma.sha256 AS model_sha256, rr.id AS runtime_release_id
  FROM model_versions mv
  JOIN model_artifacts ma ON ma.model_version_id = mv.id AND ma.artifact_type = 'gguf'
  LEFT JOIN runtime_releases rr ON rr.id = mv.runtime_release_id
  WHERE mv.model_id = $1
  ORDER BY mv.created_at DESC
  LIMIT 1
)
SELECT COALESCE(NULLIF(n.region, ''), NULLIF(f.region, ''), '') AS region,
       (
         n.state = 'AVAILABLE'
         AND ns.state = 'connected'
         AND n.quarantined_at IS NULL
         AND hb.schedule_state = 'in_window'
         AND hb.thermal_state = 'normal'
         AND hb.paused = false
         AND hb.draining = false
         AND hb.model_hash = 'sha256:' || lm.model_sha256
         AND EXISTS (
           SELECT 1 FROM runtime_release_artifacts rra
           WHERE rra.runtime_release_id = lm.runtime_release_id
             AND hb.runtime_hash = 'sha256:' || rra.sha256
         )
         AND NOT EXISTS (
           SELECT 1 FROM job_attempts ja
           WHERE ja.node_id = n.id AND ja.status IN ('offered', 'accepted', 'running')
         )
       ) AS eligible
FROM latest_model lm
JOIN nodes n ON n.current_model_id = $1
LEFT JOIN fleets f ON f.id = n.fleet_id
JOIN LATERAL (
  SELECT id, state, COALESCE(last_heartbeat_at, connected_at) AS freshness
  FROM node_sessions
  WHERE node_id = n.id AND state IN ('connected', 'draining')
  ORDER BY connected_at DESC
  LIMIT 1
) ns ON true
JOIN LATERAL (
  SELECT model_hash, runtime_hash, schedule_state, thermal_state, paused, draining
  FROM node_heartbeats
  WHERE node_id = n.id AND session_id = ns.id
  ORDER BY received_at DESC
  LIMIT 1
) hb ON true
WHERE ns.freshness >= $2
  AND hb.model_hash = 'sha256:' || lm.model_sha256
  AND EXISTS (
    SELECT 1 FROM runtime_release_artifacts rra
    WHERE rra.runtime_release_id = lm.runtime_release_id
      AND hb.runtime_hash = 'sha256:' || rra.sha256
  )
`, modelID, freshnessCutoff)
	if err != nil {
		return publicModelCapacity{}, fmt.Errorf("query public model capacity: %w", err)
	}
	defer rows.Close()
	capacity := publicModelCapacity{}
	regions := map[string]bool{}
	for rows.Next() {
		var region string
		var eligible bool
		if err := rows.Scan(&region, &eligible); err != nil {
			return publicModelCapacity{}, fmt.Errorf("scan public model capacity: %w", err)
		}
		capacity.freshNodes++
		if eligible {
			capacity.availableNodes++
			if region != "" {
				regions[region] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return publicModelCapacity{}, fmt.Errorf("iterate public model capacity: %w", err)
	}
	capacity.regions = sortedKeys(regions)
	return capacity, nil
}

func (s Store) medianOutputTokensPerSecond(ctx context.Context, modelID string, since time.Time) (*float64, error) {
	var speed sql.NullFloat64
	if err := s.Pool.QueryRow(ctx, `
SELECT percentile_cont(0.5) WITHIN GROUP (
  ORDER BY jr.completion_tokens::float8 / (GREATEST(jr.coordinator_duration_millis, jr.duration_millis)::float8 / 1000.0)
)
FROM job_results jr
JOIN jobs j ON j.id = jr.job_id
WHERE j.model_id = $1
  AND jr.accepted
  AND jr.created_at >= $2
  AND jr.completion_tokens > 0
  AND GREATEST(jr.coordinator_duration_millis, jr.duration_millis) > 0
`, modelID, since).Scan(&speed); err != nil {
		return nil, fmt.Errorf("query median output tokens/sec: %w", err)
	}
	if !speed.Valid {
		return nil, nil
	}
	value := speed.Float64
	return &value, nil
}

func (s Store) regionsOnline(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT DISTINCT COALESCE(NULLIF(n.region, ''), NULLIF(f.region, '')) AS region
FROM nodes n
LEFT JOIN fleets f ON f.id = n.fleet_id
JOIN LATERAL (
  SELECT state, COALESCE(last_heartbeat_at, connected_at) AS freshness
  FROM node_sessions
  WHERE node_id = n.id
  ORDER BY connected_at DESC
  LIMIT 1
) ns ON true
WHERE ns.state IN ('connected', 'draining')
  AND ns.freshness >= $1
  AND COALESCE(NULLIF(n.region, ''), NULLIF(f.region, '')) IS NOT NULL
ORDER BY region
`, now.Add(-s.staleAfter()))
	if err != nil {
		return nil, fmt.Errorf("query online regions: %w", err)
	}
	defer rows.Close()
	regions := []string{}
	for rows.Next() {
		var region string
		if err := rows.Scan(&region); err != nil {
			return nil, fmt.Errorf("scan online region: %w", err)
		}
		regions = append(regions, region)
	}
	return regions, rows.Err()
}

// regionNodeCounts aggregates currently online nodes per region for the public
// map. It uses the same liveness rule as regionsOnline so the map and the
// region list can never disagree.
func (s Store) regionNodeCounts(ctx context.Context, now time.Time) ([]RegionNodeCount, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT COALESCE(NULLIF(n.region, ''), NULLIF(f.region, '')) AS effective_region, count(*)
FROM nodes n
LEFT JOIN fleets f ON f.id = n.fleet_id
JOIN LATERAL (
  SELECT state, COALESCE(last_heartbeat_at, connected_at) AS freshness
  FROM node_sessions
  WHERE node_id = n.id
  ORDER BY connected_at DESC
  LIMIT 1
) ns ON true
WHERE ns.state IN ('connected', 'draining')
  AND ns.freshness >= $1
  AND COALESCE(NULLIF(n.region, ''), NULLIF(f.region, '')) IS NOT NULL
GROUP BY effective_region
ORDER BY effective_region
`, now.Add(-s.staleAfter()))
	if err != nil {
		return nil, fmt.Errorf("query region node counts: %w", err)
	}
	defer rows.Close()
	counts := []RegionNodeCount{}
	for rows.Next() {
		var count RegionNodeCount
		if err := rows.Scan(&count.Region, &count.NodeCount); err != nil {
			return nil, fmt.Errorf("scan region node count: %w", err)
		}
		counts = append(counts, count)
	}
	return counts, rows.Err()
}

// publicHosts lists machines that have held a session in the last 24 hours,
// with what each has earned. Credit comes from host_credit_holds, which carries
// node_id directly and holds exactly one row per accepted attempt, so summing
// it cannot double count a credit that has moved from pending to available.
// Reversed credit is excluded: it was not earned.
func (s Store) publicHosts(ctx context.Context, now time.Time) ([]PublicHostStatus, error) {
	rows, err := s.Pool.Query(ctx, `
WITH latest_session AS (
  SELECT DISTINCT ON (node_id) node_id, state,
         COALESCE(last_heartbeat_at, connected_at) AS freshness
  FROM node_sessions
  ORDER BY node_id, connected_at DESC
),
credit AS (
  SELECT node_id,
         count(*) FILTER (WHERE created_at >= $1) AS jobs_24h,
         COALESCE(SUM(amount_microdollars) FILTER (WHERE created_at >= $1), 0) AS credited_24h,
         COALESCE(SUM(amount_microdollars), 0) AS credited_total
  FROM host_credit_holds
  WHERE state <> 'reversed'
  GROUP BY node_id
)
SELECT n.id,
       COALESCE(NULLIF(n.region, ''), NULLIF(f.region, ''), '') AS region,
       n.state,
       ls.state,
       ls.freshness >= $2 AS fresh,
       COALESCE(c.jobs_24h, 0),
       COALESCE(c.credited_24h, 0),
       COALESCE(c.credited_total, 0)
FROM nodes n
LEFT JOIN fleets f ON f.id = n.fleet_id
JOIN latest_session ls ON ls.node_id = n.id
LEFT JOIN credit c ON c.node_id = n.id
-- A host appears once it has earned something, or while it is actually
-- connected. A machine that registered, never served, and went away is a
-- test artifact, not a contributor, and should not decorate the ticker.
WHERE ls.freshness >= $1
  AND (COALESCE(c.credited_total, 0) > 0 OR ls.state = 'connected')
ORDER BY COALESCE(c.credited_total, 0) DESC, n.id
`, now.Add(-24*time.Hour), now.Add(-s.staleAfter()))
	if err != nil {
		return nil, fmt.Errorf("query public hosts: %w", err)
	}
	defer rows.Close()
	hosts := []PublicHostStatus{}
	for rows.Next() {
		var nodeID, region, nodeState, sessionState string
		var fresh bool
		var host PublicHostStatus
		if err := rows.Scan(&nodeID, &region, &nodeState, &sessionState, &fresh,
			&host.Jobs24h, &host.CreditedMicrodollars24h, &host.CreditedMicrodollarsTotal); err != nil {
			return nil, fmt.Errorf("scan public host: %w", err)
		}
		host.Handle = HostHandle(nodeID)
		host.Region = region
		host.State = publicHostState(nodeState, sessionState, fresh)
		hosts = append(hosts, host)
	}
	return hosts, rows.Err()
}

func publicHostState(nodeState, sessionState string, fresh bool) string {
	if !fresh || (sessionState != "connected" && sessionState != "draining") {
		return HostStateOffline
	}
	if nodeState == "BUSY" {
		return HostStateServing
	}
	return HostStateIdle
}

func (s Store) ContributionCard(ctx context.Context, nodeID string, now time.Time) (ContributionCard, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ContributionCard{}, fmt.Errorf("node id is required")
	}
	if now.IsZero() {
		now = s.now()
	}
	var exists bool
	if err := s.Pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nodes WHERE id = $1)", nodeID).Scan(&exists); err != nil {
		return ContributionCard{}, fmt.Errorf("lookup card node: %w", err)
	}
	if !exists {
		return ContributionCard{}, fmt.Errorf("node %s not found", nodeID)
	}
	var card ContributionCard
	card.NodeID = nodeID
	card.NodeName = nodeID
	card.GeneratedAt = now
	if err := s.Pool.QueryRow(ctx, `
SELECT count(DISTINCT connected_at::date)
FROM node_sessions
WHERE node_id = $1
`, nodeID).Scan(&card.NightsActive); err != nil {
		return ContributionCard{}, fmt.Errorf("query active nights: %w", err)
	}
	if err := s.Pool.QueryRow(ctx, `
SELECT count(DISTINCT ja.id),
       COALESCE(SUM(jr.prompt_tokens + jr.completion_tokens), 0),
       COALESCE(SUM(h.amount_microdollars), 0)
FROM job_attempts ja
LEFT JOIN job_results jr ON jr.attempt_id = ja.id AND jr.accepted
LEFT JOIN host_credit_holds h ON h.attempt_id = ja.id
WHERE ja.node_id = $1 AND ja.status = 'succeeded'
`, nodeID).Scan(&card.JobsAccepted, &card.TokensServed, &card.CreditEarnedMicrodollars); err != nil {
		return ContributionCard{}, fmt.Errorf("query contribution card totals: %w", err)
	}
	return card, nil
}

func (s Store) CreateFleet(ctx context.Context, orgID, name string, schedule ScheduleDefaults, region string, now time.Time) (Fleet, error) {
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
	region, err := NormalizeRegion(region)
	if err != nil {
		return Fleet{}, err
	}
	fleetID, err := ids.New("fleet")
	if err != nil {
		return Fleet{}, err
	}
	var created Fleet
	err = s.Pool.QueryRow(ctx, `
INSERT INTO fleets (id, organization_id, name, enrollment_status, region, schedule_from, schedule_until, schedule_timezone, schedule_updated_at, created_at, updated_at)
VALUES ($1, $2, $3, 'active', NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), $7, $8, $8, $8)
RETURNING id, organization_id, name, COALESCE(region, ''), COALESCE(schedule_from, ''), COALESCE(schedule_until, ''), schedule_timezone, created_at
`, fleetID, orgID, strings.TrimSpace(name), region, from, until, timezone, now).Scan(&created.ID, &created.OrganizationID, &created.Name, &created.Region, &created.ScheduleFrom, &created.ScheduleUntil, &created.ScheduleTimezone, &created.CreatedAt)
	if err != nil {
		return Fleet{}, fmt.Errorf("create fleet: %w", err)
	}
	return created, nil
}

func (s Store) SetFleetRegion(ctx context.Context, fleetID, region, reason string, now time.Time) error {
	region, err := NormalizeRegion(region)
	if err != nil {
		return err
	}
	if strings.TrimSpace(fleetID) == "" {
		return fmt.Errorf("fleet id is required")
	}
	if now.IsZero() {
		now = s.now()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin fleet region update: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
UPDATE fleets
SET region = NULLIF($2, ''), updated_at = $3
WHERE id = $1
`, fleetID, region, now); err != nil {
		return fmt.Errorf("update fleet region: %w", err)
	}
	if err := recordOperatorActionTx(ctx, tx, ActionFleetSetRegion, "fleet", fleetID, reason, map[string]any{"region": region}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) SetNodeRegion(ctx context.Context, nodeID, region, reason string, now time.Time) error {
	region, err := NormalizeRegion(region)
	if err != nil {
		return err
	}
	if strings.TrimSpace(nodeID) == "" {
		return fmt.Errorf("node id is required")
	}
	if now.IsZero() {
		now = s.now()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin node region update: %w", err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `
UPDATE nodes
SET region = NULLIF($2, ''), updated_at = $3
WHERE id = $1
`, nodeID, region, now); err != nil {
		return fmt.Errorf("update node region: %w", err)
	}
	if err := recordOperatorActionTx(ctx, tx, ActionNodeSetRegion, "node", nodeID, reason, map[string]any{"region": region}, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
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

// ValidateExpectedVolume accepts an empty band or one of the published
// monthly output volume options.
func ValidateExpectedVolume(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || expectedVolumeBands[value] {
		return nil
	}
	return fmt.Errorf("expected_volume must be one of lt_1m, 1m_10m, 10m_100m, gt_100m")
}

// SubmitWaitlistApplication upserts an access application on
// (email, model_id). Applications are resubmittable: a returning applicant with
// a better use case must overwrite their previous answers rather than be
// dropped, and the same person may apply for several models. Reports whether
// the row was newly inserted.
func (s Store) SubmitWaitlistApplication(ctx context.Context, application WaitlistApplication, now time.Time) (WaitlistSignup, bool, error) {
	email := NormalizeEmail(application.Email)
	if email == "" {
		return WaitlistSignup{}, false, fmt.Errorf("email is required")
	}
	if err := ValidateExpectedVolume(application.ExpectedVolume); err != nil {
		return WaitlistSignup{}, false, err
	}
	if now.IsZero() {
		now = s.now()
	}
	name := strings.TrimSpace(application.Name)
	useCase := strings.TrimSpace(application.UseCase)
	expectedVolume := strings.TrimSpace(application.ExpectedVolume)
	modelID := strings.TrimSpace(application.ModelID)
	source := strings.TrimSpace(application.Source)
	if source == "" {
		source = "public_catalog"
	}
	signupID, err := ids.New("wait")
	if err != nil {
		return WaitlistSignup{}, false, err
	}
	var signup WaitlistSignup
	var inserted bool
	// created_at keeps the first application; last_applied_at moves. xmax is 0
	// only on a genuine insert, which is how the upsert reports which happened.
	err = s.Pool.QueryRow(ctx, `
INSERT INTO waitlist_signups (id, email, name, use_case, expected_volume, data_ack, model_id, source, created_at, last_applied_at)
VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, NULLIF($7, ''), $8, $9, $9)
ON CONFLICT (email, model_id) DO UPDATE
SET name = EXCLUDED.name,
    use_case = EXCLUDED.use_case,
    expected_volume = EXCLUDED.expected_volume,
    data_ack = EXCLUDED.data_ack,
    source = EXCLUDED.source,
    last_applied_at = EXCLUDED.last_applied_at
RETURNING id, email, COALESCE(name, ''), COALESCE(use_case, ''), COALESCE(expected_volume, ''),
          data_ack, COALESCE(model_id, ''), source, created_at, last_applied_at, (xmax = 0)
`, signupID, email, name, useCase, expectedVolume, application.DataAck, modelID, source, now).Scan(
		&signup.ID, &signup.Email, &signup.Name, &signup.UseCase, &signup.ExpectedVolume,
		&signup.DataAck, &signup.ModelID, &signup.Source, &signup.CreatedAt, &signup.LastAppliedAt, &inserted)
	if err != nil {
		return WaitlistSignup{}, false, fmt.Errorf("submit waitlist application: %w", err)
	}
	return signup, inserted, nil
}

func (s Store) ListWaitlist(ctx context.Context) ([]WaitlistSignup, error) {
	rows, err := s.Pool.Query(ctx, `
SELECT id, email, COALESCE(name, ''), COALESCE(use_case, ''), COALESCE(expected_volume, ''), data_ack, COALESCE(model_id, ''), source, created_at, last_applied_at
FROM waitlist_signups
ORDER BY last_applied_at DESC, email, model_id
LIMIT $1
`, defaultRecentListSize)
	if err != nil {
		return nil, fmt.Errorf("query waitlist signups: %w", err)
	}
	defer rows.Close()
	var out []WaitlistSignup
	for rows.Next() {
		var signup WaitlistSignup
		if err := rows.Scan(
			&signup.ID, &signup.Email, &signup.Name, &signup.UseCase, &signup.ExpectedVolume,
			&signup.DataAck, &signup.ModelID, &signup.Source, &signup.CreatedAt, &signup.LastAppliedAt,
		); err != nil {
			return nil, fmt.Errorf("scan waitlist signup: %w", err)
		}
		out = append(out, signup)
	}
	return out, rows.Err()
}

func (s Store) WaitlistCSV(ctx context.Context) ([]byte, error) {
	signups, err := s.ListWaitlist(ctx)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"id", "email", "name", "use_case", "expected_volume", "data_ack", "model_id", "source", "created_at", "last_applied_at"}); err != nil {
		return nil, err
	}
	for _, signup := range signups {
		if err := writer.Write([]string{
			signup.ID,
			signup.Email,
			signup.Name,
			signup.UseCase,
			signup.ExpectedVolume,
			strconv.FormatBool(signup.DataAck),
			signup.ModelID,
			signup.Source,
			signup.CreatedAt.Format(time.RFC3339),
			signup.LastAppliedAt.Format(time.RFC3339),
		}); err != nil {
			return nil, err
		}
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

func NormalizeRegion(value string) (string, error) {
	region := strings.ToLower(strings.TrimSpace(value))
	if region == "" {
		return "", nil
	}
	if !regionPattern.MatchString(region) {
		return "", fmt.Errorf("region must use lowercase letters, digits, and hyphen separators")
	}
	return region, nil
}

func EffectiveRegion(nodeRegion, fleetRegion string) string {
	nodeRegion = strings.TrimSpace(nodeRegion)
	if nodeRegion != "" {
		return nodeRegion
	}
	return strings.TrimSpace(fleetRegion)
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
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

func publicCapabilities(raw []byte) []string {
	var values map[string]bool
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	capabilities := make([]string, 0, len(values))
	for capability, enabled := range values {
		if enabled {
			capabilities = append(capabilities, capability)
		}
	}
	sort.Strings(capabilities)
	return capabilities
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
