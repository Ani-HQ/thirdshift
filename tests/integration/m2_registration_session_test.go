//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/coordinator/auth"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/database"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/httpapi"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
	coordinatorsessions "github.com/Ani-HQ/thirdshift/internal/coordinator/sessions"
	nodeagent "github.com/Ani-HQ/thirdshift/internal/node/agent"
	noderegistration "github.com/Ani-HQ/thirdshift/internal/node/registration"
	"github.com/Ani-HQ/thirdshift/internal/shared/protocol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRegisterHeartbeatStaleAndReconnect(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, cleanup := migratedPool(t, ctx, databaseURL)
	defer cleanup()

	store := registration.PGStore{Pool: pool}
	validator, err := protocol.NewValidator(filepath.Join("..", "..", "packages", "protocol", "schemas"))
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	signer := auth.TokenSigner{Secret: []byte("integration-secret"), TTL: time.Hour}
	server := httptest.NewServer(httpapi.NewMuxWithOptions(httpapi.Options{
		Version:           "integration",
		Registration:      registration.Service{Repository: store},
		SessionStore:      debugSessionStore{PGStore: store, t: t},
		TokenSigner:       signer,
		ProtocolValidator: validator,
		OperatorToken:     "operator-token",
		HeartbeatInterval: 20 * time.Millisecond,
	}))
	defer server.Close()

	inviteToken := createInvite(t, server.URL, "operator-token")
	dataDir := t.TempDir()
	login, err := noderegistration.Login(ctx, noderegistration.LoginOptions{
		DataDir:        dataDir,
		CoordinatorURL: server.URL,
		InviteToken:    inviteToken,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatalf("node login: %v", err)
	}
	if login.Credentials.NodeID == "" {
		t.Fatal("login did not return node id")
	}

	firstCtx, stopFirst := context.WithCancel(ctx)
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- nodeagent.Run(firstCtx, nodeagent.Options{
			DataDir:           dataDir,
			CoordinatorURL:    server.URL,
			NodeID:            login.Credentials.NodeID,
			AccessToken:       login.Credentials.AccessToken,
			ModelID:           "thirdshift-tiny-chat-v1",
			HeartbeatInterval: time.Hour,
			HTTPClient:        server.Client(),
			Validator:         validator,
			Runtime:           fakeRuntime{},
			Telemetry:         fakeTelemetry{},
		})
	}()
	waitForNodeState(t, ctx, store, "AVAILABLE", "connected")

	sweeper := coordinatorsessions.Sweeper{
		Store:      store,
		Now:        func() time.Time { return time.Now().UTC().Add(time.Hour) },
		StaleAfter: 45 * time.Second,
	}
	marked, err := sweeper.RunOnce(ctx)
	if err != nil {
		t.Fatalf("mark stale: %v", err)
	}
	if marked == 0 {
		t.Fatal("stale sweeper marked no sessions")
	}
	waitForNodeState(t, ctx, store, "OFFLINE", "stale")
	stopFirst()
	if err := <-firstErr; err != nil {
		t.Fatalf("first agent returned error: %v", err)
	}

	secondCtx, stopSecond := context.WithCancel(ctx)
	defer stopSecond()
	secondErr := make(chan error, 1)
	go func() {
		secondErr <- nodeagent.Run(secondCtx, nodeagent.Options{
			DataDir:           dataDir,
			CoordinatorURL:    server.URL,
			NodeID:            login.Credentials.NodeID,
			AccessToken:       login.Credentials.AccessToken,
			ModelID:           "thirdshift-tiny-chat-v1",
			HeartbeatInterval: 20 * time.Millisecond,
			HTTPClient:        server.Client(),
			Validator:         validator,
			Runtime:           fakeRuntime{},
			Telemetry:         fakeTelemetry{},
		})
	}()
	waitForNodeState(t, ctx, store, "AVAILABLE", "connected")
	stopSecond()
	if err := <-secondErr; err != nil {
		t.Fatalf("second agent returned error: %v", err)
	}
}

func createInvite(t *testing.T, coordinatorURL, operatorToken string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"fleet_id":           "fleet_01J0M000000000000000000000",
		"expires_in_seconds": 60,
	})
	if err != nil {
		t.Fatalf("marshal invite: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, coordinatorURL+"/internal/v1/invites", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new invite request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+operatorToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create invite status = %d", resp.StatusCode)
	}
	var decoded struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode invite: %v", err)
	}
	if decoded.Token == "" {
		t.Fatal("invite token missing")
	}
	return decoded.Token
}

func waitForNodeState(t *testing.T, ctx context.Context, store registration.PGStore, state, sessionStatus string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		nodes, err := store.ListNodes(ctx)
		if err != nil {
			t.Fatalf("list nodes: %v", err)
		}
		if len(nodes) == 1 && nodes[0].State == state && nodes[0].SessionStatus == sessionStatus {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	nodes, _ := store.ListNodes(ctx)
	t.Fatalf("timed out waiting for state=%s session=%s; got %#v", state, sessionStatus, nodes)
}

func migratedPool(t *testing.T, ctx context.Context, databaseURL string) (*pgxpool.Pool, func()) {
	t.Helper()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	schema := fmt.Sprintf("m2_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("set search path: %v", err)
	}
	if _, err := database.Apply(ctx, conn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	conn.Close(ctx)

	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize())
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	return pool, func() {
		pool.Close()
		cleanupConn, err := pgx.Connect(context.Background(), databaseURL)
		if err != nil {
			t.Fatalf("connect cleanup database: %v", err)
		}
		defer cleanupConn.Close(context.Background())
		if _, err := cleanupConn.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Fatalf("drop schema: %v", err)
		}
	}
}

type fakeRuntime struct{}

type debugSessionStore struct {
	registration.PGStore
	t *testing.T
}

func (s debugSessionStore) RecordHeartbeat(ctx context.Context, sessionID string, heartbeat protocol.NodeHeartbeatPayload, receivedAt time.Time) error {
	err := s.PGStore.RecordHeartbeat(ctx, sessionID, heartbeat, receivedAt)
	if err != nil {
		s.t.Logf("record heartbeat failed: %v", err)
	}
	return err
}

func (fakeRuntime) Prepare(context.Context, string) (nodeagent.RuntimeStatus, error) {
	return nodeagent.RuntimeStatus{
		ModelID:     "thirdshift-tiny-chat-v1",
		RuntimeHash: "sha256:runtime",
		ModelHash:   "sha256:model",
	}, nil
}

type fakeTelemetry struct{}

func (fakeTelemetry) GPUStatus(context.Context) (protocol.GPUStatus, error) {
	return protocol.GPUStatus{
		Name:               "fake-gpu",
		VRAMTotalMB:        1,
		VRAMFreeMB:         1,
		TemperatureC:       1,
		PowerW:             1,
		PowerLimitW:        1,
		UtilizationPercent: 1,
	}, nil
}
