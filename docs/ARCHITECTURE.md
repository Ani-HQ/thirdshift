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
