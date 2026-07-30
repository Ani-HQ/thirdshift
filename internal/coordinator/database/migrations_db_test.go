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

func TestModelListingStatusDefaultsToLiveAndIsConstrained(t *testing.T) {
	ctx := context.Background()
	conn, schema := migratedTestSchema(t, ctx)
	defer conn.Close(ctx)
	defer dropSchema(t, ctx, conn, schema)

	if _, err := conn.Exec(ctx, `
INSERT INTO models (id, display_name) VALUES ('listing-default', 'Listing Default');
INSERT INTO models (id, display_name, listing_status, expected_output_tokens_per_second,
                    market_typical_input_per_million_microdollars,
                    market_typical_output_per_million_microdollars,
                    market_comparison_source_note)
VALUES ('listing-waitlist', 'Listing Waitlist', 'waitlist', 30, 40000, 100000, 'typical hosted price, July 2026');
INSERT INTO models (id, display_name, listing_status) VALUES ('listing-hidden', 'Listing Hidden', 'hidden');
`); err != nil {
		t.Fatalf("insert listing rows: %v", err)
	}
	var defaultStatus string
	if err := conn.QueryRow(ctx, "SELECT listing_status FROM models WHERE id = 'listing-default'").Scan(&defaultStatus); err != nil {
		t.Fatalf("read default listing status: %v", err)
	}
	if defaultStatus != "live" {
		t.Fatalf("default listing_status = %q, want live", defaultStatus)
	}
	if _, err := conn.Exec(ctx, "INSERT INTO models (id, display_name, listing_status) VALUES ('listing-bad', 'Bad', 'coming_soon')"); err == nil {
		t.Fatal("unknown listing_status accepted")
	}
	if _, err := conn.Exec(ctx, `
INSERT INTO models (id, display_name, market_typical_input_per_million_microdollars)
VALUES ('listing-half-comparison', 'Half Comparison', 40000)`); err == nil {
		t.Fatal("half-populated market comparison accepted")
	}
}

func TestWaitlistApplicationColumnsAreConstrained(t *testing.T) {
	ctx := context.Background()
	conn, schema := migratedTestSchema(t, ctx)
	defer conn.Close(ctx)
	defer dropSchema(t, ctx, conn, schema)

	if _, err := conn.Exec(ctx, `
INSERT INTO waitlist_signups (id, email, name, use_case, expected_volume, data_ack, model_id)
VALUES ('wait_01J0M000000000000000000000', 'dev@example.com', 'Dev', 'Batch summaries', '10m_100m', true, 'qwen2.5-7b-instruct');
INSERT INTO waitlist_signups (id, email) VALUES ('wait_01J0M000000000000000000001', 'legacy@example.com');
`); err != nil {
		t.Fatalf("insert applications: %v", err)
	}
	var legacyAck bool
	if err := conn.QueryRow(ctx, "SELECT data_ack FROM waitlist_signups WHERE email = 'legacy@example.com'").Scan(&legacyAck); err != nil {
		t.Fatalf("read legacy data_ack: %v", err)
	}
	if legacyAck {
		t.Fatal("rows predating the acknowledgment must default to false")
	}
	if _, err := conn.Exec(ctx, `
INSERT INTO waitlist_signups (id, email, expected_volume)
VALUES ('wait_01J0M000000000000000000002', 'bad@example.com', 'loads')`); err == nil {
		t.Fatal("unknown expected_volume band accepted")
	}
}

func TestWaitlistUniquenessIsPerEmailAndModel(t *testing.T) {
	ctx := context.Background()
	conn, schema := migratedTestSchema(t, ctx)
	defer conn.Close(ctx)
	defer dropSchema(t, ctx, conn, schema)

	if _, err := conn.Exec(ctx, `
INSERT INTO waitlist_signups (id, email, use_case, data_ack, model_id)
VALUES ('wait_01J0M000000000000000000000', 'dev@example.com', 'Chat', true, 'qwen2.5-7b-instruct');
INSERT INTO waitlist_signups (id, email, use_case, data_ack, model_id)
VALUES ('wait_01J0M000000000000000000001', 'dev@example.com', 'Code', true, 'qwen2.5-coder-7b-instruct');
INSERT INTO waitlist_signups (id, email, use_case, data_ack)
VALUES ('wait_01J0M000000000000000000002', 'dev@example.com', 'General', true);
`); err != nil {
		t.Fatalf("one applicant across several models must be allowed: %v", err)
	}

	if _, err := conn.Exec(ctx, `
INSERT INTO waitlist_signups (id, email, use_case, data_ack, model_id)
VALUES ('wait_01J0M000000000000000000003', 'dev@example.com', 'Chat again', true, 'qwen2.5-7b-instruct')`); err == nil {
		t.Fatal("duplicate (email, model_id) insert succeeded, want unique violation")
	}

	// NULLS NOT DISTINCT: a second general application for the same address is
	// the same key, so it must conflict instead of piling up another row.
	if _, err := conn.Exec(ctx, `
INSERT INTO waitlist_signups (id, email, use_case, data_ack)
VALUES ('wait_01J0M000000000000000000004', 'dev@example.com', 'General again', true)`); err == nil {
		t.Fatal("second general application inserted, want unique violation on (email, NULL)")
	}

	// The same model for a different address is unrelated.
	if _, err := conn.Exec(ctx, `
INSERT INTO waitlist_signups (id, email, use_case, data_ack, model_id)
VALUES ('wait_01J0M000000000000000000005', 'other@example.com', 'Chat', true, 'qwen2.5-7b-instruct')`); err != nil {
		t.Fatalf("same model for a different applicant rejected: %v", err)
	}

	var lastApplied int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM waitlist_signups WHERE last_applied_at IS NOT NULL").Scan(&lastApplied); err != nil {
		t.Fatalf("count last_applied_at: %v", err)
	}
	if lastApplied != 4 {
		t.Fatalf("rows with last_applied_at = %d, want 4", lastApplied)
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
