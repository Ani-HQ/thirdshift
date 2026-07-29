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
