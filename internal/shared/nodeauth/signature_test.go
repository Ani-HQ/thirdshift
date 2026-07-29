package nodeauth

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestRefreshSignatureRoundTrip(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	req, err := SignRefresh(privateKey, "node_01J0M000000000000000000000", now)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := VerifyRefresh(publicKey, req, now, time.Minute); err != nil {
		t.Fatalf("verify: %v", err)
	}
	req.NodeID = "node_01J0M000000000000000000001"
	if err := VerifyRefresh(publicKey, req, now, time.Minute); err == nil {
		t.Fatal("tampered request verified, want error")
	}
}

func TestRefreshSignatureRejectsSkew(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	req, err := SignRefresh(privateKey, "node_01J0M000000000000000000000", now.Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := VerifyRefresh(publicKey, req, now, time.Minute); err == nil {
		t.Fatal("stale refresh request verified, want error")
	}
}
