# Security

## Reporting

Report suspected vulnerabilities privately through GitHub private vulnerability reporting or email `security@thirdshift.example` with `Thirdshift security` in the subject.

Include affected version, platform, reproduction steps, impact, and whether any data or credentials may have been exposed. Do not publish exploit details until a fix or mitigation is available.

## Supported Versions

| Version | Support |
|---|---|
| `0.1.0-alpha` | Security fixes for invited-alpha deployments |
| Older alpha snapshots | Best-effort only |

## Data-Class Policy

Thirdshift v0.1 is not a trusted execution environment and is not confidential compute. Community hosts can control the machines that execute model inference. Use Thirdshift only for public or non-sensitive text workloads.

Do not send medical, financial, student, authentication, secret, regulated, enterprise-confidential, or other sensitive data through the alpha network.

## Release Verification

Release zips are accompanied by `SHA256SUMS` and `release-manifest.json`. The manifest is signed with an Ed25519 release key, and `thirdshift update` verifies the manifest signature and artifact hash before promotion.

See [docs/RELEASE.md](docs/RELEASE.md) for key generation, signature verification, and rotation notes.

## Trust Boundaries

- Host runtime binds to `127.0.0.1`; nodes do not expose public inbound runtime ports.
- Node sessions are outbound WebSockets to the coordinator.
- Runtime and model artifacts are hash-checked before use.
- Job result envelopes are signed by node identity keys and verified by the coordinator before credit is created.
- Prompt and completion bodies must not appear in default logs.

The full threat model is in [docs/THREAT_MODEL.md](docs/THREAT_MODEL.md).

## Incident Response

1. Triage and reproduce the report privately.
2. Disable affected manifests, release artifacts, API keys, or node accounts if containment is needed.
3. Patch and test the affected component.
4. Publish a signed release or documented mitigation.
5. Add regression tests and update the threat model or decision log if the boundary changed.
