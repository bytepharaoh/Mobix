# Mobix Makefile
# Commands for building, running, testing, and deploying

.PHONY: docker-up docker-down run-driver run-trip run-gateway run-payment help setup-migrations migration migrate-up migrate-down

# Show available commands and their descriptions
help:
	@echo Available commands:
	@echo make docker-up: Create docker containers and start infrastructure (MongoDB, RabbitMQ, Jaeger)
	@echo make docker-down: Stop docker containers
	@echo make run-driver: Run driver/main.go driver service
	@echo make run-trip: Run trip/main.go trip service
	@echo make run-gateway: Run gateaway/main.go gateway service
	@echo make run-payment: Run payment/payment.go payment service
	@echo make setup-migrations: Use this before migrations for installing Nodejs and its dependencies
	@echo make migration name=test: Create a new migration file. In this migration file you should write new changes to our db and then apply them. Example "make migration name=smth"
	@echo make migrate-up: Apply last migration version to a db
	@echo make migrate-down: Go one version down in migrations

# Create docker containers and start infrastructure (MongoDB, RabbitMQ, Jaeger)
docker-up:
	docker compose up -d

# Stop docker containers
docker-down:
	docker compose down

# Run driver/main.go driver service
run-driver:
	go run ./cmd/driver/main.go

# Run trip/main.go trip service
run-trip:
	go run ./cmd/trip/main.go

# Run gateway/main.go gateway service
run-gateway:
	go run ./cmd/gateway/main.go

# Run payment/main.go payment service
run-payment:
	go run ./cmd/payment/main.go

# Create a new migration file. Example "make migration name=smth"
# Npx tool for running Nodejs without global installation
migration:
	docker compose run --rm migrations \
	npx migrate-mongo create $(name)

# Apply last migration version to a db
migrate-up:
	docker compose run --rm migrations \
	npx migrate-mongo up

# Go one version down in migrations
migrate-down:
	docker compose run --rm migrations \
	npx migrate-mongo down