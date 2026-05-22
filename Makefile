# ============================================================
#  Mobix — Makefile
#  All commands for building, running, testing, and deploying
# ============================================================

.PHONY: docker-up docker-down run-driver run-trip run-gateway run-payment run-all

docker-up: # Start local infrastructure (MongoDB, RabbitMQ, Jaeger)
	docker compose up -d

docker-down: # Stop local infrastructure
	docker compose down

run-driver: # Run driver service
	go run ./cmd/driver/main.go

run-trip: # Run trip service
	go run ./cmd/trip/main.go

run-gateway: # Run gateway service
	go run ./cmd/gateway/main.go

run-payment: # Run payment service
	go run ./cmd/payment/main.go