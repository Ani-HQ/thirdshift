CREATE TABLE users (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  email text UNIQUE,
  display_name text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE organizations (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  name text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE fleets (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  organization_id text NOT NULL REFERENCES organizations(id),
  name text NOT NULL,
  enrollment_status text NOT NULL DEFAULT 'active' CHECK (enrollment_status IN ('active', 'paused', 'closed')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE nodes (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  organization_id text REFERENCES organizations(id),
  fleet_id text REFERENCES fleets(id),
  host_user_id text REFERENCES users(id),
  display_name text,
  state text NOT NULL DEFAULT 'UNREGISTERED',
  hardware_fingerprint_hash text,
  current_model_id text,
  current_model_version_id text,
  runtime_release_id text,
  quarantined_at timestamptz,
  registered_at timestamptz,
  last_seen_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CHECK (state IN ('UNREGISTERED', 'REGISTERING', 'OFFLINE', 'STARTING', 'BENCHMARKING', 'IDLE', 'PREPARING_MODEL', 'AVAILABLE', 'BUSY', 'DRAINING', 'PAUSED', 'ERROR', 'UPDATING'))
);

CREATE TABLE node_keys (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  node_id text NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  key_type text NOT NULL,
  public_key text NOT NULL,
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'rotated', 'revoked')),
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz,
  revoked_at timestamptz,
  UNIQUE (node_id, public_key)
);

CREATE TABLE node_hardware_snapshots (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  node_id text NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  gpu_name text NOT NULL,
  gpu_uuid_hash text,
  driver_version text,
  vram_total_mb integer NOT NULL CHECK (vram_total_mb >= 0),
  ram_total_mb integer CHECK (ram_total_mb >= 0),
  disk_free_mb integer CHECK (disk_free_mb >= 0),
  raw jsonb NOT NULL DEFAULT '{}'::jsonb,
  observed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE node_sessions (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  node_id text NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  protocol_version text NOT NULL,
  remote_addr text,
  state text NOT NULL DEFAULT 'connected' CHECK (state IN ('connected', 'draining', 'closed', 'stale')),
  connected_at timestamptz NOT NULL DEFAULT now(),
  last_heartbeat_at timestamptz,
  disconnected_at timestamptz
);

CREATE TABLE node_heartbeats (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  node_id text NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  session_id text REFERENCES node_sessions(id) ON DELETE SET NULL,
  sequence bigint NOT NULL CHECK (sequence >= 0),
  state text NOT NULL,
  model_id text,
  runtime_hash text,
  model_hash text,
  gpu jsonb NOT NULL DEFAULT '{}'::jsonb,
  active_job_id text,
  uptime_seconds bigint NOT NULL DEFAULT 0 CHECK (uptime_seconds >= 0),
  received_at timestamptz NOT NULL DEFAULT now(),
  sent_at timestamptz
);

CREATE TABLE node_reputation (
  node_id text PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  total_accepted_jobs bigint NOT NULL DEFAULT 0 CHECK (total_accepted_jobs >= 0),
  rolling_success_rate numeric(5,4) NOT NULL DEFAULT 0 CHECK (rolling_success_rate >= 0 AND rolling_success_rate <= 1),
  timeout_rate numeric(5,4) NOT NULL DEFAULT 0 CHECK (timeout_rate >= 0 AND timeout_rate <= 1),
  hash_mismatch_count bigint NOT NULL DEFAULT 0 CHECK (hash_mismatch_count >= 0),
  challenge_pass_rate numeric(5,4) NOT NULL DEFAULT 0 CHECK (challenge_pass_rate >= 0 AND challenge_pass_rate <= 1),
  duplicate_disagreement_rate numeric(5,4) NOT NULL DEFAULT 0 CHECK (duplicate_disagreement_rate >= 0 AND duplicate_disagreement_rate <= 1),
  session_stability numeric(5,4) NOT NULL DEFAULT 0 CHECK (session_stability >= 0 AND session_stability <= 1),
  quarantined_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE models (
  id text PRIMARY KEY,
  display_name text NOT NULL,
  status text NOT NULL DEFAULT 'alpha' CHECK (status IN ('alpha', 'active', 'deprecated', 'disabled')),
  data_class text NOT NULL DEFAULT 'public_or_non_sensitive',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE runtime_releases (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  engine text NOT NULL,
  version text NOT NULL,
  manifest_url text NOT NULL,
  binary_sha256 text NOT NULL,
  signature_key_id text NOT NULL,
  signature_value text NOT NULL,
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deprecated', 'revoked')),
  released_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (engine, version)
);

CREATE TABLE model_versions (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  model_id text NOT NULL REFERENCES models(id) ON DELETE CASCADE,
  version text NOT NULL,
  repository text,
  revision text,
  license_identifier text NOT NULL,
  manifest_sha256 text NOT NULL,
  runtime_release_id text REFERENCES runtime_releases(id),
  status text NOT NULL DEFAULT 'alpha' CHECK (status IN ('alpha', 'active', 'deprecated', 'disabled')),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (model_id, version)
);

CREATE TABLE model_artifacts (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  model_version_id text NOT NULL REFERENCES model_versions(id) ON DELETE CASCADE,
  artifact_type text NOT NULL,
  url text NOT NULL,
  sha256 text NOT NULL,
  byte_size bigint NOT NULL CHECK (byte_size > 0),
  signature_key_id text,
  signature_value text,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (model_version_id, artifact_type, sha256)
);

CREATE TABLE model_hardware_profiles (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  model_version_id text NOT NULL REFERENCES model_versions(id) ON DELETE CASCADE,
  hardware_class text NOT NULL,
  min_vram_mb integer NOT NULL CHECK (min_vram_mb > 0),
  min_ram_mb integer NOT NULL CHECK (min_ram_mb > 0),
  tokens_per_second numeric(10,2),
  max_context_tokens integer NOT NULL CHECK (max_context_tokens > 0),
  profile jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (model_version_id, hardware_class)
);

CREATE TABLE model_prices (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  model_version_id text NOT NULL REFERENCES model_versions(id) ON DELETE CASCADE,
  price_version text NOT NULL,
  customer_input_per_million_microdollars bigint NOT NULL CHECK (customer_input_per_million_microdollars >= 0),
  customer_output_per_million_microdollars bigint NOT NULL CHECK (customer_output_per_million_microdollars >= 0),
  host_credit_per_million_output_microdollars bigint NOT NULL CHECK (host_credit_per_million_output_microdollars >= 0),
  effective_from timestamptz NOT NULL,
  effective_until timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (model_version_id, price_version)
);

CREATE TABLE api_keys (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  organization_id text NOT NULL REFERENCES organizations(id),
  name text NOT NULL,
  key_hash text NOT NULL UNIQUE,
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'revoked')),
  quota jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz
);

CREATE TABLE jobs (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  organization_id text NOT NULL REFERENCES organizations(id),
  api_key_id text REFERENCES api_keys(id),
  model_id text NOT NULL REFERENCES models(id),
  state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'leased', 'running', 'verifying', 'succeeded', 'failed', 'cancelled')),
  priority text NOT NULL DEFAULT 'standard' CHECK (priority IN ('standard', 'low', 'high')),
  request_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  deadline_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);

CREATE TABLE job_attempts (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  job_id text NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  node_id text REFERENCES nodes(id),
  attempt_number integer NOT NULL CHECK (attempt_number > 0),
  lease_nonce text NOT NULL,
  lease_expires_at timestamptz NOT NULL,
  deadline_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'offered' CHECK (status IN ('offered', 'accepted', 'running', 'succeeded', 'failed', 'cancelled', 'expired', 'rejected')),
  transient_failure boolean NOT NULL DEFAULT false,
  error_code text,
  created_at timestamptz NOT NULL DEFAULT now(),
  accepted_at timestamptz,
  started_at timestamptz,
  finished_at timestamptz,
  UNIQUE (job_id, attempt_number),
  UNIQUE (lease_nonce)
);

CREATE UNIQUE INDEX job_attempts_one_success_per_job
ON job_attempts (job_id)
WHERE status = 'succeeded';

CREATE TABLE job_results (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  job_id text NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  attempt_id text NOT NULL REFERENCES job_attempts(id) ON DELETE CASCADE,
  model_hash text NOT NULL,
  runtime_hash text NOT NULL,
  prompt_tokens integer NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
  completion_tokens integer NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
  duration_millis integer NOT NULL DEFAULT 0 CHECK (duration_millis >= 0),
  response_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  accepted boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (attempt_id)
);

CREATE TABLE verification_events (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  job_id text NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  attempt_id text REFERENCES job_attempts(id) ON DELETE SET NULL,
  node_id text REFERENCES nodes(id) ON DELETE SET NULL,
  event_type text NOT NULL,
  status text NOT NULL CHECK (status IN ('pending', 'accepted', 'rejected', 'quarantined')),
  details jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_records (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  api_key_id text NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  endpoint text NOT NULL,
  idempotency_key text NOT NULL,
  request_hash text NOT NULL,
  response_status integer,
  response_metadata jsonb,
  job_id text REFERENCES jobs(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  UNIQUE (api_key_id, endpoint, idempotency_key)
);

CREATE TABLE ledger_accounts (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  owner_type text NOT NULL CHECK (owner_type IN ('customer', 'host', 'platform')),
  owner_id text NOT NULL,
  account_type text NOT NULL,
  currency text NOT NULL DEFAULT 'USD_MICRO',
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closed')),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (owner_type, owner_id, account_type, currency)
);

CREATE TABLE ledger_transactions (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  transaction_type text NOT NULL,
  status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'posted', 'void')),
  reference_type text,
  reference_id text,
  memo text,
  created_at timestamptz NOT NULL DEFAULT now(),
  posted_at timestamptz
);

CREATE TABLE ledger_entries (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  transaction_id text NOT NULL REFERENCES ledger_transactions(id) ON DELETE RESTRICT,
  account_id text NOT NULL REFERENCES ledger_accounts(id) ON DELETE RESTRICT,
  amount_microdollars bigint NOT NULL,
  currency text NOT NULL DEFAULT 'USD_MICRO',
  memo text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE payout_batches (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  organization_id text REFERENCES organizations(id),
  status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'exported', 'paid', 'void')),
  currency text NOT NULL DEFAULT 'USD_MICRO',
  total_microdollars bigint NOT NULL DEFAULT 0 CHECK (total_microdollars >= 0),
  created_by_user_id text REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  exported_at timestamptz,
  paid_at timestamptz
);

CREATE TABLE payout_items (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  payout_batch_id text NOT NULL REFERENCES payout_batches(id) ON DELETE CASCADE,
  host_user_id text REFERENCES users(id),
  ledger_transaction_id text REFERENCES ledger_transactions(id),
  amount_microdollars bigint NOT NULL CHECK (amount_microdollars >= 0),
  currency text NOT NULL DEFAULT 'USD_MICRO',
  external_reference text,
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'exported', 'paid', 'failed', 'void')),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE audit_events (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  actor_type text NOT NULL,
  actor_id text,
  action text NOT NULL,
  target_type text,
  target_id text,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE security_events (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  severity text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
  event_type text NOT NULL,
  node_id text REFERENCES nodes(id) ON DELETE SET NULL,
  organization_id text REFERENCES organizations(id) ON DELETE SET NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE operator_actions (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  operator_user_id text REFERENCES users(id),
  action text NOT NULL,
  target_type text NOT NULL,
  target_id text NOT NULL,
  reason text,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX nodes_state_idx ON nodes (state);
CREATE INDEX node_heartbeats_node_received_idx ON node_heartbeats (node_id, received_at DESC);
CREATE INDEX jobs_state_created_idx ON jobs (state, created_at);
CREATE INDEX job_attempts_job_idx ON job_attempts (job_id);
CREATE INDEX ledger_entries_transaction_idx ON ledger_entries (transaction_id);
CREATE INDEX ledger_entries_account_idx ON ledger_entries (account_id);
CREATE INDEX audit_events_created_idx ON audit_events (created_at DESC);
CREATE INDEX security_events_created_idx ON security_events (created_at DESC);

CREATE OR REPLACE FUNCTION assert_posted_ledger_transaction_balanced()
RETURNS trigger AS $$
DECLARE
  checked_transaction_id text;
  checked_status text;
  balance bigint;
BEGIN
  IF TG_TABLE_NAME = 'ledger_transactions' THEN
    checked_transaction_id := NEW.id;
    checked_status := NEW.status;
  ELSIF TG_OP = 'DELETE' THEN
    checked_transaction_id := OLD.transaction_id;
    SELECT status INTO checked_status FROM ledger_transactions WHERE id = checked_transaction_id;
  ELSE
    checked_transaction_id := NEW.transaction_id;
    SELECT status INTO checked_status FROM ledger_transactions WHERE id = checked_transaction_id;
  END IF;

  IF checked_status = 'posted' THEN
    SELECT COALESCE(SUM(amount_microdollars), 0)
    INTO balance
    FROM ledger_entries
    WHERE transaction_id = checked_transaction_id;

    IF balance <> 0 THEN
      RAISE EXCEPTION 'ledger transaction % does not balance to zero: % microdollars', checked_transaction_id, balance
        USING ERRCODE = '23514';
    END IF;
  END IF;

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION prevent_posted_ledger_entry_mutation()
RETURNS trigger AS $$
DECLARE
  checked_transaction_id text;
  checked_status text;
BEGIN
  IF TG_OP = 'DELETE' THEN
    checked_transaction_id := OLD.transaction_id;
  ELSE
    checked_transaction_id := NEW.transaction_id;
  END IF;

  SELECT status
  INTO checked_status
  FROM ledger_transactions
  WHERE id = checked_transaction_id;

  IF checked_status = 'posted' THEN
    RAISE EXCEPTION 'posted ledger entries are immutable; create a reversal transaction instead'
      USING ERRCODE = '25006';
  END IF;

  IF TG_OP = 'DELETE' THEN
    RETURN OLD;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE CONSTRAINT TRIGGER ledger_entries_balance_zero
AFTER INSERT OR UPDATE OR DELETE ON ledger_entries
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION assert_posted_ledger_transaction_balanced();

CREATE CONSTRAINT TRIGGER ledger_transactions_balance_zero
AFTER INSERT OR UPDATE OF status ON ledger_transactions
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
WHEN (NEW.status = 'posted')
EXECUTE FUNCTION assert_posted_ledger_transaction_balanced();

CREATE TRIGGER ledger_entries_no_posted_insert
BEFORE INSERT ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_posted_ledger_entry_mutation();

CREATE TRIGGER ledger_entries_no_posted_update_delete
BEFORE UPDATE OR DELETE ON ledger_entries
FOR EACH ROW
EXECUTE FUNCTION prevent_posted_ledger_entry_mutation();
