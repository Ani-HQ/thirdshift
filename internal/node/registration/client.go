package registration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	nodeconfig "github.com/Ani-HQ/thirdshift/internal/node/config"
	"github.com/Ani-HQ/thirdshift/internal/node/identity"
	"github.com/Ani-HQ/thirdshift/internal/shared/nodeauth"
)

type LoginOptions struct {
	DataDir        string
	CoordinatorURL string
	InviteToken    string
	HTTPClient     *http.Client
	Now            func() time.Time
}

type Result struct {
	Credentials identity.Credentials
	Registered  bool
	Refreshed   bool
}

type registerRequest struct {
	InviteToken             string `json:"invite_token"`
	PublicKey               string `json:"public_key"`
	HardwareFingerprintHash string `json:"hardware_fingerprint_hash"`
}

type registerResponse struct {
	NodeID                  string         `json:"node_id"`
	BootstrapToken          string         `json:"bootstrap_token"`
	BootstrapTokenExpiresAt time.Time      `json:"bootstrap_token_expires_at"`
	FleetSchedule           *fleetSchedule `json:"fleet_schedule,omitempty"`
}

type fleetSchedule struct {
	From     string `json:"from"`
	Until    string `json:"until"`
	Timezone string `json:"timezone"`
}

type tokenRequest struct {
	NodeID         string `json:"node_id"`
	BootstrapToken string `json:"bootstrap_token"`
}

type tokenResponse struct {
	AccessToken          string    `json:"access_token"`
	AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`
}

func Login(ctx context.Context, opts LoginOptions) (Result, error) {
	cfg, err := nodeconfig.Load(opts.DataDir)
	if err != nil {
		return Result{}, err
	}
	if opts.CoordinatorURL != "" {
		cfg.CoordinatorURL = opts.CoordinatorURL
	}
	if cfg.CoordinatorURL == "" {
		return Result{}, fmt.Errorf("coordinator URL is required; pass --coordinator or set THIRDSHIFT_COORDINATOR_URL")
	}
	if _, err := url.ParseRequestURI(cfg.CoordinatorURL); err != nil {
		return Result{}, fmt.Errorf("invalid coordinator URL: %w", err)
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	privateKey, publicKey, err := identity.LoadOrCreateKey(cfg.DataDir)
	if err != nil {
		return Result{}, err
	}
	if opts.InviteToken == "" {
		creds, err := identity.LoadCredentials(cfg.DataDir)
		if err != nil {
			return Result{}, err
		}
		if opts.CoordinatorURL != "" {
			creds.CoordinatorURL = opts.CoordinatorURL
		}
		refreshed, err := refresh(ctx, client, cfg.CoordinatorURL, privateKey, creds.NodeID, opts.now())
		if err != nil {
			return Result{}, err
		}
		creds.AccessToken = refreshed.AccessToken
		creds.AccessTokenExpiresAt = refreshed.AccessTokenExpiresAt
		if err := nodeconfig.Save(cfg); err != nil {
			return Result{}, err
		}
		if err := identity.SaveCredentials(cfg.DataDir, creds); err != nil {
			return Result{}, err
		}
		return Result{Credentials: creds, Refreshed: true}, nil
	}

	fingerprint := identity.HardwareFingerprintHash()
	registered, err := postJSON[registerResponse](ctx, client, cfg.CoordinatorURL+"/v1/node/register", registerRequest{
		InviteToken:             opts.InviteToken,
		PublicKey:               publicKey,
		HardwareFingerprintHash: fingerprint,
	})
	if err != nil {
		return Result{}, err
	}
	if registered.FleetSchedule != nil && !nodeconfig.HasScheduleOverride(cfg.DataDir) {
		if registered.FleetSchedule.From != "" && registered.FleetSchedule.Until != "" {
			cfg.ScheduleFrom = registered.FleetSchedule.From
			cfg.ScheduleUntil = registered.FleetSchedule.Until
		}
	}
	token, err := postJSON[tokenResponse](ctx, client, cfg.CoordinatorURL+"/v1/node/token", tokenRequest{
		NodeID:         registered.NodeID,
		BootstrapToken: registered.BootstrapToken,
	})
	if err != nil {
		return Result{}, err
	}
	creds := identity.Credentials{
		NodeID:                  registered.NodeID,
		CoordinatorURL:          cfg.CoordinatorURL,
		PublicKey:               publicKey,
		AccessToken:             token.AccessToken,
		AccessTokenExpiresAt:    token.AccessTokenExpiresAt,
		HardwareFingerprintHash: fingerprint,
	}
	if err := nodeconfig.Save(cfg); err != nil {
		return Result{}, err
	}
	if err := identity.SaveCredentials(cfg.DataDir, creds); err != nil {
		return Result{}, err
	}
	return Result{Credentials: creds, Registered: true}, nil
}

func refresh(ctx context.Context, client *http.Client, coordinatorURL string, privateKey ed25519.PrivateKey, nodeID string, now time.Time) (tokenResponse, error) {
	req, err := nodeauth.SignRefresh(privateKey, nodeID, now)
	if err != nil {
		return tokenResponse{}, err
	}
	return postJSON[tokenResponse](ctx, client, coordinatorURL+"/v1/node/token/refresh", req)
}

func postJSON[T any](ctx context.Context, client *http.Client, endpoint string, body any) (T, error) {
	var zero T
	encoded, err := json.Marshal(body)
	if err != nil {
		return zero, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		if apiErr.Error == "" {
			apiErr.Error = resp.Status
		}
		return zero, fmt.Errorf("%s: %s", endpoint, strings.TrimSpace(apiErr.Error))
	}
	var decoded T
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return zero, fmt.Errorf("decode response: %w", err)
	}
	return decoded, nil
}

func (o LoginOptions) now() time.Time {
	if o.Now != nil {
		return o.Now().UTC()
	}
	return time.Now().UTC()
}

// RefreshOptions carries what a long-running agent needs to renew its own
// access token without going through the full login path.
type RefreshOptions struct {
	CoordinatorURL string
	NodeID         string
	PrivateKey     ed25519.PrivateKey
	DataDir        string
	HTTPClient     *http.Client
	Now            func() time.Time
}

// RefreshAccessToken renews the node's access token and persists it, so a
// process that has been running for days reconnects with a live token rather
// than the one it was handed at startup.
func RefreshAccessToken(ctx context.Context, opts RefreshOptions) (string, time.Time, error) {
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	refreshed, err := refresh(ctx, client, opts.CoordinatorURL, opts.PrivateKey, opts.NodeID, now())
	if err != nil {
		return "", time.Time{}, err
	}
	if opts.DataDir != "" {
		if creds, err := identity.LoadCredentials(opts.DataDir); err == nil {
			creds.AccessToken = refreshed.AccessToken
			creds.AccessTokenExpiresAt = refreshed.AccessTokenExpiresAt
			_ = identity.SaveCredentials(opts.DataDir, creds)
		}
	}
	return refreshed.AccessToken, refreshed.AccessTokenExpiresAt, nil
}
