package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadSaveConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		DataDir:           dir,
		CoordinatorURL:    "http://127.0.0.1:8080",
		ModelID:           "thirdshift-tiny-chat-v1",
		HeartbeatInterval: 5 * time.Second,
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.CoordinatorURL != cfg.CoordinatorURL {
		t.Fatalf("coordinator_url = %q", got.CoordinatorURL)
	}
	if got.HeartbeatInterval != 5*time.Second {
		t.Fatalf("heartbeat interval = %s", got.HeartbeatInterval)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil {
		t.Fatalf("config file missing: %v", err)
	}
}
