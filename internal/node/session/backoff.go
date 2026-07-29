package session

import (
	"math/rand/v2"
	"time"
)

type Backoff struct {
	Base   time.Duration
	Max    time.Duration
	Jitter float64
}

func (b Backoff) Delay(attempt int) time.Duration {
	base := b.Base
	if base <= 0 {
		base = time.Second
	}
	maxDelay := b.Max
	if maxDelay <= 0 {
		maxDelay = time.Minute
	}
	if attempt < 0 {
		attempt = 0
	}
	delay := base
	for i := 0; i < attempt && delay < maxDelay; i++ {
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
			break
		}
	}
	jitter := b.Jitter
	if jitter <= 0 {
		jitter = 0.2
	}
	if jitter > 1 {
		jitter = 1
	}
	spread := int64(float64(delay) * jitter)
	if spread <= 0 {
		return delay
	}
	offset := rand.Int64N(spread*2+1) - spread
	got := delay + time.Duration(offset)
	if got < 0 {
		return 0
	}
	if got > maxDelay {
		return maxDelay
	}
	return got
}
