# Thirdshift

**Your gaming PC sleeps. AI does not.**

[![Build](https://img.shields.io/badge/build-alpha-lightgrey)](#development) [![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE) [![Security](https://img.shields.io/badge/security-non--sensitive%20workloads-orange)](SECURITY.md)

Thirdshift runs approved open models on your NVIDIA or AMD gaming PC during your idle hours, completes useful AI jobs, and credits you for accepted work.

One install. No exposed ports. No questions about your hardware: the node detects your GPU and picks the largest model it can run. Stops the moment you need your PC.

```text
thirdshift start
RTX 4070 detected, 12282MB VRAM
selected qwen2.5-14b-instruct: 12282MB VRAM available, needs 11264MB
Model ready
Available until 08:00
Connected to the network
Job accepted: 2,184 output tokens
Credit earned: $0.04
```

## What it is

- An open-source Windows host agent (Go, single binary) that serves curated GGUF models through a pinned llama.cpp runtime, bound to loopback only.
- A managed coordinator that routes developer requests to eligible nodes over an outbound-only secure WebSocket, verifies results, and meters accepted work.
- An OpenAI-compatible API for developers: call a model by name, never think about GPUs.

## Status

Alpha launch readiness. The code path supports invited hosts, an operator console, routed completions, verification, ledger accounting, release packaging, and a public status page. Human launch tasks still remain: Windows hardware verification, release-key generation, repository visibility, domain setup, and demo recording.

See [docs/HANDOFF.md](docs/HANDOFF.md) for the full product and engineering specification, and [docs/DECISIONS.md](docs/DECISIONS.md) for the decision log.

## Quick Install

Windows 11 x64 with an eligible NVIDIA GPU is the v0.1 host target.

```powershell
irm https://thirdshift.ani.computer/install.ps1 | iex
thirdshift doctor
thirdshift login --invite THIRDSHIFT-ALPHA-INVITE --coordinator https://coordinator.example
thirdshift configure --from 23:00 --until 08:00 --max-temp 78
thirdshift start
thirdshift status
```

`thirdshift start` needs no model flag. It measures VRAM, RAM and free disk, then
runs the largest catalog model that fits, printing which one it chose and why.
Pass `--model auto` to ask for that explicitly, or `--model <id>` to pin one; a
pinned model that does not fit the hardware is refused up front rather than
failing later under load.

Uninstall keeps node data by default:

```powershell
irm https://thirdshift.ani.computer/uninstall.ps1 | iex
```

Use `-PurgeData` only when you also want local node credentials and model cache removed.

## How It Works

A developer calls one model by name. Thirdshift routes the request to an invited gaming PC that has never accepted an inbound internet connection. The result returns in a familiar API shape. The coordinator verifies it, bills the developer, and credits the host. The host can stop the system instantly, and neither side needs to know which GPU served the request.

## Operator Quickstart

```sh
cp deploy/env.example .env
docker compose -f deploy/docker-compose.yml --env-file .env up --build
```

Open `http://127.0.0.1:8081/internal-console` for the operator console and `http://127.0.0.1:8081/status` for the public status page. Use the operator token from `THIRDSHIFT_OPERATOR_TOKEN`.

Core bootstrap commands:

```sh
admin-cli org create --name "Alpha Org"
admin-cli fleet create --org <org_id> --name "Cafe Alpha" --schedule-from 23:00 --schedule-until 08:00
admin-cli catalog sync
admin-cli apikey create --org <org_id> --model thirdshift-tiny-chat-v1
admin-cli invite create --fleet <fleet_id>
```

The full drill is in [docs/OPERATOR_RUNBOOK.md](docs/OPERATOR_RUNBOOK.md).

## Development

Common checks:

```sh
make lint
go test -count=1 ./...
GOOS=windows GOARCH=amd64 go build ./...
make console-build
make console-test
make ps-check
```

Database-backed tests need `TEST_DATABASE_URL`:

```sh
TEST_DATABASE_URL=postgres://postgres:dev@127.0.0.1:55433/thirdshift?sslmode=disable go test -count=1 -tags integration ./...
```

Useful local targets: `make dev`, `make migrate`, `make doctor-json`, `make run-local`, `make chat-demo`, `make report-economics`.

## Security And Data Class

Community nodes are **not** confidential compute. Thirdshift v0.1 is for public or non-sensitive text workloads only. Do not send medical, financial, student, authentication, secret, regulated, or enterprise-confidential data. Hosts never open inbound runtime ports; the node connects out to the coordinator and the model runtime binds to `127.0.0.1`.

Report vulnerabilities through [SECURITY.md](SECURITY.md).

## Documentation Map

- [Architecture](docs/ARCHITECTURE.md)
- [Protocol](docs/PROTOCOL.md)
- [Threat model](docs/THREAT_MODEL.md)
- [Release process](docs/RELEASE.md)
- [Launch checklist](docs/LAUNCH_CHECKLIST.md)
- [Demo storyboard](docs/DEMO.md)
- [Model catalog notes](docs/MODEL_CATALOG.md)

## License

Apache-2.0
