-- Public listing semantics are separate from the operational model lifecycle in
-- models.status: a model can be routable but hidden from the public catalog, or
-- publicly listed as waitlist while no supply exists yet.
ALTER TABLE models
ADD COLUMN listing_status text NOT NULL DEFAULT 'live'
  CHECK (listing_status IN ('live', 'waitlist', 'hidden')),
ADD COLUMN expected_output_tokens_per_second numeric(10, 2)
  CHECK (expected_output_tokens_per_second IS NULL OR expected_output_tokens_per_second > 0),
ADD COLUMN market_typical_input_per_million_microdollars bigint
  CHECK (market_typical_input_per_million_microdollars IS NULL OR market_typical_input_per_million_microdollars > 0),
ADD COLUMN market_typical_output_per_million_microdollars bigint
  CHECK (market_typical_output_per_million_microdollars IS NULL OR market_typical_output_per_million_microdollars > 0),
ADD COLUMN market_comparison_source_note text;

ALTER TABLE models
ADD CONSTRAINT models_market_comparison_complete_check
CHECK (
  (market_typical_input_per_million_microdollars IS NULL) =
  (market_typical_output_per_million_microdollars IS NULL)
);

CREATE INDEX models_listing_status_idx ON models (listing_status);

-- The bare waitlist becomes a reviewed application: reviewers need the use
-- case, the volume band, and proof the applicant saw the data-class boundary.
ALTER TABLE waitlist_signups
ADD COLUMN name text CHECK (name IS NULL OR char_length(name) <= 200),
ADD COLUMN expected_volume text
  CHECK (expected_volume IS NULL OR expected_volume IN ('lt_1m', '1m_10m', '10m_100m', 'gt_100m')),
ADD COLUMN data_ack boolean NOT NULL DEFAULT false,
ADD COLUMN model_id text CHECK (model_id IS NULL OR char_length(model_id) <= 128);

CREATE INDEX waitlist_signups_model_idx ON waitlist_signups (model_id) WHERE model_id IS NOT NULL;
