package httpapi

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	operatorstore "github.com/Ani-HQ/thirdshift/internal/coordinator/operator"
)

type waitlistRateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string][]time.Time
}

func newWaitlistRateLimiter(limit int, window time.Duration, now func() time.Time) *waitlistRateLimiter {
	if limit <= 0 {
		limit = 5
	}
	if window <= 0 {
		window = time.Minute
	}
	return &waitlistRateLimiter{
		limit:  limit,
		window: window,
		now:    now,
		hits:   map[string][]time.Time{},
	}
}

func (l *waitlistRateLimiter) allow(key string) bool {
	if l == nil {
		return true
	}
	now := time.Now().UTC()
	if l.now != nil {
		now = l.now().UTC()
	}
	cutoff := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	existing := l.hits[key]
	kept := existing[:0]
	for _, hit := range existing {
		if hit.After(cutoff) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	kept = append(kept, now)
	l.hits[key] = kept
	return true
}

func (o Options) waitlistSignupHandler(limiter *waitlistRateLimiter) http.HandlerFunc {
	type request struct {
		Email   string `json:"email"`
		UseCase string `json:"use_case,omitempty"`
	}
	type response struct {
		Status    string `json:"status"`
		Duplicate bool   `json:"duplicate"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := o.requireOperatorStore(w)
		if !ok {
			return
		}
		var req request
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		email := operatorstore.NormalizeEmail(req.Email)
		if err := validateWaitlistEmail(email); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		exists, err := store.WaitlistEmailExists(r.Context(), email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if exists {
			writeJSON(w, http.StatusOK, response{Status: "ok", Duplicate: true})
			return
		}
		if !limiter.allow(clientRateLimitKey(r)) {
			writeError(w, http.StatusTooManyRequests, "waitlist signup rate limit exceeded")
			return
		}
		_, created, err := store.CreateWaitlistSignup(r.Context(), email, req.UseCase, "public_catalog", o.now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, response{Status: "ok", Duplicate: !created})
	}
}

func validateWaitlistEmail(email string) error {
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if strings.ContainsAny(email, " \t\r\n") {
		return fmt.Errorf("email is invalid")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return fmt.Errorf("email is invalid")
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || !strings.Contains(domain, ".") {
		return fmt.Errorf("email is invalid")
	}
	return nil
}

func clientRateLimitKey(r *http.Request) string {
	host := strings.TrimSpace(r.RemoteAddr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	if host == "" {
		host = "unknown"
	}
	return host
}
