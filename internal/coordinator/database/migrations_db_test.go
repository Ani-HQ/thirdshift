//go:build db

package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestMigrationsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	conn, schema := migratedTestSchema(t, ctx)
	defer conn.Close(ctx)
	defer dropSchema(t, ctx, conn, schema)

	applied, err := Apply(ctx, conn, migrationDir(t))
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("second apply applied %d migrations, want 0", len(applied))
	}
}

func TestLedgerPostedTransactionsMustBalance(t *testing.T) {
	ctx := context.Background()
	conn, schema := migratedTestSchema(t, ctx)
	defer conn.Close(ctx)
	defer dropSchema(t, ctx, conn, schema)

	_, err := conn.Exec(ctx, `
INSERT INTO ledger_accounts (id, owner_type, owner_id, account_type, currency)
VALUES
  ('acct_01J0M000000000000000000000', 'customer', 'usr_01J0M000000000000000000000', 'customer_usage', 'USD_MICRO'),
  ('acct_01J0M000000000000000000001', 'platform', 'org_01J0M000000000000000000000', 'platform_margin', 'USD_MICRO');
INSERT INTO ledger_transactions (id, transaction_type, status, reference_type, reference_id)
VALUES ('ltx_01J0M000000000000000000000', 'usage_charge', 'draft', 'job', 'job_01J0M000000000000000000000');
INSERT INTO ledger_entries (id, transaction_id, account_id, amount_microdollars, currency)
VALUES
  ('le_01J0M000000000000000000000', 'ltx_01J0M000000000000000000000', 'acct_01J0M000000000000000000000', 100, 'USD_MICRO'),
  ('le_01J0M000000000000000000001', 'ltx_01J0M000000000000000000000', 'acct_01J0M000000000000000000001', -100, 'USD_MICRO');
UPDATE ledger_transactions SET status = 'posted', posted_at = now()
WHERE id = 'ltx_01J0M000000000000000000000';
`)
	if err != nil {
		t.Fatalf("post balanced transaction: %v", err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO ledger_transactions (id, transaction_type, status, reference_type, reference_id)
VALUES ('ltx_01J0M000000000000000000001', 'usage_charge', 'draft', 'job', 'job_01J0M000000000000000000001');
INSERT INTO ledger_entries (id, transaction_id, account_id, amount_microdollars, currency)
VALUES ('le_01J0M000000000000000000002', 'ltx_01J0M000000000000000000001', 'acct_01J0M000000000000000000000', 50, 'USD_MICRO');
UPDATE ledger_transactions SET status = 'posted', posted_at = now()
WHERE id = 'ltx_01J0M000000000000000000001';
`)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("prepare unbalanced transaction: %v", err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("commit unbalanced transaction succeeded, want error")
	}
}

func TestOneSuccessfulAttemptPerJob(t *testing.T) {
	ctx := context.Background()
	conn, schema := migratedTestSchema(t, ctx)
	defer conn.Close(ctx)
	defer dropSchema(t, ctx, conn, schema)

	_, err := conn.Exec(ctx, `
INSERT INTO organizations (id, name)
VALUES ('org_01J0M000000000000000000000', 'Test Org');
INSERT INTO models (id, display_name)
VALUES ('thirdshift-small-chat-v1', 'Thirdshift Small Chat v1');
INSERT INTO jobs (id, organization_id, model_id, state)
VALUES ('job_01J0M000000000000000000000', 'org_01J0M000000000000000000000', 'thirdshift-small-chat-v1', 'queued');
INSERT INTO job_attempts (id, job_id, attempt_number, lease_nonce, lease_expires_at, deadline_at, status)
VALUES ('att_01J0M000000000000000000000', 'job_01J0M000000000000000000000', 1, 'nonce-1', now() + interval '10 seconds', now() + interval '2 minutes', 'succeeded');
`)
	if err != nil {
		t.Fatalf("seed first successful attempt: %v", err)
	}

	_, err = conn.Exec(ctx, `
INSERT INTO job_attempts (id, job_id, attempt_number, lease_nonce, lease_expires_at, deadline_at, status)
VALUES ('att_01J0M000000000000000000001', 'job_01J0M000000000000000000000', 2, 'nonce-2', now() + interval '10 seconds', now() + interval '2 minutes', 'succeeded');
`)
	if err == nil {
		t.Fatal("second successful attempt insert succeeded, want unique partial index error")
	}
}

func TestIdempotencyKeyUniquePerAPIKeyEndpoint(t *testing.T) {
	ctx := context.Background()
	conn, schema := migratedTestSchema(t, ctx)
	defer conn.Close(ctx)
	defer dropSchema(t, ctx, conn, schema)

	_, err := conn.Exec(ctx, `
INSERT INTO organizations (id, name)
VALUES ('org_01J0M000000000000000000000', 'Test Org');
INSERT INTO api_keys (id, organization_id, name, key_hash)
VALUES ('ak_01J0M000000000000000000000', 'org_01J0M000000000000000000000', 'test', 'hash-1');
INSERT INTO idempotency_records (id, api_key_id, endpoint, idempotency_key, request_hash, expires_at)
VALUES ('idem_01J0M000000000000000000000', 'ak_01J0M000000000000000000000', '/v1/jobs', 'demo-001', 'req-hash-1', now() + interval '1 day');
`)
	if err != nil {
		t.Fatalf("seed idempotency record: %v", err)
	}

	_, err = conn.Exec(ctx, `
INSERT INTO idempotency_records (id, api_key_id, endpoint, idempotency_key, request_hash, expires_at)
VALUES ('idem_01J0M000000000000000000001', 'ak_01J0M000000000000000000000', '/v1/jobs', 'demo-001', 'req-hash-1', now() + interval '1 day');
`)
	if err == nil {
		t.Fatal("duplicate idempotency record insert succeeded, want unique constraint error")
	}

	_, err = conn.Exec(ctx, `
INSERT INTO idempotency_records (id, api_key_id, endpoint, idempotency_key, request_hash, expires_at)
VALUES ('idem_01J0M000000000000000000002', 'ak_01J0M000000000000000000000', '/v1/chat/completions', 'demo-001', 'req-hash-1', now() + interval '1 day');
`)
	if err != nil {
		t.Fatalf("same idempotency key on different endpoint: %v", err)
	}
}

func TestPostedLedgerEntriesAreImmutable(t *testing.T) {
	ctx := context.Background()
	conn, schema := migratedTestSchema(t, ctx)
	defer conn.Close(ctx)
	defer dropSchema(t, ctx, conn, schema)

	_, err := conn.Exec(ctx, `
INSERT INTO ledger_accounts (id, owner_type, owner_id, account_type, currency)
VALUES
  ('acct_01J0M000000000000000000000', 'customer', 'usr_01J0M000000000000000000000', 'customer_usage', 'USD_MICRO'),
  ('acct_01J0M000000000000000000001', 'platform', 'org_01J0M000000000000000000000', 'platform_margin', 'USD_MICRO');
INSERT INTO ledger_transactions (id, transaction_type, status, reference_type, reference_id)
VALUES ('ltx_01J0M000000000000000000000', 'usage_charge', 'draft', 'job', 'job_01J0M000000000000000000000');
INSERT INTO ledger_entries (id, transaction_id, account_id, amount_microdollars, currency)
VALUES
  ('le_01J0M000000000000000000000', 'ltx_01J0M000000000000000000000', 'acct_01J0M000000000000000000000', 100, 'USD_MICRO'),
  ('le_01J0M000000000000000000001', 'ltx_01J0M000000000000000000000', 'acct_01J0M000000000000000000001', -100, 'USD_MICRO');
UPDATE ledger_transactions SET status = 'posted', posted_at = now()
WHERE id = 'ltx_01J0M000000000000000000000';
`)
	if err != nil {
		t.Fatalf("seed posted transaction: %v", err)
	}

	if _, err := conn.Exec(ctx, "UPDATE ledger_entries SET amount_microdollars = 1 WHERE id = 'le_01J0M000000000000000000000'"); err == nil {
		t.Fatal("update posted ledger entry succeeded, want error")
	}
	if _, err := conn.Exec(ctx, "DELETE FROM ledger_entries WHERE id = 'le_01J0M000000000000000000000'"); err == nil {
		t.Fatal("delete posted ledger entry succeeded, want error")
	}
}

func migratedTestSchema(t *testing.T, ctx context.Context) (*pgx.Conn, string) {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}

	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		conn.Close(ctx)
		t.Fatalf("create schema: %v", err)
	}
	if _, err := conn.Exec(ctx, "SET search_path TO "+pgx.Identifier{schema}.Sanitize()); err != nil {
		conn.Close(ctx)
		t.Fatalf("set search_path: %v", err)
	}

	applied, err := Apply(ctx, conn, migrationDir(t))
	if err != nil {
		conn.Close(ctx)
		t.Fatalf("apply migrations: %v", err)
	}
	if len(applied) == 0 {
		conn.Close(ctx)
		t.Fatalf("first apply applied no migrations")
	}

	return conn, schema
}

func dropSchema(t *testing.T, ctx context.Context, conn *pgx.Conn, schema string) {
	t.Helper()
	if _, err := conn.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
}

func migrationDir(t *testing.T) string {
	t.Helper()
	return filepath.Clean("../../../migrations")
}
