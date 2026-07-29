package jobs

import (
	"testing"
	"time"
)

func TestSchedulerFiltersAndScoringDeterministic(t *testing.T) {
	scheduler := Scheduler{Weights: SchedulerWeights{
		WarmModelBonus:            40,
		RollingSuccessRate:        20,
		NormalizedTokensPerSecond: 15,
		LowRecentFailureBonus:     10,
		ThermalHeadroom:           5,
		HostFairness:              5,
		RegionalPreference:        5,
	}}
	got, ok := scheduler.Choose([]Candidate{
		{NodeID: "node_b", RollingSuccessRate: 0.8, TokensPerSecond: 0.5, RecentFailureBonus: 1, ThermalHeadroom: 1, HostFairness: 1, RegionalPreference: 1},
		{NodeID: "node_a", RollingSuccessRate: 0.9, TokensPerSecond: 1, RecentFailureBonus: 1, ThermalHeadroom: 1, HostFairness: 1, RegionalPreference: 1},
	})
	if !ok {
		t.Fatal("no candidate chosen")
	}
	if got.NodeID != "node_a" {
		t.Fatalf("chosen node = %s, want node_a", got.NodeID)
	}
}

func TestSchedulerTieBreaksByNodeID(t *testing.T) {
	got, ok := (Scheduler{}).Choose([]Candidate{{NodeID: "node_b"}, {NodeID: "node_a"}})
	if !ok || got.NodeID != "node_a" {
		t.Fatalf("tie break got %#v ok=%v", got, ok)
	}
}

func TestLeaseClockExpiry(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	clock := LeaseClock{Now: func() time.Time { return now }}
	expires := clock.LeaseExpiresAt(10 * time.Second)
	if !expires.Equal(now.Add(10 * time.Second)) {
		t.Fatalf("expires = %s", expires)
	}
	now = now.Add(10 * time.Second)
	if !clock.Expired(expires) {
		t.Fatal("lease should be expired at exact expiry")
	}
}
