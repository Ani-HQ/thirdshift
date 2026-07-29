GO ?= go
MIGRATIONS_DIR ?= migrations

.PHONY: build test lint migrate dev test-slice test-integration build-windows doctor-json run-local coordinator org catalog-sync apikey fake-runtime chat-demo invite nodes start status

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

start:
	$(GO) run ./cmd/thirdshift start

status:
	$(GO) run ./cmd/thirdshift status
