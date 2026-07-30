package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	operatorstore "github.com/Ani-HQ/thirdshift/internal/coordinator/operator"
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

func TestValidateWaitlistApplication(t *testing.T) {
	valid := operatorstore.WaitlistApplication{
		Email:          "dev@example.com",
		Name:           "Dev",
		UseCase:        "Nightly evaluation harness",
		ExpectedVolume: "1m_10m",
		DataAck:        true,
		ModelID:        "qwen2.5-7b-instruct",
	}
	if err := validateWaitlistApplication(valid); err != nil {
		t.Fatalf("valid application rejected: %v", err)
	}
	minimal := operatorstore.WaitlistApplication{Email: "dev@example.com", UseCase: "Prototype", DataAck: true}
	if err := validateWaitlistApplication(minimal); err != nil {
		t.Fatalf("application without optional fields rejected: %v", err)
	}

	for name, mutate := range map[string]func(operatorstore.WaitlistApplication) operatorstore.WaitlistApplication{
		"missing email": func(a operatorstore.WaitlistApplication) operatorstore.WaitlistApplication {
			a.Email = ""
			return a
		},
		"invalid email": func(a operatorstore.WaitlistApplication) operatorstore.WaitlistApplication {
			a.Email = "not-an-email"
			return a
		},
		"missing use case": func(a operatorstore.WaitlistApplication) operatorstore.WaitlistApplication {
			a.UseCase = ""
			return a
		},
		"missing data ack": func(a operatorstore.WaitlistApplication) operatorstore.WaitlistApplication {
			a.DataAck = false
			return a
		},
		"unknown volume band": func(a operatorstore.WaitlistApplication) operatorstore.WaitlistApplication {
			a.ExpectedVolume = "all-of-them"
			return a
		},
		"oversized name": func(a operatorstore.WaitlistApplication) operatorstore.WaitlistApplication {
			a.Name = strings.Repeat("n", maxWaitlistNameBytes+1)
			return a
		},
		"oversized use case": func(a operatorstore.WaitlistApplication) operatorstore.WaitlistApplication {
			a.UseCase = strings.Repeat("u", maxWaitlistUseCaseBytes+1)
			return a
		},
		"oversized model id": func(a operatorstore.WaitlistApplication) operatorstore.WaitlistApplication {
			a.ModelID = strings.Repeat("m", 129)
			return a
		},
	} {
		if err := validateWaitlistApplication(mutate(valid)); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}

func TestValidateExpectedVolumeBands(t *testing.T) {
	for _, band := range []string{"", "lt_1m", "1m_10m", "10m_100m", "gt_100m"} {
		if err := operatorstore.ValidateExpectedVolume(band); err != nil {
			t.Fatalf("band %q rejected: %v", band, err)
		}
	}
	if err := operatorstore.ValidateExpectedVolume("1m-10m"); err == nil {
		t.Fatal("unknown band accepted")
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
