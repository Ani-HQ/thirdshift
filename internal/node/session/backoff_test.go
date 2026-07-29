package session

import (
	"testing"
	"time"
)

func TestBackoffCapsDelayWithJitter(t *testing.T) {
	b := Backoff{Base: time.Second, Max: 8 * time.Second, Jitter: 0.1}
	for attempt := 0; attempt < 12; attempt++ {
		delay := b.Delay(attempt)
		if delay < 0 {
			t.Fatalf("attempt %d delay is negative: %s", attempt, delay)
		}
		if delay > 8*time.Second {
			t.Fatalf("attempt %d delay = %s, want capped at 8s", attempt, delay)
		}
	}
}

func TestBackoffDefaultScheduleIsBounded(t *testing.T) {
	var b Backoff
	delay := b.Delay(10)
	if delay > time.Minute {
		t.Fatalf("default delay = %s, want <= 1m", delay)
	}
}
