GO ?= go
MIGRATIONS_DIR ?= migrations

.PHONY: build test lint migrate dev test-slice test-integration build-windows doctor-json run-local coordinator org catalog-sync apikey fake-runtime chat-demo invite nodes credits-release payout-create payout-export payout-confirm payout-void report-economics console-dev console-build console-test start status configure pause resume

build:
	$(GO) build ./...

test:
	$(GO) test ./...

lint:
	@test -z "$$(gofmt -l .)" || (echo "gofmt required:"; gofmt -l .; exit 1)
	$(GO) vet ./...

migrate:
	$(GO) run ./cmd/admin-cli migrate --migrations-dir $(MIGRATIONS_DIR)

dev:
	docker compose -f deploy/docker-compose.yml up --build

test-slice:
	$(GO) test -tags slice ./cmd/thirdshift

test-integration:
	$(GO) test -tags integration ./...

build-windows:
	GOOS=windows GOARCH=amd64 $(GO) build ./...

doctor-json:
	$(GO) run ./cmd/thirdshift doctor --json

run-local:
	$(GO) run ./cmd/thirdshift run-local --model thirdshift-tiny-chat-v1 --prompt "$${PROMPT:-Say hello from Thirdshift.}"

coordinator:
	$(GO) run ./cmd/coordinator

org:
	$(GO) run ./cmd/admin-cli org create --name "$${ORG_NAME:-Thirdshift Dev}"

catalog-sync:
	$(GO) run ./cmd/admin-cli catalog sync

apikey:
	$(GO) run ./cmd/admin-cli apikey create --org "$${ORG_ID:?set ORG_ID}" --model "$${MODEL_ID:-thirdshift-tiny-chat-v1}"

fake-runtime:
	$(GO) run ./tests/fixtures/fake-llama-server --host 127.0.0.1 --port "$${FAKE_RUNTIME_PORT:-18081}" --model fake.gguf

chat-demo:
	curl -sS "$${THIRDSHIFT_COORDINATOR_URL:-http://127.0.0.1:8080}/v1/chat/completions" -H "Authorization: Bearer $${THIRDSHIFT_API_KEY:?set THIRDSHIFT_API_KEY}" -H "Content-Type: application/json" -H "Idempotency-Key: demo-001" -d '{"model":"thirdshift-tiny-chat-v1","messages":[{"role":"user","content":"Write one Thirdshift demo sentence."}],"temperature":0.2,"max_tokens":32,"stream":false}'

invite:
	$(GO) run ./cmd/admin-cli invite create --fleet "$${FLEET_ID:?set FLEET_ID}"

nodes:
	$(GO) run ./cmd/admin-cli nodes list

credits-release:
	$(GO) run ./cmd/admin-cli credits release

payout-create:
	$(GO) run ./cmd/admin-cli payout create

payout-export:
	$(GO) run ./cmd/admin-cli payout export --batch "$${BATCH_ID:?set BATCH_ID}" --out "$${PAYOUT_CSV:-payout.csv}"

payout-confirm:
	$(GO) run ./cmd/admin-cli payout confirm --batch "$${BATCH_ID:?set BATCH_ID}" --file "$${PAYOUT_CSV:-payout.csv}"

payout-void:
	$(GO) run ./cmd/admin-cli payout void --batch "$${BATCH_ID:?set BATCH_ID}"

report-economics:
	$(GO) run ./cmd/admin-cli report economics

console-dev:
	cd web/console && npm run dev

console-build:
	cd web/console && npm ci && npm run typecheck && npm run build

console-test:
	cd web/console && npm ci && npm test -- --run

start:
	$(GO) run ./cmd/thirdshift start

status:
	$(GO) run ./cmd/thirdshift status

configure:
	$(GO) run ./cmd/thirdshift configure --from "$${THIRDSHIFT_SCHEDULE_FROM:-23:00}" --until "$${THIRDSHIFT_SCHEDULE_UNTIL:-08:00}"

pause:
	$(GO) run ./cmd/thirdshift pause

resume:
	$(GO) run ./cmd/thirdshift resume
