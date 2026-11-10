# Mobix

Mobix is a Go backend for a ride-sharing workflow. Register a driver, request a ride, dispatch it to an available driver, and follow it through completion. RabbitMQ delivers completed rides to the payment service, which records a simulated receipt.

## Quick start

Install Docker with Compose v2, Bash, `make`, `curl`, and `jq`. From a clone of this repository, run:

```sh
make up
make demo
```

Open the [API explorer](http://127.0.0.1:8080/docs) to send requests against the running gateway. Replace the example driver details on repeat runs and use the IDs returned by the API for follow-up requests. The demo creates a new driver and ride, completes the ride, and waits for the receipt. The [OpenAPI contract](api/openapi/mobix.json) is also served at `/openapi.json`.

The Compose gateway binds to `127.0.0.1`. To use a different port and require a local bearer token:

```sh
export GATEWAY_PORT=18080
export MOBIX_URL=http://127.0.0.1:18080
export MOBIX_API_KEY=mobix-local-key
make up
make demo
```

Compose accepts the overrides listed in [`.env.example`](.env.example). The demo and verification scripts read `MOBIX_URL` and `MOBIX_API_KEY` from the shell. Use the explorer’s API key field or send `Authorization: Bearer <key>` for `/v1/` requests when a key is configured. Run `make logs` to follow service logs and `make down` to stop the stack while keeping its volumes.

## Ride lifecycle

| Step | Request | Result |
| --- | --- | --- |
| Register a driver | `POST /v1/drivers` | Creates an offline driver. Email, phone and license plate are unique. |
| Set availability | `PATCH /v1/drivers/{id}/availability` | Accepts `available` or `offline`. Use `PATCH /v1/drivers/{id}/location` to update coordinates. |
| Request a ride | `POST /v1/trips` | Stores pickup and drop-off and returns an estimated fare in USD cents. An optional `Idempotency-Key` makes create retries safe. |
| Dispatch | `PATCH /v1/trips/{id}/status` | Send `{"status":"assigned","driver_id":"<id>"}`. The driver must be available and can have one active ride. |
| Start and complete | `PATCH /v1/trips/{id}/status` | Send `{"status":"in_progress"}` and then `{"status":"completed"}`. A requested or assigned ride can also be cancelled. |
| View driver activity | `GET /v1/drivers/{id}/activity` | Returns effective status, an active trip ID when present, and completed trip count. |
| Read the receipt | `GET /v1/payments/{tripID}` | Returns the simulated payment. Retry briefly on `PAYMENT_NOT_FOUND` after completion. |

Use `GET /v1/trips/{id}` to inspect a ride at any step. Trip creation retry keys are unique across the gateway. Retrying with the same key and trip details returns the existing trip in its current state. Reusing a key with different details returns HTTP 409. Repeating a transition with the same target state returns the current trip while it is still in that state.

## Screenshots

The running API explorer and a receipt retrieved from a completed demo ride.

![Mobix API explorer showing the ride request form](docs/screenshots/api-explorer.png)

![Mobix API explorer showing the live response for a simulated payment receipt](docs/screenshots/payment-receipt.png)

## How the services work together

The **gateway** exposes the HTTP API. **Driver** manages registration, declared availability and location. **Trip** owns dispatch, the fare estimate, the state machine and driver activity derived from trip history. **Payment** consumes completion events and writes receipts to its own MongoDB database.

Trip state changes use a conditional MongoDB update. A partial unique index allows a driver to appear on one assigned or in-progress trip at a time. The driver service supplies the availability snapshot used during dispatch. Going offline opts out of future dispatch checks while an existing assignment remains active. Fare estimates use straight-line distance, a $3.50 base fare, and $1.20 per kilometer, rounded to cents.

Completing a trip writes its payment event into the trip document atomically. The publisher removes the pending event after RabbitMQ confirms delivery to the durable queue. During a broker outage the event stays in MongoDB, including across trip-service restarts. Delivery is at least once, and MongoDB enforces one receipt per trip. The consumer acknowledges after recording a receipt and sends malformed messages to the dead-letter queue.

`GET /health` checks process liveness. `GET /ready` checks the backend HTTP services and their MongoDB connections. A broker outage can delay receipts while the gateway stays ready. Inspect trip and payment logs, the `pending_payment` fields in MongoDB, and the RabbitMQ queue `mobix.payment.trip-completed.v1` and its `.dead` counterpart when investigating a backlog.

## Kubernetes

For a local demo, build the four app images with Docker Compose and load them into a local [kind](https://kind.sigs.k8s.io/) cluster. Use a cluster with a default StorageClass for the MongoDB and RabbitMQ volumes.

```sh
docker compose build --provenance=false
kind create cluster --name mobix
kind load docker-image --name mobix \
  mobix/gateway:local mobix/driver:local mobix/trip:local mobix/payment:local
kubectl --context kind-mobix apply -k deployments/k8s/demo
kubectl --context kind-mobix -n mobix rollout status deployment/gateway --timeout=180s
kubectl --context kind-mobix -n mobix port-forward service/gateway 18081:8080
```

In another terminal:

```sh
MOBIX_URL=http://127.0.0.1:18081 MOBIX_API_KEY=mobix-demo-key make demo
```

For a deployment with external dependencies, use [the base manifests](deployments/k8s/base) in an environment-specific Kustomize overlay. Set the four images to immutable registry references and provide a `mobix-dependencies` Secret in the `mobix` namespace with these keys:

| Keys | Values |
| --- | --- |
| `gateway-api-key` | Shared operator bearer token |
| `trip-mongo-uri`, `driver-mongo-uri`, `payment-mongo-uri` | MongoDB connection strings. Give each service access to its own collections and required index operations. |
| `trip-rabbitmq-url`, `payment-rabbitmq-url` | AMQP connection strings scoped to the Mobix queues and their declaration permissions. |

The gateway Service is cluster-local. The internal HTTP ingress policies require a CNI that enforces NetworkPolicy. Use TLS and an identity-aware ingress for shared environments. The demo uses single-node infrastructure and development credentials. Dispatch is manual, payments are simulated, and the bearer token grants operator-level access. A public rider or driver deployment requires per-user authentication and resource authorization.

## Development and verification

Use Go 1.27.2 to match the container builder and GitHub Actions.

```sh
make check       # formatting and vet
make build       # compile the four services
make test-unit   # tests without Docker
make test        # race detector and MongoDB Testcontainers tests
make verify      # HTTP validation, concurrent dispatch and broker recovery
make k8s-render  # render the local Kubernetes overlay
```

Run `make verify` against a local test stack. It stops and restarts RabbitMQ and restarts the trip service while a payment event is pending. GitHub Actions runs the Go checks, a fresh Compose demo, the outage test, and Kustomize rendering on pull requests.

## License

[MIT](LICENSE)
