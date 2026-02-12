# Product Catalog Service Roadmap

> Local planning file for catalog-service implementation.

## ✅ Current State
- Service directory initialized.
- Architecture and cross-service requirements defined in root `specs.md`.
- Approved stack: **Golang**.

---

## 🎯 Service Scope
- Product CRUD (admin/seller controlled).
- Category/attribute management.
- Price metadata and product visibility.
- Inventory read model (authoritative stock may be externalized later).

---

## 🔜 Next (recommended order)

1. **Bootstrap service (Go)**
   - Initialize Go module and project layout (`cmd/`, `internal/`, `pkg/`).
   - Add HTTP framework, config loader, logging, health endpoints.

2. **Data model + persistence**
   - Add Postgres schema/migrations for products/categories.
   - Add repository layer and transactional write paths.

3. **API contracts**
   - Publish OpenAPI for product/category APIs.
   - Enforce auth/role checks based on user-service claims.

4. **Eventing**
   - Emit `product.created`, `product.updated`, `product.deleted` events.
   - Add outbox + idempotent publisher.

5. **Search integration**
   - Produce index update events for search-service.

6. **Quality & operations**
   - Tests, linting, CI checks, tracing, metrics, dashboards.
