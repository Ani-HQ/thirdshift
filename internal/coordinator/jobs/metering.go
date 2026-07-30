package jobs

import (
	"errors"
	"strings"

	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
)

type MeteringIssue struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

func ValidateCompletionForAcceptance(request protocol.ChatRequest, payload protocol.JobCompletedPayload) ([]MeteringIssue, error) {
	issues := PlausibilityIssues(request, payload)
	for _, issue := range issues {
		if issue.Severity == "reject" {
			return issues, errors.New(issue.Message)
		}
	}
	return issues, nil
}

func PlausibilityIssues(request protocol.ChatRequest, payload protocol.JobCompletedPayload) []MeteringIssue {
	var issues []MeteringIssue
	if payload.Message == nil {
		return append(issues, MeteringIssue{Code: "missing_message", Message: "completion message is missing", Severity: "reject"})
	}
	if payload.Message.Role != "assistant" {
		issues = append(issues, MeteringIssue{Code: "bad_role", Message: "completion role must be assistant", Severity: "reject"})
	}
	if strings.TrimSpace(payload.Message.Content) == "" {
		issues = append(issues, MeteringIssue{Code: "empty_completion", Message: "completion content is empty", Severity: "reject"})
	}
	if payload.Usage.PromptTokens < 0 || payload.Usage.CompletionTokens < 0 || payload.Usage.TotalTokens < 0 {
		issues = append(issues, MeteringIssue{Code: "negative_usage", Message: "token counts cannot be negative", Severity: "reject"})
	}
	if payload.Usage.TotalTokens != payload.Usage.PromptTokens+payload.Usage.CompletionTokens {
		issues = append(issues, MeteringIssue{Code: "usage_total_mismatch", Message: "total_tokens must equal prompt_tokens plus completion_tokens", Severity: "reject"})
	}
	estimatedPrompt := 0
	for _, message := range request.Messages {
		estimatedPrompt += estimateTokens(message.Content)
	}
	if estimatedPrompt > 0 && payload.Usage.PromptTokens > estimatedPrompt*8+128 {
		issues = append(issues, MeteringIssue{Code: "prompt_tokens_outlier", Message: "prompt token count is implausibly high", Severity: "flag"})
	}
	outputLimit := request.MaxTokens
	if outputLimit <= 0 {
		outputLimit = 1024
	}
	if payload.Usage.CompletionTokens > outputLimit*2+16 {
		issues = append(issues, MeteringIssue{Code: "completion_tokens_outlier", Message: "completion token count exceeds accepted output bounds", Severity: "reject"})
	}
	estimatedCompletion := estimateTokens(payload.Message.Content)
	if estimatedCompletion > 0 && payload.Usage.CompletionTokens > estimatedCompletion*8+128 {
		issues = append(issues, MeteringIssue{Code: "completion_length_outlier", Message: "completion token count is implausibly high for response length", Severity: "flag"})
	}
	if payload.DurationMillis > 0 && payload.Usage.TotalTokens > 0 {
		tokensPerSecond := float64(payload.Usage.TotalTokens) / (float64(payload.DurationMillis) / 1000)
		if tokensPerSecond > 5000 {
			issues = append(issues, MeteringIssue{Code: "duration_outlier", Message: "reported token throughput is implausibly high", Severity: "flag"})
		}
	}
	return issues
}

func meteringStatus(issues []MeteringIssue) string {
	status := "accepted"
	for _, issue := range issues {
		if issue.Severity == "reject" {
			return "rejected"
		}
		if issue.Severity == "flag" {
			status = "flagged"
		}
	}
	return status
}
