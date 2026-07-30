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
		ScheduleFrom:      "23:00",
		ScheduleUntil:     "08:00",
		MaxTempC:          77,
		HardTempC:         90,
		ThermalHysteresis: 4,
		PauseIdleTimeout:  30 * time.Second,
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
	if got.ScheduleFrom != "23:00" || got.ScheduleUntil != "08:00" {
		t.Fatalf("schedule = %s-%s", got.ScheduleFrom, got.ScheduleUntil)
	}
	if got.MaxTempC != 77 || got.HardTempC != 90 || got.ThermalHysteresis != 4 {
		t.Fatalf("thermal config = %#v", got)
	}
	if got.PauseIdleTimeout != 30*time.Second {
		t.Fatalf("pause idle timeout = %s", got.PauseIdleTimeout)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); err != nil {
		t.Fatalf("config file missing: %v", err)
	}
	if !HasScheduleOverride(dir) {
		t.Fatal("saved config should be treated as a local schedule override")
	}
}

func TestHasScheduleOverrideFalseWithoutConfig(t *testing.T) {
	if HasScheduleOverride(t.TempDir()) {
		t.Fatal("empty data dir should not have a schedule override")
	}
}
