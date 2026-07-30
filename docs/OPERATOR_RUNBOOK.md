# Operator Runbook

Milestone 0 establishes the repository contracts and database foundation.

Initial local workflow:

```sh
make dev
make migrate
curl http://localhost:8080/healthz
```

Operational runbooks for registration, node quarantine, ledger reconciliation, and payout export will be filled in during later milestones.

## Milestone 2 Local Registration And Session

Use two terminals. The fleet id below is valid for the alpha schema and can be reused in a disposable local database.

Terminal 1:

```sh
export THIRDSHIFT_OPERATOR_TOKEN=dev-operator-token
export THIRDSHIFT_ACCESS_TOKEN_SECRET=dev-access-token-secret-change-me
export THIRDSHIFT_COORDINATOR_URL=http://127.0.0.1:8080
export THIRDSHIFT_DATABASE_URL='postgres://thirdshift:thirdshift_dev_password@localhost:5432/thirdshift?sslmode=disable'

make dev
```

Terminal 2:

```sh
export THIRDSHIFT_OPERATOR_TOKEN=dev-operator-token
export THIRDSHIFT_ACCESS_TOKEN_SECRET=dev-access-token-secret-change-me
export THIRDSHIFT_COORDINATOR_URL=http://127.0.0.1:8080
export THIRDSHIFT_DATABASE_URL='postgres://thirdshift:thirdshift_dev_password@localhost:5432/thirdshift?sslmode=disable'
export THIRDSHIFT_NODE_DATA_DIR="$(pwd)/.local/thirdshift-node"
export FLEET_ID=fleet_01J0M000000000000000000000

make migrate
admin-cli invite create --fleet "$FLEET_ID" --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
```

Copy the printed invite token into `THIRDSHIFT_INVITE_TOKEN`, then run:

```sh
export THIRDSHIFT_INVITE_TOKEN='paste-token-here'

thirdshift login --invite "$THIRDSHIFT_INVITE_TOKEN" --coordinator "$THIRDSHIFT_COORDINATOR_URL" --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
thirdshift start --coordinator "$THIRDSHIFT_COORDINATOR_URL" --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
```

In a third terminal while the node is running:

```sh
export THIRDSHIFT_OPERATOR_TOKEN=dev-operator-token
export THIRDSHIFT_COORDINATOR_URL=http://127.0.0.1:8080
export THIRDSHIFT_NODE_DATA_DIR="$(pwd)/.local/thirdshift-node"

thirdshift status --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
admin-cli nodes list --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
thirdshift pause --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
thirdshift resume --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
```

Expected result: `thirdshift status` shows the node id, `AVAILABLE` or `PAUSED` state, model id, GPU block, and session connectivity. `admin-cli nodes list` shows the same node with recent heartbeat age and `connected` session status.

## Milestone 3 Routed Local Request

This sequence runs the coordinator locally, attaches a node to the in-repo fake llama-compatible runtime, and completes one developer request through `/v1/chat/completions`.

Terminal 1:

```sh
docker compose -f deploy/docker-compose.yml up -d postgres

export THIRDSHIFT_OPERATOR_TOKEN=dev-operator-token
export THIRDSHIFT_ACCESS_TOKEN_SECRET=dev-access-token-secret-change-me
export THIRDSHIFT_COORDINATOR_URL=http://127.0.0.1:8080
export THIRDSHIFT_COORDINATOR_ADDR=:8080
export THIRDSHIFT_DATABASE_URL='postgres://thirdshift:thirdshift_dev_password@localhost:5432/thirdshift?sslmode=disable'

go run ./cmd/admin-cli migrate --database-url "$THIRDSHIFT_DATABASE_URL"
go run ./cmd/coordinator
```

Terminal 2:

```sh
go run ./tests/fixtures/fake-llama-server --host 127.0.0.1 --port 18081 --model fake.gguf
```

Terminal 3:

```sh
export THIRDSHIFT_OPERATOR_TOKEN=dev-operator-token
export THIRDSHIFT_COORDINATOR_URL=http://127.0.0.1:8080
export THIRDSHIFT_NODE_DATA_DIR="$(pwd)/.local/thirdshift-m3-node"
export FLEET_ID=fleet_01J0M000000000000000000000

go run ./cmd/admin-cli catalog sync --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"

export ORG_ID="$(
  go run ./cmd/admin-cli org create --name "Thirdshift Dev" --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN" |
  awk '/^org_id:/ {print $2}'
)"

export THIRDSHIFT_API_KEY="$(
  go run ./cmd/admin-cli apikey create --org "$ORG_ID" --model thirdshift-tiny-chat-v1 --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN" |
  awk '/^key:/ {print $2}'
)"

export THIRDSHIFT_INVITE_TOKEN="$(
  go run ./cmd/admin-cli invite create --fleet "$FLEET_ID" --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN" |
  awk '/^token:/ {print $2}'
)"

go run ./cmd/thirdshift login --invite "$THIRDSHIFT_INVITE_TOKEN" --coordinator "$THIRDSHIFT_COORDINATOR_URL" --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
go run ./cmd/thirdshift start --coordinator "$THIRDSHIFT_COORDINATOR_URL" --data-dir "$THIRDSHIFT_NODE_DATA_DIR" --runtime-base-url http://127.0.0.1:18081 --heartbeat-interval 5s &
export THIRDSHIFT_NODE_PID=$!
sleep 3

curl -sS "$THIRDSHIFT_COORDINATOR_URL/v1/chat/completions" \
  -H "Authorization: Bearer $THIRDSHIFT_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: demo-001" \
  -d '{
    "model": "thirdshift-tiny-chat-v1",
    "messages": [{"role":"user","content":"Write one short Thirdshift demo sentence."}],
    "temperature": 0.2,
    "max_tokens": 32,
    "stream": false
  }'

go run ./cmd/thirdshift status --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
go run ./cmd/admin-cli nodes list --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
kill "$THIRDSHIFT_NODE_PID"
```

Expected result: the curl response has `object: "chat.completion"`, one assistant message, `usage`, and a `thirdshift` object containing `job_id`, `attempts`, `data_class`, and `served_region`.

## Milestone 4 Safety And Reliability Drill

Use the Milestone 3 setup with one coordinator, one fake runtime, and one node.

Configure an overnight host window and verify status:

```sh
export THIRDSHIFT_NODE_DATA_DIR="$(pwd)/.local/thirdshift-m3-node"

go run ./cmd/thirdshift configure --data-dir "$THIRDSHIFT_NODE_DATA_DIR" --from 23:00 --until 08:00 --max-temp 78 --hard-temp 88 --thermal-hysteresis 5
go run ./cmd/thirdshift status --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
go run ./cmd/admin-cli nodes list --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
```

Pause and resume:

```sh
go run ./cmd/thirdshift pause --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
go run ./cmd/thirdshift status --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
go run ./cmd/admin-cli nodes list --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"

go run ./cmd/thirdshift resume --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
go run ./cmd/thirdshift status --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
```

Drain behavior:

```sh
# In one terminal, send a normal chat completion request.
make chat-demo

# While a real long-running request is active, pause should show DRAINING.
go run ./cmd/thirdshift pause --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
go run ./cmd/thirdshift status --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
```

Expected result: an idle paused node is not assigned new work. If a request is already running, the node reports `DRAINING`, completes that request, then reports `PAUSED`.

Thermal drill on a Windows NVIDIA host:

```powershell
nvidia-smi --query-gpu=name,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits
go run ./cmd/thirdshift status --data-dir $env:THIRDSHIFT_NODE_DATA_DIR
go run ./cmd/admin-cli nodes list --coordinator $env:THIRDSHIFT_COORDINATOR_URL --operator-token $env:THIRDSHIFT_OPERATOR_TOKEN
```

Expected result: normal temperatures show `thermal_state: normal`. Test-only scripted telemetry covers warm/hard-limit transitions in CI; do not induce unsafe temperatures on real hardware.

## Milestone 5 Verification, Ledger, And Payout Drill

Use the Milestone 3 routed local request flow to create at least one successful completion. The coordinator will record token usage, price version, a balanced posted ledger transaction, and a pending host credit hold.

Inspect unit economics:

```sh
export THIRDSHIFT_DATABASE_URL='postgres://thirdshift:thirdshift_dev_password@localhost:5432/thirdshift?sslmode=disable'

go run ./cmd/admin-cli report economics --database-url "$THIRDSHIFT_DATABASE_URL"
```

Release credits whose hold has elapsed:

```sh
go run ./cmd/admin-cli credits release --database-url "$THIRDSHIFT_DATABASE_URL"
```

Create and export a payout batch:

```sh
export BATCH_ID="$(
  go run ./cmd/admin-cli payout create --database-url "$THIRDSHIFT_DATABASE_URL" |
  awk '/^batch_id:/ {print $2}'
)"

go run ./cmd/admin-cli payout export --database-url "$THIRDSHIFT_DATABASE_URL" --batch "$BATCH_ID" --out payout.csv
```

Review `payout.csv`. It contains:

```csv
host_id,account_reference,amount_microdollars,memo
```

After an operator has paid the listed hosts externally, confirm the batch:

```sh
go run ./cmd/admin-cli payout confirm --database-url "$THIRDSHIFT_DATABASE_URL" --batch "$BATCH_ID" --file payout.csv
go run ./cmd/admin-cli report economics --database-url "$THIRDSHIFT_DATABASE_URL"
```

Expected result: the batch moves `draft -> exported -> paid`, associated host credit holds move to `paid`, and the payout ledger transaction balances to zero. Exported payout item contents are immutable; corrections must be handled by void/reversal workflow rather than editing posted ledger rows.

To void an unpaid batch and release reserved credits back to available:

```sh
go run ./cmd/admin-cli payout void --database-url "$THIRDSHIFT_DATABASE_URL" --batch "$BATCH_ID" --reason "operator correction"
```

Voiding a paid batch posts a reversal transaction for the payout ledger transaction.

Challenge and duplicate verification are coordinator policy paths. Integration tests cover duplicate sampling on a second node and challenge quarantine after repeated failures. A single challenge disagreement records reputation impact but does not quarantine the node.

## Milestone 6 Operator Console And Fleet Drill

Start the local stack with the console behind Caddy:

```sh
export THIRDSHIFT_OPERATOR_TOKEN=dev-operator-token
export THIRDSHIFT_ACCESS_TOKEN_SECRET=dev-access-token-secret-change-me
docker compose -f deploy/docker-compose.yml up --build
```

Open `http://127.0.0.1:8081/internal-console` and enter the operator token. The console uses `sessionStorage`; closing the tab or pressing Lock removes the in-memory session for that tab.

Create an organization, fleet, catalog entries, API key, and invite:

```sh
export THIRDSHIFT_COORDINATOR_URL=http://127.0.0.1:8081
export THIRDSHIFT_OPERATOR_TOKEN=dev-operator-token

export ORG_ID="$(
  go run ./cmd/admin-cli org create --name "Cafe Alpha" --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN" |
  awk '/^org_id:/ {print $2}'
)"

export FLEET_ID="$(
  go run ./cmd/admin-cli fleet create --org "$ORG_ID" --name "Cafe Alpha Floor" --schedule-from 23:00 --schedule-until 08:00 --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN" |
  awk '/^fleet_id:/ {print $2}'
)"

go run ./cmd/admin-cli catalog sync --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
go run ./cmd/admin-cli apikey create --org "$ORG_ID" --model thirdshift-tiny-chat-v1 --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
go run ./cmd/admin-cli invite create --fleet "$FLEET_ID" --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
```

After a node logs in with that invite, verify the fleet schedule default:

```sh
go run ./cmd/thirdshift status --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
```

Exercise console actions from the Nodes and Jobs pages:

```sh
curl -sS -X POST "$THIRDSHIFT_COORDINATOR_URL/internal/v1/nodes/$NODE_ID/drain" \
  -H "Authorization: Bearer $THIRDSHIFT_OPERATOR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"reason":"operator drill"}'

curl -sS "$THIRDSHIFT_COORDINATOR_URL/internal/v1/audit" \
  -H "Authorization: Bearer $THIRDSHIFT_OPERATOR_TOKEN"
```

Expected result: drain, pause, quarantine, retry, cancel, and payout actions appear in the Audit page and in `/internal/v1/audit`. Job pages never show prompt or completion bodies.

Export a fleet report:

```sh
go run ./cmd/admin-cli fleet report --fleet "$FLEET_ID" --from 2026-01-01 --to 2027-01-01 --out fleet-report.csv --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
```

The report columns are:

```csv
fleet_id,node_id,jobs_succeeded,jobs_failed,prompt_tokens,completion_tokens,host_credit_microdollars
```

## Milestone 7 Launch Readiness Drill

Public status:

```sh
curl -sS http://127.0.0.1:8081/v1/status | jq .
open http://127.0.0.1:8081/status
```

Expected result: the API and page show connected nodes, model availability, completed jobs, output tokens served, and estimated GPU-hours reused. List fields return `[]` when empty.

Contribution card:

```sh
go run ./cmd/thirdshift card --data-dir "$THIRDSHIFT_NODE_DATA_DIR"
go run ./cmd/thirdshift card --data-dir "$THIRDSHIFT_NODE_DATA_DIR" --json
curl -sS "$THIRDSHIFT_COORDINATOR_URL/v1/nodes/$NODE_ID/card" | jq .
```

Expected result: the card shows aggregate host contribution only: node name/id, nights active, jobs accepted, tokens served, and credit earned.

Windows install path:

```powershell
irm https://github.com/Ani-HQ/thirdshift/releases/latest/download/install.ps1 | iex
thirdshift --version
thirdshift doctor
thirdshift login --invite $env:THIRDSHIFT_INVITE_TOKEN --coordinator $env:THIRDSHIFT_COORDINATOR_URL
thirdshift start
thirdshift status
```

Updater verification:

```powershell
thirdshift update --manifest https://github.com/Ani-HQ/thirdshift/releases/latest/download/release-manifest.json
thirdshift --version
```

Uninstall:

```powershell
irm https://github.com/Ani-HQ/thirdshift/releases/latest/download/uninstall.ps1 | iex
```

Use `-PurgeData` only when intentionally removing node credentials and model cache.

Release operator checklist:

```sh
make lint
go test -count=1 ./...
GOOS=windows GOARCH=amd64 go build ./...
make console-build
make console-test
make ps-check
```

Before publishing a draft release, confirm that `release-manifest.json` has a non-empty Ed25519 signature and that `SHA256SUMS` matches every zip. Follow [docs/RELEASE.md](RELEASE.md) for key generation, signing, and user verification.

## Public Catalog And Waitlist Drill

Set aggregate regions:

```sh
export THIRDSHIFT_COORDINATOR_URL=http://127.0.0.1:8081
export THIRDSHIFT_OPERATOR_TOKEN=dev-operator-token

go run ./cmd/admin-cli fleet set-region --fleet "$FLEET_ID" --region in-south --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
go run ./cmd/admin-cli node set-region --node "$NODE_ID" --region eu-west --coordinator "$THIRDSHIFT_COORDINATOR_URL" --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
```

Node region overrides fleet region. Clear an override by passing an empty region value.

Preview the public catalog without Docker or Caddy:

```sh
THIRDSHIFT_OPERATOR_TOKEN=dev-operator-token THIRDSHIFT_ACCESS_TOKEN_SECRET=dev-access-token-secret-change-me THIRDSHIFT_DATABASE_URL="$THIRDSHIFT_DATABASE_URL" go run ./cmd/coordinator
```

In a second shell:

```sh
cd web/console && THIRDSHIFT_COORDINATOR_URL=http://127.0.0.1:8080 npm run dev
```

Open `http://127.0.0.1:3000/status`. The Next dev server proxies `/v1/*` to the coordinator when `THIRDSHIFT_COORDINATOR_URL` is set.

Check the public API directly:

```sh
curl -sS -H 'X-Geo-Region: in-south' http://127.0.0.1:8080/v1/status | jq .
curl -sS -X POST http://127.0.0.1:8080/v1/waitlist \
  -H 'Content-Type: application/json' \
  -d '{"email":"dev@example.com","use_case":"alpha model eval"}' | jq .
```

Read and export the waitlist:

```sh
go run ./cmd/admin-cli waitlist list --coordinator http://127.0.0.1:8080 --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
go run ./cmd/admin-cli waitlist export --out waitlist.csv --coordinator http://127.0.0.1:8080 --operator-token "$THIRDSHIFT_OPERATOR_TOKEN"
```
