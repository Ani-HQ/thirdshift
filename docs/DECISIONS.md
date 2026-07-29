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

