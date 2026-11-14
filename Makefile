SHELL := /bin/sh
SERVICES := gateway trip driver payment

.PHONY: help up down docker-up docker-down logs build clean test test-unit test-repo demo dispatch-demo realtime-demo rate-limit-demo verify k8s-render fmt fmt-check vet check mock

help:
	@printf '%s\n' \
	  'make up          Build and start the full local stack' \
	  'make down        Stop the stack and keep persisted data' \
	  'make logs        Follow all service logs' \
	  'make demo        Run a complete ride and print its payment receipt' \
	  'make dispatch-demo  Run a ride through automatic driver offers' \
	  'make realtime-demo  Verify an authenticated SSE trip stream' \
	  'make rate-limit-demo  Verify enforcement and Redis outage behavior' \
	  'make verify      Run HTTP acceptance and broker recovery checks' \
	  'make build       Compile all four binaries into bin/' \
	  'make test        Run all Go tests with the race detector (needs Docker)' \
	  'make test-unit   Run tests that do not require Docker' \
	  'make check       Check formatting and run go vet' \
	  'make k8s-render  Render the local Kubernetes overlay'

up docker-up:
	docker compose up -d --build --wait

down docker-down:
	docker compose down

logs:
	docker compose logs -f

build:
	@mkdir -p bin
	@set -e; for service in $(SERVICES); do go build -o "bin/$$service" "./cmd/$$service"; done

clean:
	rm -rf bin
	rm -f coverage.out coverage.html

test:
	go test -race ./...

test-unit:
	go test -short -race ./...

test-repo:
	go test -race ./internal/driver/repository/... ./internal/trip/repository/... ./internal/payment/repository/...

demo:
	bash scripts/demo.sh

dispatch-demo:
	bash scripts/dispatch-demo.sh

realtime-demo:
	bash scripts/realtime-demo.sh

rate-limit-demo:
	bash scripts/rate-limit-demo.sh

verify:
	bash scripts/verify.sh

k8s-render:
	kubectl kustomize deployments/k8s/demo

fmt:
	gofmt -w $$(find api cmd internal pkg -name '*.go' -type f)

fmt-check:
	@files="$$(gofmt -l $$(find api cmd internal pkg -name '*.go' -type f))"; \
	if [ -n "$$files" ]; then printf '%s\n' "$$files"; exit 1; fi

vet:
	go vet ./...

check: fmt-check vet

mock:
	go run go.uber.org/mock/mockgen@v0.6.0 -source=internal/trip/repository/repository.go \
	    -destination=internal/trip/service/mocks/mock_repository.go -package=mocks
