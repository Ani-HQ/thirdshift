package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

const Version = "1.0"

type MessageType string

const (
	TypeNodeHello              MessageType = "node.hello"
	TypeNodeHeartbeat          MessageType = "node.heartbeat"
	TypeNodeStateChanged       MessageType = "node.state_changed"
	TypeModelDownloadProgress  MessageType = "model.download_progress"
	TypeModelReady             MessageType = "model.ready"
	TypeJobAccepted            MessageType = "job.accepted"
	TypeJobRejected            MessageType = "job.rejected"
	TypeJobStarted             MessageType = "job.started"
	TypeJobCompleted           MessageType = "job.completed"
	TypeJobFailed              MessageType = "job.failed"
	TypeNodeSafetyEvent        MessageType = "node.safety_event"
	TypeNodeLogEvent           MessageType = "node.log_event"
	TypeSessionAccepted        MessageType = "session.accepted"
	TypeNodeConfigUpdated      MessageType = "node.config_updated"
	TypeModelAssign            MessageType = "model.assign"
	TypeModelUnload            MessageType = "model.unload"
	TypeJobOffer               MessageType = "job.offer"
	TypeJobCancel              MessageType = "job.cancel"
	TypeNodeDrain              MessageType = "node.drain"
	TypeRuntimeUpdateAvailable MessageType = "runtime.update_available"
)

var RequiredMessageTypes = []MessageType{
	TypeNodeHello,
	TypeNodeHeartbeat,
	TypeNodeStateChanged,
	TypeModelDownloadProgress,
	TypeModelReady,
	TypeJobAccepted,
	TypeJobRejected,
	TypeJobStarted,
	TypeJobCompleted,
	TypeJobFailed,
	TypeNodeSafetyEvent,
	TypeNodeLogEvent,
	TypeSessionAccepted,
	TypeNodeConfigUpdated,
	TypeModelAssign,
	TypeModelUnload,
	TypeJobOffer,
	TypeJobCancel,
	TypeNodeDrain,
	TypeRuntimeUpdateAvailable,
}

type Envelope struct {
	ProtocolVersion string          `json:"protocol_version"`
	MessageID       string          `json:"message_id"`
	Type            MessageType     `json:"type"`
	SentAt          time.Time       `json:"sent_at"`
	Payload         json.RawMessage `json:"payload"`
}

func NewEnvelope(messageID string, typ MessageType, sentAt time.Time, payload any) (Envelope, error) {
	if !IsKnownMessageType(typ) {
		return Envelope{}, fmt.Errorf("unknown message type %q", typ)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal payload: %w", err)
	}
	return Envelope{
		ProtocolVersion: Version,
		MessageID:       messageID,
		Type:            typ,
		SentAt:          sentAt.UTC(),
		Payload:         body,
	}, nil
}

func Marshal(envelope Envelope) ([]byte, error) {
	if envelope.ProtocolVersion != Version {
		return nil, fmt.Errorf("unsupported protocol version %q", envelope.ProtocolVersion)
	}
	if !IsKnownMessageType(envelope.Type) {
		return nil, fmt.Errorf("unknown message type %q", envelope.Type)
	}
	if !json.Valid(envelope.Payload) {
		return nil, fmt.Errorf("payload is not valid JSON")
	}
	return json.Marshal(envelope)
}

func Unmarshal(data []byte) (Envelope, error) {
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Envelope{}, err
	}
	if envelope.ProtocolVersion != Version {
		return Envelope{}, fmt.Errorf("unsupported protocol version %q", envelope.ProtocolVersion)
	}
	if !IsKnownMessageType(envelope.Type) {
		return Envelope{}, fmt.Errorf("unknown message type %q", envelope.Type)
	}
	if !json.Valid(envelope.Payload) {
		return Envelope{}, fmt.Errorf("payload is not valid JSON")
	}
	return envelope, nil
}

func IsKnownMessageType(typ MessageType) bool {
	for _, known := range RequiredMessageTypes {
		if typ == known {
			return true
		}
	}
	return false
}

type NodeHelloPayload struct {
	NodeID                    string   `json:"node_id"`
	AgentVersion              string   `json:"agent_version"`
	SupportedProtocolVersions []string `json:"supported_protocol_versions"`
	Capabilities              []string `json:"capabilities"`
	Hostname                  string   `json:"hostname,omitempty"`
}

type GPUStatus struct {
	Name               string `json:"name"`
	VRAMTotalMB        int64  `json:"vram_total_mb"`
	VRAMFreeMB         int64  `json:"vram_free_mb"`
	TemperatureC       int    `json:"temperature_c"`
	PowerW             int    `json:"power_w"`
	PowerLimitW        int    `json:"power_limit_w"`
	UtilizationPercent int    `json:"utilization_percent"`
}

type NodeHeartbeatPayload struct {
	NodeID        string    `json:"node_id"`
	Sequence      int64     `json:"sequence"`
	State         string    `json:"state"`
	ModelID       string    `json:"model_id,omitempty"`
	RuntimeHash   string    `json:"runtime_hash,omitempty"`
	ModelHash     string    `json:"model_hash,omitempty"`
	GPU           GPUStatus `json:"gpu"`
	ActiveJobID   *string   `json:"active_job_id"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	Timestamp     time.Time `json:"timestamp"`
}

type NodeStateChangedPayload struct {
	NodeID        string    `json:"node_id"`
	PreviousState string    `json:"previous_state"`
	State         string    `json:"state"`
	Reason        string    `json:"reason,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

type ModelDownloadProgressPayload struct {
	NodeID          string    `json:"node_id"`
	ModelID         string    `json:"model_id"`
	ArtifactID      string    `json:"artifact_id"`
	BytesDownloaded int64     `json:"bytes_downloaded"`
	BytesTotal      int64     `json:"bytes_total"`
	Percent         float64   `json:"percent"`
	Timestamp       time.Time `json:"timestamp"`
}

type ModelReadyPayload struct {
	NodeID      string    `json:"node_id"`
	ModelID     string    `json:"model_id"`
	ModelHash   string    `json:"model_hash"`
	RuntimeHash string    `json:"runtime_hash"`
	LoadedAt    time.Time `json:"loaded_at"`
}

type JobAcceptedPayload struct {
	JobID      string    `json:"job_id"`
	AttemptID  string    `json:"attempt_id"`
	AcceptedAt time.Time `json:"accepted_at"`
}

type JobRejectedPayload struct {
	JobID      string    `json:"job_id"`
	AttemptID  string    `json:"attempt_id"`
	ReasonCode string    `json:"reason_code"`
	Message    string    `json:"message"`
	Retryable  bool      `json:"retryable"`
	RejectedAt time.Time `json:"rejected_at"`
}

type JobStartedPayload struct {
	JobID     string    `json:"job_id"`
	AttemptID string    `json:"attempt_id"`
	StartedAt time.Time `json:"started_at"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type JobCompletedPayload struct {
	JobID          string    `json:"job_id"`
	AttemptID      string    `json:"attempt_id"`
	ModelID        string    `json:"model_id"`
	RuntimeHash    string    `json:"runtime_hash"`
	ModelHash      string    `json:"model_hash"`
	Usage          Usage     `json:"usage"`
	DurationMillis int64     `json:"duration_millis"`
	FinishReason   string    `json:"finish_reason"`
	CompletedAt    time.Time `json:"completed_at"`
}

type JobFailedPayload struct {
	JobID     string    `json:"job_id"`
	AttemptID string    `json:"attempt_id"`
	ErrorCode string    `json:"error_code"`
	Message   string    `json:"message"`
	Retryable bool      `json:"retryable"`
	FailedAt  time.Time `json:"failed_at"`
}

type NodeSafetyEventPayload struct {
	NodeID       string    `json:"node_id"`
	EventCode    string    `json:"event_code"`
	Severity     string    `json:"severity"`
	Message      string    `json:"message"`
	TemperatureC *int      `json:"temperature_c,omitempty"`
	PowerW       *int      `json:"power_w,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
}

type NodeLogEventPayload struct {
	NodeID   string            `json:"node_id"`
	Level    string            `json:"level"`
	Message  string            `json:"message"`
	Fields   map[string]string `json:"fields,omitempty"`
	LoggedAt time.Time         `json:"logged_at"`
}

type SessionAcceptedPayload struct {
	NodeID                   string    `json:"node_id"`
	SessionID                string    `json:"session_id"`
	HeartbeatIntervalSeconds int       `json:"heartbeat_interval_seconds"`
	AcceptedAt               time.Time `json:"accepted_at"`
}

type NodeConfigUpdatedPayload struct {
	ConfigVersion            string    `json:"config_version"`
	HeartbeatIntervalSeconds int       `json:"heartbeat_interval_seconds"`
	MaxJobSeconds            int       `json:"max_job_seconds"`
	UpdatedAt                time.Time `json:"updated_at"`
}

type ModelAssignPayload struct {
	AssignmentID string    `json:"assignment_id"`
	ModelID      string    `json:"model_id"`
	VersionID    string    `json:"version_id"`
	ManifestURL  string    `json:"manifest_url"`
	ManifestHash string    `json:"manifest_hash"`
	AssignedAt   time.Time `json:"assigned_at"`
}

type ModelUnloadPayload struct {
	ModelID    string    `json:"model_id"`
	Reason     string    `json:"reason"`
	DeadlineAt time.Time `json:"deadline_at"`
}

type ChatRequest struct {
	Messages    []ChatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens"`
	Seed        *int64        `json:"seed,omitempty"`
	Stream      bool          `json:"stream"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type JobVerification struct {
	Kind        string  `json:"kind"`
	ChallengeID *string `json:"challenge_id"`
}

type JobOfferPayload struct {
	JobID          string          `json:"job_id"`
	AttemptID      string          `json:"attempt_id"`
	LeaseExpiresAt time.Time       `json:"lease_expires_at"`
	DeadlineAt     time.Time       `json:"deadline_at"`
	ModelID        string          `json:"model_id"`
	Request        ChatRequest     `json:"request"`
	Verification   JobVerification `json:"verification"`
}

type JobCancelPayload struct {
	JobID       string    `json:"job_id"`
	AttemptID   string    `json:"attempt_id,omitempty"`
	Reason      string    `json:"reason"`
	CancelledAt time.Time `json:"cancelled_at"`
}

type NodeDrainPayload struct {
	Reason  string     `json:"reason"`
	DrainBy *time.Time `json:"drain_by,omitempty"`
}

type RuntimeUpdateAvailablePayload struct {
	RuntimeID       string    `json:"runtime_id"`
	Version         string    `json:"version"`
	ManifestURL     string    `json:"manifest_url"`
	BinarySHA256    string    `json:"binary_sha256"`
	Required        bool      `json:"required"`
	ReleaseNotesURL string    `json:"release_notes_url,omitempty"`
	AvailableAt     time.Time `json:"available_at"`
}
