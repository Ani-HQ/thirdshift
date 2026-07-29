package jobs

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

func NewAPIKey() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", fmt.Errorf("generate api key: %w", err)
	}
	return "tsak_" + base64.RawURLEncoding.EncodeToString(data[:]), nil
}

func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "sha256:" + hex.EncodeToString(sum[:])
}

type RateLimiter struct {
	LimitPerMinute int
	Now            func() time.Time
	mu             sync.Mutex
	buckets        map[string]rateBucket
}

type rateBucket struct {
	windowStart time.Time
	count       int
}

func (r *RateLimiter) Allow(keyID string) bool {
	if r.LimitPerMinute <= 0 {
		return true
	}
	now := r.now().Truncate(time.Minute)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.buckets == nil {
		r.buckets = map[string]rateBucket{}
	}
	bucket := r.buckets[keyID]
	if !bucket.windowStart.Equal(now) {
		bucket = rateBucket{windowStart: now}
	}
	if bucket.count >= r.LimitPerMinute {
		r.buckets[keyID] = bucket
		return false
	}
	bucket.count++
	r.buckets[keyID] = bucket
	return true
}

func (r *RateLimiter) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}
