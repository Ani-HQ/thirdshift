# Threat Model

Thirdshift v0.1 assumes community host machines are not confidential compute. Only public or non-sensitive text workloads are in scope.

## Threat Directions

- Malicious model to host: controlled through curated GGUF-only manifests, signed runtime and catalog artifacts, pinned revisions, SHA-256 verification, and no arbitrary repository code.
- Malicious customer to host: controlled through text-only schemas, size and token limits, no tools, no file mounts, no URLs, runtime timeouts, and loopback-only inference.
- Malicious host to customer: controlled through disclosure, model and runtime hashes, challenge jobs, duplicate execution, reputation, and coordinator-delayed credit.
- Malicious node to platform: controlled through coordinator-owned job IDs, leases, metering, pricing, idempotency, rate limits, benchmark challenges, and quarantine.
- Malicious operator browser/session to platform: controlled in alpha by bearer-token gating, session-scoped console storage, audited actions, and never returning prompt bodies through internal job endpoints.
- Malicious release mirror to host: controlled through release `SHA256SUMS`, signed release manifests, updater hash verification, and previous-binary rollback.
- Public status scraping to platform: controlled by aggregate-only unauthenticated responses, 10-second caching, and no prompt/account/payout fields in status or contribution-card JSON.

## Implemented Malicious-Node Controls

- Nodes cannot create billable work directly; the coordinator owns job IDs, attempts, leases, deadlines, and accepted-work decisions.
- `job.completed` payloads must be signed by the active node Ed25519 key and must match the expected model and runtime hashes.
- Replayed result envelopes and duplicate attempts cannot create a second successful job attempt or another job-acceptance ledger transaction.
- Node usage reports are plausibility checked before acceptance. Rejected usage creates no host credit; suspicious-but-accepted usage is recorded as a verification event.
- Duplicate verification runs sampled jobs on another eligible node without double-charging the customer. Disagreements update reputation.
- Challenge failures update reputation and quarantine only after repeated failures, not a single disagreement.
- Ledger postings are balanced, posted entries are immutable, and corrections use reversal transactions.
- Operator actions for drain, pause, quarantine, retry, cancel, and payouts are written to `operator_actions`.
- Internal job APIs return scheduler and accounting metadata only; prompt and completion text are excluded from console payloads.
- `thirdshift update` verifies Ed25519 release manifests and artifact hashes before promoting an app binary.
- Public contribution-card endpoints expose aggregate node contribution stats only.

## Trust Boundaries

- Nodes establish outbound sessions; host machines never expose public inference ports.
- Node-reported metering is untrusted until coordinator checks accept it.
- Prompt and completion content must be absent from normal logs.
- Ledger corrections use reversal transactions, not destructive edits to posted entries.
- The operator console stores the bearer token in `sessionStorage` and deploys same-origin behind Caddy; durable browser storage and cross-origin internal API access remain out of scope.
- The installer and updater trust the embedded release public key placeholder until the first real release-key ceremony. Key generation and rotation are documented in `docs/RELEASE.md`.

## Open Work

- Generate and publish the real release signing public key.
- Exercise the installer and updater on real Windows 11 x64 NVIDIA hardware.
- Add broader adversarial tests for runtime binding, manifest tampering, and payout-operator mistakes.
- Replace the alpha operator token with user-scoped operator identity, MFA, and scoped permissions.
