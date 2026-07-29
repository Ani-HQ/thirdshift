CREATE TABLE invite_tokens (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  fleet_id text NOT NULL REFERENCES fleets(id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE,
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'used', 'expired', 'revoked')),
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  used_by_node_id text REFERENCES nodes(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (
    (status = 'used' AND used_at IS NOT NULL AND used_by_node_id IS NOT NULL)
    OR (status <> 'used' AND used_at IS NULL AND used_by_node_id IS NULL)
  )
);

CREATE INDEX invite_tokens_active_lookup
ON invite_tokens (token_hash, expires_at)
WHERE status = 'active';

CREATE TABLE node_bootstrap_tokens (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  node_id text NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX node_bootstrap_tokens_active_lookup
ON node_bootstrap_tokens (node_id, token_hash, expires_at)
WHERE used_at IS NULL;
