# Changelog

## 0.1.0-alpha.3 - 2026-07-29

- Added developer API key bootstrap, catalog sync, `/v1/models`, OpenAI-compatible chat completions, and async job create/get/cancel endpoints.
- Added scheduler eligibility, configurable scoring weights, leases, node-side job execution, signed results, idempotent response replay, and integration coverage for routed fake-runtime completions.
- Added migrations for API key model permissions, manifest limits, scheduler indexes, and CPU-compatible model hardware profiles.

## 0.1.0-alpha.2 - 2026-07-29

- Added invite-based node registration, local Ed25519 key storage, bootstrap exchange, and signed access-token refresh.
- Added the persistent node WebSocket session with schema-validated `node.hello`, `session.accepted`, and `node.heartbeat` messages.
- Added node state machine enforcement, local pause/resume/status control, heartbeat persistence, stale-session sweeping, and `admin-cli nodes list`.
- Added Milestone 2 migration, integration test scaffold, deployment env examples, Windows verification steps, and operator runbook commands.

## 0.1.0-alpha.1 - 2026-07-29

- Added `thirdshift doctor` with JSON and human output.
- Added signed runtime manifest verification, artifact installation, rollback metadata, model cache, loopback launcher, and local completion flow.
- Pinned llama.cpp `b10180` and `thirdshift-tiny-chat-v1` for the local CPU demo path.

## 0.1.0-alpha.0 - 2026-07-29

- Initialized the Thirdshift monorepo for Milestone 0.
- Added protocol schemas and examples for the v1 JSON message envelope.
- Added the initial PostgreSQL schema migration, migration runner, Docker Compose stack, CI, and project docs.
