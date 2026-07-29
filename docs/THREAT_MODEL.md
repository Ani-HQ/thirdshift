# Threat Model

Thirdshift v0.1 assumes community host machines are not confidential compute. Only public or non-sensitive text workloads are in scope.

## Threat Directions

- Malicious model to host: controlled through curated GGUF-only manifests, signed runtime and catalog artifacts, pinned revisions, SHA-256 verification, and no arbitrary repository code.
- Malicious customer to host: controlled through text-only schemas, size and token limits, no tools, no file mounts, no URLs, runtime timeouts, and loopback-only inference.
- Malicious host to customer: controlled through disclosure, model and runtime hashes, challenge jobs, duplicate execution, reputation, and coordinator-delayed credit.
- Malicious node to platform: controlled through coordinator-owned job IDs, leases, metering, pricing, idempotency, rate limits, benchmark challenges, and quarantine.

## Trust Boundaries

- Nodes establish outbound sessions; host machines never expose public inference ports.
- Node-reported metering is untrusted until coordinator checks accept it.
- Prompt and completion content must be absent from normal logs.
- Ledger corrections use reversal transactions, not destructive edits to posted entries.

## Open Work

- Complete signed release verification instructions.
- Define incident response runbooks.
- Add security tests for runtime binding, manifest tampering, and payload redaction.

