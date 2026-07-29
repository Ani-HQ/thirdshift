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

