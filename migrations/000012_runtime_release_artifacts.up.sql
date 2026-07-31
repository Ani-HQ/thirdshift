CREATE TABLE runtime_release_artifacts (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  runtime_release_id text NOT NULL REFERENCES runtime_releases(id) ON DELETE CASCADE,
  platform_key text NOT NULL,
  sha256 text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (runtime_release_id, platform_key)
);

-- Legacy rows: the single binary_sha256 column predates per-platform artifact
-- tracking. Seed it under a synthetic platform key so existing nodes stay
-- eligible until the next catalog sync repopulates real platforms.
INSERT INTO runtime_release_artifacts (id, runtime_release_id, platform_key, sha256)
SELECT 'rra_01J0M' || upper(substr(md5(id), 1, 21)), id, 'legacy', binary_sha256
FROM runtime_releases
ON CONFLICT DO NOTHING;
