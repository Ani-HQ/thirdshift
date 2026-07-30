//go:build integration

package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ani-HQ/thirdshift/internal/coordinator/database"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/httpapi"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/jobs"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/ledger"
	operatorstore "github.com/Ani-HQ/thirdshift/internal/coordinator/operator"
	"github.com/Ani-HQ/thirdshift/internal/coordinator/registration"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFleetCommandsAgainstCoordinator(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, cleanup := adminCLIMigratedPool(t, ctx, databaseURL)
	defer cleanup()

	regStore := registration.PGStore{Pool: pool}
	jobStore := jobs.PGStore{Pool: pool}
	opStore := operatorstore.Store{
		Pool:        pool,
		JobStore:    jobStore,
		LedgerStore: ledger.Store{Pool: pool},
	}
	server := httptest.NewServer(httpapi.NewMuxWithOptions(httpapi.Options{
		Version:       "admin-cli-test",
		Registration:  registration.Service{Repository: regStore},
		SessionStore:  regStore,
		JobService:    &jobs.Service{Store: jobStore},
		OperatorStore: &opStore,
		OperatorToken: "operator-token",
	}))
	defer server.Close()

	orgID, err := jobStore.CreateOrg(ctx, "CLI Fleet Org")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := run([]string{"fleet", "create", "--org", orgID, "--name", "CLI Cafe", "--schedule-from", "23:00", "--schedule-until", "08:00", "--coordinator", server.URL, "--operator-token", "operator-token"}); err != nil {
		t.Fatalf("fleet create command: %v", err)
	}
	var fleetID string
	if err := pool.QueryRow(ctx, "SELECT id FROM fleets WHERE organization_id = $1 AND name = 'CLI Cafe'", orgID).Scan(&fleetID); err != nil {
		t.Fatalf("load created fleet: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "fleet-report.csv")
	if err := run([]string{"fleet", "report", "--fleet", fleetID, "--from", "2026-01-01", "--to", "2027-01-01", "--out", outPath, "--coordinator", server.URL, "--operator-token", "operator-token"}); err != nil {
		t.Fatalf("fleet report command: %v", err)
	}
	file, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open report: %v", err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("parse report csv: %v", err)
	}
	if len(records) == 0 || strings.Join(records[0], ",") != "fleet_id,node_id,jobs_succeeded,jobs_failed,prompt_tokens,completion_tokens,host_credit_microdollars" {
		t.Fatalf("unexpected fleet report header: %#v", records)
	}
}

func adminCLIMigratedPool(t *testing.T, ctx context.Context, databaseURL string) (*pgxpool.Pool, func()) {
	t.Helper()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	schema := fmt.Sprintf("admin_cli_%d", time.Now().UnixNano())
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
