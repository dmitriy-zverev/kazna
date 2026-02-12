# Shopping Cart Service Roadmap

> Local planning file for cart-service implementation.

## ✅ Current State
- Service directory initialized.
- Shared architecture documented in root `specs.md`.
- Approved stack: **Golang**.

---

## 🎯 Service Scope
- User cart lifecycle (create/get/update/clear).
- Add/remove line items and quantity operations.
- Cart pricing snapshot and validation hooks.

---

## 🔜 Next (recommended order)

1. **Bootstrap service (Go)**
   - Initialize module and service layout.
   - Add health/readiness endpoints.

2. **Persistence model**
   - Use Redis as primary cart store with TTL strategy.
   - Optional Postgres snapshot table for recovery/audit.

3. **API + auth integration**
   - Build cart APIs with user claims validation (`sub`, `roles`, `is_verified`).
   - Publish OpenAPI contract.

4. **Catalog/order integration**
   - Validate SKUs/prices against catalog-service.
   - Produce cart-checkout events for order-service.

5. **Reliability + ops**
   - Idempotent cart mutation endpoints.
   - Metrics, traces, and CI quality gates.
