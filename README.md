# Kazna

Universal payment service. Applications integrate once with this API using their
own key and call `pay()`; they never integrate with YooKassa directly.

Current state: **Stage 1 — skeleton and contracts**. The API starts, connects to
PostgreSQL, applies migrations and serves health probes. Payment, provider,
webhook, outbox and SDK modules arrive in later stages.

## Stack

Go 1.26 · chi · pgx v5 · sqlc · golang-migrate · PostgreSQL 17 · Docker Compose · Caddy.

## Documentation

| Document | Contents |
|---|---|
| `docs/TECHNICAL_PLAN.md` | Full development plan and acceptance criteria |
| `docs/ARCHITECTURE.md` | System, processes, packages, dependency rules |
| `docs/PAYMENT_LIFECYCLE.md` | States, transitions, idempotency |
| `docs/RELIABILITY.md` | Recovery, retries, event delivery |
| `docs/SECURITY.md` | Key storage, isolation, URL validation |
| `docs/FINANCIAL_INVARIANTS.md` | Non-negotiable payment rules for acceptance |
| `docs/VERIFICATION_HARNESS.md` | Env seams for independent black-box testing |
| `api/openapi.yaml` | External API contract |

## Quick start (Docker)

```bash
cp .env.example .env          # optional; compose has development defaults
make compose-up               # build and start postgres + api + worker
curl localhost:18080/health/live
curl localhost:18080/health/ready
```

The API is published on `127.0.0.1:18080` by default (override with `API_PORT`).
Postgres is not exposed to the host by default; uncomment the port mapping in
`deploy/compose.yaml` if you need it.

Migrations are applied automatically when the API starts. Stop with
`make compose-down`.

Release acceptance of the mandatory scenarios runs in the separate [kazna-verification](https://github.com/dmitriy-zverev/kazna-verification) repository against a built Docker image. In-repo integration tests remain required and run in this repo's CI, but they do not replace that acceptance.

## Local development

```bash
# PostgreSQL must be reachable via DATABASE_URL.
export DATABASE_URL='postgres://kazna:kazna@localhost:5432/kazna?sslmode=disable'

make migrate-up      # apply migrations explicitly
make run-api         # or: make run-worker
```

## Commands

```bash
make help            # list all targets
make build           # compile binaries into ./bin
make test            # unit tests
make lint            # go vet + gofmt check
make generate        # regenerate sqlc code (requires sqlc)
make compose-up      # start the full stack
make compose-logs    # follow logs
make migrate-version # show current migration version
```

## Layout

```
cmd/api  cmd/worker  cmd/migrate   process entrypoints
internal/                          application packages
migrations/                        embedded SQL migrations
db/queries/                        sqlc query sources
api/openapi.yaml                   external contract
deploy/                            compose.yaml, Caddyfile
docs/                              documentation set
```

## Environment

See `.env.example`. `DATABASE_URL` is required; `MASTER_ENCRYPTION_KEY` is
required in production. Never commit real secrets.
