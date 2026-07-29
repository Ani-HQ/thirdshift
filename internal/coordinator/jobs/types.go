package jobs

import (
	"encoding/json"
	"time"

	"github.com/anianroid/thirdshift/internal/shared/protocol"
)

const (
	CodeInvalidRequest   = "invalid_request"
	CodeUnauthorized     = "unauthorized"
	CodeQuotaExceeded    = "quota_exceeded"
	CodeModelNotFound    = "model_not_found"
	CodeModelUnavailable = "model_unavailable"
	CodeNoCapacity       = "no_capacity"
	CodeJobTimeout       = "job_timeout"
	CodeJobFailed        = "job_failed"
	CodeContentRejected  = "content_rejected"
	CodeInternalError    = "internal_error"
)

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	RequestID string `json:"request_id"`
	Status    int    `json:"-"`
}

func (e APIError) Error() string {
	return e.Code + ": " + e.Message
}

type APIKeyPrincipal struct {
	ID             string
	OrganizationID string
	AllowedModels  map[string]bool
}

type ModelInfo struct {
	ID           string       `json:"id"`
	DisplayName  string       `json:"display_name"`
	Capabilities []string     `json:"capabilities"`
	Pricing      ModelPricing `json:"pricing"`
	DataClass    string       `json:"data_class"`
	Availability Availability `json:"availability"`
	Version      string       `json:"version"`
	Limits       ModelLimits  `json:"limits"`
	ModelHash    string       `json:"-"`
	RuntimeHash  string       `json:"-"`
}

type ModelPricing struct {
	CustomerInputPerMillionMicrodollars            int64 `json:"customer_input_per_million_microdollars"`
	CustomerOutputPerMillionMicrodollars           int64 `json:"customer_output_per_million_microdollars"`
	HostCreditPerMillionAcceptedOutputMicrodollars int64 `json:"host_credit_per_million_accepted_output_microdollars"`
}

type Availability struct {
	AvailableNodes int `json:"available_nodes"`
}

type ModelLimits struct {
	MaxInputTokens  int `json:"max_input_tokens"`
	MaxOutputTokens int `json:"max_output_tokens"`
	MaxRequestBytes int `json:"max_request_bytes"`
}

type ChatCompletionRequest struct {
	Model       string                 `json:"model"`
	Messages    []protocol.ChatMessage `json:"messages"`
	Temperature *float64               `json:"temperature,omitempty"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
	Stream      bool                   `json:"stream"`
	Tools       json.RawMessage        `json:"tools,omitempty"`
	ToolChoice  json.RawMessage        `json:"tool_choice,omitempty"`
}

type AsyncJobRequest struct {
	Model           string                `json:"model"`
	Input           ChatCompletionRequest `json:"input"`
	Priority        string                `json:"priority,omitempty"`
	DeadlineSeconds int                   `json:"deadline_seconds,omitempty"`
	Metadata        map[string]string     `json:"metadata,omitempty"`
}

type OpenAIResponse struct {
	ID         string                 `json:"id"`
	Object     string                 `json:"object"`
	Created    int64                  `json:"created"`
	Model      string                 `json:"model"`
	Choices    []OpenAIChoice         `json:"choices"`
	Usage      protocol.Usage         `json:"usage"`
	Thirdshift ThirdshiftResponseMeta `json:"thirdshift"`
}

type OpenAIChoice struct {
	Index        int                  `json:"index"`
	Message      protocol.ChatMessage `json:"message"`
	FinishReason string               `json:"finish_reason"`
}

type ThirdshiftResponseMeta struct {
	JobID        string `json:"job_id"`
	Attempts     int    `json:"attempts"`
	DataClass    string `json:"data_class"`
	ServedRegion string `json:"served_region"`
}

type JobStatus struct {
	ID        string          `json:"id"`
	State     string          `json:"state"`
	Model     string          `json:"model"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Result    *OpenAIResponse `json:"result,omitempty"`
	Error     *APIError       `json:"error,omitempty"`
}

type Candidate struct {
	NodeID             string
	SessionID          string
	ModelHash          string
	RuntimeHash        string
	RollingSuccessRate float64
	TokensPerSecond    float64
	RecentFailureBonus float64
	ThermalHeadroom    float64
	HostFairness       float64
	RegionalPreference float64
}

type SchedulerWeights struct {
	WarmModelBonus            float64
	RollingSuccessRate        float64
	NormalizedTokensPerSecond float64
	LowRecentFailureBonus     float64
	ThermalHeadroom           float64
	HostFairness              float64
	RegionalPreference        float64
}

func DefaultSchedulerWeights() SchedulerWeights {
	return SchedulerWeights{
		WarmModelBonus:            40,
		RollingSuccessRate:        20,
		NormalizedTokensPerSecond: 15,
		LowRecentFailureBonus:     10,
		ThermalHeadroom:           5,
		HostFairness:              5,
		RegionalPreference:        5,
	}
}

type ScheduledAttempt struct {
	JobID          string
	AttemptID      string
	NodeID         string
	SessionID      string
	LeaseExpiresAt time.Time
	DeadlineAt     time.Time
	ModelHash      string
	RuntimeHash    string
}

type ExpiredAttempt struct {
	JobID     string
	AttemptID string
	NodeID    string
}
