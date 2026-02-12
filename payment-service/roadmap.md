# Payment Service Roadmap

> Local planning file for payment-service implementation.

## ✅ Current State
- Service directory initialized.
- Platform integration requirements are defined in root `specs.md`.
- Approved stack: **Golang**.

---

## 🎯 Service Scope
- Payment intent creation and state tracking.
- Provider integration (cards/wallets/bank methods as adapters).
- Webhook verification and reconciliation.

---

## 🔜 Next (recommended order)

1. **Bootstrap service (Go)**
   - Initialize module, config, health endpoints, structured logs.

2. **Payment domain model**
   - Payment intent, transaction attempts, provider response metadata.
   - Postgres migrations + repository layer.

3. **Provider adapter design**
   - Abstract provider interface and implement first adapter.
   - Add webhook endpoint with signature verification.

4. **Order integration**
   - Consume `order.created` and emit `payment.succeeded` / `payment.failed`.
   - Enforce idempotency keys for all payment create/confirm paths.

5. **Reliability + compliance**
   - Outbox + retry + DLQ strategy.
   - Audit trails and secure handling of payment metadata.

6. **Quality & operations**
   - Contract tests, traces/metrics, alerting for failure spikes.
