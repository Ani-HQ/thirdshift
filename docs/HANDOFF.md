# NightNode
## Product and Engineering Handoff

**Working title:** NightNode  
**Version:** 0.1  
**Date:** July 29, 2026  
**Status:** Build-ready alpha specification  
**Primary objective:** Ship an open-source Windows node that turns an idle NVIDIA gaming PC into a worker for a managed network serving curated open models.

> **One-line product:** Run approved open models on your gaming PC during idle hours, complete useful AI jobs, and earn from accepted work.

> **Customer abstraction:** Model availability and completed inference, not GPU rental.

```{=openxml}
<w:p><w:r><w:br w:type="page"/></w:r></w:p>
```

# 0. Master Build Prompt for the Implementation Agent

Copy the block below into the coding agent that will build the project.

```text
You are the founding engineer for NightNode. Read this entire handoff before writing code.

Build the v0.1 alpha exactly within scope:
- Windows 11 x64 host node
- NVIDIA GPUs only
- Curated GGUF models only
- llama.cpp as the model runtime
- Outbound-only secure connection from node to coordinator
- One model loaded per node
- One inference job at a time per node
- Non-streaming, non-sensitive text inference only
- OpenAI-compatible public endpoint plus an asynchronous jobs endpoint
- Credits ledger and manual payout export, not automated payouts
- No blockchain, no token, no arbitrary containers, no arbitrary Hugging Face code

Use the architecture, protocol, data model, state machines, API contracts, security rules, acceptance criteria, and milestone order in this document. Do not broaden the product without recording a decision in docs/DECISIONS.md.

Execution rules:
1. Create a monorepo using the exact proposed structure unless a clearly documented technical blocker exists.
2. Keep the node and coordinator in Go. Use PostgreSQL for durable state. Use Next.js only for the operator and host web console.
3. Build vertical slices. The first meaningful milestone is one remote Windows PC serving one completion through the coordinator.
4. Add tests with each component. Do not postpone all tests until the end.
5. Maintain docs/DECISIONS.md, docs/THREAT_MODEL.md, docs/PROTOCOL.md, and CHANGELOG.md as code changes.
6. Never execute arbitrary model repository code. Accept only signed catalog manifests and verified GGUF files.
7. Bind the local inference runtime to 127.0.0.1 only. The host machine must never expose a public inbound inference port.
8. Redact prompt bodies from logs by default.
9. Treat node-reported metering as untrusted. The coordinator owns billing and accepted-work accounting.
10. Stop after each milestone and report: completed work, tests, known gaps, next milestone, and any decision that differs from this handoff.

Start with Milestone 0 and Milestone 1. Do not begin payments, gamification, AMD support, or a public marketplace until the end-to-end inference path is reliable.
```

---

# 1. Executive Summary

NightNode is an open-source host agent plus a managed control plane. It converts idle gaming PCs into workers that serve curated open models. Developers do not rent a GPU or choose NVIDIA versus AMD. They request a model or submit an AI job. NightNode selects an eligible machine, runs the model, verifies the result, meters accepted work, and credits the host.

The alpha is deliberately narrow. It targets Windows gaming PCs with NVIDIA GPUs, because this is the most reachable and operationally coherent supply in the founder's network. It serves only approved GGUF models through llama.cpp. It supports text generation only, with non-streaming requests and no sensitive data. Hosts never open public ports. Their node agent creates an outbound connection to the coordinator and receives signed jobs.

The open-source release is the distribution wedge. It should feel like a magical local demo:

```text
nightnode start
RTX 4070 detected
Model ready: nightnode-small-chat
Available until 08:00
Connected to the network
Job accepted: 2,184 output tokens
Credit earned: $0.04
```

The company is not a GPU marketplace. The long-term product is a model-availability and capability layer. Hardware availability is an internal implementation detail.

## 1.1 Core thesis

Open models are becoming commercially useful, smaller models are being pushed further through post-training and optimization, and agents are creating large volumes of asynchronous inference. At the same time, many gaming GPUs sit idle for predictable hours. NightNode attempts to turn this fragmented supply into dependable model availability.

## 1.2 The hardest problem

The installer is not the hard part. The hard part is making unreliable and potentially untrusted machines look like a reliable inference provider. The durable value will live in:

- Model-to-hardware benchmarking
- Scheduling and model placement
- Node reputation and verification
- Host safety and customer privacy controls
- Metering, billing, and payout accounting
- Developer distribution and recurring demand

---

# 2. Product Positioning

## 2.1 External positioning

**For hosts:**

> Earn from useful AI work when your gaming PC would otherwise be idle.

**For developers:**

> Access curated open models through one API without managing GPUs or servers.

**For model builders, later:**

> Publish an optimized model once and make it available across suitable machines.

## 2.2 What the customer should see

```text
Model: nightnode-small-chat-v1
Price: $X per 1M input tokens / $Y per 1M output tokens
Availability: 97.8% alpha
Typical speed: 31 tokens/sec
Data class: Public or non-sensitive only
Version: Pinned
```

## 2.3 What the customer should not see

```text
GPU: RTX 3060
CUDA version: 12.x
Node: gaming-cafe-07-pc-12
Windows build: 26100
GPU-hour price: $0.18
```

Those details belong to NightNode's scheduler and operator tooling.

## 2.4 Differentiation

NightNode is differentiated by the combination below, not by any single feature:

1. Model-native product instead of raw GPU rental
2. Windows-first host experience
3. Open-source host node and auditable protocol
4. Curated model catalog rather than arbitrary code execution
5. Outbound-only networking with no public host endpoint
6. Asynchronous and background-agent workloads first
7. Conventional dollar-denominated ledger and payouts
8. Gaming-cafe fleets as the initial supply wedge

---

# 3. Product Principles

1. **Model availability is the product.** Hardware is hidden.
2. **Demand creates earnings.** Never pay merely for being online.
3. **Curated beats arbitrary.** The alpha supports a small approved catalog.
4. **Outbound-only by default.** No inbound ports on host machines.
5. **Open-source the trust boundary.** Hosts can inspect the software running locally.
6. **Start with public and non-sensitive jobs.** Community hardware is not confidential compute.
7. **Reliability before decentralization.** A centralized coordinator is acceptable.
8. **Boring payments before crypto.** Use a normal ledger and manual payout export in alpha.
9. **One vertical slice at a time.** Do not build the marketplace before one node serves one real request.
10. **Host control is non-negotiable.** Scheduling, pause, thermal limits, and a kill switch must be obvious.

---

# 4. Users and Jobs To Be Done

## 4.1 Host: individual gamer

**Job:** Let my PC complete paid AI work during hours I choose without affecting my normal use or exposing my machine.

Needs:

- One-command install
- Clear compatibility check
- Visible model, temperature, power, jobs, and credit
- Auto-pause or drain when the machine becomes active
- Easy uninstall and data cleanup
- Honest earnings language

## 4.2 Host: gaming-cafe owner

**Job:** Turn a fleet of idle PCs into an additional revenue source with central controls.

Needs:

- Fleet enrollment token
- One dashboard for many nodes
- Shared model cache later
- Schedules by machine group
- Exportable job and payout report
- No impact on opening hours

## 4.3 Developer

**Job:** Call an open model without renting or configuring a GPU.

Needs:

- Familiar API
- Pinned model version
- Clear price and data class
- Job status and retry semantics
- Usage metadata
- Predictable errors

## 4.4 Operator

**Job:** Maintain model supply, protect hosts, validate work, investigate failures, and reconcile credits.

Needs:

- Node health and inventory
- Model assignment controls
- Job replay and quarantine
- Reputation and fraud signals
- Ledger and payout export
- Audit logs without prompt content

---

# 5. MVP Definition

## 5.1 P0 scope

The v0.1 alpha must support:

- Windows 11 x64
- NVIDIA desktop GPUs with at least 8 GB VRAM
- One invited host or gaming-cafe fleet
- Node registration with an invitation token
- Hardware detection and benchmark
- Curated GGUF catalog
- Signed manifests and SHA-256 verification
- llama.cpp `llama-server` on loopback only
- Persistent outbound TLS WebSocket to coordinator
- One loaded model and one job at a time per node
- Non-streaming chat completion
- Asynchronous job submission and polling
- Node heartbeats and state transitions
- Retry on another node after transient failure
- Basic challenge-job verification
- Coordinator-owned metering
- Dollar-denominated host credits
- Manual CSV payout export
- Operator dashboard
- Host CLI status view

## 5.2 P1 after alpha reliability

- System tray application
- Automatic pause based on Windows user activity
- Shared LAN model cache for cafes
- Multiple models cached per node
- Embeddings and transcription
- Streaming responses
- Function calling and structured output
- Linux support
- Fleet management improvements
- Automated payouts after legal and compliance review
- Provider integrations with model routers

## 5.3 Explicit non-goals

Do not build any of the following in v0.1:

- Blockchain, token, staking, or mining rewards
- GPU-hour marketplace
- Arbitrary customer containers
- Arbitrary Hugging Face model execution
- `trust_remote_code`
- Pickle model files
- Training or fine-tuning
- Multi-GPU tensor parallelism
- Splitting one inference request across multiple internet nodes
- AMD, Intel, Apple Silicon, or mobile support
- Sensitive enterprise workloads
- Medical, financial, student, or authentication data
- Guaranteed passive income
- Automated KYC, tax withholding, or payouts
- A public permissionless network

---

# 6. User Experience Flows

## 6.1 Host onboarding

1. Host opens the project site or GitHub README.
2. Host runs the PowerShell installer.
3. `nightnode doctor` checks OS, NVIDIA driver, GPU, VRAM, RAM, free disk, and outbound HTTPS/WebSocket access.
4. Host enters an invitation token or completes device-code login.
5. Node registers and receives a node certificate and short identifier.
6. Host chooses:
   - Available hours
   - Maximum GPU temperature
   - Optional power limit where supported
   - Maximum disk cache
   - Approved model preferences or Auto mode
7. Node downloads the signed runtime and assigned model.
8. Node verifies hashes and launches the runtime locally.
9. Node reports `AVAILABLE`.
10. Host can see status and credit.

### CLI target

```powershell
irm https://nightnode.example/install.ps1 | iex
nightnode doctor
nightnode login --invite NN-ALPHA-XXXX
nightnode configure --from 23:00 --until 08:00 --max-temp 78 --cache 80GB
nightnode start
nightnode status
```

### Host status target

```text
NightNode v0.1.0
Node            cafe-01-pc-03
State           AVAILABLE
GPU             NVIDIA GeForce RTX 4070, 12 GB
Model           nightnode-small-chat-v1
Runtime         llama.cpp build pinned by manifest
Schedule        23:00 to 08:00 local time
Temperature     62 C / limit 78 C
Power           146 W / configured limit 180 W
Jobs tonight    14 accepted, 1 retried
Tokens tonight  31,442 input, 18,200 output
Pending credit  $0.38
```

## 6.2 Developer request

1. Developer creates an API key.
2. Developer retrieves the model catalog.
3. Developer sends a non-streaming completion request.
4. API gateway validates limits and creates a job.
5. Scheduler selects an eligible node.
6. Node acknowledges the lease and executes locally.
7. Coordinator receives the output and usage.
8. Verification policy accepts, duplicates, or quarantines the result.
9. Developer receives the response.
10. Host credit and customer charge are written to the ledger.

## 6.3 Host pause and drain

- `pause` stops new job assignment immediately.
- If no job is running, state becomes `PAUSED`.
- If a job is running, state becomes `DRAINING` and finishes the current job unless a hard safety limit is reached.
- Temperature or power safety events may terminate the current job.
- `resume` returns to `PREPARING_MODEL` or `AVAILABLE`.

---

# 7. System Architecture

## 7.1 High-level architecture

```text
Developer / Agent
        |
        | HTTPS, API key
        v
+-----------------------+
| Public API Gateway    |
| Auth, limits, pricing |
+-----------+-----------+
            |
            v
+-----------------------+       +----------------------+
| Job Service           |------>| PostgreSQL           |
| Durable job state     |       | jobs, nodes, ledger  |
+-----------+-----------+       +----------------------+
            |
            v
+-----------------------+
| Scheduler             |
| eligibility + score   |
+-----------+-----------+
            |
            | outbound-established TLS WebSocket
            v
+-----------------------+       127.0.0.1 only       +------------------+
| NightNode Host Agent  |---------------------------->| llama-server     |
| Windows, open source  |                             | curated GGUF     |
+-----------+-----------+                             +------------------+
            |
            v
+-----------------------+
| Metrics and Ledger    |
| verify, meter, credit |
+-----------------------+
```

## 7.2 Component responsibilities

### Host agent

- Hardware discovery
- Schedule and host controls
- Secure registration
- Persistent coordinator connection
- Runtime download and signature verification
- Model download, resume, cache, and hash verification
- Runtime process lifecycle
- Local health checks
- Job execution
- Thermal and power monitoring
- Redacted local logs
- Credit and status display

### Coordinator

- Node identity and authentication
- Persistent node sessions
- Node state and heartbeat processing
- Manifest distribution
- Job leasing
- Retry and cancellation
- Result ingestion
- Audit events

### Public API gateway

- Developer authentication
- Request validation
- Model catalog
- Rate limits and quotas
- OpenAI-compatible response shape
- Asynchronous job API

### Scheduler

- Hard eligibility filters
- Node scoring
- Lease creation
- Retry placement
- Fairness across nodes
- Warm-model preference

### Verification service

- Challenge jobs
- Random duplicate execution
- Response shape checks
- Runtime and model hash validation
- Node reputation updates

### Ledger

- Immutable entries for customer charge and host credit
- Reversals instead of destructive edits
- Payout batch export
- Reconciliation reports

---

# 8. Proposed Technology Stack

## 8.1 Languages and frameworks

| Area | Choice | Reason |
|---|---|---|
| Host node | Go | Single binary, good Windows support, low overhead, simple services and networking |
| Coordinator/API | Go | Shared types and protocol logic, efficient long-lived connections |
| Database | PostgreSQL | Durable jobs, transactions, ledger, operational familiarity |
| Web console | Next.js + TypeScript | Fast operator UI and future host dashboard |
| Model runtime | llama.cpp `llama-server` | GGUF support, consumer hardware support, OpenAI-compatible endpoints [R1][R2] |
| Protocol | JSON over secure WebSocket | Easy debugging, outbound-friendly, sufficient for alpha |
| Schema | JSON Schema + generated Go types | Explicit contracts and validation |
| Deployment | Docker Compose on one Linux VM for alpha | Low operational complexity |
| TLS/reverse proxy | Caddy or managed load balancer | Automatic HTTPS and WebSocket support |
| Object storage | S3-compatible or cloud object storage | Runtime packages, model metadata, optional logs |

## 8.2 Why llama.cpp

The current llama.cpp server supports OpenAI-compatible chat completions, responses, embeddings, parallel decoding, continuous batching, monitoring endpoints, and quantized models across CPU and GPU backends [R1]. Its build system supports NVIDIA CUDA and other hardware backends, which provides a future path beyond NVIDIA without changing the product API [R2].

For v0.1, use only a pinned, signed llama.cpp build. Do not download the latest binary at runtime.

## 8.3 Why GGUF only

GGUF is designed for inference with GGML-family executors and fast model loading [R3]. Restricting the alpha to GGUF removes arbitrary Python and most model-repository execution risk. It also makes model packages easier to hash, cache, and reason about.

## 8.4 Monorepo structure

```text
nightnode/
  README.md
  LICENSE
  SECURITY.md
  CONTRIBUTING.md
  CHANGELOG.md
  Makefile
  go.work
  .github/
    workflows/
  cmd/
    nightnode/                 # Host CLI entry point
    coordinator/               # Coordinator and public API
    admin-cli/                 # Operator utility
  internal/
    node/
      config/
      hardware/
      runtime/
      models/
      jobs/
      telemetry/
      updater/
      windows/
    coordinator/
      auth/
      sessions/
      scheduler/
      jobs/
      verifier/
      ledger/
      catalog/
      admin/
    shared/
      protocol/
      crypto/
      logging/
  packages/
    protocol/
      schemas/
      examples/
  models/
    catalog/
      nightnode-small-chat-v1.yaml
  web/
    console/
  migrations/
  deploy/
    docker-compose.yml
    Caddyfile
    env.example
  scripts/
    install.ps1
    uninstall.ps1
    release.ps1
  tests/
    e2e/
    fixtures/
  docs/
    ARCHITECTURE.md
    PROTOCOL.md
    THREAT_MODEL.md
    MODEL_CATALOG.md
    OPERATOR_RUNBOOK.md
    DECISIONS.md
```

---

# 9. Node State Machine

```text
UNREGISTERED
    -> REGISTERING
    -> OFFLINE
    -> STARTING
    -> BENCHMARKING (first run or changed hardware)
    -> IDLE (outside schedule or no model assignment)
    -> PREPARING_MODEL
    -> AVAILABLE
    -> BUSY
    -> AVAILABLE

Any active state may transition to:
    -> DRAINING
    -> PAUSED
    -> ERROR
    -> UPDATING
    -> OFFLINE
```

## 9.1 State rules

- `AVAILABLE` means the model is loaded, health check passes, schedule permits work, thermal status is safe, and the node has an active coordinator session.
- `BUSY` means exactly one active job in v0.1.
- `DRAINING` rejects new jobs and attempts to finish the current job.
- `PAUSED` preserves cached files but unloads the model after an idle timeout.
- `ERROR` must include a stable error code and human-readable remediation.
- Coordinator state is authoritative for scheduling. Local state is authoritative for host safety.

## 9.2 Heartbeat

Default heartbeat interval: 15 seconds.

Payload includes:

```json
{
  "type": "node.heartbeat",
  "node_id": "node_01J...",
  "sequence": 1832,
  "state": "AVAILABLE",
  "model_id": "nightnode-small-chat-v1",
  "runtime_hash": "sha256:...",
  "model_hash": "sha256:...",
  "gpu": {
    "name": "NVIDIA GeForce RTX 4070",
    "vram_total_mb": 12282,
    "vram_free_mb": 10400,
    "temperature_c": 62,
    "power_w": 146,
    "power_limit_w": 180,
    "utilization_percent": 4
  },
  "active_job_id": null,
  "uptime_seconds": 19420,
  "timestamp": "2026-07-29T08:00:00Z"
}
```

Coordinator marks the node unavailable after 45 seconds without a valid heartbeat.

---

# 10. Registration and Identity

## 10.1 Alpha registration

1. Operator creates an invitation token scoped to a host account or fleet.
2. Host runs `nightnode login --invite <token>`.
3. Node generates an Ed25519 keypair locally.
4. Public key and hardware fingerprint are submitted over HTTPS.
5. Coordinator creates the node and returns a short-lived bootstrap token.
6. Node exchanges the token for a renewable node certificate or signed access token.
7. Private key remains in Windows Credential Manager or a protected local file with restricted ACLs.

## 10.2 Identity rules

- A node identity belongs to one physical installation.
- Reinstall creates a new node unless the operator performs recovery.
- Hardware fingerprint is a fraud signal, not a sole identity source.
- Tokens are rotated.
- The node cannot choose its own credit balance, model hash, or reputation.

---

# 11. Hardware Detection and Benchmarking

## 11.1 Minimum eligibility

- Windows 11 x64
- NVIDIA driver functional
- NVIDIA GPU with at least 8 GB VRAM
- 16 GB system RAM minimum, 32 GB preferred
- 40 GB free disk minimum
- Stable outbound HTTPS and secure WebSocket
- Host has administrator rights for installation, but the runtime itself runs with reduced privilege

## 11.2 Detection

Use NVIDIA's supported management interfaces or `nvidia-smi` to query GPU identity, memory, temperature, utilization, and power information [R8]. Do not parse the human table format. Use explicit CSV query flags where available.

Example internal command:

```powershell
nvidia-smi --query-gpu=name,uuid,memory.total,driver_version,temperature.gpu,power.draw,power.limit,utilization.gpu --format=csv,noheader,nounits
```

## 11.3 Benchmark sequence

1. Verify runtime can start.
2. Load a small benchmark GGUF.
3. Run fixed prompts with pinned parameters.
4. Record:
   - Model load time
   - Prompt tokens per second
   - Output tokens per second
   - Peak VRAM
   - Average and peak power
   - Maximum temperature
   - Error rate over five runs
5. Submit signed benchmark result to coordinator.
6. Coordinator classifies the node into a hardware performance class.

Do not use host self-reported GPU model or benchmark numbers as trusted billing inputs.

---

# 12. Model Catalog and Manifest

## 12.1 Catalog policy

The host must not execute arbitrary Hugging Face repositories. The operator reviews each model's license and model card. Hugging Face documents model cards as the place for model metadata and limitations, and it explicitly requires users to respect repository licenses [R4][R5].

P0 requirements for every model:

- GGUF file
- Commercially usable license reviewed by the operator
- Pinned repository revision
- Exact file URL or object-storage mirror
- SHA-256 hash
- No remote code
- No pickle
- Tested on each supported GPU class
- Known context and memory profile
- Defined request and output limits
- Data-class policy
- Price and host-credit policy

Hugging Face warns that pickle scanning cannot eliminate pickle exploit risk and that trusting remote code grants significant trust to model authors [R6][R7]. NightNode therefore rejects both in v0.1.

## 12.2 Example manifest

```yaml
schema_version: 1
model_id: nightnode-small-chat-v1
display_name: NightNode Small Chat v1
status: alpha
source:
  provider: huggingface
  repository: approved-org/approved-model-gguf
  revision: 0123456789abcdef
  file: approved-model-q4_k_m.gguf
  sha256: 6b2f...deadbeef
license:
  identifier: apache-2.0
  reviewed_at: 2026-07-29
  notes: Commercial hosted inference permitted after operator review.
runtime:
  engine: llama.cpp
  build_id: llama-cpp-nightnode-2026-07-01
  binary_sha256: 91aa...beef
  arguments:
    context_size: 8192
    batch_size: 512
    gpu_layers: auto
    parallel: 1
    host: 127.0.0.1
    port: dynamic
hardware:
  min_vram_mb: 8192
  min_ram_mb: 16384
  min_disk_mb: 12000
  eligible_gpu_classes:
    - nn-nvidia-8gb-v1
    - nn-nvidia-12gb-v1
limits:
  max_input_tokens: 4096
  max_output_tokens: 1024
  max_request_bytes: 262144
capabilities:
  chat_completions: true
  streaming: false
  tools: false
  embeddings: false
policy:
  data_class: public_or_non_sensitive
  content_filter_profile: alpha-default
pricing:
  customer_input_per_million_usd: 0.20
  customer_output_per_million_usd: 0.60
  host_credit_per_million_accepted_output_usd: 0.30
verification:
  duplicate_sample_rate: 0.02
  challenge_rate: 0.01
signature:
  key_id: catalog-key-2026-01
  value: base64-signature
```

## 12.3 Model selection

Developer selects the exact model ID.

Host can select:

- **Auto:** Any eligible approved model. Default and recommended.
- **Preferred models:** A subset of the approved catalog.
- **Blocked models:** A host-level exclusion list.

The scheduler does not assign a model outside host consent.

---

# 13. Runtime Lifecycle

## 13.1 Runtime download

- Runtime release manifest is signed by the NightNode release key.
- Node downloads to a temporary path.
- Node verifies SHA-256 and signature.
- Node atomically promotes the binary into the versioned runtime directory.
- Rollback retains the previous known-good runtime.

## 13.2 Model download

- Use HTTP range requests for resumable download.
- Store by content hash, not display name.
- Download to `.partial`.
- Verify exact byte length and SHA-256.
- Atomically rename after verification.
- Never load an unverified partial file.
- Enforce cache quota with least-recently-used eviction, excluding the active model.

## 13.3 Launch

- Bind `llama-server` to `127.0.0.1` on a dynamically selected port.
- Pass only manifest-approved arguments.
- Set working directory to a dedicated NightNode runtime directory.
- Redirect stdout and stderr into bounded, rotating logs.
- Do not log request prompt bodies.
- Add a Windows Firewall rule that blocks inbound access to the runtime executable.
- Run under a dedicated low-privilege user or restricted token where feasible.
- Use a Windows Job Object to ensure child-process cleanup and resource limits.

Microsoft documents AppContainer as a low-integrity isolation mechanism with constrained filesystem and network access [R9]. Full AppContainer integration is P1 if it blocks GPU or loopback access needed by llama.cpp. P0 must still use restricted process permissions, strict file ACLs, loopback binding, and firewall rules.

## 13.4 Health checks

Node calls local endpoints every 10 seconds:

- Runtime process alive
- `/health` or equivalent successful
- Loaded model matches manifest
- Model hash and runtime hash match expected values
- GPU memory is within expected range
- Temperature below host limit

---

# 14. Node-Control Protocol

Use versioned JSON messages over one persistent secure WebSocket.

## 14.1 Envelope

```json
{
  "protocol_version": "1.0",
  "message_id": "msg_01J...",
  "type": "job.offer",
  "sent_at": "2026-07-29T08:00:00Z",
  "payload": {}
}
```

## 14.2 Required message types

Node to coordinator:

- `node.hello`
- `node.heartbeat`
- `node.state_changed`
- `model.download_progress`
- `model.ready`
- `job.accepted`
- `job.rejected`
- `job.started`
- `job.completed`
- `job.failed`
- `node.safety_event`
- `node.log_event`

Coordinator to node:

- `session.accepted`
- `node.config_updated`
- `model.assign`
- `model.unload`
- `job.offer`
- `job.cancel`
- `node.drain`
- `runtime.update_available`

## 14.3 Job lease

```json
{
  "protocol_version": "1.0",
  "message_id": "msg_01J...",
  "type": "job.offer",
  "sent_at": "2026-07-29T08:00:00Z",
  "payload": {
    "job_id": "job_01J...",
    "attempt_id": "att_01J...",
    "lease_expires_at": "2026-07-29T08:00:10Z",
    "deadline_at": "2026-07-29T08:02:00Z",
    "model_id": "nightnode-small-chat-v1",
    "request": {
      "messages": [
        {"role": "user", "content": "Summarize this public text..."}
      ],
      "temperature": 0.2,
      "max_tokens": 512,
      "seed": 42,
      "stream": false
    },
    "verification": {
      "kind": "standard",
      "challenge_id": null
    }
  }
}
```

Node must accept or reject within 2 seconds. A job offer is not a billable event. Only a coordinator-accepted completion creates credit.

---

# 15. Public Developer API

## 15.1 Authentication

- Bearer API key
- Keys stored as hashes
- Per-key quotas and model permissions
- Every write endpoint supports `Idempotency-Key`

## 15.2 Model catalog

`GET /v1/models`

Returns model ID, capabilities, pricing, data class, current availability, and version.

## 15.3 OpenAI-compatible completion

`POST /v1/chat/completions`

P0 restrictions:

- `stream` must be `false`
- Text messages only
- No tools or function calling
- No images or files
- Model must be exact catalog ID
- Input and output limits from manifest
- Request timeout maximum 120 seconds

Example:

```bash
curl https://api.nightnode.example/v1/chat/completions \
  -H "Authorization: Bearer $NIGHTNODE_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: demo-001" \
  -d '{
    "model": "nightnode-small-chat-v1",
    "messages": [{"role":"user","content":"Write three product taglines."}],
    "temperature": 0.4,
    "max_tokens": 180,
    "stream": false
  }'
```

Response must use a stable OpenAI-compatible shape and add NightNode metadata under a namespaced field:

```json
{
  "id": "chatcmpl_01J...",
  "object": "chat.completion",
  "created": 1785312000,
  "model": "nightnode-small-chat-v1",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "..."},
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 42,
    "completion_tokens": 118,
    "total_tokens": 160
  },
  "nightnode": {
    "job_id": "job_01J...",
    "attempts": 1,
    "data_class": "public_or_non_sensitive",
    "served_region": "in-south"
  }
}
```

## 15.4 Asynchronous jobs

`POST /v1/jobs`

```json
{
  "model": "nightnode-small-chat-v1",
  "input": {
    "messages": [{"role": "user", "content": "..."}],
    "temperature": 0.2,
    "max_tokens": 512
  },
  "priority": "standard",
  "deadline_seconds": 900
}
```

`GET /v1/jobs/{job_id}`

States:

- `queued`
- `leased`
- `running`
- `verifying`
- `succeeded`
- `failed`
- `cancelled`

`POST /v1/jobs/{job_id}/cancel`

Cancellation is best effort after execution starts.

## 15.5 Error model

```json
{
  "error": {
    "code": "no_capacity",
    "message": "No eligible node is currently available for this model.",
    "retryable": true,
    "request_id": "req_01J..."
  }
}
```

Stable error codes:

- `invalid_request`
- `unauthorized`
- `quota_exceeded`
- `model_not_found`
- `model_unavailable`
- `no_capacity`
- `job_timeout`
- `job_failed`
- `content_rejected`
- `internal_error`

---

# 16. Scheduler

## 16.1 Hard filters

A node is eligible only if all are true:

- Session connected
- Heartbeat fresh
- State `AVAILABLE`
- Exact model loaded and hash verified
- Runtime hash verified
- Host schedule allows work
- Host has not blocked the model
- Data-class policy matches
- VRAM and health thresholds pass
- No active job
- Reputation above model minimum
- Node is not quarantined

## 16.2 Scoring

Start with a deterministic weighted score:

```text
score =
  40 * warm_model_bonus
+ 20 * rolling_success_rate
+ 15 * normalized_tokens_per_second
+ 10 * low_recent_failure_bonus
+  5 * thermal_headroom
+  5 * host_fairness
+  5 * regional_preference
```

Weights are configuration, not constants scattered through code.

## 16.3 Leasing

- Create an attempt row before sending `job.offer`.
- Lease expires after 10 seconds unless accepted.
- Node rejects if state changed or model is not ready.
- After acceptance, job deadline governs execution.
- A lost session marks the attempt transiently failed.
- Retry once on a different node for transient failures.
- Never create two customer-visible successes for one idempotency key.

## 16.4 Duplicate verification

Randomly send 1% to 2% of eligible jobs to a second node in alpha. Do not double-charge the customer. Treat verification cost as platform overhead. Use deterministic parameters and seeds where the model supports them.

---

# 17. Job Execution

## 17.1 Local request path

```text
Coordinator job offer
    -> Node validates lease and manifest
    -> Node accepts
    -> Node calls http://127.0.0.1:<port>/v1/chat/completions
    -> Node captures status, duration, and usage
    -> Node deletes request body from memory where practical
    -> Node returns signed result envelope
    -> Coordinator verifies and accepts or rejects
```

## 17.2 Job limits

- Maximum request body: 256 KB
- Maximum input tokens: manifest-defined, alpha default 4,096
- Maximum output tokens: manifest-defined, alpha default 1,024
- Maximum wall-clock: 120 seconds for sync, 15 minutes for async
- No external tools or URLs
- No runtime network access
- No host filesystem access

## 17.3 Logging

Allowed:

- Job ID
- Attempt ID
- Model ID
- Token counts
- Timings
- Status and error code
- Hashes
- GPU telemetry

Forbidden by default:

- Prompt text
- Completion text
- API keys
- Invitation tokens
- Node private key
- Customer file contents

A temporary operator debug mode may log payloads only in a local controlled environment and must never be enabled on community nodes.

---

# 18. Security and Threat Model

## 18.1 Threat directions

### Malicious model to host

Risk:

- Arbitrary code from model repositories
- Unsafe serialization
- Compromised runtime package

Controls:

- Curated manifests only
- GGUF only
- No pickle
- No `trust_remote_code`
- Signed runtime and manifest
- SHA-256 verification
- Pinned revisions
- Operator license and security review

### Malicious customer to host

Risk:

- Prompt injection does not directly execute code, but malformed payloads may exploit the runtime
- Resource exhaustion
- Network or filesystem probing if tools are exposed

Controls:

- Text-only schema
- Strict size and token limits
- No tools, shell, file mounts, URLs, or customer containers
- Runtime network block
- Dedicated working directory
- Process limits and timeout
- Current pinned runtime with security update process

### Malicious host to customer

Risk:

- Read prompts or outputs
- Substitute a model
- Modify output
- Return cached or fabricated result

Controls:

- Alpha policy: public or non-sensitive data only
- Model and runtime hashes
- Challenge jobs
- Random duplicates
- Reputation
- Result plausibility and shape checks
- Delayed credit until coordinator acceptance
- Clear customer disclosure that community hosts are not confidential compute

### Malicious node to platform

Risk:

- Fake GPU
- Fake token counts
- Replay results
- Farm credits with collusion
- Protocol flooding

Controls:

- Coordinator-owned job IDs, leases, pricing, and metering
- Unique attempt nonce
- Signed result envelope
- Benchmark challenges
- Rate limits
- Hardware fingerprint as a weak signal
- Node reputation and quarantine
- No credit for uptime

## 18.2 Security boundary statement

NightNode v0.1 is not a trusted execution environment. Transport is encrypted, but a determined machine owner may inspect data processed on their machine. Public documentation and API responses must state this clearly.

## 18.3 Security files required before launch

- `SECURITY.md` with vulnerability reporting process
- Threat model with trust boundaries
- Supported versions policy
- Signed release verification instructions
- Data-class policy
- Incident response runbook
- Dependency and binary provenance list

---

# 19. Verification and Reputation

## 19.1 Accepted work

A host earns credit only when:

1. The coordinator issued a valid job lease.
2. The node accepted within the lease window.
3. The result arrived before the deadline.
4. The model and runtime hashes matched.
5. Usage and response shape passed checks.
6. Verification policy accepted the output.
7. The job was not already completed by another attempt.

## 19.2 Challenge jobs

A challenge job is indistinguishable from a normal job to the node. It may have:

- Known expected structural output
- Deterministic seed and narrow answer range
- Hidden canary tokens
- Comparison against a trusted reference node

Challenge failures reduce reputation and may quarantine the node. Do not instantly ban on one model-quality disagreement.

## 19.3 Reputation fields

- Total accepted jobs
- Rolling 7-day success rate
- Timeout rate
- Hash mismatch count
- Challenge pass rate
- Duplicate disagreement rate
- Session stability
- Operator intervention history

Start new nodes at low but usable reputation. Increase limits gradually.

---

# 20. Metering, Pricing, and Ledger

## 20.1 Principle

Pay for accepted useful work, not time online.

## 20.2 Metering sources

Coordinator records:

- Prompt token count from runtime result
- Completion token count from runtime result
- Coordinator-observed duration
- Manifest price version
- Attempt and verification outcome

Node-reported token counts are cross-checked through periodic tokenization or trusted reference execution. Do not let a node submit an arbitrary billable amount.

## 20.3 Ledger model

Use immutable double-entry-style events even if the first UI is simple.

Example:

```text
Customer usage charge      +$0.00120
Host pending credit        -$0.00072
Platform verification cost -$0.00006
Platform gross margin       $0.00042
```

Store integer microdollars to avoid floating-point errors.

## 20.4 Tables

- `ledger_accounts`
- `ledger_transactions`
- `ledger_entries`
- `payout_batches`
- `payout_items`

Corrections are reversal transactions. Never mutate posted entries.

## 20.5 Alpha payout flow

1. Credits remain `pending` during a configurable hold.
2. Accepted credits become `available`.
3. Operator creates a payout batch.
4. System exports CSV with host, amount, and reference.
5. Operator pays manually through an approved method.
6. Operator imports confirmation and posts the payout transaction.

Do not automate public payouts until legal, tax, KYC, sanctions, and payments-provider requirements have been reviewed for each launch market.

## 20.6 Unit economics dashboard

Track:

```text
Customer revenue
- Host credits
- Duplicate/challenge overhead
- Failed-attempt overhead
- Payment fees
- Coordinator and storage cost
- Support cost allocation
= Contribution margin
```

Host view should estimate electricity cost separately and label it as an estimate.

---

# 21. Data Model

Core PostgreSQL tables:

## 21.1 Identity and inventory

- `users`
- `organizations`
- `fleets`
- `nodes`
- `node_keys`
- `node_hardware_snapshots`
- `node_sessions`
- `node_heartbeats`
- `node_reputation`

## 21.2 Model catalog

- `models`
- `model_versions`
- `model_artifacts`
- `runtime_releases`
- `model_hardware_profiles`
- `model_prices`

## 21.3 Jobs

- `api_keys`
- `jobs`
- `job_attempts`
- `job_results`
- `verification_events`
- `idempotency_records`

## 21.4 Finance

- `ledger_accounts`
- `ledger_transactions`
- `ledger_entries`
- `payout_batches`
- `payout_items`

## 21.5 Audit

- `audit_events`
- `security_events`
- `operator_actions`

## 21.6 Critical database rules

- Use UUIDv7 or ULID-style sortable IDs.
- Every job state change occurs in a transaction.
- One successful attempt per job enforced by a unique partial index.
- Idempotency key unique per API key and endpoint.
- Ledger entries balance to zero per transaction.
- Prompt and output content are stored only for the minimum time needed by the requested API. Default sync path should not persist content after response unless explicit retention is enabled for debugging in a closed alpha.

---

# 22. Operator Console

P0 pages:

1. **Overview**
   - Online nodes
   - Available nodes by model
   - Jobs per hour
   - Success rate
   - Pending host credit
   - Alerts

2. **Nodes**
   - State, GPU, model, temperature, power, last heartbeat
   - Reputation
   - Drain, pause, quarantine
   - Recent errors

3. **Models**
   - Catalog status
   - Active versions
   - Available nodes
   - Benchmark profiles
   - Price version

4. **Jobs**
   - State, model, attempts, timings, error code
   - Retry or cancel
   - No prompt body by default

5. **Ledger**
   - Customer charges
   - Host pending and available credit
   - Payout batch creation and export

6. **Audit**
   - Operator actions
   - Security events
   - Manifest changes

---

# 23. Observability

## 23.1 Metrics

Coordinator:

- Active node sessions
- Nodes by state and model
- Heartbeat age
- Job queue depth
- Scheduler placement latency
- Job success, retry, timeout, and cancellation rates
- End-to-end latency
- Tokens per second by model and GPU class
- Challenge pass rate
- Ledger posting failures

Node:

- Runtime health
- Model load time
- Download progress
- GPU temperature and power
- VRAM use
- Jobs and tokens
- Coordinator connection state
- Local error count

## 23.2 Tracing

Use request ID, job ID, and attempt ID across every log event. OpenTelemetry is optional in the first vertical slice but required before a multi-cafe alpha.

## 23.3 Alerts

- Node disconnect spike
- Job failure rate above threshold
- Hash mismatch
- Runtime crash loop
- GPU over-temperature event
- Ledger imbalance
- API authentication anomaly
- No capacity for a published model

---

# 24. Testing Strategy

## 24.1 Unit tests

- State machine transitions
- Manifest signature and hash validation
- Scheduler eligibility and scoring
- Lease expiration and retry
- Idempotency behavior
- Ledger balancing
- Log redaction
- Cache eviction

## 24.2 Integration tests

- Coordinator with PostgreSQL
- Node registration
- WebSocket reconnect
- Runtime launch and health
- Model download resume
- Job completion and retry
- Payout export

## 24.3 End-to-end tests

Use a tiny test GGUF and CPU mode in CI where GPU is unavailable.

Required E2E path:

1. Start Postgres and coordinator.
2. Register test node.
3. Start fake or CPU llama.cpp runtime.
4. Publish test model.
5. Send completion request.
6. Route to node.
7. Return result.
8. Post ledger entries.
9. Verify idempotent replay does not duplicate work or charges.

## 24.4 Windows hardware test matrix

At minimum before public alpha:

- RTX 3060 12 GB
- RTX 4060 8 GB
- RTX 4070 12 GB
- One 16 GB or 24 GB GPU if available
- Windows 11 current stable build
- Clean installation and upgrade path
- Low disk, network interruption, sleep/wake, driver reset, and user pause

## 24.5 Security tests

- Tampered runtime binary rejected
- Tampered model file rejected
- Invalid manifest signature rejected
- Runtime cannot bind public interface through manifest override
- Oversized request rejected
- Expired lease rejected
- Replayed result rejected
- Prompt body absent from normal logs
- Node private key permissions verified

---

# 25. Deployment

## 25.1 Local development

`docker compose up` starts:

- PostgreSQL
- Coordinator
- Caddy or local reverse proxy
- Web console

The Windows node runs natively outside Docker.

## 25.2 Alpha production

Start simple:

- One Ubuntu VM for coordinator and web services
- Managed PostgreSQL
- Object storage for releases and metadata
- Caddy or cloud load balancer for TLS
- Daily database backups
- Release artifacts signed in CI

Do not introduce Kubernetes until connection count or deployment needs justify it.

## 25.3 Secrets

- Store production secrets in a managed secret store.
- Never commit release signing keys.
- Use separate keys for runtime releases, model manifests, and API token signing.
- Keep an offline recovery key.

---

# 26. Milestone Plan

## Milestone 0: Repository and contracts

Deliverables:

- Monorepo structure
- Go modules
- Docker Compose with PostgreSQL
- Protocol JSON schemas
- Initial database migrations
- `docs/DECISIONS.md`
- CI for lint and tests

Acceptance:

- Clean clone can run tests and start coordinator locally.
- Protocol examples validate against schemas.

## Milestone 1: Single-machine vertical slice

Deliverables:

- `nightnode doctor`
- Local llama.cpp runtime launcher
- One approved tiny GGUF manifest
- Local completion command through node

Acceptance:

- On a Windows NVIDIA PC, one command starts the runtime and returns a completion.
- Runtime binds only to loopback.
- Model and runtime hash checks pass.

## Milestone 2: Remote node connection

Deliverables:

- Registration
- Node key storage
- WebSocket session
- Heartbeats
- Node state display

Acceptance:

- Remote node remains connected through normal network changes.
- Coordinator marks stale node unavailable within 45 seconds.

## Milestone 3: Routed developer request

Deliverables:

- API key auth
- `/v1/models`
- `/v1/chat/completions`
- Scheduler and job lease
- Result return

Acceptance:

- A developer request reaches the remote Windows node and returns an OpenAI-compatible response.
- Duplicate idempotency key returns the same result without duplicate credit.

## Milestone 4: Safety and reliability

Deliverables:

- Schedules
- Pause, drain, resume
- Thermal safety
- Retry on another node
- Model cache and resumable download
- Redacted logs

Acceptance:

- Node stops receiving jobs outside its schedule.
- Over-temperature event prevents new work.
- A disconnected node causes one retry on a different eligible node.

## Milestone 5: Verification and ledger

Deliverables:

- Challenge jobs
- Random duplicate sample
- Reputation
- Metering
- Ledger
- Manual payout CSV

Acceptance:

- Only accepted work creates host credit.
- Ledger transactions balance.
- Replayed and duplicate results do not create extra credit.

## Milestone 6: Invited gaming-cafe alpha

Deliverables:

- Fleet enrollment
- Operator console
- Five machines
- One recurring workload
- Operator runbook

Acceptance over 14 nights:

- At least 95% accepted job completion after retries
- No public inbound runtime port
- No interference with cafe operating hours
- Fewer than 15 minutes of manual support per 10 node-nights after onboarding
- Complete electricity, revenue, credit, and failure report

## Milestone 7: Open-source release

Deliverables:

- Public repository
- Installer
- README demo
- Security policy
- Live network status page
- Shareable contribution card

Acceptance:

- A new invited user can install, diagnose, register, and serve a test job without founder assistance.

---

# 27. Prioritized Issue Backlog

## P0 engineering issues

### NN-001: Initialize monorepo

Acceptance:

- Repository matches proposed structure.
- `make test` works on Linux CI.
- Go formatting and linting run in CI.

### NN-002: Define protocol schemas

Acceptance:

- Versioned schemas for heartbeat, model assignment, job offer, result, and error.
- Example messages validate.

### NN-003: Create database migrations

Acceptance:

- Nodes, sessions, models, jobs, attempts, idempotency, and ledger tables exist.
- Constraints prevent two successful attempts for one job.

### NN-004: Implement `nightnode doctor`

Acceptance:

- Reports OS, GPU, VRAM, driver, RAM, disk, network, and actionable errors.
- JSON output available for automation.

### NN-005: Implement signed runtime manager

Acceptance:

- Downloads pinned runtime.
- Rejects wrong hash or signature.
- Keeps previous version for rollback.

### NN-006: Implement model cache

Acceptance:

- Resumable download.
- Content-hash path.
- Atomic finalization.
- Quota and LRU eviction.

### NN-007: Launch llama.cpp locally

Acceptance:

- Binds only to `127.0.0.1`.
- Uses manifest-approved arguments.
- Health check and cleanup work.

### NN-008: Node registration

Acceptance:

- Invitation token exchanged once.
- Local keypair generated.
- Node can reconnect without re-entering token.

### NN-009: Persistent session and heartbeat

Acceptance:

- Reconnect with exponential backoff and jitter.
- Heartbeat every 15 seconds.
- Coordinator state updates correctly.

### NN-010: Scheduler and lease

Acceptance:

- Hard filters and weighted score implemented.
- Lease expires safely.
- Busy node cannot receive second job.

### NN-011: OpenAI-compatible endpoint

Acceptance:

- Supports P0 request subset.
- Stable errors.
- Idempotency works.

### NN-012: Local job executor

Acceptance:

- Validates offer, calls local runtime, returns signed result.
- Deadline and cancellation work.

### NN-013: Retry behavior

Acceptance:

- Transient failure retries once on another node.
- Permanent invalid request does not retry.

### NN-014: Host schedule and pause

Acceptance:

- Local timezone schedule works across midnight.
- Pause drains current job.
- Resume restores availability.

### NN-015: Thermal guard

Acceptance:

- Node reports temperature.
- Above configured limit enters draining or safety stop.
- Safety event appears in operator console.

### NN-016: Redacted structured logging

Acceptance:

- Prompt and completion absent in default logs.
- Request, job, attempt IDs present.

### NN-017: Verification sample

Acceptance:

- Challenge and duplicate policies configurable per model.
- Outcome updates reputation.

### NN-018: Ledger

Acceptance:

- Integer microdollar entries.
- Balanced transaction constraint.
- Host credit only after acceptance.

### NN-019: Operator console

Acceptance:

- Nodes, models, jobs, errors, and ledger visible.
- Drain and quarantine actions audited.

### NN-020: Payout CSV

Acceptance:

- Creates immutable batch.
- Exports host ID, account reference, amount, and memo.
- Supports import of paid confirmation.

### NN-021: Windows installer and updater

Acceptance:

- Install, update, and uninstall documented and tested.
- Binary signatures verified.
- User data cleanup option included.

### NN-022: E2E test harness

Acceptance:

- Entire request path runs in CI with fake or CPU runtime.
- Idempotency, retry, and ledger assertions included.

### NN-023: Security documentation

Acceptance:

- `SECURITY.md`, threat model, supported versions, and data-class policy are public.

### NN-024: Gaming-cafe fleet pilot

Acceptance:

- Five nodes grouped under one fleet.
- Schedule and report work at fleet level.
- 14-night pilot report generated.

---

# 28. Definition of Done for v0.1

The alpha is complete only when all statements are true:

1. A clean Windows 11 gaming PC with an eligible NVIDIA GPU can install NightNode.
2. The node verifies a signed runtime and approved GGUF model.
3. The local model server listens only on loopback.
4. The node establishes an outbound secure session with the coordinator.
5. A developer can list models and send a non-streaming completion request.
6. The scheduler routes the request to an eligible node.
7. The node executes and returns a valid response.
8. Retry succeeds when the first node disconnects.
9. Prompt and completion content are absent from default logs.
10. A model or runtime hash mismatch prevents execution.
11. Host pause, schedule, and thermal safety work.
12. Only coordinator-accepted work creates host credit.
13. Ledger entries balance and payout CSV can be generated.
14. Five gaming-cafe machines operate for 14 nights without affecting business hours.
15. Public documentation clearly states the community-node privacy limitation.

---

# 29. Success Metrics and Go/No-Go Gates

## 29.1 Technical

- Job completion after retries: at least 95% in invited alpha, then 98% target
- Stale-node detection: under 45 seconds
- Scheduler placement latency: under 1 second at alpha scale
- No public runtime ports detected
- Hash verification failure always blocks execution
- Support time: under 15 minutes per 10 node-nights after onboarding

## 29.2 Supply

- Five stable cafe nodes first
- At least 70% of scheduled hours connected
- At least 50% of connected hours model-ready
- Host returns for a second 14-night period

## 29.3 Demand

- One recurring workload, not only demo traffic
- At least 100,000 accepted output tokens or an equivalent workload volume
- At least one developer uses the API on three separate days

## 29.4 Economics

- Host credit exceeds estimated incremental electricity for the pilot workload
- Contribution margin is measurable and not structurally negative
- Verification and retry overhead remains below 15% of paid compute

## 29.5 Stop or pivot conditions

Reconsider the model if:

- Host electricity-adjusted earnings remain negligible at realistic utilization
- Support burden remains high after the first installation week
- Community-node privacy eliminates all reachable demand
- Centralized API pricing is consistently cheaper after full overhead
- Node churn makes model preloading uneconomic

---

# 30. Launch Plan for the Open-Source Repo

## 30.1 README hero

```text
# Your gaming PC sleeps. AI does not.

NightNode runs approved open models during your idle hours, completes useful AI jobs, and credits you for accepted work.

One install. No exposed ports. Stops when you need your PC.
```

## 30.2 Demo sequence

A 20 to 30 second video should show:

1. PowerShell install
2. RTX GPU detected
3. Model download and verification
4. Node appears on live map
5. Developer sends API request
6. Node serves completion
7. Host sees a small credit

## 30.3 Launch event

> Can 100 sleeping gaming PCs become an open-model network for one night?

Live page:

- Connected nodes
- Cities
- Models available
- Jobs completed
- Tokens served
- Estimated GPU-hours reused
- Share card per host

## 30.4 Messaging guardrail

Use:

> Earn from accepted AI work when demand is available.

Do not use:

> Guaranteed passive income every night.

---

# 31. First 48 Hours for the Build Agent

1. Create repository, docs, Go workspace, CI, and Docker Compose.
2. Pin a llama.cpp release and choose a tiny test GGUF with reviewed license.
3. Implement `nightnode doctor` for Windows NVIDIA detection.
4. Implement model and runtime hash verification.
5. Launch `llama-server` on loopback and complete one local request.
6. Define protocol schemas before network code.
7. Create coordinator registration and heartbeat endpoint.
8. Connect one Windows node to the coordinator.
9. Add a temporary operator CLI to offer one job manually.
10. Return the result and record it in PostgreSQL.

At that point, stop and demonstrate the vertical slice before adding dashboards, ledger, or automatic scheduling.

---

# 32. Decision Log Seed

Create these entries in `docs/DECISIONS.md`:

| ID | Decision | Rationale |
|---|---|---|
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

---

# 33. Open Questions to Resolve During Alpha

These are research questions, not blockers for Milestone 1:

1. Which approved model and quantization produce the best demand-weighted economics on 8 GB and 12 GB cards?
2. How much model cache churn occurs when demand changes?
3. How frequently do cafe machines reboot, sleep, or lose connectivity?
4. Can a Windows restricted-token process retain full CUDA performance?
5. What percentage of jobs require duplicate verification?
6. Which workload can become anchor demand?
7. Are developers willing to accept community-node data classification for a meaningful discount?
8. What host credit is required to cover electricity and still motivate participation?
9. Is shared LAN model caching necessary in the first five-machine cafe?
10. Does the viral open-source story attract useful recurring nodes or mostly one-time installs?

---

```{=openxml}
<w:p><w:r><w:br w:type="page"/></w:r></w:p>
```

# 34. Reference Notes

The implementation agent should re-check current versions before pinning dependencies. The architectural claims below are grounded in official documentation available on July 29, 2026.

- **[R1] llama.cpp server README.** Documents OpenAI-compatible chat completions, responses, embeddings, continuous batching, parallel decoding, monitoring, and GPU/CPU inference.  
  https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md

- **[R2] llama.cpp build documentation.** Documents CUDA and other hardware acceleration backends.  
  https://github.com/ggml-org/llama.cpp/blob/master/docs/build.md

- **[R3] GGUF specification and Hugging Face GGUF documentation.** Describes GGUF as an inference-oriented binary format designed for fast loading and saving.  
  https://github.com/ggml-org/ggml/blob/master/docs/gguf.md  
  https://huggingface.co/docs/hub/en/gguf

- **[R4] Hugging Face Model Cards.** Model cards provide metadata, limitations, and reproducibility information.  
  https://huggingface.co/docs/hub/model-cards

- **[R5] Hugging Face repository licenses.** Requires users to identify and respect repository licenses.  
  https://huggingface.co/docs/hub/repositories-licenses

- **[R6] Hugging Face pickle scanning.** Explains that pickle-specific risks are not fully covered by ordinary antivirus scanning.  
  https://huggingface.co/docs/hub/security-pickle

- **[R7] Hugging Face text-generation inference safety.** Explains the trust implications of pickle conversion and `trust_remote_code`.  
  https://huggingface.co/docs/text-generation-inference/basic_tutorials/safety

- **[R8] NVIDIA System Management Interface documentation.** Documents GPU monitoring and supported power and temperature management capabilities.  
  https://docs.nvidia.com/deploy/nvidia-smi/index.html

- **[R9] Microsoft Windows application isolation.** Describes AppContainer low-integrity process isolation and restricted resource access.  
  https://learn.microsoft.com/en-us/windows/security/book/application-security-application-isolation

---

# 35. Final Product Test

The simplest proof that NightNode is real is this:

> A developer calls one model by name. NightNode routes the request to a gaming PC that has never accepted an inbound internet connection. The result returns in a familiar API shape. The coordinator verifies it, bills the developer, and credits the host. The host can stop the system instantly, and neither side needs to know which GPU served the request.

Build that first. Everything else is expansion.
