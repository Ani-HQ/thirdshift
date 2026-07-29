package ledger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/anianroid/thirdshift/internal/shared/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	CurrencyUSDMicro = "USD_MICRO"

	AccountCustomerUsage        = "customer_usage_charge"
	AccountHostPendingCredit    = "host_pending_credit"
	AccountHostAvailableCredit  = "host_available_credit"
	AccountPlatformMargin       = "platform_margin"
	AccountPlatformVerification = "platform_verification_overhead"
	AccountPlatformPayoutCash   = "platform_payout_cash"
	AccountPlatformFailed       = "platform_failed_attempt_overhead"

	TransactionJobAcceptance       = "job_acceptance"
	TransactionHostCreditRelease   = "host_credit_release"
	TransactionHostPayout          = "host_payout"
	TransactionLedgerReversal      = "ledger_reversal"
	TransactionVerificationCost    = "verification_overhead"
	TransactionFailedAttemptCost   = "failed_attempt_overhead"
	DefaultPlatformLedgerOwnerID   = "platform_thirdshift"
	DefaultPayoutMemoPrefix        = "Thirdshift host payout"
	DefaultCreditHoldDurationHours = 24
)

var (
	ErrDuplicatePosting  = errors.New("ledger posting already exists")
	ErrNoAvailableCredit = errors.New("no available host credit")
)

type Store struct {
	Pool *pgxpool.Pool
}

type queryer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type AcceptedJobPosting struct {
	JobID                            string
	AttemptID                        string
	ReceivedAt                       time.Time
	CreditHold                       time.Duration
	PromptTokens                     int
	CompletionTokens                 int
	CoordinatorDurationMillis        int64
	VerificationOverheadMicrodollars int64
}

type VerificationOverheadPosting struct {
	JobID                     string
	AttemptID                 string
	ReceivedAt                time.Time
	CreditHold                time.Duration
	CompletionTokens          int
	CoordinatorDurationMillis int64
}

type AcceptedJobLedger struct {
	TransactionID         string
	CreditHoldID          string
	CustomerCharge        int64
	HostCredit            int64
	PlatformMargin        int64
	PriceVersion          string
	AvailableAt           time.Time
	CoordinatorDurationMS int64
}

type PayoutBatch struct {
	ID                  string
	Status              string
	TotalMicrodollars   int64
	ItemCount           int
	TransactionID       string
	ExportedCSVChecksum string
}

type EconomicsReport struct {
	CustomerRevenueMicrodollars       int64
	HostCreditsMicrodollars           int64
	VerificationOverheadMicrodollars  int64
	FailedAttemptOverheadMicrodollars int64
	ContributionMarginMicrodollars    int64
}

func (s Store) PostAcceptedJobTx(ctx context.Context, tx pgx.Tx, posting AcceptedJobPosting) (AcceptedJobLedger, error) {
	return postAcceptedJob(ctx, tx, posting)
}

func (s Store) PostAcceptedJob(ctx context.Context, posting AcceptedJobPosting) (AcceptedJobLedger, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("begin accepted job ledger posting: %w", err)
	}
	defer tx.Rollback(context.Background())
	result, err := postAcceptedJob(ctx, tx, posting)
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("commit accepted job ledger posting: %w", err)
	}
	return result, nil
}

func (s Store) PostVerificationOverhead(ctx context.Context, posting VerificationOverheadPosting) (AcceptedJobLedger, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("begin verification overhead ledger posting: %w", err)
	}
	defer tx.Rollback(context.Background())
	result, err := postVerificationOverhead(ctx, tx, posting)
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("commit verification overhead ledger posting: %w", err)
	}
	return result, nil
}

func postAcceptedJob(ctx context.Context, q queryer, posting AcceptedJobPosting) (AcceptedJobLedger, error) {
	if posting.ReceivedAt.IsZero() {
		posting.ReceivedAt = time.Now().UTC()
	}
	if posting.CreditHold <= 0 {
		posting.CreditHold = DefaultCreditHoldDurationHours * time.Hour
	}
	var orgID, nodeID, priceVersion string
	var inputPrice, outputPrice, hostPrice int64
	err := q.QueryRow(ctx, `
SELECT j.organization_id, ja.node_id, mp.price_version,
       mp.customer_input_per_million_microdollars,
       mp.customer_output_per_million_microdollars,
       mp.host_credit_per_million_output_microdollars
FROM jobs j
JOIN job_attempts ja ON ja.job_id = j.id AND ja.id = $2
JOIN model_versions mv ON mv.model_id = j.model_id
JOIN LATERAL (
  SELECT *
  FROM model_prices
  WHERE model_version_id = mv.id
    AND effective_from <= $3
    AND (effective_until IS NULL OR effective_until > $3)
  ORDER BY effective_from DESC
  LIMIT 1
) mp ON true
WHERE j.id = $1
ORDER BY mv.created_at DESC
LIMIT 1
`, posting.JobID, posting.AttemptID, posting.ReceivedAt).Scan(&orgID, &nodeID, &priceVersion, &inputPrice, &outputPrice, &hostPrice)
	if err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("load accepted job pricing: %w", err)
	}

	customerCharge := chargeForTokens(posting.PromptTokens, inputPrice) + chargeForTokens(posting.CompletionTokens, outputPrice)
	hostCredit := chargeForTokens(posting.CompletionTokens, hostPrice)
	verificationOverhead := posting.VerificationOverheadMicrodollars
	if verificationOverhead < 0 {
		verificationOverhead = 0
	}
	platformMargin := customerCharge - hostCredit - verificationOverhead
	txID, err := ids.New("ltx")
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	metadata := fmt.Sprintf(`{"prompt_tokens":%d,"completion_tokens":%d,"coordinator_duration_millis":%d,"price_version":%q}`,
		posting.PromptTokens, posting.CompletionTokens, posting.CoordinatorDurationMillis, priceVersion)
	tag, err := q.Exec(ctx, `
INSERT INTO ledger_transactions (id, transaction_type, status, reference_type, reference_id, memo, metadata, created_at)
VALUES ($1, $2, 'draft', 'job', $3, 'accepted job', $4::jsonb, $5)
ON CONFLICT DO NOTHING
`, txID, TransactionJobAcceptance, posting.JobID, metadata, posting.ReceivedAt)
	if err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("insert accepted job ledger transaction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return AcceptedJobLedger{}, ErrDuplicatePosting
	}

	customerAccount, err := ensureAccount(ctx, q, "customer", orgID, AccountCustomerUsage)
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	hostPendingAccount, err := ensureAccount(ctx, q, "host", nodeID, AccountHostPendingCredit)
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	platformMarginAccount, err := ensureAccount(ctx, q, "platform", DefaultPlatformLedgerOwnerID, AccountPlatformMargin)
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	if err := insertEntry(ctx, q, txID, customerAccount, customerCharge, "customer usage charge", posting.ReceivedAt); err != nil {
		return AcceptedJobLedger{}, err
	}
	if err := insertEntry(ctx, q, txID, hostPendingAccount, -hostCredit, "host pending credit", posting.ReceivedAt); err != nil {
		return AcceptedJobLedger{}, err
	}
	if verificationOverhead > 0 {
		verificationAccount, err := ensureAccount(ctx, q, "platform", DefaultPlatformLedgerOwnerID, AccountPlatformVerification)
		if err != nil {
			return AcceptedJobLedger{}, err
		}
		if err := insertEntry(ctx, q, txID, verificationAccount, verificationOverhead, "verification overhead", posting.ReceivedAt); err != nil {
			return AcceptedJobLedger{}, err
		}
	}
	if err := insertEntry(ctx, q, txID, platformMarginAccount, -platformMargin, "platform margin", posting.ReceivedAt); err != nil {
		return AcceptedJobLedger{}, err
	}
	if _, err := q.Exec(ctx, "UPDATE ledger_transactions SET status = 'posted', posted_at = $2 WHERE id = $1", txID, posting.ReceivedAt); err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("post accepted job ledger transaction: %w", err)
	}

	holdID, err := ids.New("hcredit")
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	availableAt := posting.ReceivedAt.Add(posting.CreditHold)
	if _, err := q.Exec(ctx, `
INSERT INTO host_credit_holds (id, node_id, job_id, attempt_id, ledger_transaction_id, amount_microdollars, state, available_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, $8, $8)
`, holdID, nodeID, posting.JobID, posting.AttemptID, txID, hostCredit, availableAt, posting.ReceivedAt); err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("insert host credit hold: %w", err)
	}
	return AcceptedJobLedger{
		TransactionID:         txID,
		CreditHoldID:          holdID,
		CustomerCharge:        customerCharge,
		HostCredit:            hostCredit,
		PlatformMargin:        platformMargin,
		PriceVersion:          priceVersion,
		AvailableAt:           availableAt,
		CoordinatorDurationMS: posting.CoordinatorDurationMillis,
	}, nil
}

func postVerificationOverhead(ctx context.Context, q queryer, posting VerificationOverheadPosting) (AcceptedJobLedger, error) {
	if posting.ReceivedAt.IsZero() {
		posting.ReceivedAt = time.Now().UTC()
	}
	if posting.CreditHold <= 0 {
		posting.CreditHold = DefaultCreditHoldDurationHours * time.Hour
	}
	var nodeID, priceVersion string
	var hostPrice int64
	err := q.QueryRow(ctx, `
SELECT ja.node_id, mp.price_version, mp.host_credit_per_million_output_microdollars
FROM jobs j
JOIN job_attempts ja ON ja.job_id = j.id AND ja.id = $2
JOIN model_versions mv ON mv.model_id = j.model_id
JOIN LATERAL (
  SELECT *
  FROM model_prices
  WHERE model_version_id = mv.id
    AND effective_from <= $3
    AND (effective_until IS NULL OR effective_until > $3)
  ORDER BY effective_from DESC
  LIMIT 1
) mp ON true
WHERE j.id = $1
ORDER BY mv.created_at DESC
LIMIT 1
`, posting.JobID, posting.AttemptID, posting.ReceivedAt).Scan(&nodeID, &priceVersion, &hostPrice)
	if err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("load verification overhead pricing: %w", err)
	}
	hostCredit := chargeForTokens(posting.CompletionTokens, hostPrice)
	txID, err := ids.New("ltx")
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	metadata := fmt.Sprintf(`{"completion_tokens":%d,"coordinator_duration_millis":%d,"price_version":%q}`,
		posting.CompletionTokens, posting.CoordinatorDurationMillis, priceVersion)
	if _, err := q.Exec(ctx, `
INSERT INTO ledger_transactions (id, transaction_type, status, reference_type, reference_id, memo, metadata, created_at)
VALUES ($1, $2, 'draft', 'job', $3, 'verification overhead', $4::jsonb, $5)
`, txID, TransactionVerificationCost, posting.JobID, metadata, posting.ReceivedAt); err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("insert verification overhead ledger transaction: %w", err)
	}
	verificationAccount, err := ensureAccount(ctx, q, "platform", DefaultPlatformLedgerOwnerID, AccountPlatformVerification)
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	hostPendingAccount, err := ensureAccount(ctx, q, "host", nodeID, AccountHostPendingCredit)
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	if err := insertEntry(ctx, q, txID, verificationAccount, hostCredit, "verification host credit overhead", posting.ReceivedAt); err != nil {
		return AcceptedJobLedger{}, err
	}
	if err := insertEntry(ctx, q, txID, hostPendingAccount, -hostCredit, "host pending verification credit", posting.ReceivedAt); err != nil {
		return AcceptedJobLedger{}, err
	}
	if _, err := q.Exec(ctx, "UPDATE ledger_transactions SET status = 'posted', posted_at = $2 WHERE id = $1", txID, posting.ReceivedAt); err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("post verification overhead ledger transaction: %w", err)
	}

	holdID, err := ids.New("hcredit")
	if err != nil {
		return AcceptedJobLedger{}, err
	}
	availableAt := posting.ReceivedAt.Add(posting.CreditHold)
	if _, err := q.Exec(ctx, `
INSERT INTO host_credit_holds (id, node_id, job_id, attempt_id, ledger_transaction_id, amount_microdollars, state, available_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, $8, $8)
`, holdID, nodeID, posting.JobID, posting.AttemptID, txID, hostCredit, availableAt, posting.ReceivedAt); err != nil {
		return AcceptedJobLedger{}, fmt.Errorf("insert verification host credit hold: %w", err)
	}
	return AcceptedJobLedger{
		TransactionID:         txID,
		CreditHoldID:          holdID,
		HostCredit:            hostCredit,
		PriceVersion:          priceVersion,
		AvailableAt:           availableAt,
		CoordinatorDurationMS: posting.CoordinatorDurationMillis,
	}, nil
}

func (s Store) PromoteAvailableCredits(ctx context.Context, now time.Time) (int64, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin credit release: %w", err)
	}
	defer tx.Rollback(context.Background())
	rows, err := tx.Query(ctx, `
SELECT id, node_id, amount_microdollars
FROM host_credit_holds
WHERE state = 'pending' AND available_at <= $1
ORDER BY available_at, id
FOR UPDATE
`, now)
	if err != nil {
		return 0, fmt.Errorf("select releasable credits: %w", err)
	}
	type releasableCredit struct {
		holdID string
		nodeID string
		amount int64
	}
	var credits []releasableCredit
	for rows.Next() {
		var credit releasableCredit
		if err := rows.Scan(&credit.holdID, &credit.nodeID, &credit.amount); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan releasable credit: %w", err)
		}
		credits = append(credits, credit)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate releasable credits: %w", err)
	}
	var released int64
	for _, credit := range credits {
		txID, err := ids.New("ltx")
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO ledger_transactions (id, transaction_type, status, reference_type, reference_id, memo, created_at)
VALUES ($1, $2, 'draft', 'host_credit_hold', $3, 'host credit hold released', $4)
`, txID, TransactionHostCreditRelease, credit.holdID, now); err != nil {
			return 0, fmt.Errorf("insert credit release transaction: %w", err)
		}
		pendingAccount, err := ensureAccount(ctx, tx, "host", credit.nodeID, AccountHostPendingCredit)
		if err != nil {
			return 0, err
		}
		availableAccount, err := ensureAccount(ctx, tx, "host", credit.nodeID, AccountHostAvailableCredit)
		if err != nil {
			return 0, err
		}
		if err := insertEntry(ctx, tx, txID, pendingAccount, credit.amount, "release pending host credit", now); err != nil {
			return 0, err
		}
		if err := insertEntry(ctx, tx, txID, availableAccount, -credit.amount, "host available credit", now); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, "UPDATE ledger_transactions SET status = 'posted', posted_at = $2 WHERE id = $1", txID, now); err != nil {
			return 0, fmt.Errorf("post credit release transaction: %w", err)
		}
		if _, err := tx.Exec(ctx, "UPDATE host_credit_holds SET state = 'available', updated_at = $2 WHERE id = $1", credit.holdID, now); err != nil {
			return 0, fmt.Errorf("mark credit available: %w", err)
		}
		released++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit credit release: %w", err)
	}
	return released, nil
}

func (s Store) ReverseTransaction(ctx context.Context, transactionID, memo string, now time.Time) (string, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", fmt.Errorf("begin reversal: %w", err)
	}
	defer tx.Rollback(context.Background())
	reversalID, err := reverseTransaction(ctx, tx, transactionID, memo, now)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit reversal: %w", err)
	}
	return reversalID, nil
}

func reverseTransaction(ctx context.Context, q queryer, transactionID, memo string, now time.Time) (string, error) {
	reversalID, err := ids.New("ltx")
	if err != nil {
		return "", err
	}
	if memo == "" {
		memo = "ledger reversal"
	}
	if _, err := q.Exec(ctx, `
INSERT INTO ledger_transactions (id, transaction_type, status, reference_type, reference_id, memo, reverses_transaction_id, created_at)
SELECT $1, $2, 'draft', reference_type, reference_id, $3, id, $4
FROM ledger_transactions
WHERE id = $5 AND status = 'posted'
`, reversalID, TransactionLedgerReversal, memo, now, transactionID); err != nil {
		return "", fmt.Errorf("insert reversal transaction: %w", err)
	}
	rows, err := q.Query(ctx, "SELECT account_id, amount_microdollars, currency, COALESCE(memo, '') FROM ledger_entries WHERE transaction_id = $1 ORDER BY id", transactionID)
	if err != nil {
		return "", fmt.Errorf("select original ledger entries: %w", err)
	}
	type reversalEntry struct {
		accountID string
		currency  string
		memo      string
		amount    int64
	}
	var entries []reversalEntry
	for rows.Next() {
		var entry reversalEntry
		if err := rows.Scan(&entry.accountID, &entry.amount, &entry.currency, &entry.memo); err != nil {
			rows.Close()
			return "", fmt.Errorf("scan original ledger entry: %w", err)
		}
		entries = append(entries, entry)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate original ledger entries: %w", err)
	}
	for _, entry := range entries {
		entryID, err := ids.New("le")
		if err != nil {
			return "", err
		}
		if _, err := q.Exec(ctx, `
INSERT INTO ledger_entries (id, transaction_id, account_id, amount_microdollars, currency, memo, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
`, entryID, reversalID, entry.accountID, -entry.amount, entry.currency, "reversal: "+entry.memo, now); err != nil {
			return "", fmt.Errorf("insert reversal entry: %w", err)
		}
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("posted transaction %s has no entries", transactionID)
	}
	if _, err := q.Exec(ctx, "UPDATE ledger_transactions SET status = 'posted', posted_at = $2 WHERE id = $1", reversalID, now); err != nil {
		return "", fmt.Errorf("post reversal transaction: %w", err)
	}
	return reversalID, nil
}

func (s Store) VoidPayoutBatch(ctx context.Context, batchID, memo string, now time.Time) (PayoutBatch, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PayoutBatch{}, fmt.Errorf("begin payout void: %w", err)
	}
	defer tx.Rollback(context.Background())
	var batch PayoutBatch
	if err := tx.QueryRow(ctx, "SELECT id, status, total_microdollars FROM payout_batches WHERE id = $1 FOR UPDATE", batchID).Scan(&batch.ID, &batch.Status, &batch.TotalMicrodollars); err != nil {
		return PayoutBatch{}, fmt.Errorf("select payout batch: %w", err)
	}
	if batch.Status == "void" {
		return batch, tx.Commit(ctx)
	}
	if batch.Status == "paid" {
		var payoutTxID sql.NullString
		if err := tx.QueryRow(ctx, `
SELECT ledger_transaction_id
FROM payout_items
WHERE payout_batch_id = $1 AND ledger_transaction_id IS NOT NULL
LIMIT 1
`, batchID).Scan(&payoutTxID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return PayoutBatch{}, fmt.Errorf("select payout transaction for reversal: %w", err)
		}
		if payoutTxID.Valid {
			reversalID, err := reverseTransaction(ctx, tx, payoutTxID.String, firstNonEmpty(memo, "void payout batch"), now)
			if err != nil {
				return PayoutBatch{}, err
			}
			batch.TransactionID = reversalID
		}
		if _, err := tx.Exec(ctx, "UPDATE host_credit_holds SET state = 'reversed', updated_at = $2 WHERE payout_batch_id = $1", batchID, now); err != nil {
			return PayoutBatch{}, fmt.Errorf("mark paid payout credits reversed: %w", err)
		}
	} else {
		if _, err := tx.Exec(ctx, `
UPDATE host_credit_holds
SET state = 'available', payout_batch_id = NULL, updated_at = $2
WHERE payout_batch_id = $1 AND state = 'payout_pending'
`, batchID, now); err != nil {
			return PayoutBatch{}, fmt.Errorf("release reserved payout credits: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, "UPDATE payout_items SET status = 'void' WHERE payout_batch_id = $1 AND status <> 'void'", batchID); err != nil {
		return PayoutBatch{}, fmt.Errorf("void payout items: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE payout_batches SET status = 'void' WHERE id = $1", batchID); err != nil {
		return PayoutBatch{}, fmt.Errorf("void payout batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PayoutBatch{}, fmt.Errorf("commit payout void: %w", err)
	}
	batch.Status = "void"
	return batch, nil
}

func (s Store) CreatePayoutBatch(ctx context.Context, organizationID string, now time.Time) (PayoutBatch, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PayoutBatch{}, fmt.Errorf("begin payout batch: %w", err)
	}
	defer tx.Rollback(context.Background())
	batchID, err := ids.New("payout")
	if err != nil {
		return PayoutBatch{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO payout_batches (id, organization_id, status, currency, created_at)
VALUES ($1, NULLIF($2, ''), 'draft', $3, $4)
`, batchID, organizationID, CurrencyUSDMicro, now); err != nil {
		return PayoutBatch{}, fmt.Errorf("insert payout batch: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT h.id, h.node_id, h.amount_microdollars
FROM host_credit_holds h
JOIN nodes n ON n.id = h.node_id
WHERE h.state = 'available'
  AND h.amount_microdollars > 0
  AND (NULLIF($1, '') IS NULL OR n.organization_id = $1)
ORDER BY h.node_id, h.created_at, h.id
FOR UPDATE
`, organizationID)
	if err != nil {
		return PayoutBatch{}, fmt.Errorf("select payout credits: %w", err)
	}
	defer rows.Close()
	type payoutCredit struct {
		holdID string
		nodeID string
		amount int64
	}
	var credits []payoutCredit
	for rows.Next() {
		var credit payoutCredit
		if err := rows.Scan(&credit.holdID, &credit.nodeID, &credit.amount); err != nil {
			rows.Close()
			return PayoutBatch{}, fmt.Errorf("scan payout credit: %w", err)
		}
		credits = append(credits, credit)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PayoutBatch{}, fmt.Errorf("iterate payout credits: %w", err)
	}
	var total int64
	items := 0
	for _, credit := range credits {
		itemID, err := ids.New("pitem")
		if err != nil {
			return PayoutBatch{}, err
		}
		memo := fmt.Sprintf("%s %s", DefaultPayoutMemoPrefix, batchID)
		if _, err := tx.Exec(ctx, `
INSERT INTO payout_items (id, payout_batch_id, host_node_id, credit_hold_id, amount_microdollars, currency, external_reference, status, memo, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $3, 'pending', $7, $8)
`, itemID, batchID, credit.nodeID, credit.holdID, credit.amount, CurrencyUSDMicro, memo, now); err != nil {
			return PayoutBatch{}, fmt.Errorf("insert payout item: %w", err)
		}
		if _, err := tx.Exec(ctx, "UPDATE host_credit_holds SET state = 'payout_pending', payout_batch_id = $2, updated_at = $3 WHERE id = $1", credit.holdID, batchID, now); err != nil {
			return PayoutBatch{}, fmt.Errorf("reserve payout credit: %w", err)
		}
		total += credit.amount
		items++
	}
	if items == 0 {
		return PayoutBatch{}, ErrNoAvailableCredit
	}
	if _, err := tx.Exec(ctx, "UPDATE payout_batches SET total_microdollars = $2 WHERE id = $1", batchID, total); err != nil {
		return PayoutBatch{}, fmt.Errorf("update payout batch total: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PayoutBatch{}, fmt.Errorf("commit payout batch: %w", err)
	}
	return PayoutBatch{ID: batchID, Status: "draft", TotalMicrodollars: total, ItemCount: items}, nil
}

func (s Store) ExportPayoutBatch(ctx context.Context, batchID string, now time.Time) ([]byte, PayoutBatch, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, PayoutBatch{}, fmt.Errorf("begin payout export: %w", err)
	}
	defer tx.Rollback(context.Background())
	var batch PayoutBatch
	if err := tx.QueryRow(ctx, "SELECT id, status, total_microdollars FROM payout_batches WHERE id = $1 FOR UPDATE", batchID).Scan(&batch.ID, &batch.Status, &batch.TotalMicrodollars); err != nil {
		return nil, PayoutBatch{}, fmt.Errorf("select payout batch: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT COALESCE(host_node_id, ''), COALESCE(external_reference, ''), amount_microdollars, COALESCE(memo, '')
FROM payout_items
WHERE payout_batch_id = $1 AND status <> 'void'
ORDER BY host_node_id, id
`, batchID)
	if err != nil {
		return nil, PayoutBatch{}, fmt.Errorf("select payout export rows: %w", err)
	}
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"host_id", "account_reference", "amount_microdollars", "memo"}); err != nil {
		return nil, PayoutBatch{}, err
	}
	items := 0
	for rows.Next() {
		var hostID, reference, memo string
		var amount int64
		if err := rows.Scan(&hostID, &reference, &amount, &memo); err != nil {
			rows.Close()
			return nil, PayoutBatch{}, fmt.Errorf("scan payout export row: %w", err)
		}
		if err := writer.Write([]string{hostID, reference, strconv.FormatInt(amount, 10), memo}); err != nil {
			rows.Close()
			return nil, PayoutBatch{}, err
		}
		items++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, PayoutBatch{}, fmt.Errorf("iterate payout export rows: %w", err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, PayoutBatch{}, err
	}
	sum := sha256.Sum256(buf.Bytes())
	checksum := "sha256:" + hex.EncodeToString(sum[:])
	if batch.Status == "draft" {
		if _, err := tx.Exec(ctx, "UPDATE payout_items SET status = 'exported' WHERE payout_batch_id = $1 AND status = 'pending'", batchID); err != nil {
			return nil, PayoutBatch{}, fmt.Errorf("mark payout items exported: %w", err)
		}
		if _, err := tx.Exec(ctx, "UPDATE payout_batches SET status = 'exported', exported_at = $2 WHERE id = $1", batchID, now); err != nil {
			return nil, PayoutBatch{}, fmt.Errorf("mark payout batch exported: %w", err)
		}
		batch.Status = "exported"
	}
	batch.ItemCount = items
	batch.ExportedCSVChecksum = checksum
	if err := tx.Commit(ctx); err != nil {
		return nil, PayoutBatch{}, fmt.Errorf("commit payout export: %w", err)
	}
	return buf.Bytes(), batch, nil
}

func (s Store) ConfirmPayoutBatch(ctx context.Context, batchID string, confirmation io.Reader, now time.Time) (PayoutBatch, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	reader := csv.NewReader(confirmation)
	records, err := reader.ReadAll()
	if err != nil {
		return PayoutBatch{}, fmt.Errorf("read payout confirmation CSV: %w", err)
	}
	if len(records) < 2 {
		return PayoutBatch{}, fmt.Errorf("payout confirmation CSV has no item rows")
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PayoutBatch{}, fmt.Errorf("begin payout confirm: %w", err)
	}
	defer tx.Rollback(context.Background())
	var batch PayoutBatch
	if err := tx.QueryRow(ctx, "SELECT id, status, total_microdollars FROM payout_batches WHERE id = $1 FOR UPDATE", batchID).Scan(&batch.ID, &batch.Status, &batch.TotalMicrodollars); err != nil {
		return PayoutBatch{}, fmt.Errorf("select payout batch: %w", err)
	}
	if batch.Status != "exported" {
		return PayoutBatch{}, fmt.Errorf("payout batch must be exported before confirmation")
	}
	expected := map[string]int64{}
	rows, err := tx.Query(ctx, "SELECT COALESCE(host_node_id, ''), amount_microdollars FROM payout_items WHERE payout_batch_id = $1 AND status = 'exported' ORDER BY host_node_id, id", batchID)
	if err != nil {
		return PayoutBatch{}, fmt.Errorf("select payout items: %w", err)
	}
	for rows.Next() {
		var hostID string
		var amount int64
		if err := rows.Scan(&hostID, &amount); err != nil {
			rows.Close()
			return PayoutBatch{}, fmt.Errorf("scan payout item: %w", err)
		}
		expected[hostID] += amount
	}
	rows.Close()
	for _, record := range records[1:] {
		if len(record) < 3 {
			return PayoutBatch{}, fmt.Errorf("payout confirmation row has fewer than 3 columns")
		}
		amount, err := strconv.ParseInt(record[2], 10, 64)
		if err != nil {
			return PayoutBatch{}, fmt.Errorf("parse payout confirmation amount: %w", err)
		}
		expected[record[0]] -= amount
	}
	for hostID, diff := range expected {
		if diff != 0 {
			return PayoutBatch{}, fmt.Errorf("payout confirmation mismatch for host %s: %d microdollars", hostID, diff)
		}
	}

	txID, err := ids.New("ltx")
	if err != nil {
		return PayoutBatch{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO ledger_transactions (id, transaction_type, status, reference_type, reference_id, memo, created_at)
VALUES ($1, $2, 'draft', 'payout_batch', $3, 'host payout paid', $4)
`, txID, TransactionHostPayout, batchID, now); err != nil {
		return PayoutBatch{}, fmt.Errorf("insert payout transaction: %w", err)
	}
	rows, err = tx.Query(ctx, "SELECT id, COALESCE(host_node_id, ''), amount_microdollars FROM payout_items WHERE payout_batch_id = $1 AND status = 'exported' ORDER BY id", batchID)
	if err != nil {
		return PayoutBatch{}, fmt.Errorf("select payout ledger items: %w", err)
	}
	type payoutItem struct {
		itemID string
		nodeID string
		amount int64
	}
	var payoutItems []payoutItem
	for rows.Next() {
		var item payoutItem
		if err := rows.Scan(&item.itemID, &item.nodeID, &item.amount); err != nil {
			rows.Close()
			return PayoutBatch{}, fmt.Errorf("scan payout ledger item: %w", err)
		}
		payoutItems = append(payoutItems, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PayoutBatch{}, fmt.Errorf("iterate payout ledger items: %w", err)
	}
	total := int64(0)
	for _, item := range payoutItems {
		availableAccount, err := ensureAccount(ctx, tx, "host", item.nodeID, AccountHostAvailableCredit)
		if err != nil {
			return PayoutBatch{}, err
		}
		if err := insertEntry(ctx, tx, txID, availableAccount, item.amount, "host payout consumed available credit", now); err != nil {
			return PayoutBatch{}, err
		}
		if _, err := tx.Exec(ctx, "UPDATE payout_items SET status = 'paid', ledger_transaction_id = $2 WHERE id = $1", item.itemID, txID); err != nil {
			return PayoutBatch{}, fmt.Errorf("mark payout item paid: %w", err)
		}
		total += item.amount
	}
	if total <= 0 {
		return PayoutBatch{}, fmt.Errorf("payout batch has no payable total")
	}
	platformCashAccount, err := ensureAccount(ctx, tx, "platform", DefaultPlatformLedgerOwnerID, AccountPlatformPayoutCash)
	if err != nil {
		return PayoutBatch{}, err
	}
	if err := insertEntry(ctx, tx, txID, platformCashAccount, -total, "manual payout cash out", now); err != nil {
		return PayoutBatch{}, err
	}
	if _, err := tx.Exec(ctx, "UPDATE ledger_transactions SET status = 'posted', posted_at = $2 WHERE id = $1", txID, now); err != nil {
		return PayoutBatch{}, fmt.Errorf("post payout ledger transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE host_credit_holds SET state = 'paid', updated_at = $2 WHERE payout_batch_id = $1 AND state = 'payout_pending'", batchID, now); err != nil {
		return PayoutBatch{}, fmt.Errorf("mark payout credits paid: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE payout_batches SET status = 'paid', paid_at = $2 WHERE id = $1", batchID, now); err != nil {
		return PayoutBatch{}, fmt.Errorf("mark payout batch paid: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PayoutBatch{}, fmt.Errorf("commit payout confirm: %w", err)
	}
	batch.Status = "paid"
	batch.TransactionID = txID
	return batch, nil
}

func (s Store) EconomicsReport(ctx context.Context, from, until time.Time) (EconomicsReport, error) {
	if from.IsZero() {
		from = time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if until.IsZero() {
		until = time.Now().UTC().Add(time.Second)
	}
	rows, err := s.Pool.Query(ctx, `
SELECT la.account_type, lt.transaction_type, COALESCE(SUM(le.amount_microdollars), 0)
FROM ledger_entries le
JOIN ledger_transactions lt ON lt.id = le.transaction_id AND lt.status = 'posted'
JOIN ledger_accounts la ON la.id = le.account_id
WHERE COALESCE(lt.posted_at, lt.created_at) >= $1
  AND COALESCE(lt.posted_at, lt.created_at) < $2
GROUP BY la.account_type, lt.transaction_type
`, from, until)
	if err != nil {
		return EconomicsReport{}, fmt.Errorf("query economics report: %w", err)
	}
	defer rows.Close()
	amounts := map[string]int64{}
	for rows.Next() {
		var accountType, transactionType string
		var amount int64
		if err := rows.Scan(&accountType, &transactionType, &amount); err != nil {
			return EconomicsReport{}, fmt.Errorf("scan economics report: %w", err)
		}
		key := transactionType + ":" + accountType
		amounts[key] = amount
	}
	if err := rows.Err(); err != nil {
		return EconomicsReport{}, fmt.Errorf("iterate economics report: %w", err)
	}
	report := EconomicsReport{
		CustomerRevenueMicrodollars:       amounts[TransactionJobAcceptance+":"+AccountCustomerUsage],
		HostCreditsMicrodollars:           -amounts[TransactionJobAcceptance+":"+AccountHostPendingCredit],
		VerificationOverheadMicrodollars:  amounts[TransactionJobAcceptance+":"+AccountPlatformVerification] + amounts[TransactionVerificationCost+":"+AccountPlatformVerification],
		FailedAttemptOverheadMicrodollars: amounts[TransactionFailedAttemptCost+":"+AccountPlatformFailed],
	}
	report.ContributionMarginMicrodollars = report.CustomerRevenueMicrodollars - report.HostCreditsMicrodollars - report.VerificationOverheadMicrodollars - report.FailedAttemptOverheadMicrodollars
	return report, nil
}

func ensureAccount(ctx context.Context, q queryer, ownerType, ownerID, accountType string) (string, error) {
	accountID, err := ids.New("acct")
	if err != nil {
		return "", err
	}
	var actual string
	if err := q.QueryRow(ctx, `
INSERT INTO ledger_accounts (id, owner_type, owner_id, account_type, currency, created_at)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (owner_type, owner_id, account_type, currency) DO UPDATE
SET status = 'active'
RETURNING id
`, accountID, ownerType, ownerID, accountType, CurrencyUSDMicro).Scan(&actual); err != nil {
		return "", fmt.Errorf("ensure ledger account %s/%s/%s: %w", ownerType, ownerID, accountType, err)
	}
	return actual, nil
}

func insertEntry(ctx context.Context, q queryer, txID, accountID string, amount int64, memo string, now time.Time) error {
	entryID, err := ids.New("le")
	if err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `
INSERT INTO ledger_entries (id, transaction_id, account_id, amount_microdollars, currency, memo, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
`, entryID, txID, accountID, amount, CurrencyUSDMicro, memo, now); err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}
	return nil
}

func chargeForTokens(tokens int, perMillionMicrodollars int64) int64 {
	if tokens <= 0 || perMillionMicrodollars <= 0 {
		return 0
	}
	return (int64(tokens)*perMillionMicrodollars + 500_000) / 1_000_000
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
