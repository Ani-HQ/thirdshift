package operator

import (
	"slices"
	"testing"
	"time"
)

func TestBuildAlertsThresholds(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	alerts := BuildAlerts(AlertCounts{
		Disconnects:      3,
		JobsTotal:        10,
		JobsFailed:       4,
		HashMismatches:   1,
		RuntimeCrashes:   3,
		OverTemps:        1,
		LedgerImbalances: 1,
		AuthAnomalies:    5,
		NoCapacityModels: []string{"thirdshift-tiny-chat-v1"},
	}, DefaultAlertConfig(), now)
	var codes []string
	for _, alert := range alerts {
		codes = append(codes, alert.Code)
	}
	for _, want := range []string{"node_disconnect_spike", "job_failure_rate", "hash_mismatch", "runtime_crash_loop", "gpu_over_temp", "ledger_imbalance", "auth_anomaly", "no_capacity"} {
		if !slices.Contains(codes, want) {
			t.Fatalf("alert codes missing %q: %#v", want, codes)
		}
	}
}

func TestEffectiveSchedulePrecedence(t *testing.T) {
	local := ScheduleDefaults{From: "22:00", Until: "07:00", Timezone: "local"}
	fleet := ScheduleDefaults{From: "23:00", Until: "08:00", Timezone: "local"}

	got := EffectiveSchedule(local, true, fleet)
	if got.From != "22:00" || got.Until != "07:00" || got.Source != "node" {
		t.Fatalf("local override schedule = %#v", got)
	}
	got = EffectiveSchedule(ScheduleDefaults{}, false, fleet)
	if got.From != "23:00" || got.Until != "08:00" || got.Source != "fleet" {
		t.Fatalf("fleet default schedule = %#v", got)
	}
	got = EffectiveSchedule(ScheduleDefaults{}, false, ScheduleDefaults{})
	if got.From != "00:00" || got.Until != "00:00" || got.Source != "node" {
		t.Fatalf("fallback schedule = %#v", got)
	}
}
