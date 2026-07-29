package sessions

import (
	"context"
	"testing"
	"time"
)

func TestSweeperUsesFakeClockAndConfiguredStaleAfter(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 45, 0, time.UTC)
	store := &fakeStaleStore{}
	sweeper := Sweeper{
		Store:      store,
		Now:        func() time.Time { return now },
		StaleAfter: 45 * time.Second,
	}
	count, err := sweeper.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run once: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if !store.cutoff.Equal(now.Add(-45 * time.Second)) {
		t.Fatalf("cutoff = %s, want %s", store.cutoff, now.Add(-45*time.Second))
	}
	if !store.now.Equal(now) {
		t.Fatalf("now = %s, want %s", store.now, now)
	}
}

type fakeStaleStore struct {
	cutoff time.Time
	now    time.Time
}

func (f *fakeStaleStore) MarkStale(_ context.Context, cutoff, now time.Time) (int64, error) {
	f.cutoff = cutoff
	f.now = now
	return 2, nil
}
