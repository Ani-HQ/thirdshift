package sessions

import (
	"context"
	"fmt"
	"time"
)

type StaleStore interface {
	MarkStale(ctx context.Context, cutoff, now time.Time) (int64, error)
}

type Sweeper struct {
	Store      StaleStore
	Now        func() time.Time
	StaleAfter time.Duration
}

func (s Sweeper) RunOnce(ctx context.Context) (int64, error) {
	if s.Store == nil {
		return 0, fmt.Errorf("stale sweeper store is required")
	}
	now := s.now()
	staleAfter := s.StaleAfter
	if staleAfter <= 0 {
		staleAfter = 45 * time.Second
	}
	return s.Store.MarkStale(ctx, now.Add(-staleAfter), now)
}

func (s Sweeper) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := s.RunOnce(ctx); err != nil {
				return err
			}
		}
	}
}

func (s Sweeper) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}
