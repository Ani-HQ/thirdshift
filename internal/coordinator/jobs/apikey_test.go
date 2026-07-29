package jobs

import (
	"testing"
	"time"
)

func TestRateLimiterPerMinute(t *testing.T) {
	now := time.Date(2026, 7, 29, 8, 0, 0, 0, time.UTC)
	limiter := &RateLimiter{LimitPerMinute: 2, Now: func() time.Time { return now }}
	if !limiter.Allow("ak") || !limiter.Allow("ak") {
		t.Fatal("first two requests should be allowed")
	}
	if limiter.Allow("ak") {
		t.Fatal("third request allowed, want rate limit")
	}
	now = now.Add(time.Minute)
	if !limiter.Allow("ak") {
		t.Fatal("request in next minute rejected")
	}
}

func TestAPIKeyHashStable(t *testing.T) {
	if HashAPIKey("secret") != HashAPIKey("secret") {
		t.Fatal("api key hash is not stable")
	}
	if HashAPIKey("secret") == HashAPIKey("other") {
		t.Fatal("different api keys produced same hash")
	}
}
