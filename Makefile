# Mobix Makefile
# Commands for building, running, testing, linting, and deploying

.PHONY: help docker-up docker-down \
        run-gateway run-trip run-driver run-payment \
        build clean test test-repo test-coverage \
        fmt lint vet check \
        setup-migrations migration migrate-up migrate-down

# ——— Help ——————————————————————————————————————————————
help:
	@echo ""
	@echo "Usage: make <command>"
	@echo ""
	@echo "Infrastructure:"
	@echo "  docker-up          Start MongoDB, RabbitMQ, Jaeger"
	@echo "  docker-down        Stop all containers"
	@echo ""
	@echo "Services:"
	@echo "  run-gateway        Run the gateway service"
	@echo "  run-trip           Run the trip service"
	@echo "  run-driver         Run the driver service"
	@echo "  run-payment        Run the payment service"
	@echo ""
	@echo "Build:"
	@echo "  build              Compile all four services"
	@echo "  clean              Remove compiled binaries"
	@echo ""
	@echo "Testing:"
	@echo "  test               Run all tests with race detector"
	@echo "  test-repo          Run repository integration tests only"
	@echo "  test-coverage      Run tests and generate HTML coverage report"
	@echo ""
	@echo "Code quality:"
	@echo "  fmt                Format all Go files"
	@echo "  lint               Run golangci-lint"
	@echo "  vet                Run go vet"
	@echo "  check              Run fmt + vet + lint (run before committing)"
	@echo ""
	@echo "Migrations:"
	@echo "  migration name=x   Create a new migration file"
	@echo "  migrate-up         Apply latest migration"
	@echo "  migrate-down       Roll back last migration"
	@echo ""

# ——— Infrastructure ————————————————————————————————————
docker-up:
	docker compose up -d

docker-down:
	docker compose down

# ——— Services ——————————————————————————————————————————
run-gateway:
	go run ./cmd/gateway

run-trip:
	go run ./cmd/trip

run-driver:
	go run ./cmd/driver

run-payment:
	go run ./cmd/payment

# ——— Build —————————————————————————————————————————————
build:
	@mkdir -p bin
	go build -o bin/gateway ./cmd/gateway
	go build -o bin/trip ./cmd/trip
	go build -o bin/driver ./cmd/driver
	go build -o bin/payment ./cmd/payment

clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# ——— Testing ———————————————————————————————————————————
test:
	go test -v -race ./...

test-repo:
	go test -v -race ./internal/trip/repository/... ./internal/driver/repository/...

test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# ——— Code quality ——————————————————————————————————————
fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	golangci-lint run ./...

check: fmt vet lint
	@echo "All checks passed — safe to push"

# ——— Migrations ————————————————————————————————————————
migration:
	docker run --rm \
		-v $(PWD)/db/migrations:/migrations \
		-v $(PWD)/migrate-mongo-config.js:/migrate-mongo-config.js \
		--network host \
		node:20-alpine sh -c "npm install -g migrate-mongo && migrate-mongo create $(name)"

migrate-up:
	docker run --rm \
		-v $(PWD)/db/migrations:/migrations \
		-v $(PWD)/migrate-mongo-config.js:/migrate-mongo-config.js \
		--network host \
		node:20-alpine sh -c "npm install -g migrate-mongo && migrate-mongo up"

migrate-down:
	docker run --rm \
		-v $(PWD)/db/migrations:/migrations \
		-v $(PWD)/migrate-mongo-config.js:/migrate-mongo-config.js \
		--network host \
		node:20-alpine sh -c "npm install -g migrate-mongo && migrate-mongo down"