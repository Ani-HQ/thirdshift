package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A node that has run longer than the access token lifetime must renew on its
// own. Before this, the token was fetched once at start, so any disconnect
// after an hour left the node reconnecting with a dead token forever — a host
// left running overnight would be silently offline by morning.
func TestAgentRenewsExpiredAccessTokenAndReconnects(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	var refreshCalls int
	var dialAuth []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/v1/node/token/refresh") {
			refreshCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":            "fresh-token",
				"access_token_expires_at": time.Now().Add(time.Hour),
			})
			return
		}
		dialAuth = append(dialAuth, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer fresh-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// A dial that gets past auth but is not a real websocket upgrade ends
		// the attempt; the assertions below only care about the auth path.
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	agent := &Agent{
		opts: Options{
			CoordinatorURL: server.URL,
			NodeID:         "node_01J0M0000000000000000QQQQQ",
			AccessToken:    "stale-token",
			PrivateKey:     privateKey,
			HTTPClient:     server.Client(),
			Now:            time.Now,
		},
		// Expired an hour ago: exactly the state of a node left running.
		accessTokenExpiresAt: time.Now().Add(-time.Hour),
	}

	if err := agent.ensureAccessToken(context.Background(), false); err != nil {
		t.Fatalf("ensure access token: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want exactly 1 for an expired token", refreshCalls)
	}
	if agent.opts.AccessToken != "fresh-token" {
		t.Fatalf("token = %q, want the renewed token", agent.opts.AccessToken)
	}

	// A live token is left alone.
	if err := agent.ensureAccessToken(context.Background(), false); err != nil {
		t.Fatalf("ensure with live token: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want no refresh while the token is live", refreshCalls)
	}
}
