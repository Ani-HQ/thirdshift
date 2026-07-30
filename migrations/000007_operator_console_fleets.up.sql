ALTER TABLE fleets
ADD COLUMN schedule_from text,
ADD COLUMN schedule_until text,
ADD COLUMN schedule_timezone text NOT NULL DEFAULT 'local',
ADD COLUMN schedule_updated_at timestamptz,
ADD CONSTRAINT fleets_schedule_defaults_check
CHECK (
  (schedule_from IS NULL AND schedule_until IS NULL)
  OR (
    schedule_from ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'
    AND schedule_until ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'
  )
);

ALTER TABLE nodes
ADD COLUMN schedule_from text,
ADD COLUMN schedule_until text,
ADD COLUMN schedule_source text NOT NULL DEFAULT 'node'
  CHECK (schedule_source IN ('node', 'fleet')),
ADD CONSTRAINT nodes_schedule_effective_check
CHECK (
  (schedule_from IS NULL AND schedule_until IS NULL)
  OR (
    schedule_from ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'
    AND schedule_until ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'
  )
);

CREATE INDEX fleets_organization_idx ON fleets (organization_id);
CREATE INDEX nodes_fleet_idx ON nodes (fleet_id);
CREATE INDEX operator_actions_created_idx ON operator_actions (created_at DESC);
CREATE INDEX operator_actions_target_idx ON operator_actions (target_type, target_id, created_at DESC);
