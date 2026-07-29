package jobs

import (
	"sort"
	"time"
)

type Scheduler struct {
	Weights SchedulerWeights
}

func (s Scheduler) Choose(candidates []Candidate) (Candidate, bool) {
	if len(candidates) == 0 {
		return Candidate{}, false
	}
	weights := s.Weights
	if weights == (SchedulerWeights{}) {
		weights = DefaultSchedulerWeights()
	}
	sorted := append([]Candidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		left := scoreCandidate(sorted[i], weights)
		right := scoreCandidate(sorted[j], weights)
		if left == right {
			return sorted[i].NodeID < sorted[j].NodeID
		}
		return left > right
	})
	return sorted[0], true
}

func scoreCandidate(c Candidate, w SchedulerWeights) float64 {
	return w.WarmModelBonus*1 +
		w.RollingSuccessRate*c.RollingSuccessRate +
		w.NormalizedTokensPerSecond*c.TokensPerSecond +
		w.LowRecentFailureBonus*c.RecentFailureBonus +
		w.ThermalHeadroom*c.ThermalHeadroom +
		w.HostFairness*c.HostFairness +
		w.RegionalPreference*c.RegionalPreference
}

type LeaseClock struct {
	Now func() time.Time
}

func (c LeaseClock) LeaseExpiresAt(ttl time.Duration) time.Time {
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	return c.now().Add(ttl).UTC()
}

func (c LeaseClock) Expired(expiresAt time.Time) bool {
	return !c.now().Before(expiresAt)
}

func (c LeaseClock) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}
