# Changelog

## Unreleased

- Fixed scheduler eligibility and result verification comparing node runtime hashes against a single expected binary hash: runtime releases now record per-platform artifact hashes (migration `000012`), so Windows nodes are eligible instead of only nodes matching the manifest's one pinned binary. Found when the first Windows node sat AVAILABLE yet every request returned no_capacity.

- Fixed released binaries failing at startup outside a repo checkout: protocol schemas and the model catalog (including runtime release manifests) are now embedded in the binary, with disk copies taking precedence when present. Found by the first real installer-based node start on Windows.

- Fixed the RAM minimum rejecting real 16 GB machines: Windows reports usable RAM (~15.9 GB on a 16 GB host), so the doctor floor and 7B manifest minimums now use 15 GiB usable as the "16 GB installed" proxy. Found on first real-hardware verification (RTX 3060 Ti host).
- Llama 3.2 Community License signed off (2026-07-30) with manifest-driven compliance: "Built with Llama" attribution, license notice, and AUP link on the public catalog; the vendored license agreement is written next to the cached model on every host via `license.distribute_with_model`; model preparation fails if required license distribution is impossible (migration `000011`).

- Added public model catalog data to `/v1/status`: model cards, public prices, limits, aggregate availability state, regions, requester-region hint, and 24-hour median output speed.
- Added fleet/node aggregate region fields and operator commands for region assignment without exposing host or per-node details publicly.
- Added unauthenticated developer waitlist signup plus operator waitlist list/export commands.
- Reworked the public console `/status` route into a model-first catalog and network status page with waitlist capture.
- Added demand-first catalog listing semantics: a manifest `listing` block, `models.listing_status`, and `/v1/status` support for `waitlist` and `hidden` models via migration `000009`.
- Added pinned `qwen2.5-7b-instruct`, `qwen2.5-coder-7b-instruct`, and `llama-3.2-3b-instruct` manifests with verified Q4_K_M GGUF revisions, sizes, and SHA-256 hashes, listed as available on request.
- Hid `thirdshift-tiny-chat-v1` from the public catalog while keeping it routable for internal testing.
- Replaced the waitlist form with a reviewed access application: required use case and data-class acknowledgment, optional name and expected monthly volume, requested model, and the new fields in `admin-cli waitlist list/export`.
- Added market price comparison and a rounded cheaper tag to public model rows, sourced from manifest `listing.market_comparison`.
- Rethemed the public `/status` page to a light minimalist layout with hairline model rows and honest waitlist states; the operator console is unchanged.
- Fixed a data race in the shared protocol validator's schema cache, which could crash the coordinator with a concurrent map write when several node sessions validated a new message type at the same time.
- Fixed access applications silently dropping a returning applicant's payload: submissions now upsert on `(email, model_id)` via migration `000010`, so a resubmission overwrites the previous answers and one person can apply for several models. Adds `last_applied_at`, and `POST /v1/waitlist` no longer discloses whether an address had applied before.
- Raised the integration test wall-clock budgets so a saturated machine running `go test -tags integration ./...` no longer fails scheduling waits that were merely slow.

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
