package jobs

import (
	"testing"

	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
)

func TestValidateCompletionForAcceptanceRejectsBadShape(t *testing.T) {
	req := protocol.ChatRequest{
		Messages:  []protocol.ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 8,
	}
	payload := protocol.JobCompletedPayload{
		Message: &protocol.ChatMessage{Role: "assistant", Content: "ok"},
		Usage: protocol.Usage{
			PromptTokens:     1,
			CompletionTokens: 99,
			TotalTokens:      100,
		},
	}
	issues, err := ValidateCompletionForAcceptance(req, payload)
	if err == nil {
		t.Fatal("ValidateCompletionForAcceptance accepted an oversized completion")
	}
	if meteringStatus(issues) != "rejected" {
		t.Fatalf("metering status = %s, want rejected; issues=%#v", meteringStatus(issues), issues)
	}
}

func TestPlausibilityIssuesFlagsOutlierButAccepts(t *testing.T) {
	req := protocol.ChatRequest{
		Messages:  []protocol.ChatMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 128,
	}
	payload := protocol.JobCompletedPayload{
		Message: &protocol.ChatMessage{Role: "assistant", Content: "short answer"},
		Usage: protocol.Usage{
			PromptTokens:     200,
			CompletionTokens: 20,
			TotalTokens:      220,
		},
		DurationMillis: 1000,
	}
	issues, err := ValidateCompletionForAcceptance(req, payload)
	if err != nil {
		t.Fatalf("ValidateCompletionForAcceptance rejected flagged-only payload: %v", err)
	}
	if meteringStatus(issues) != "flagged" {
		t.Fatalf("metering status = %s, want flagged; issues=%#v", meteringStatus(issues), issues)
	}
}

func TestSampleByIDBoundaries(t *testing.T) {
	if sampleByID("job_demo", 0) {
		t.Fatal("sampleByID sampled at zero rate")
	}
	if !sampleByID("job_demo", 1) {
		t.Fatal("sampleByID skipped at full rate")
	}
	if sampleByID("job_demo", -1) {
		t.Fatal("sampleByID sampled at negative rate")
	}
}
