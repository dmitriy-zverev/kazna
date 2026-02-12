# Notification Service Roadmap

> Local planning file for notification-service implementation.

## ✅ Current State
- Service directory initialized.
- Messaging and integration baselines are defined in root `specs.md`.
- Approved stack: **Python (Celery-focused)** or **Golang consumer workers**.

---

## 🎯 Service Scope
- Multi-channel notification dispatch (email/sms/push).
- Template rendering and localization support.
- Delivery status tracking and retry handling.

---

## 🔜 Next (recommended order)

1. **Bootstrap service runtime**
   - Start with Python worker stack (Celery) to align with existing user-service email flow.
   - Add broker consumers, structured logging, and health checks.

2. **Template + provider abstraction**
   - Define template model and provider adapters (SMTP/provider API).
   - Add fallback strategy per channel.

3. **Event consumers**
   - Consume `user.created`, `user.verified`, `order.*`, `payment.*` events.
   - Ensure idempotent handling by event id.

4. **Delivery lifecycle**
   - Persist notification attempts and status transitions.
   - Add retries, DLQ consumption, and dead-letter replay tooling.

5. **Contracts and operations**
   - Publish AsyncAPI consumer contracts.
   - Add metrics for send success/failure/latency and alert thresholds.
