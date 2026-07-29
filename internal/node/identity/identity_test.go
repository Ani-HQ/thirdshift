package identity

import (
	"os"
	"runtime"
	"testing"
	"time"
)

func TestLoadOrCreateKeyWritesRestrictedFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	_, publicKey, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatalf("load or create key: %v", err)
	}
	if publicKey == "" {
		t.Fatal("public key is empty")
	}
	stat, err := os.Stat(KeyPath(dir))
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if got := stat.Mode().Perm(); got != 0o600 {
		t.Fatalf("key file permissions = %o, want 0600", got)
	}
}

func TestCredentialsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Credentials{
		NodeID:                  "node_01J0M000000000000000000000",
		CoordinatorURL:          "http://127.0.0.1:8080",
		PublicKey:               "public",
		AccessToken:             "access",
		AccessTokenExpiresAt:    time.Date(2026, 7, 29, 9, 0, 0, 0, time.UTC),
		HardwareFingerprintHash: "sha256:hardware",
	}
	if err := SaveCredentials(dir, want); err != nil {
		t.Fatalf("save credentials: %v", err)
	}
	got, err := LoadCredentials(dir)
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}
	if got.NodeID != want.NodeID || got.AccessToken != want.AccessToken {
		t.Fatalf("credentials mismatch got %#v want %#v", got, want)
	}
}
