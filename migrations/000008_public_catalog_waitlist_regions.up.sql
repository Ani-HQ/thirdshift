ALTER TABLE fleets
ADD COLUMN region text,
ADD CONSTRAINT fleets_region_format_check
CHECK (region IS NULL OR region ~ '^[a-z0-9]+(-[a-z0-9]+)*$');

ALTER TABLE nodes
ADD COLUMN region text,
ADD CONSTRAINT nodes_region_format_check
CHECK (region IS NULL OR region ~ '^[a-z0-9]+(-[a-z0-9]+)*$');

ALTER TABLE models
ADD COLUMN description text;

CREATE INDEX fleets_region_idx ON fleets (region) WHERE region IS NOT NULL;
CREATE INDEX nodes_region_idx ON nodes (region) WHERE region IS NOT NULL;

CREATE TABLE waitlist_signups (
  id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*_[0-9A-HJKMNP-TV-Z]{26}$'),
  email text NOT NULL UNIQUE,
  use_case text,
  source text NOT NULL DEFAULT 'public_catalog',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX waitlist_signups_created_idx ON waitlist_signups (created_at DESC);
