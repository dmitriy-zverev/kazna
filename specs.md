# Kazna Microservices Integration Specification (Production-Ready Baseline)

## 1. Purpose

This document defines how Kazna services are linked together as a production-grade ecommerce platform.

Primary goals:
- stable service boundaries,
- secure cross-service communication,
- reliable event-driven propagation,
- observable and operable runtime behavior,
- contract-first evolution.

---

## 2. Service responsibilities

- **user-service**
  - identity, authentication, profile, roles, email verification.
- **product-catalog-service**
  - products, categories, pricing metadata, inventory view model.
- **shopping-cart-service**
  - cart state per user/session.
- **order-service**
  - order lifecycle + state transitions.
- **payment-service**
  - payment intents, provider integrations, webhook processing.
- **notification-service**
  - email/sms/push fanout and delivery status.
- **search-service**
  - denormalized index for search use cases.

Rule: each service owns its database and exposes data via API/events only.

### Approved language strategy

To minimize operational complexity and maximize delivery speed, Kazna uses:
- **Python** (primarily Django/DRF and Celery) for identity/admin-heavy domains,
- **Golang** for high-throughput and latency-sensitive domain services.

Java and Node.js are intentionally not part of the baseline stack for now.

Recommended mapping:
- **user-service**: Python (Django/DRF) ✅
- **product-catalog-service**: Golang
- **shopping-cart-service**: Golang
- **order-service**: Golang
- **payment-service**: Golang
- **notification-service**: Python (Celery workers) or Golang consumer workers
- **search-service**: Golang

---

## 3. Identity and authorization model

### 3.1 External client auth
- Client authenticates against **user-service** (JWT-based).
- JWT is validated at edge (API gateway).

### 3.2 Internal service auth
- Do not rely only on end-user JWT for service-to-service trust.
- Use service identity (API key/mTLS/workload identity) for internal calls.

### 3.3 Canonical claims contract
Minimal claims expected downstream:
- `sub` (user id)
- `email`
- `roles` (buyer/seller/moderator/admin)
- `is_verified`
- `iat`, `exp`, `iss`, `aud`

Downstream services must enforce least privilege from these claims.

---

## 4. Sync vs async communication policy

### 4.1 Synchronous APIs (HTTP/gRPC)
Use when caller needs immediate response (reads, immediate command result).

### 4.2 Asynchronous events (RabbitMQ)
Use for propagation and eventual consistency.

Current broker baseline:
- RabbitMQ in root `docker-compose.yml`
- queue-based dispatch already used by user-service verification pipeline.

---

## 5. Event contract baseline

Initial domain events to standardize:
- `user.created`
- `user.verified`
- `user.role_changed`
- `order.created`
- `payment.succeeded`
- `product.updated`

Event envelope (required fields):
- `event_id` (uuid)
- `event_type`
- `event_version`
- `occurred_at` (ISO8601 UTC)
- `producer`
- `correlation_id`
- `payload`

Contract governance:
- version events,
- additive-first changes,
- deprecate with transition window.

---

## 6. Reliability requirements

- Implement **outbox pattern** per service for durable event publishing.
- Consumers must be **idempotent** (safe duplicate handling).
- Configure retries + DLQ for poison messages.
- Use idempotency keys on order/payment write APIs.
- Model multi-step workflows (order/payment/inventory) as saga/state machine.

---

## 7. API gateway and edge requirements

Gateway responsibilities:
- auth validation,
- request routing,
- rate limiting,
- correlation id injection/propagation,
- centralized audit logging.

Forwarded context headers (example baseline):
- `X-Request-Id`
- `X-Correlation-Id`
- `X-User-Id`
- `X-User-Roles`
- `X-User-Verified`

---

## 8. Observability and operations

Mandatory across services:
- structured logs (JSON),
- metrics (RED/USE basics),
- distributed tracing (OpenTelemetry),
- health/readiness endpoints,
- alerts and SLO definitions.

Minimum runtime dashboards:
- API latency/error rates,
- queue depth/consumer lag,
- DB pool saturation,
- critical workflow success rate (checkout pipeline).

---

## 9. Security and compliance baseline

- secrets via env/secret manager only,
- TLS in transit,
- strict CORS/CSRF/host allowlists at edge,
- PII minimization in logs/events,
- audit trail for sensitive actions (role changes, verification, payment states).

---

## 10. Contract-first development process

- OpenAPI for synchronous APIs.
- AsyncAPI for event contracts.
- Consumer-driven contract tests in CI.
- Breaking changes require explicit version bump and migration plan.

---

## 11. User-service readiness in this architecture

Already implemented in user-service:
- JWT auth and role model,
- email verification tokens + state transition (`is_verified`),
- async queue dispatch with RabbitMQ/Celery,
- e2e smoke command against real worker+broker,
- regression tests.

Next integration steps for user-service:
1. publish `user.*` events with versioned schema,
2. add outbox for guaranteed publish semantics,
3. formalize identity claims consumed by downstream services,
4. add contract tests for auth/event payload compatibility.

---

## 12. Phased rollout plan

### Phase 1 (foundation)
- finalize shared auth claims contract,
- standardize envelope for events,
- introduce gateway header conventions.
- lock engineering standards around Python + Golang (templates, linters, CI baselines).

### Phase 2 (event reliability)
- add outbox + idempotent consumers,
- add DLQ/retry strategy per domain queue.

### Phase 3 (cross-service flows)
- wire order ↔ payment ↔ inventory saga,
- wire notification consumers for user/order/payment events.

### Phase 4 (operability hardening)
- full tracing/metrics dashboards,
- SLOs and alerting,
- resilience/load testing for checkout critical path.
