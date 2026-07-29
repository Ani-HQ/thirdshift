ALTER TABLE model_manifest_limits
ADD COLUMN price_version text NOT NULL DEFAULT 'alpha',
ADD COLUMN duplicate_sample_rate numeric(5,4) NOT NULL DEFAULT 0 CHECK (duplicate_sample_rate >= 0 AND duplicate_sample_rate <= 1),
ADD COLUMN challenge_rate numeric(5,4) NOT NULL DEFAULT 0 CHECK (challenge_rate >= 0 AND challenge_rate <= 1);

ALTER TABLE job_results
ADD COLUMN coordinator_duration_millis integer NOT NULL DEFAULT 0 CHECK (coordinator_duration_millis >= 0),
ADD COLUMN price_version text NOT NULL DEFAULT 'alpha',
ADD COLUMN metering_status text NOT NULL DEFAULT 'accepted' CHECK (metering_status IN ('accepted', 'flagged', 'rejected')),
ADD COLUMN verification_status text NOT NULL DEFAULT 'accepted' CHECK (verification_status IN ('accepted', 'pending', 'rejected'));

ALTER TABLE job_attempts
DROP CONSTRAINT job_attempts_status_check;

ALTER TABLE job_attempts
ADD CONSTRAINT job_attempts_status_check
CHECK (status IN ('offered', 'accepted', 'running', 'succeeded', 'failed', 'cancelled', 'expired', 'rejected', 'verified'));

ALTER TABLE ledger_transactions
ADD COLUMN reverses_transaction_id text REFERENCES ledger_transactions(id),
ADD COLUMN metadata jsonb NOT NULL DEFAULT '{}'::jsonb;

CREATE UNIQUE INDEX ledger_transactions_one_job_acceptance
ON ledger_transactions (reference_type, reference_id, transaction_type)
WHERE transaction_type = 'job_acceptance' AND status = 'posted';

ALTER TABLE node_reputation
ALTER COLUMN rolling_success_rate SET DEFAULT 0.6000,
ALTER COLUMN challenge_pass_rate SET DEFAULT 1.0000,
ALTER COLUMN duplicate_disagreement_rate SET DEFAULT 0.0000,
ALTER COLUMN session_stability SET DEFAULT 0.6000,
ADD COLUMN attempt_total bigint NOT NULL DEFAULT 0 CHECK (attempt_total >= 0),
ADD COLUMN attempt_failed bigint NOT NULL DEFAULT 0 CHECK (attempt_failed >= 0),
ADD COLUMN challenge_total bigint NOT NULL DEFAULT 0 CHECK (challenge_total >= 0),
ADD COLUMN challenge_failed bigint NOT NULL DEFAULT 0 CHECK (challenge_failed >= 0),
ADD COLUMN duplicate_total bigint NOT NULL DEFAULT 0 CHECK (duplicate_total >= 0),
ADD COLUMN duplicate_disagreements bigint NOT NULL DEFAULT 0 CHECK (duplicate_disagreements >= 0),
ADD COLUMN session_disconnects bigint NOT NULL DEFAULT 0 CHECK (session_disconnects >= 0),
ADD COLUMN last_quarantine_reason text;

CREATE TABLE host_credit_holds (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  node_id text NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  job_id text NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  attempt_id text NOT NULL REFERENCES job_attempts(id) ON DELETE CASCADE,
  ledger_transaction_id text NOT NULL REFERENCES ledger_transactions(id) ON DELETE RESTRICT,
  amount_microdollars bigint NOT NULL CHECK (amount_microdollars >= 0),
  currency text NOT NULL DEFAULT 'USD_MICRO',
  state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'available', 'payout_pending', 'paid', 'reversed')),
  available_at timestamptz NOT NULL,
  payout_batch_id text REFERENCES payout_batches(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (attempt_id)
);

CREATE INDEX host_credit_holds_state_available_idx
ON host_credit_holds (state, available_at);

CREATE INDEX host_credit_holds_node_state_idx
ON host_credit_holds (node_id, state);

ALTER TABLE payout_items
ADD COLUMN host_node_id text REFERENCES nodes(id) ON DELETE SET NULL,
ADD COLUMN credit_hold_id text REFERENCES host_credit_holds(id) ON DELETE RESTRICT,
ADD COLUMN memo text;

CREATE UNIQUE INDEX payout_items_one_credit_hold
ON payout_items (credit_hold_id)
WHERE credit_hold_id IS NOT NULL AND status <> 'void';

CREATE OR REPLACE FUNCTION prevent_exported_payout_item_mutation()
RETURNS trigger AS $$
DECLARE
  batch_status text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    SELECT status INTO batch_status FROM payout_batches WHERE id = OLD.payout_batch_id;
  ELSE
    SELECT status INTO batch_status FROM payout_batches WHERE id = NEW.payout_batch_id;
  END IF;

  IF TG_OP = 'DELETE' AND batch_status IN ('exported', 'paid', 'void') THEN
    RAISE EXCEPTION 'exported payout batch contents are immutable'
      USING ERRCODE = '25006';
  END IF;

  IF TG_OP = 'UPDATE' AND batch_status IN ('exported', 'paid', 'void') AND (
    NEW.payout_batch_id IS DISTINCT FROM OLD.payout_batch_id
    OR NEW.host_user_id IS DISTINCT FROM OLD.host_user_id
    OR NEW.host_node_id IS DISTINCT FROM OLD.host_node_id
    OR NEW.credit_hold_id IS DISTINCT FROM OLD.credit_hold_id
    OR NEW.amount_microdollars IS DISTINCT FROM OLD.amount_microdollars
    OR NEW.currency IS DISTINCT FROM OLD.currency
    OR NEW.external_reference IS DISTINCT FROM OLD.external_reference
    OR NEW.memo IS DISTINCT FROM OLD.memo
  ) THEN
    RAISE EXCEPTION 'exported payout batch contents are immutable'
      USING ERRCODE = '25006';
  END IF;

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER payout_items_no_exported_update_delete
BEFORE UPDATE OR DELETE ON payout_items
FOR EACH ROW
EXECUTE FUNCTION prevent_exported_payout_item_mutation();
