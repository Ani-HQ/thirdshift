# Changelog

## Unreleased

- Raised llama-server startup health timeout to 5 minutes for cold large-model loads while still failing fast when the runtime process exits before readiness.
- Added Apple Silicon host support: `thirdshift doctor` now passes on `darwin/arm64`, detects the GPU via `system_profiler` and unified memory via `sysctl`, and reports vendor `apple`. macOS uses the existing signed `darwin/arm64` llama.cpp build, which has Metal compiled in, so no new runtime artifact was needed.
- Apple Silicon reports a GPU working-set budget of 65% of unified memory in place of discrete VRAM, which feeds the VRAM doctor check and auto model selection.
- Added `apple` to the `gpu_vendor` enum in the `node.heartbeat` schema; without it the coordinator rejected every Apple heartbeat.

- Fixed `thirdshift start` with no `--model` failing on installed binaries: automatic selection listed the catalog directory from disk instead of falling back to the catalog embedded in the binary.

- Fixed nodes going permanently offline about an hour after starting: access tokens were fetched once at startup and never renewed, so any reconnect past the token lifetime was rejected with 401 until the process was restarted by hand. The agent now renews proactively near expiry and retries once on a 401.

- The public earnings ticker now lists a host once it has earned credit, or while it is connected, instead of every node seen in the last 24 hours — test registrations no longer linger on the page.

- Auto model selection now assumes the 8GB platform floor when VRAM cannot be measured, instead of selecting on RAM alone: a high-RAM host with an unreadable GPU would otherwise be handed the largest model in the catalog.

- `thirdshift start` now selects a model automatically: it measures GPU vendor, VRAM, RAM and free disk, runs the largest catalog model that fits, and logs the choice with the numbers behind it. `--model auto` requests this explicitly; `--model <id>` still pins one and is now refused up front when the hardware cannot run it. Hidden-listing models are never auto-selected.

- Added AMD Radeon host support on Windows via llama.cpp's Vulkan backend: runtime release manifests gain optional per-backend artifact keys (`windows/amd64/cuda`, `windows/amd64/vulkan`) with the bare platform key as a fallback, and the node picks its build from the detected GPU vendor.
- Added GPU vendor detection to `thirdshift doctor` (nvidia, amd, unknown) using `Win32_VideoController` via PowerShell on Windows; AMD hosts pass with a note that Vulkan is used. AMD VRAM is reported as unknown when `AdapterRAM` lands in the 32-bit wrap band rather than being estimated.
- Added an optional `gpu_vendor` field to the `node.heartbeat` payload and schema.

- Added Qwen2.5 14B and 32B Instruct manifests (Apache-2.0, pinned Q4_K_M GGUFs) so 12GB and 24GB hosts can serve models that command materially higher prices per token than the 7B tier.

- Fixed the pinned Windows runtime having no GPU backend: llama.cpp b10180 shipped only a CPU build for Windows, so nodes served every token on CPU (7.4 tok/s measured on an RTX 3060 Ti) while `--n-gpu-layers` was silently ignored. Runtime re-pinned to b10182 with the real CUDA 12.4 build, and `gpu_layers` is now an explicit full-offload layer count.

- Added a live host earnings ticker to the public catalog: `/v1/status` now carries a `hosts` array of anonymous adjective-animal handles with region, state, 24h jobs, and 24h/lifetime host credit, summed from `host_credit_holds`. No node id, hostname, GPU, or fleet ever appears.
- Added `region_node_counts` to `/v1/status` and a full-bleed dot-matrix world map as the opening visual above the wordmark, drawn from a committed Natural Earth land mask with per-region heat, a subtle breathe animation, and native hover tooltips.

- Fixed the job accept window being enforced against node-reported timestamps: a host clock a few seconds off NTP failed every accept with "not accepted within the offer window" and the session was torn down. All authoritative job-lifecycle times now come from the coordinator clock; node timestamps are informational only. job_failed responses also moved from HTTP 502 to 503 so Cloudflare stops masking the JSON error body, and job-message handling errors are now logged before closing a session.

- Fixed the job.offer schema rejecting real catalog model ids: the Milestone 0 pattern only allowed `thirdshift-*-vN` names, so offers for `qwen2.5-7b-instruct` failed the coordinator's own outbound validation. Also logs the previously swallowed scheduling error.

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
