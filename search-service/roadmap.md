# Search Service Roadmap

> Local planning file for search-service implementation.

## ✅ Current State
- Service directory initialized.
- Global architecture and event contracts are defined in root `specs.md`.
- Approved stack: **Golang**.

---

## 🎯 Service Scope
- Product search/query APIs.
- Filtering, sorting, pagination, and relevance tuning.
- Index synchronization from domain events.

---

## 🔜 Next (recommended order)

1. **Bootstrap service (Go)**
   - Initialize module and HTTP API skeleton.
   - Add health/readiness endpoints and base telemetry.

2. **Index backend integration**
   - Integrate OpenSearch/Elasticsearch client.
   - Define index schema and versioning strategy.

3. **Ingestion pipeline**
   - Consume `product.*` events and maintain index projections.
   - Add reindex job for backfill/recovery.

4. **Query API contracts**
   - Publish OpenAPI for search endpoints.
   - Add filtering facets and relevance defaults.

5. **Reliability and performance**
   - Idempotent event ingestion + retry/DLQ handling.
   - Benchmarks for p95/p99 query latency and throughput goals.
