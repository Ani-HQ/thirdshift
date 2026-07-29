package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTokenIssueVerify(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	signer := TokenSigner{
		Secret: []byte("test-secret"),
		Now:    func() time.Time { return now },
		TTL:    time.Minute,
	}
	token, expiry, err := signer.Issue("node_01J0M000000000000000000000")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !expiry.Equal(now.Add(time.Minute)) {
		t.Fatalf("expiry = %s, want %s", expiry, now.Add(time.Minute))
	}
	claims, err := signer.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.NodeID != "node_01J0M000000000000000000000" {
		t.Fatalf("node_id = %q", claims.NodeID)
	}
}

func TestTokenRejectsTamperingAndExpiry(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	signer := TokenSigner{
		Secret: []byte("test-secret"),
		Now:    func() time.Time { return now },
		TTL:    time.Second,
	}
	token, _, err := signer.Issue("node_01J0M000000000000000000000")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := signer.Verify(strings.Replace(token, ".", "x.", 1)); err == nil {
		t.Fatal("tampered token verified, want error")
	}
	signer.Now = func() time.Time { return now.Add(2 * time.Second) }
	if _, err := signer.Verify(token); err == nil {
		t.Fatal("expired token verified, want error")
	}
}
