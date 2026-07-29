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
