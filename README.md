# Mobix

Mobix is a Go backend for a ride-sharing workflow with individual rider, driver, and operator accounts. Register a driver, request a ride, dispatch it to an available driver, and follow it through completion. RabbitMQ delivers completed rides to the payment service, which records a simulated receipt.

## Quick start

Install Docker with Compose v2, Bash, `make`, `curl`, and `jq`. Run these commands from your clone.

```sh
cp .env.example .env
make up
make demo
```

Open the [API explorer](http://127.0.0.1:8080/docs) and use **Sign in** with the development operator credentials from `.env.example`. A successful sign-in sets the explorer's access token for subsequent requests. The operator can provision accounts, and each rider and driver signs in with their own credentials. The demo provisions a rider and driver, dispatches a ride as the operator, completes it as the assigned driver, and reads the receipt as the rider.

The Compose gateway binds to `127.0.0.1`. Set these variables to use another port.

```sh
export GATEWAY_PORT=18080
export MOBIX_URL=http://127.0.0.1:18080
make up
make demo
```

Compose reads the configuration in [`.env.example`](.env.example) through your local `.env` file. The scripts read `MOBIX_URL` and the operator credentials from your shell. When you customize the operator account, export `MOBIX_OPERATOR_EMAIL` and `MOBIX_OPERATOR_PASSWORD` before running `make demo` or `make verify`. Run `make logs` to follow service logs and `make down` to stop the stack while keeping its volumes.

## Accounts and permissions

An initial operator is created when the gateway starts with `MOBIX_BOOTSTRAP_EMAIL` and `MOBIX_BOOTSTRAP_PASSWORD`. Existing accounts and passwords are preserved across restarts. Operators provision accounts with `POST /v1/accounts`. Passwords use bcrypt, and the database stores a SHA-256 digest of each random session token. Sessions expire after 12 hours, and signing out revokes the current session immediately at the gateway.

| Endpoint | Purpose |
| --- | --- |
| `POST /v1/auth/login` | Exchange email and password for an access token. |
| `GET /v1/auth/me` | Read the signed-in account. |
| `POST /v1/auth/logout` | Revoke the current access token. |
| `POST /v1/accounts` | Provision a rider, driver, or operator account as an operator. |

Send `Authorization: Bearer <access_token>` on protected requests. The [OpenAPI contract](api/openapi/mobix.json), also served at `/openapi.json`, includes the request bodies and examples.

| Action | Rider | Driver | Operator |
| --- | --- | --- | --- |
| Create a trip | Own account | — | — |
| Read a trip | Own trip | Assigned trip | Any trip |
| Assign a driver | — | — | Allowed |
| Start or complete a trip | — | Assigned driver | — |
| Cancel before the trip starts | Own trip | — | Any trip |
| Register or update a driver profile | — | Own profile | — |
| Read a driver profile or activity | — | Own profile | Any profile |
| Read a payment receipt | Own trip | — | Any trip |

The gateway resolves the session identity and creates a short-lived service-specific signed assertion. Backend services verify the assertion and enforce their resource permissions. Trip obtains dispatch information through an authenticated internal driver endpoint. Payment checks access through Trip before returning a receipt.

## Ride lifecycle

| Step | Request | Result |
| --- | --- | --- |
| Register a driver | `POST /v1/drivers` | A signed-in driver creates one offline profile. Email, phone and license plate are unique. Use `GET /v1/drivers/me` to find your profile later. |
| Set availability | `PATCH /v1/drivers/{id}/availability` | Accepts `available` or `offline`. Use `PATCH /v1/drivers/{id}/location` to update coordinates. |
| Request a ride | `POST /v1/trips` | Uses the signed-in rider as the owner, stores pickup and drop-off, and returns an estimated fare in USD cents. An optional `Idempotency-Key` makes create retries safe. |
| Dispatch | `PATCH /v1/trips/{id}/status` | The operator sends `{"status":"assigned","driver_id":"<id>"}`. The driver must be available and can have one active ride. |
| Start and complete | `PATCH /v1/trips/{id}/status` | The assigned driver sends `{"status":"in_progress"}` and then `{"status":"completed"}`. A requested or assigned ride can also be cancelled. |
| View driver activity | `GET /v1/drivers/{id}/activity` | Returns effective status, an active trip ID when present, and completed trip count. |
| Read the receipt | `GET /v1/payments/{tripID}` | Returns the simulated payment. Retry briefly on `PAYMENT_NOT_FOUND` after completion. |

Use `GET /v1/trips/{id}` to inspect a ride at any step. Trip creation retry keys are scoped to the signed-in rider. Different riders can use the same key independently. Retrying with the same key and trip details returns the existing trip in its current state. Reusing a key with different details returns HTTP 409. Repeating a transition with the same target state returns the current trip while it is still in that state.

## Screenshots

The original operator demo’s API explorer and simulated receipt. The current explorer opens on the sign-in form.

![Mobix API explorer showing the ride request form](docs/screenshots/api-explorer.png)

![Mobix API explorer showing the live response for a simulated payment receipt](docs/screenshots/payment-receipt.png)

## How the services work together

The **gateway** exposes the HTTP API. **Driver** manages registration, declared availability and location. **Trip** owns dispatch, the fare estimate, the state machine and driver activity derived from trip history. **Payment** consumes completion events and writes receipts to its own MongoDB database.

![Mobix service architecture showing HTTP calls, RabbitMQ events, and MongoDB access](docs/screenshots/Mobix%20Service%20Architecture%20Diagram.png)

*The gateway authenticates account sessions and delegates signed identities to the services over HTTP. RabbitMQ carries trip completion events to the payment service. The driver records shown in the diagram live in the `driver` collection.*

Trip state changes use a conditional MongoDB update. A partial unique index allows a driver to appear on one assigned or in-progress trip at a time. The driver service supplies the availability snapshot used during dispatch. Going offline opts out of future dispatch checks while an existing assignment remains active. Fare estimates use straight-line distance, a $3.50 base fare, and $1.20 per kilometer, rounded to cents.

Completing a trip writes its payment event into the trip document atomically. The publisher removes the pending event after RabbitMQ confirms delivery to the durable queue. During a broker outage the event stays in MongoDB, including across trip-service restarts. Delivery is at least once, and MongoDB enforces one receipt per trip. The consumer acknowledges after recording a receipt and sends malformed messages to the dead-letter queue.

![Illustrated sequence from ride request through asynchronous payment processing](docs/screenshots/Ride%20Request%20to%20Payment%20Receipt%20Flow.png)

*The sequence uses illustrative endpoint labels. In Mobix, `POST /v1/trips` creates the ride and `PATCH /v1/trips/{id}/status` handles assignment, start, and completion. Trip checks availability through `GET /internal/drivers/{id}/dispatch-profile`. The gateway sends `GET /v1/payments/{tripID}` to Payment for the `simulated_paid` receipt.*

`GET /health` checks process liveness. `GET /ready` checks the backend HTTP services and their MongoDB connections. A broker outage can delay receipts while the gateway stays ready. Inspect trip and payment logs, the `pending_payment` fields in MongoDB, and the RabbitMQ queue `mobix.payment.trip-completed.v1` and its `.dead` counterpart when investigating a backlog.

## Kubernetes

For a local demo, build the four app images with Docker Compose and load them into a local [kind](https://kind.sigs.k8s.io/) cluster. Use a cluster with a default StorageClass for the MongoDB and RabbitMQ volumes.

![Single-node kind deployment with Go services, MongoDB, RabbitMQ, and persistent storage](docs/screenshots/Mobix%20Kubernetes%20Architecture%20Diagram.png)

*The local kind overlay runs four Go Deployments and gives MongoDB and RabbitMQ their own persistent volumes. Kustomize generates the Secret from `mobix-dependencies`, and the broker connection uses `RABBITMQ_URL`.*

```sh
docker compose build --provenance=false
kind create cluster --name mobix
kind load docker-image --name mobix \
  mobix/gateway:local mobix/driver:local mobix/trip:local mobix/payment:local
kubectl --context kind-mobix apply -k deployments/k8s/demo
kubectl --context kind-mobix -n mobix rollout status deployment/gateway --timeout=180s
kubectl --context kind-mobix -n mobix port-forward service/gateway 18081:8080
```

Run the demo in another terminal.

```sh
MOBIX_URL=http://127.0.0.1:18081 \
MOBIX_OPERATOR_EMAIL=operator@mobix.test \
MOBIX_OPERATOR_PASSWORD=mobix-demo-operator-password make demo
```

For a deployment with external dependencies, use [the base manifests](deployments/k8s/base) in an environment-specific Kustomize overlay. Set the four images to immutable registry references and provide a `mobix-dependencies` Secret in the `mobix` namespace with the keys below.

| Keys | Values |
| --- | --- |
| `internal-signing-key` | At least 32 bytes shared by the gateway and backend services for delegated identities |
| `accounts-mongo-uri` | MongoDB connection string for the gateway account and session database |
| `bootstrap-email`, `bootstrap-password` | First operator credentials. Use a unique password of 12 to 72 UTF-8 bytes. |
| `trip-mongo-uri`, `driver-mongo-uri`, `payment-mongo-uri` | MongoDB connection strings. Give each service access to its own collections and required index operations. |
| `trip-rabbitmq-url`, `payment-rabbitmq-url` | AMQP connection strings scoped to the Mobix queues and their declaration permissions. |

The gateway Service is cluster-local. The internal HTTP ingress policies require a CNI that enforces NetworkPolicy. Use TLS and an identity-aware ingress for shared environments. The demo uses single-node infrastructure and development credentials. Its MongoDB container lets glibc own rseq for compatibility with affected Linux kernels. Dispatch is manual and payments are simulated. Use unique operator credentials and a random internal signing key in shared deployments. [MongoDB container compatibility notes](https://github.com/docker-library/mongo/issues/765)

## Upgrading from the operator demo

This release requires individual sessions on business endpoints. Trip creation accepts pickup and drop-off, and derives `rider_id` from the signed-in account. Set the internal signing key for all four services and configure the gateway account database before deploying the application containers together.

Operators retain read access to existing trips, driver records, and receipts. Rider and driver access depends on the account bindings stored by this release. Finish any active legacy rides before upgrading. To enroll an existing driver without duplicating their profile, provision a driver account, verify the link, and set `account_id` on that record in the `driver` collection through an administrative migration. Only link profiles whose `account_id` is absent. Existing payment events continue to be processed.

The new rider-scoped retry index uses a separate field. Old global retry keys remain attached to their legacy trips, and new requests use the authenticated rider's scope.

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

Run `make verify` against a local test stack. It stops and restarts RabbitMQ and restarts the trip service while a payment event is pending. The verification script provisions two riders and two drivers, checks cross-account access and role permissions, and confirms recovery after a broker outage. GitHub Actions runs Go authorization and MongoDB integration tests, the Compose demo, the outage test, and Kustomize rendering on pull requests.

## Roadmap

![Proposed internal gRPC architecture with REST at the gateway and RabbitMQ for trip events](docs/screenshots/Mobix%20Internal%20gRPC%20Roadmap.png)

*The proposed design adds Protocol Buffers contracts and unary gRPC for internal calls. Clients would continue to use REST through the gateway, and RabbitMQ would continue to carry trip completion events.*

## License

[MIT](LICENSE)
