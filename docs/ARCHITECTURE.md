# Architecture

Thirdshift is a centrally coordinated network for serving curated open models on enrolled host PCs.

## Components

- Host agent: discovers hardware, manages local model/runtime lifecycle, connects outbound to the coordinator, executes one job at a time, and redacts prompt content from logs.
- Coordinator: authenticates nodes, owns sessions and heartbeats, issues leases, ingests results, records audit events, and owns scheduler-visible state.
- Public API gateway: will authenticate developer requests, enforce catalog limits, and expose OpenAI-compatible and asynchronous job APIs.
- Scheduler: will filter eligible nodes, score placements, create attempts, and retry transient failures.
- Verification service: will run challenge and duplicate checks before accepted work affects reputation or credits.
- Ledger: stores immutable double-entry-style transactions using integer microdollars.

## Milestone 0 Shape

The current repository contains the contracts, database schema, coordinator health server, migration utility, deployment scaffold, and CI required before implementation of WebSocket sessions or inference.

## Node Local Runtime

The Milestone 1 node code is split behind interfaces so macOS development can use fakes while Windows-specific host concerns remain build-tagged.

- Doctor checks report platform, NVIDIA CSV detection, VRAM, RAM, disk, and configured outbound HTTPS/WSS reachability. Non-Windows hosts report unsupported host status without crashing.
- Runtime manifests are signed with Ed25519. The manager verifies the manifest, downloads artifacts to a temporary path, checks SHA-256 and byte length, extracts archives, promotes into a versioned runtime directory, and tracks previous/current installs for rollback.
- Model cache downloads by HTTP Range into `<sha256>.partial`, verifies byte length and SHA-256, atomically promotes to `<sha256>.gguf`, and applies LRU eviction without removing the active model.
- The launcher accepts only typed manifest-approved llama.cpp arguments, rejects non-loopback hosts, chooses a dynamic loopback port, writes bounded redacted logs, polls `/health`, and stops gracefully before killing on timeout.
- `thirdshift run-local` loads a catalog manifest, ensures the runtime and model are present, launches `llama-server` on `127.0.0.1`, sends one non-streaming chat completion request, and prints completion text plus usage.

## Node Identity And Session

Milestone 2 adds invited registration and persistent coordinator sessions.

- `admin-cli invite create --fleet <fleet_id>` calls the coordinator internal API with an operator bearer token. The coordinator stores only a SHA-256 hash of the single-use invite and creates a short-lived bootstrap token after registration.
- `thirdshift login` creates an Ed25519 keypair locally, stores the private key in a restricted file under the node data directory, submits the public key plus a hardware fingerprint hash, and stores the returned node credentials.
- Re-login without an invite signs a token refresh request with the stored private key. The coordinator verifies the active public key and issues a fresh short-lived HMAC access token.
- `thirdshift start` loads the signed runtime and model through the Milestone 1 managers, starts `llama-server` on loopback, opens one outbound WebSocket, sends `node.hello`, and heartbeats current state, model/runtime hashes, and GPU telemetry.
- The node state machine is explicit and rejects illegal transitions. `pause`, `resume`, and `status` use a local control channel: Unix socket on non-Windows, localhost-bound Windows stub until named-pipe integration is implemented.
- The coordinator persists sessions and heartbeats in PostgreSQL. A configurable sweeper marks sessions stale after missed heartbeats and sets the scheduler-visible node state to `OFFLINE`.

## Routed Developer Requests

Milestone 3 adds the first coordinator-mediated inference path.

- Operators create organizations, sync valid catalog manifests into PostgreSQL, and issue API keys through operator-authenticated internal endpoints.
- Public `/v1/*` developer APIs authenticate bearer API keys, enforce per-key model permissions, apply an in-memory per-minute rate limit, and return the stable §15.5 error shape.
- `/v1/models` reports catalog metadata, pricing, data class, version, limits, and current availability based on connected `AVAILABLE` nodes with fresh heartbeats.
- `/v1/chat/completions` accepts only the P0 OpenAI-compatible subset: non-streaming, text-only messages, no tools/images/files, exact model ids, and manifest request/token limits.
- The scheduler applies hard filters for connected session, fresh heartbeat, `AVAILABLE` state, exact model, verified model/runtime hashes, no active job, and no quarantine. Schedule, data-class, and richer reputation filters are wired as later-milepost TODOs.
- Scoring uses coordinator config weights and deterministic tie-breaking. A lease creates a `job_attempts` row before the `job.offer` envelope is sent.
- The node executor accepts one eligible offer, transitions to `BUSY`, calls the loopback llama-compatible runtime, signs `job.completed` with the node identity key, and returns to `AVAILABLE`.
- The coordinator verifies the node signature and model/runtime hashes before accepting the result, storing it, completing the job, and waking the waiting sync request.
- `Idempotency-Key` records map a request hash to the completed job response; duplicate matching requests replay the same stored response without re-execution.

## Safety And Reliability

Milestone 4 closes the first reliability loop around routed jobs.

- Host schedules are stored in node config as local `HH:MM` windows. The node heartbeats `schedule_state`, and the coordinator excludes `out_of_window` nodes from scheduling.
- `thirdshift pause` and `thirdshift resume` use the local control channel. Pause moves an idle node to `PAUSED`; a busy node moves to `DRAINING` and finishes the in-flight job before pausing. Paused nodes unload the model after a configurable idle timeout.
- GPU telemetry drives `thermal_state`. Above the soft temperature limit the node drains and emits `node.safety_event`; after cooling below the hysteresis threshold it can become `AVAILABLE` again. A hard limit during a job cancels the runtime request and reports a retryable safety failure.
- The scheduler retries transient attempt failures exactly once on a different eligible node. The successful attempt partial index still prevents more than one accepted success for a job.
- Node and coordinator logs use structured `slog` records with a shared redaction handler. Logs carry request, job, attempt, and node IDs but not prompt text, completion text, API keys, invite tokens, access tokens, or private keys.

## Verification And Ledger

Milestone 5 closes the first accounting loop.

- The coordinator accepts credit only for results with a valid lease, timely acceptance, timely completion, matching model/runtime hashes, valid node signature, plausible shape and usage, verification acceptance, and no previous successful attempt.
- Node-reported token counts are treated as untrusted. The coordinator records prompt/completion counts, coordinator-observed duration, price version, and metering status; implausible reports are rejected or recorded as verification events.
- Accepted jobs post one balanced ledger transaction in integer microdollars: customer usage charge, host pending credit, and platform margin. Result persistence, job success, and ledger posting commit together.
- Duplicate verification samples completed jobs using the per-model sample rate and routes a second `job.offer` with `verification.kind=duplicate` to another eligible node when available. Customer charges are not duplicated; verification host credit is posted as platform overhead.
- Challenge outcomes update reputation and can quarantine a node after repeated failures. A single disagreement is recorded but does not quarantine.
- Reputation now tracks accepted jobs, attempt success, timeouts, hash mismatches, challenge pass rate, duplicate disagreement rate, and session stability defaults. Scheduler scoring consumes the rolling success rate.
- Host credits remain pending until the hold elapses and an operator runs `admin-cli credits release`. Payout batches reserve available credits, export immutable CSV rows, and confirm payment by posting the payout ledger transaction.
