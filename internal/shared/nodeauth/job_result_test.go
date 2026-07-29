package nodeauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/anianroid/thirdshift/internal/shared/protocol"
)

func TestJobCompletedSignatureRoundTrip(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	payload := protocol.JobCompletedPayload{
		JobID:       "job_01J0M000000000000000000000",
		AttemptID:   "att_01J0M000000000000000000000",
		ModelID:     "thirdshift-tiny-chat-v1",
		RuntimeHash: "sha256:runtime",
		ModelHash:   "sha256:model",
		Message:     &protocol.ChatMessage{Role: "assistant", Content: "ok"},
		Usage:       protocol.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
		CompletedAt: now,
	}
	signature, err := SignJobCompleted(privateKey, "node-key", payload, now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	payload.Signature = &signature
	if err := VerifyJobCompleted(publicKey, payload); err != nil {
		t.Fatalf("verify: %v", err)
	}
	payload.ModelHash = "sha256:tampered"
	if err := VerifyJobCompleted(publicKey, payload); err == nil {
		t.Fatal("tampered payload verified, want error")
	}
}
