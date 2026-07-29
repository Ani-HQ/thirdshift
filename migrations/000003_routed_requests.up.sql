CREATE TABLE api_key_model_permissions (
  api_key_id text NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
  model_id text NOT NULL REFERENCES models(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (api_key_id, model_id)
);

CREATE TABLE model_manifest_limits (
  model_id text PRIMARY KEY REFERENCES models(id) ON DELETE CASCADE,
  max_input_tokens integer NOT NULL CHECK (max_input_tokens > 0),
  max_output_tokens integer NOT NULL CHECK (max_output_tokens > 0),
  max_request_bytes integer NOT NULL CHECK (max_request_bytes > 0),
  capabilities jsonb NOT NULL DEFAULT '{}'::jsonb,
  content_filter_profile text,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX node_sessions_connected_recent
ON node_sessions (node_id, last_heartbeat_at DESC)
WHERE state = 'connected';

CREATE INDEX node_heartbeats_latest_by_session
ON node_heartbeats (session_id, received_at DESC);

CREATE INDEX job_attempts_active_by_node
ON job_attempts (node_id, status)
WHERE status IN ('offered', 'accepted', 'running');

CREATE INDEX jobs_state_created_at
ON jobs (state, created_at);
