# Order Service Roadmap

> Local planning file for order-service implementation.

## ✅ Current State
- Service directory initialized.
- System-level contracts and requirements documented in `specs.md`.
- Approved stack: **Golang**.

---

## 🎯 Service Scope
- Order creation from cart snapshot.
- Order state machine (created/paid/cancelled/fulfilled/refunded).
- Order history and status APIs.

---

## 🔜 Next (recommended order)

1. **Bootstrap service (Go)**
   - Initialize module + architecture layout.
   - Add health/readiness and structured logging.

2. **Core domain model**
   - Order aggregate + line items + status transitions.
   - Postgres migrations and repository layer.

3. **Checkout orchestration**
   - Integrate with cart-service for snapshot intake.
   - Trigger payment workflow and consume payment outcomes.

4. **Events and reliability**
   - Emit `order.created`, `order.paid`, `order.cancelled`.
   - Add outbox pattern + idempotent event handling.

5. **Policy and security**
   - Enforce user ownership and role-based order visibility.

6. **Quality & operations**
   - API/event contract tests, tracing, metrics, alert hooks.
