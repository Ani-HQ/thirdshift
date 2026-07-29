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

