# Changelog

## Unreleased

- Added public model catalog data to `/v1/status`: model cards, public prices, limits, aggregate availability state, regions, requester-region hint, and 24-hour median output speed.
- Added fleet/node aggregate region fields and operator commands for region assignment without exposing host or per-node details publicly.
- Added unauthenticated developer waitlist signup plus operator waitlist list/export commands.
- Reworked the public console `/status` route into a model-first catalog and network status page with waitlist capture.

## 0.1.0-alpha - 2026-07-30

- Added launch readiness surfaces: signed app updater, Windows install/uninstall scripts, tag-driven release workflow, release docs, and PowerShell script checks.
- Added unauthenticated `/v1/status`, public console `/status`, per-node contribution card JSON, and `thirdshift card`.
- Normalized list responses to encode empty arrays instead of `null` across HTTP JSON endpoints.
- Added README, SECURITY, CONTRIBUTING, launch checklist, and demo storyboard polish for open-source alpha readiness.

## 0.1.0-alpha.6 - 2026-07-30

- Added the `/internal/v1` operator API for overview, nodes, models, jobs, ledger, audit, alerts, fleet creation, fleet reporting, and audited operator actions.
- Added a Next.js TypeScript operator console under `web/console` with overview, nodes, models, jobs, ledger, and audit pages plus fast component tests.
- Added fleet schedule defaults, fleet-scoped enrollment behavior, `admin-cli fleet create/report`, console deployment wiring, CI console build/test, and migration `000007`.

## 0.1.0-alpha.5 - 2026-07-29

- Added coordinator metering plausibility checks, verification events, duplicate verification sampling, challenge reputation/quarantine updates, and verification-overhead accounting.
- Added balanced accepted-job ledger posting, pending-to-available host credit release, immutable payout batch create/export/confirm, and economics reporting.
- Added migration `000006`, M5 unit/integration coverage, operator payout runbook steps, and Windows verification script checks.

## 0.1.0-alpha.4 - 2026-07-29

- Added local host schedule configuration, schedule-aware heartbeats, and scheduler exclusion for out-of-window nodes.
- Added pause, drain, resume, thermal guard safety events, hard-limit runtime cancellation, and enriched node status/list output.
- Added exactly-once transient retry on a different eligible node plus structured redacted logging for routed requests.
- Added migration `000005`, protocol heartbeat fields, M4 integration coverage, and safety/reliability runbook steps.

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
