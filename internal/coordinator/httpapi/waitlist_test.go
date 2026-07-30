package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateWaitlistEmail(t *testing.T) {
	for _, email := range []string{"dev@example.com", "alpha.user+test@sub.example.co"} {
		if err := validateWaitlistEmail(email); err != nil {
			t.Fatalf("valid email %q rejected: %v", email, err)
		}
	}
	for _, email := range []string{"", "not-an-email", "dev@example", "dev @example.com"} {
		if err := validateWaitlistEmail(email); err == nil {
			t.Fatalf("invalid email %q accepted", email)
		}
	}
}

func TestWaitlistRateLimiter(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	limiter := newWaitlistRateLimiter(2, time.Minute, func() time.Time { return now })
	if !limiter.allow("127.0.0.1") || !limiter.allow("127.0.0.1") {
		t.Fatal("first two requests should be allowed")
	}
	if limiter.allow("127.0.0.1") {
		t.Fatal("third request should be rate limited")
	}
	now = now.Add(time.Minute + time.Second)
	if !limiter.allow("127.0.0.1") {
		t.Fatal("request after window should be allowed")
	}
}

func TestRequesterRegionHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/status", nil)
	req.Header.Set("X-Geo-Region", "in-south")
	got := (Options{RequesterRegionHeader: "X-Geo-Region"}).requesterRegion(req)
	if got == nil || *got != "in-south" {
		t.Fatalf("requester region = %v, want in-south", got)
	}
	req = httptest.NewRequest("GET", "/v1/status", nil)
	req.Header.Set("CF-IPCountry", "IN")
	got = (Options{RequesterRegionHeader: "X-Geo-Region"}).requesterRegion(req)
	if got == nil || *got != "IN" {
		t.Fatalf("fallback requester region = %v, want IN", got)
	}
}

func TestPublicEndpointsSendCORSHeaders(t *testing.T) {
	handler := NewMux("test")
	req := httptest.NewRequest("OPTIONS", "/v1/waitlist", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("preflight allow-origin = %q, want *", got)
	}
	req = httptest.NewRequest("GET", "/v1/status", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("status allow-origin = %q, want *", got)
	}
}
