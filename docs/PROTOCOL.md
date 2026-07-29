# Protocol

Thirdshift nodes and the coordinator exchange versioned JSON messages over one secure, outbound WebSocket session.

## Envelope

Every protocol message uses this envelope:

```json
{
  "protocol_version": "1.0",
  "message_id": "msg_01J...",
  "type": "job.offer",
  "sent_at": "2026-07-29T08:00:00Z",
  "payload": {}
}
```

## Message Types

Node to coordinator:

- `node.hello`
- `node.heartbeat`
- `node.state_changed`
- `model.download_progress`
- `model.ready`
- `job.accepted`
- `job.rejected`
- `job.started`
- `job.completed`
- `job.failed`
- `node.safety_event`
- `node.log_event`

Coordinator to node:

- `session.accepted`
- `node.config_updated`
- `model.assign`
- `model.unload`
- `job.offer`
- `job.cancel`
- `node.drain`
- `runtime.update_available`

## Versioning Rules

- `protocol_version` is required and currently fixed at `1.0`.
- Unknown message types are rejected.
- Additive optional payload fields are allowed only after schema and example updates.
- Breaking payload changes require a new protocol version and compatibility tests.
- Job offers are leases, not billable events. Credit is created only after coordinator acceptance.

Schemas live in `packages/protocol/schemas`; executable examples live in `packages/protocol/examples`.

## Session Lifecycle

Node registration is performed over HTTPS endpoints before the WebSocket starts. The node generates an Ed25519 keypair locally, exchanges a single-use invite for a bootstrap token, and exchanges that bootstrap token for a short-lived signed access token. Later login refreshes the access token with a request signed by the stored node private key.

The persistent node session uses `GET /v1/node/session` with `Authorization: Bearer <access-token>`. The first WebSocket message from the node must be `node.hello`. The coordinator validates the envelope and payload schema, creates a `node_sessions` row, and replies with `session.accepted`.

After `session.accepted`, the node sends `node.heartbeat` every 15 seconds by default. The coordinator validates each envelope against `packages/protocol/schemas`, inserts a `node_heartbeats` row, updates `node_sessions.last_heartbeat_at`, and updates `nodes.state` plus `nodes.last_seen_at`.

A coordinator sweeper marks a connected session stale after 45 seconds without a valid heartbeat. Stale or closed sessions make the scheduler-visible node state `OFFLINE`. The node reconnect loop uses exponential backoff with jitter and keeps one outbound WebSocket session active.
