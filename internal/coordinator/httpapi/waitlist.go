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

// maxWaitlistFieldBytes bounds the free-text application fields before they
// reach the database CHECK constraints.
const (
	maxWaitlistNameBytes    = 200
	maxWaitlistUseCaseBytes = 2000
)

func (o Options) waitlistSignupHandler(limiter *waitlistRateLimiter) http.HandlerFunc {
	type request struct {
		Email          string `json:"email"`
		Name           string `json:"name,omitempty"`
		UseCase        string `json:"use_case"`
		ExpectedVolume string `json:"expected_volume,omitempty"`
		DataAck        bool   `json:"data_ack"`
		ModelID        string `json:"model_id,omitempty"`
	}
	// The response is identical whether this was a first application or a
	// resubmission, so it cannot be used to probe which addresses have applied.
	type response struct {
		Status string `json:"status"`
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
		application := operatorstore.WaitlistApplication{
			Email:          email,
			Name:           strings.TrimSpace(req.Name),
			UseCase:        strings.TrimSpace(req.UseCase),
			ExpectedVolume: strings.TrimSpace(req.ExpectedVolume),
			DataAck:        req.DataAck,
			ModelID:        strings.TrimSpace(req.ModelID),
			Source:         "public_catalog",
		}
		// Every field is validated before the write so an incomplete
		// application can never be answered with a 200.
		if err := validateWaitlistApplication(application); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !limiter.allow(clientRateLimitKey(r)) {
			writeError(w, http.StatusTooManyRequests, "waitlist signup rate limit exceeded")
			return
		}
		_, inserted, err := store.SubmitWaitlistApplication(r.Context(), application, o.now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if o.Cairo != nil {
			o.Cairo.NotifyAccessApplicationAsync(application.Email, map[string]any{
				"name":            application.Name,
				"use_case":        application.UseCase,
				"expected_volume": application.ExpectedVolume,
				"model_id":        application.ModelID,
				"source":          application.Source,
				"is_new":          inserted,
			})
		}
		writeJSON(w, http.StatusOK, response{Status: "ok"})
	}
}

func validateWaitlistApplication(application operatorstore.WaitlistApplication) error {
	if err := validateWaitlistEmail(application.Email); err != nil {
		return err
	}
	if len(application.Name) > maxWaitlistNameBytes {
		return fmt.Errorf("name is too long")
	}
	if application.UseCase == "" {
		return fmt.Errorf("use_case is required")
	}
	if len(application.UseCase) > maxWaitlistUseCaseBytes {
		return fmt.Errorf("use_case is too long")
	}
	if err := operatorstore.ValidateExpectedVolume(application.ExpectedVolume); err != nil {
		return err
	}
	if !application.DataAck {
		return fmt.Errorf("data_ack is required")
	}
	if len(application.ModelID) > 128 {
		return fmt.Errorf("model_id is invalid")
	}
	return nil
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
