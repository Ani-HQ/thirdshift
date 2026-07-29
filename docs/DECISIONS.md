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
