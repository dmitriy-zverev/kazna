# Kazna - shop for your budget

Core Microservices:

- User Service: Handles user registration, authentication, and profile management.
- Product Catalog Service: Manages product listings, categories, and inventory.
- Shopping Cart Service: Manages users' shopping carts, including adding/removing items and updating quantities.
- Order Service: Processes orders, including placing orders, tracking order status, and managing order history.
- Payment Service: Handles payment processing, integrating with external payment gateways (e.g., Stripe, PayPal).
- Notification Service: Sends email and SMS notifications for various events (e.g., order confirmation, shipping updates). You can use third-party services like Twilio or SendGrid for this purpose.
- Search Service: Searches through items (ELK stack).

Idea: https://roadmap.sh/projects/scalable-ecommerce-platform


## Description

- **User Service** (Django + DRF + PostgreSQL + PyJWT + Redis + Swagger)
    1. **Auth (registration, login, password reset, token refresh)**: Implement user signup with email/username validation and password hashing; login to generate JWT tokens; password reset via email links with token expiry; token refresh to extend sessions without re-login. Use Django's built-in User model extended via AbstractUser, DRF for API endpoints (e.g., serializers for input validation), PostgreSQL for storing user creds securely, PyJWT for encoding/decoding tokens, and Swagger (via drf-spectacular) for API docs. Logic: On register, check uniqueness in DB, hash with Django's make_password; on login, verify with check_password and issue JWT; reset flow sends temp token, verifies on callback. Unhinged note: Don't assume emails always deliver—add retries, but for MVP, one shot is fine if you're not paranoid about spam filters.
    2. **Roles (admin, seller, buyer, moderator with permission checks)**: Define role-based access using groups or custom fields on User model; check permissions in views (e.g., sellers can add products via integration). Use Django's permissions system with DRF's permission_classes, PostgreSQL to store role assignments. Logic: On register, default to 'buyer'; admins assign via API; decorators like @permission_required enforce checks. Logical caveat: Roles sound fancy but can bloat if unused—question if 'moderator' is MVP-essential or just overcomplicating for a basic store.
    3. **Caching with Redis (for sessions and frequent profile queries)**: Cache user profiles and sessions to speed up reads. Use Redis via django-redis as backend. Logic: On profile fetch, check Redis first (e.g., key 'user:{id}:profile'), fallback to PostgreSQL if miss, then cache with TTL (e.g., 5min). For sessions, store JWT refresh tokens. Don't take caching for granted—invalidate on updates to avoid stale data bugs.
    4. **Celery + RabbitMQ for tasks like email verification or async profile updates**: Queue background jobs for non-blocking ops. Use Celery tasks with RabbitMQ broker. Logic: On register, queue verification email; task sends via Django's send_mail or SMTP. For updates, queue DB writes if sync is risky. MVP: Simple fire-and-forget; add retries later if emails flop often.

- **Product Catalog Service** (FastAPI + MongoDB + Redis + Elasticsearch hooks + Uvicorn)
    1. **CRUD operations for products (listings with descriptions, prices, images)**: Create endpoints for adding/editing/deleting products with validation; read with pagination. Use FastAPI routers and Pydantic models for schemas (e.g., ProductBase with fields like name, desc, price, image_url), MongoDB for storage (pymongo/Motor for async inserts/queries), Uvicorn for serving. Logic: On create, validate inputs, store in collection 'products'; reads use find() with limits. Handle images as URLs (upload to S3 separately for MVP). Unhinged: Image handling is a pain—don't assume URLs are eternal; add checks but skip for MVP unless you want storage hell.
    2. **Category management (hierarchical or tagged structures)**: CRUD for categories, linking to products. Use MongoDB embedded docs for hierarchies (e.g., parent_id) or arrays for tags. FastAPI for APIs. Logic: Create category with name/parent; products reference category_ids. Queries use aggregation for tree traversal. Logical: Hierarchies scale poorly in NoSQL—test with nested data; if it sucks, flatten for MVP.
    3. **Inventory tracking (stock levels, variants like sizes/colors)**: Update stock on changes; track variants as sub-docs. MongoDB for nested arrays (e.g., product.variants: [{size: 'M', stock: 10}]). Redis for caching totals. Logic: On update, atomic $inc for stock; validate >0 on reads. Don't grant assumptions—concurrent updates need locks (use Redis locks for MVP).
    4. **Event publishing to Search Service (for indexing on create/update/delete)**: Emit events post-CRUD. Use RabbitMQ (pika) or direct HTTP, but async for decoupling. Logic: After DB op, publish JSON payload (e.g., {'action': 'update', 'product_id': id}) to queue. FastAPI background tasks for this. Caveat: Events can drop—add dead-letter queues later; MVP ignores failures.

- **Search Service** (FastAPI + Elasticsearch + Redis + RabbitMQ)
    1. **Full-text search (fuzzy queries, facets like price/range filters)**: Endpoint for queries returning results. Use Elasticsearch query DSL (elasticsearch-py), FastAPI for API. Logic: Build multi_match for fuzzy, aggs for facets (e.g., terms on category, range on price). Return hits with scores. Unhinged: Fuzzy is overrated for exact matches—tune tolerance or users get junk results.
    2. **Indexing from Product Catalog (async syncing via events)**: Consume events to index/update docs. RabbitMQ consumer (pika), Elasticsearch index ops. Logic: Listener pulls from queue, maps product data to ES doc (e.g., _id=product_id, fields=name,desc), uses index() or update(). Handle deletes with delete(). Logical: Sync lags are real—don't assume real-time; poll if events fail.
    3. **Relevance ranking (boosting popular or recent products)**: Customize scoring in queries. Elasticsearch scripts or function_score. Logic: Boost by date (e.g., decay on _source.created_at) or views (add field, boost multiplier). MVP: Simple date boost; add ML later if needed.
    4. **Caching hot searches with Redis to reduce latency**: Cache query results. Redis (aioredis) with keys like 'search:{query_hash}'. Logic: On query, check cache; miss hits ES, then set with TTL (e.g., 1min). Invalidate on index events. Don't take it for granted—cache poisoning from bad queries can happen; limit keys.

- **Shopping Cart Service** (FastAPI + Redis + RabbitMQ)
    1. **Cart management (add/remove items, update quantities per user)**: Endpoints for cart ops. FastAPI with Pydantic (CartItem model). Redis hashes for storage (key 'cart:{user_id}'). Logic: HSET for add/update (item_id: qty), HDEL for remove. Atomic with pipelines.
    2. **Session-based persistence (keyed by user_id or anon sessions)**: Support logged-in/guest carts. Redis for anon (generate session_id via UUID). Logic: Merge anon to user on login; expire anon after TTL (e.g., 24h). Unhinged: Sessions bloat Redis—prune inactive or face OOM.
    3. **Validation (check stock availability from catalog)**: On add/update, verify stock. HTTP call to catalog service (httpx async). Logic: Fetch inventory, if qty > stock, reject with error. Logical: Network calls add latency—cache stock in Redis but sync carefully.
    4. **Event emission (e.g., "cart_updated" for order previews)**: Publish changes. RabbitMQ (pika). Logic: Post-op, send event with cart snapshot. Background tasks in FastAPI. Caveat: Over-publishing clogs queues—MVP only on checkout intent.

- **Order Service** (FastAPI + PostgreSQL + RabbitMQ + Celery)
    1. **Order placement (from cart, with user auth)**: Endpoint to create order. FastAPI, Pydantic for OrderCreate. PostgreSQL (SQLAlchemy async) for storage. Logic: Pull cart, validate auth (JWT from user service), insert order row, deduct inventory via catalog call, publish "order_placed".
    2. **Status tracking (pending, shipped, delivered, canceled)**: Update/retrieve status. DB enum field. Logic: PATCH endpoint checks permissions (e.g., admin for ship), updates row transactionally. Unhinged: Statuses multiply states—keep finite or debug hell ensues.
    3. **History management (user order logs)**: List orders per user. DB queries with filters. Logic: SELECT with user_id join, paginate. Cache in Redis if frequent.
    4. **Async tasks (integrate with payment and notifications via queues)**: Queue post-placement jobs. Celery with RabbitMQ. Logic: Task triggers payment service call, then notifications on success. MVP: Sequential; parallelize later.

- **Payment Service** (FastAPI + Stripe SDK + RabbitMQ + PostgreSQL for logs)
    1. **Payment processing (charges, refunds via gateways like Stripe/PayPal)**: Handle charge on event. Stripe SDK (stripe-python). Logic: Consume "order_placed", create PaymentIntent, confirm. Store minimal in PostgreSQL (e.g., tx_id).
    2. **Webhook handling (for async confirmations)**: Endpoint for gateway callbacks. FastAPI router. Logic: Verify signature, update order status via call, publish "payment_success". Don't assume webhooks always fire—poll for MVP fallbacks.
    3. **Transaction logging (for audits, minimal DB)**: Log all actions. PostgreSQL table. Logic: Insert on every op with timestamps. Logical: Audits are boring but legally required—don't skimp.
    4. **Event listening (from orders, emit success/fail to notifications)**: RabbitMQ consumer. Logic: Pull queue, process, publish outcome. Use threads for long-running.

- **Notification Service** (FastAPI + Twilio/SendGrid SDKs + RabbitMQ + Celery)
    1. **Event-driven sends (emails/SMS for order confirmations, updates)**: Consume events, send msgs. Twilio for SMS, SendGrid for email. Logic: Map event type to template, personalize, dispatch.
    2. **Template management (customizable messages)**: Store/load templates. Use Jinja2 for rendering. Logic: DB or file-based templates; render with context (e.g., order_id). Unhinged: Templates invite XSS—sanitize inputs.
    3. **Queue consumption (from other services like users/orders)**: RabbitMQ listener. Celery workers. Logic: Bind to topics, process in parallel.
    4. **Retry logic for failed deliveries**: Handle errors. Celery retries. Logic: On exception (e.g., API rate limit), retry exponential backoff (max 3). MVP: Log fails, don't block.

## Roadmap

Phase 0: Prep and Foundation (1-2 Weeks: Don't Skip or You'll Regret It)

- [] Research overall architecture: Draw a diagram (use draw.io) showing services, data flows, and comms (HTTP sync vs. RabbitMQ async). Question assumptions: Does everything need microservices, or is a monolith easier for MVP? Resource: "Building Microservices" by Sam Newman (chapters 1-3).
- [] Set up monorepo: Init Git repo with folders per service, .gitignore, README skeleton. Add shared docker-compose.yml for infra (Postgres, Mongo, Redis, RabbitMQ, Elasticsearch). Test basic up/down. Unhinged truth: Docker networks flake—debug early.
- [] Configure shared tools: Env vars (.env.example), common requirements.txt. Install linters (black, flake8) and test framework (pytest). Resource: Real Python's "Python Project Structure" article.
- [] Prototype DB connections: Quick scripts to connect Postgres/Mongo/Redis from Python. Don't assume seamless—test transactions.
- [] Set up CI/CD basics: GitHub Actions workflow for lint/test/build. Use your past experience; add Docker builds. Resource: GitHub docs on Actions for Python.

Phase 1: User Service MVP (2-3 Weeks: Start Here, Auth is King)

- [] Bootstrap Django project: Create app, extend User model for custom fields. Resource: Django docs tutorial.
- [] Implement auth logic: Registration endpoint (validate unique email), login with JWT issuance, password reset flow (email link with expiry), token refresh. Use DRF serializers, PyJWT. Test edge cases (bad passwords). Logical check: Sessions vs. JWT—JWT for stateless, but verify token revokes work.
- [] Add roles: Custom permissions for admin/seller/buyer/moderator. Use Django groups; API to assign/check. Don't overdo—MVP: Default buyer, admin manual.
- [] Integrate caching: Redis for profiles/sessions. Implement get-or-set logic; invalidate on updates. Resource: django-redis docs.
- [] Set up async tasks: Celery + RabbitMQ for email verification. Write tasks, test queuing. Unhinged: Emails fail silently—log everything.
- [] Dockerize: Dockerfile for Django + Gunicorn; add to compose. Expose Swagger docs.
- [] Tests: Unit for endpoints, integration for DB. Coverage >70%. Deploy to remote server for smoke test.

Phase 2: Product Catalog Service MVP (2 Weeks: Data Heart, Keep Flexible)

- [] Set up FastAPI app: Routers, Pydantic models for products/categories. Resource: FastAPI docs quickstart.
- [] CRUD for products: Endpoints for create/read/update/delete with validation (prices >0). Use Motor for async Mongo inserts. Handle image URLs (no uploads yet).
- [] Category management: Hierarchical via parent_id in Mongo. APIs for linking to products. Test aggregations.
- [] Inventory logic: Nested variants in docs; atomic updates for stock. Add Redis cache for totals.
- [] Event publishing: Post-CRUD, publish to RabbitMQ for search indexing. Use background tasks.
- [] Dockerize with Uvicorn; integrate into compose.
- [] Tests: Mock DB, test concurrency (e.g., stock race). Question: Mongo flexibility worth schema chaos? Benchmark vs. Postgres.

Phase 3: Search Service MVP (1-2 Weeks: Offload Query Smarts)

- [] Install Elasticsearch: Add to docker-compose; basic index setup. Resource: Elastic docs Python client.
- [] Indexing logic: RabbitMQ consumer to sync from catalog events. Map products to ES docs; handle updates/deletes.
- [] Search endpoint: Fuzzy queries with facets (price filters). Use DSL for ranking (boost recent).
- [] Caching: Redis for hot queries; invalidate on indexes.
- [] Dockerize FastAPI app.
- [] Tests: Load fake data, query accuracy. Unhinged: ES eats RAM—monitor or it'll crash your dev machine.

Phase 4: Shopping Cart Service MVP (1 Week: Stateless Fun)

- [] FastAPI setup: Models for cart items.
- [] Cart ops: Add/remove/update in Redis hashes (user_id keys).
- [] Session handling: Anon carts via UUID; merge on login.
- [] Validation: Async HTTP to catalog for stock checks.
- [] Event emission: Publish updates to RabbitMQ.
- [] Dockerize.
- [] Tests: Simulate multi-user; check expiries.

Phase 5: Order Service MVP (2 Weeks: Ties It Together)

- [] FastAPI + SQLAlchemy for Postgres orders table.
- [] Placement logic: From cart (HTTP pull), auth check (JWT validate), insert order, deduct inventory.
- [] Status updates: PATCH with permissions.
- [] History: Filtered queries per user; cache in Redis.
- [] Async integration: Celery tasks to trigger payment/notifications via queues.
- [] Dockerize.
- [] Tests: End-to-end with mocks. Logical: Transactions across services? Use sagas if distributed—complex for MVP.

Phase 6: Payment Service MVP (1-2 Weeks: Money Matters, Test Fake)

- [] FastAPI app; integrate Stripe SDK.
- [] Processing: Consume "order_placed" from RabbitMQ, create intent, confirm.
- [] Webhooks: Endpoint for callbacks; update order status.
- [] Logging: Minimal Postgres for audits.
- [] Event emits: Success/fail to notifications.
- [] Dockerize.
- [] Tests: Use Stripe test keys; mock webhooks. Unhinged: Gateways charge fees—don't test live.

Phase 7: Notification Service MVP (1 Week: Async Polish)

- [] FastAPI minimal; Twilio/SendGrid SDKs.
- [] Event consumption: RabbitMQ for sends (e.g., order confirm).
- [] Template rendering: Jinja2 for personalization.
- [] Retry logic: Celery built-in.
- [] Dockerize.
- [] Tests: Mock APIs; check retries.

Phase 8: Integration, Testing, and Hardening (2-3 Weeks: Where It Breaks)

- [] Wire comms: Test full flows (register -> add product -> search -> cart -> order -> pay -> notify). Use Postman for APIs.
- [] Security: Add JWT middleware everywhere; rate limits.
- [] Monitoring: Basic logs (structlog); add Prometheus if ambitious.
- [] Load testing: Locust scripts for bottlenecks.
- [] CI/CD full: Auto-deploy to remote on push. Resource: Your past projects.
- [] Refactor: Profile (cProfile), optimize slow bits. Don't assume done—iterate.

Phase 9: Deployment and Beyond (Ongoing: Real World Sucks)

- [] Prod setup: Kubernetes or ECS for scaling; managed DBs.
- [] Monitoring/Alerts: Sentry for errors.
- [] Add-ons: If needed, more services (e.g., shipping).
- [] Review: What failed? Pivot if microservices overkill.