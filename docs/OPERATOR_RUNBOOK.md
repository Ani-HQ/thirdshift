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
