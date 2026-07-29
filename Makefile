GO ?= go
MIGRATIONS_DIR ?= migrations

.PHONY: build test lint migrate dev

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
