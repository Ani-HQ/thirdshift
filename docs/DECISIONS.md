# Decisions

| ID | Decision | Rationale |
|---|---|---|
| D-000 | The working title "NightNode" is renamed to Thirdshift | Aligns implementation, binaries, environment variables, and model IDs with the product name |
| D-001 | Product sells model access, not GPU rental | Keeps hardware invisible and aligns with downstream value |
| D-002 | Windows NVIDIA only for v0.1 | Matches reachable gaming-cafe supply and reduces compatibility surface |
| D-003 | llama.cpp and GGUF | Consumer hardware support and reduced arbitrary-code risk |
| D-004 | Outbound WebSocket | Avoids port forwarding, CGNAT, and public host endpoints |
| D-005 | Curated catalog | Preserves security, licensing, reliability, and liquidity |
| D-006 | Non-sensitive data only | Community nodes cannot guarantee host-blind confidentiality |
| D-007 | Central coordinator | Reliability and iteration before decentralization |
| D-008 | Dollar ledger, manual payout | Validates economics before payment automation and compliance work |
| D-009 | One job and one model per node | Simplifies memory, scheduling, and failure handling |
| D-010 | No streaming in v0.1 | Simplifies retries and response semantics |
| D-011 | Use a single Go module at the repository root | This deviates from handoff section 8.4's `go.work` example; one module is simpler for the current monorepo and keeps shared internal packages importable without workspace coordination |
| D-012 | Use a small internal migration runner built on pgx | Avoids adding a migration framework while still giving idempotent SQL application and checksum protection |
| D-013 | Use `github.com/santhosh-tekuri/jsonschema/v6` for schema tests | Provides draft 2020-12 JSON Schema validation with a small, focused dependency |
| D-014 | Use prefixed ULID-style sortable text IDs | Keeps IDs human-scannable by resource type while preserving chronological sorting and avoiding database-specific ID generation |
| D-015 | `thirdshift doctor` reports unsupported non-Windows hosts without failing the process | Lets macOS development and CI exercise the command while keeping Windows 11 x64 NVIDIA as the host target |
| D-016 | Pin llama.cpp release `b10180` for Milestone 1 | Current official release checked on 2026-07-29; macOS arm64 archive SHA-256 `789f717355fb2574becfaa70601714c78908bd1e5d6c6cadd6b8ccc98060d0f7` was downloaded and verified |
| D-017 | Pin Windows runtime artifacts from llama.cpp `b10180` as executable plus CUDA support archive | Official release splits Windows x64 CPU executable archive SHA-256 `55d092fa5cd85e996ac556d00f427b313267dc27a18f8eb3f6ad3530782d41e0` from CUDA 12.4 DLL archive SHA-256 `8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6`; both were downloaded and verified |
| D-018 | Runtime release manifests are Ed25519-signed over canonical JSON excluding the signature field | Keeps signature verification deterministic with only the Go standard library and avoids committing private signing material |
| D-019 | Pin `unsloth/SmolLM2-135M-Instruct-GGUF` Q2_K as `thirdshift-tiny-chat-v1` | Apache-2.0 model card metadata and 88,201,792-byte GGUF SHA-256 `c53fe6626c7165ebfd8de5db22edc3f719b813da001e662bc5cb453f2540a076` were verified from the pinned Hugging Face revision `9e6855bc4be717fca1ef21360a1db4b29d5c559a` |
| D-020 | The Milestone 1 tiny model uses `gpu_layers: 0` and `device: none` | Makes the macOS CPU demo path reliable while the doctor command still enforces Windows NVIDIA host eligibility for real hosting |
| D-021 | The model cache stores files as `<sha256>.gguf` and evicts by modification-time LRU | Content-hash addressing prevents name confusion; mtime gives a simple stdlib-only access signal and active model protection |
| D-022 | Use `nhooyr.io/websocket` v1.8.17 for node sessions | Provides a small, maintained WebSocket implementation with context-aware dial, accept, read, write, and close semantics |
| D-023 | Use HMAC-SHA256 stateless node access tokens for Milestone 2 | Keeps token issuance stdlib-only and lets the coordinator verify short-lived bearer tokens without another persistence table |
| D-024 | Use key-signed token refresh for invite-free re-login | A stored Ed25519 private key can request a fresh access token without reusing the single-use invite or bootstrap token |
| D-025 | Store node config as a minimal TOML file parsed with the standard library | Satisfies the host-readable config requirement without adding a YAML or TOML dependency during the alpha |
| D-026 | Store node private keys in 0600 local files for Milestone 2 and keep Windows Credential Manager as a compiling stub | Preserves restricted local storage now while isolating the Windows credential integration behind build tags for later implementation |
| D-027 | Use a Unix socket for local node control on non-Windows and a 127.0.0.1 control-port stub on Windows | Keeps pause, resume, and status behind a local-only control channel that remains cross-compilable before real Windows named-pipe work |
| D-028 | Use operator bearer auth from `THIRDSHIFT_OPERATOR_TOKEN` for internal coordinator endpoints in Milestone 2 | Keeps invite creation and node inventory protected until the operator console and account model exist |
| D-029 | Default reconnect backoff starts at 1 second and caps at 60 seconds with 20 percent jitter | Prevents tight reconnect loops while keeping recovery quick for a single alpha node |
| D-030 | Developer API keys use `tsak_` secrets with only `sha256:` hashes stored in PostgreSQL | The secret is shown once at creation while the coordinator can still authenticate bearer requests without storing recoverable API keys |
| D-031 | Model permissions are explicit per API key and created during `admin-cli apikey create` | Keeps P0 access control simple and testable before account and billing policy land |
| D-032 | `admin-cli catalog sync` loads valid local catalog manifests into the coordinator database and skips incomplete placeholder manifests | The repository still contains early placeholder manifests; P0 routing should sync the pinned tiny model without mutating read-only catalog files |
| D-033 | Public API rate limiting is an in-memory per-key fixed one-minute window for alpha | Satisfies abuse protection without adding Redis or another dependency; distributed limits move to a later production milestone |
| D-034 | Scheduler weights are loaded from coordinator config with defaults centralized in `internal/coordinator/jobs` | Keeps scoring deterministic in tests and configurable without scattering numeric weights across scheduling code |
| D-035 | Job results are signed with the node identity Ed25519 key over the `job.completed` payload excluding the signature field | Reuses the existing node trust root and lets the coordinator verify result integrity before accepting completion output |
| D-036 | CPU-class model hardware profiles may set `min_vram_mb = 0` | The local CPU demo model is legitimate supply for development and tests, so migration `000004` relaxes the original strictly-positive VRAM constraint to non-negative |
| D-037 | `thirdshift start --runtime-base-url` is a development-only loopback attach path | It enables the documented fake-runtime routed demo while preserving scheduler eligibility through catalog model and runtime hashes |
| D-038 | Host work schedules are local-time windows with start inclusive and end exclusive; equal start/end means always in window | This matches the host-facing mental model, handles overnight windows such as 23:00-08:00, and avoids storing timezone-specific recurring calendar data before M4 needs it |
| D-039 | Thermal recovery uses a configurable 5 C default hysteresis below the soft maximum temperature | Prevents rapid AVAILABLE/DRAINING oscillation around the configured temperature limit while keeping the host eligible again after meaningful cooling |
| D-040 | M4 transient failures retry exactly once on a different eligible node | Limits duplicate work and user-visible latency while proving the lease/result path before reputation and multi-retry scheduling arrive |
| D-041 | Coordinator and node structured logs use a shared stdlib `slog` redaction handler and log IDs instead of prompt or completion bodies | Keeps request, job, and attempt traceability while reducing the chance of leaking prompts, completions, API keys, invite tokens, or private material |
| D-042 | Accepted job results post one balanced microdollar ledger transaction inside the coordinator completion transaction | Couples customer charge, host pending credit, platform margin, result persistence, and job success so duplicate or replayed results cannot create extra credit |
| D-043 | Host credits move from pending to available through `admin-cli credits release`, with `THIRDSHIFT_CREDIT_HOLD_SECONDS` defaulting to 24 hours | Keeps the alpha hold process explicit and testable before a background payout/release worker exists |
| D-044 | Duplicate verification sampling is deterministic from the job id and per-model `duplicate_sample_rate` | Makes tests reproducible and avoids adding random scheduler state while preserving percentage-based sampling behavior |
| D-045 | Node-reported token counts are accepted only after coordinator plausibility checks | The runtime result is still node supplied, so malformed usage, impossible totals, and oversized completions are rejected before ledger credit; outliers are recorded as verification events |
| D-046 | Challenge quarantine requires repeated failures against an injected threshold; one disagreement never quarantines | Matches the handoff safety rule while allowing tests and future policy code to choose stricter or looser per-model thresholds |
| D-047 | Payout create/export/confirm and economics reports are direct database admin commands | Accounting operations need to work even when the HTTP coordinator is stopped; operator web workflows remain out of scope for Milestone 5 |
| D-048 | Use Next.js `15.5.21` with TypeScript `5.6.3` for the operator console | This line is fully supported by local Node `18.20.8` and avoids production advisories npm reports for the older Next 14 line |
| D-049 | The operator console stores the operator token only in `sessionStorage` | Keeps the alpha login simple while avoiding durable browser storage for the bearer secret |
| D-050 | The console talks to the coordinator with same-origin direct fetch behind Caddy | `/internal-console` and `/internal/v1` share one origin in deployment, so no browser CORS trust expansion is needed |
| D-051 | Fleet schedule defaults apply only when a node has no local schedule override | Fleet enrollment can set cafe-wide defaults, but host-local `thirdshift configure` settings and schedule env vars remain authoritative |
| D-052 | M6 alerts are computed from persisted coordinator state with configurable thresholds | The alpha console can surface operational risk without adding another alerting service before the pilot |
| D-053 | Console production dependency overrides pin patched `postcss` and `sharp` transitive versions | Keeps the Node 18-compatible Next line while avoiding known production dependency advisories in the generated lockfile |
| D-054 | Use Vitest `3.2.6` for console component tests | It is the current patched Vitest line that supports Node 18; Vitest 4 requires Node 20 or newer |
| D-055 | Reuse the signed runtime release-manifest shape for Thirdshift app updates | One Ed25519-signed artifact manifest format keeps installer, updater, and runtime verification code paths aligned |
| D-056 | `thirdshift update` retains the previous binary beside the active binary | Side-by-side rollback metadata is simple, cross-platform, and testable before a full Windows service updater exists |
| D-057 | Public `/v1/status` is cached in the coordinator for 10 seconds | Launch traffic can refresh the status page frequently without adding a cache service or hitting aggregate SQL on every request |
| D-058 | Contribution card JSON exposes only per-node aggregate stats | The share card can be public enough for launch while avoiding prompt, completion, account, payout, or hardware fingerprint data |
| D-059 | The console app serves without a Next.js basePath; Caddy maps both `/internal-console` and `/status` | Keeps `/status` a real public route while preserving the M6 internal console URL through proxy prefix stripping |
| D-060 | `make ps-check` uses PowerShell's parser when available and a Go structural fallback otherwise | macOS contributors can run a meaningful local check while CI still performs true PowerShell parsing on Windows |
