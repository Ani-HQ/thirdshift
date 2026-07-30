# Contributing

Thirdshift is an alpha system with a narrow v0.1 scope: invited Windows NVIDIA hosts, curated GGUF models, outbound node sessions, non-streaming OpenAI-compatible completions, verification, ledger accounting, and operator tooling.

## Setup

Prerequisites:

- Go `1.26.x`
- Node `18.x` and npm `10.x` for `web/console`
- PostgreSQL `16` for integration tests
- PowerShell or Go for `make ps-check`

Useful commands:

```sh
make lint
go test -count=1 ./...
GOOS=windows GOARCH=amd64 go build ./...
make console-build
make console-test
make ps-check
```

Integration tests:

```sh
TEST_DATABASE_URL=postgres://postgres:dev@127.0.0.1:55433/thirdshift?sslmode=disable go test -count=1 -tags integration ./...
```

## Test Matrix

| Area | Command |
|---|---|
| Go formatting and vet | `make lint` |
| Offline Go unit tests | `go test -count=1 ./...` |
| Windows compile | `GOOS=windows GOARCH=amd64 go build ./...` |
| Database integration | `TEST_DATABASE_URL=... go test -count=1 -tags integration ./...` |
| Console typecheck/build | `make console-build` |
| Console tests | `make console-test` |
| PowerShell structure/parser | `make ps-check` |

`make ps-check` uses PowerShell's parser when `pwsh` is installed. Without `pwsh`, it runs a Go structural fallback that catches unbalanced strings and brackets; CI performs the real parser check on `windows-latest`.

## Decision Log Rule

Update [docs/DECISIONS.md](docs/DECISIONS.md) when a change adds a lasting product, security, dependency, release, protocol, storage, or operator-behavior decision. Keep entries short and concrete.

## Scope Guardrails

Do not add v0.1 support for blockchain rewards, arbitrary customer containers, arbitrary Hugging Face execution, `trust_remote_code`, pickle model files, training/fine-tuning, multi-GPU tensor parallelism, Apple Silicon or mobile hosts, sensitive workloads, guaranteed income claims, automated KYC/tax/payouts, or a public permissionless network.

For protocol changes, add schemas and examples without breaking existing message types. For database changes, add a new numbered migration; do not edit prior migrations.
