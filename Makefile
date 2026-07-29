GO ?= go
MIGRATIONS_DIR ?= migrations

.PHONY: build test lint migrate dev test-slice build-windows doctor-json run-local

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

build-windows:
	GOOS=windows GOARCH=amd64 $(GO) build ./...

doctor-json:
	$(GO) run ./cmd/thirdshift doctor --json

run-local:
	$(GO) run ./cmd/thirdshift run-local --model thirdshift-tiny-chat-v1 --prompt "$${PROMPT:-Say hello from Thirdshift.}"
