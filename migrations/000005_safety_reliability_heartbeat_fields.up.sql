ALTER TABLE node_heartbeats
ADD COLUMN schedule_state text NOT NULL DEFAULT 'in_window'
  CHECK (schedule_state IN ('in_window', 'out_of_window')),
ADD COLUMN thermal_state text NOT NULL DEFAULT 'normal'
  CHECK (thermal_state IN ('normal', 'warm', 'hard_limit')),
ADD COLUMN paused boolean NOT NULL DEFAULT false,
ADD COLUMN draining boolean NOT NULL DEFAULT false;

CREATE INDEX node_heartbeats_m4_eligibility_idx
ON node_heartbeats (node_id, session_id, schedule_state, thermal_state, paused, draining, received_at DESC);
