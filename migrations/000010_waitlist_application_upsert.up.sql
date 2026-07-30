-- Applications are resubmittable: the same person may apply again for a model
-- (with a better use case) and may apply for several models. Uniqueness moves
-- from the email alone to (email, model_id) so a repeat application updates the
-- existing row instead of being silently dropped by ON CONFLICT DO NOTHING.
ALTER TABLE waitlist_signups
ADD COLUMN last_applied_at timestamptz;

-- Rows that predate this column were applied for exactly once, when created.
UPDATE waitlist_signups SET last_applied_at = created_at WHERE last_applied_at IS NULL;

ALTER TABLE waitlist_signups
ALTER COLUMN last_applied_at SET NOT NULL,
ALTER COLUMN last_applied_at SET DEFAULT now();

ALTER TABLE waitlist_signups
DROP CONSTRAINT waitlist_signups_email_key;

-- NULLS NOT DISTINCT is what makes this correct for general applications: a
-- NULL model_id means "no specific model", and without it two such rows for the
-- same email would both be allowed, reintroducing the duplicate-row bug for
-- every applicant who does not name a model. Legacy rows carry model_id NULL
-- and therefore become that applicant's general-application row.
ALTER TABLE waitlist_signups
ADD CONSTRAINT waitlist_signups_email_model_key UNIQUE NULLS NOT DISTINCT (email, model_id);

CREATE INDEX waitlist_signups_last_applied_idx ON waitlist_signups (last_applied_at DESC);
