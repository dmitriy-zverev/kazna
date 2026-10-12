# Architecture

Status: Stage 3 baseline. This document is the standing context for AI agents
working on the repository. It records decisions that must not be silently
reversed.

See also:
- `docs/TECHNICAL_PLAN.md` — the full development plan.
- `docs/PAYMENT_LIFECYCLE.md` — states, transitions, idempotency.
- `docs/RELIABILITY.md` — recovery and retry rules.
- `docs/SECURITY.md` — key storage, auth, webhooks.
- `api/openapi.yaml` — the external contract.

## 1. What this service is

A standalone payment service that many applications share. Each application
integrates once with our API using its own API key and calls `pay()`; it never
integrates with YooKassa directly.

Hard requirements:

1. **Applications only know our API.** All YooKassa logic — errors, statuses,
   webhooks, retries, protocol quirks — lives inside this service.
2. **Reliability must not depend on YooKassa webhooks being configured.**
   The service polls and reconciles its own payments.

## 2. Processes

| Process | Command | Responsibility |
|---|---|---|
| API | `cmd/api` | HTTP API, auth, payment creation, webhook intake |
| Worker | `cmd/worker` | Reconciliation, retries, event delivery, recovery |
| Migrate | `cmd/migrate` | Explicit migration control (up/down/version/force) |

API and worker are the same Go binary family and share all business modules.
They are separate processes so they scale and restart independently.

PostgreSQL is the single source of truth for payment state and the background
job queue. No Redis or RabbitMQ in MVP; `FOR UPDATE SKIP LOCKED` provides the
queue semantics.

## 3. Style: modular monolith

One Go module, `github.com/dmitriy-zverev/kazna`, split into independent
packages. Not a set of network microservices. This reduces failure points,
simplifies testing and keeps AI-agent changes local.

Both processes must support running multiple instances.

## 4. Package map

```
cmd/api, cmd/worker, cmd/migrate   process entrypoints
internal/config                    environment configuration
internal/logging                   structured JSON logging
internal/version                   build metadata
internal/database                  pool, migrations, sqlc-backed helpers
internal/applications              applications, API keys, provider connections
internal/auth                      API key and admin token middleware
internal/credentials               API key generation, hashing, verification
internal/security                  AES-256-GCM cipher, outbound URL validation
internal/payments                  payment domain, state machine, service
internal/providers                 provider-agnostic contract and error codes
internal/providers/yookassa        YooKassa adapter
internal/metrics                   Prometheus collectors
internal/http                      router, handlers, middleware, respond, dto
internal/database/sqlcgen          generated query code (do not edit)
migrations/                        embedded SQL migrations
db/queries/                        sqlc query sources
tests/integration/                 black-box tests against real PostgreSQL
api/openapi.yaml                   external contract
deploy/                            compose.yaml, Caddyfile
docs/                              this documentation set
```

Packages added in later stages: `internal/webhooks`, `internal/outbox`,
`internal/delivery`, `internal/reconciliation`, `dashboard/`, `sdk/`.

## 5. Dependency rules

1. All payment state changes go through a single domain service. HTTP handlers
   and SDKs contain no payment business logic.
2. YooKassa is reached only through the provider adapter interface.
3. External provider calls are never made inside a long-running PostgreSQL
   transaction.
4. Every payment has a permanent internal id, independent of the provider id.
5. Any operation that moves money is idempotent.
6. An unknown external result is never treated as failure and never triggers an
   automatic new operation.

Dependency direction: `http` → `payments` → `providers`; `database` is a leaf
used by domain services. Domain packages must not import `http`.

## 6. Request lifecycle (Stage 2)

```
client → Caddy (optional) → chi router
         ├─ RequestID, RealIP, RequestLogger, Recoverer, Timeout
         ├─ GET  /health/live        → 200 always (no dependencies)
         ├─ GET  /health/ready       → 200 / 503 (runs a DB query via sqlc)
         ├─ GET  /v1/me              → API-key authenticated
         └─ /admin/*                 → admin token authenticated
```

Unauthenticated requests to `/v1/*` are rejected with 401 before routing
resolves, so the API surface cannot be enumerated without a key. Unmatched
routes and methods return the normalized error envelope from
`internal/http/respond`.

Authentication resolves to an `applications.Authenticated` value in the request
context. `application_id` is never read from a request body or query string.

## 7. Deployment

Docker Compose stack: `postgres`, `api`, `worker`, and `caddy` (profile
`proxy`). The API applies embedded migrations on startup. PostgreSQL uses a
named volume. Caddy terminates TLS and hides `/health/*` from the public.

## 8. Build and tooling

- Go 1.26, `net/http` + chi, pgx v5, sqlc, golang-migrate.
- `make build`, `make test`, `make lint`, `make generate`.
- Version metadata is injected with `-ldflags` into `internal/version`.

## 9. Decisions and open items

Recorded decisions (do not silently reverse):

- **No receipts in MVP.** The YooKassa adapter does not send a `receipt` block.
  Consequence: shops that require fiscalization will reject payment creation
  with `INVALID_REQUEST` until receipt support lands. Unlocking it means adding
  a per-application receipt configuration (fixed template or per-request items),
  sending it in `providers/yookassa.CreatePayment`, and covering it with an
  emulator test.
- **Read-through refresh on `GET /v1/payments/{id}`.** An unfinished payment is
  refreshed from the provider when `next_check_at` has passed, so a correct
  status is obtainable before the Stage 4 worker exists. The refresh is best
  effort: a provider failure returns the stored state.
- **Internal payment ids are UUIDs**, used directly in the API. There is no
  `pay_` prefix layer.

Open items:

- Per-application `return_url` domain allowlist (`docs/SECURITY.md` §4).
- Delivery-time re-validation of outbound addresses (`docs/SECURITY.md` §4).
