# User Service Roadmap (Local Progress Tracker)

> This file is intentionally gitignored for local planning/tracking.

## ✅ Done

### Phase A hardening completed
- Switched API auth to JWT (`rest_framework_simplejwt.authentication.JWTAuthentication`).
- Switched Djoser auth routes to JWT endpoints (`/api/auth/jwt/...`).
- Tightened global permissions to `IsAuthenticated` and made registration public only on create.
- Fixed view correctness issue in `GroupViewSet.retrieve()`.
- Added safer permission handling for anonymous users.
- Added `IsSelfOrAdmin` object-level permission for user detail update/retrieve flows.

### Caching improvements
- Replaced broad `cache.clear()` invalidation strategy.
- Implemented targeted cache key strategy for:
  - user list/detail/me
  - group list/detail
- Implemented targeted invalidation in write operations and signals via `delete_pattern`.
- Extracted shared cache utilities to `core/cache_utils.py`.
- Refactored viewsets and signals to use shared cache key/invalidation helpers.

### Stability and testing
- Wired signals loading via `CoreConfig.ready()`.
- Hardened role helper properties in `users.models.User` to avoid `RelatedObjectDoesNotExist`.
- Added baseline regression/API tests in `core/tests.py`.
- Tests currently passing: `python user_service/manage.py test core.tests` (7/7).

### Developer workflow
- Updated `makefile` with improved targets:
  - `help`, `setup`, `install-hooks`, `format`, `test-core`, `check`, etc.
- Added hook installation commands for pre-commit in git commit workflow.

### Documentation
- Updated `user-service/README.md` to reflect actual implementation status.
- Added future improvements section aligned with roadmap priorities.
- Added JWT usage examples (`create`, `refresh`, `verify`, protected endpoint call).

### JWT/Auth UX improvements
- Confirmed and documented JWT flow endpoints.
- Added regression tests for invalid JWT and expired JWT access to protected endpoints.
- Test suite now: `python user_service/manage.py test core.tests` (9/9).

### Permission matrix completion
- Restricted `users` list endpoint to admin/moderator only.
- Added role-focused permission tests for:
  - admin/moderator/buyer access to `users` and `groups` listing
  - moderator restriction on group creation
- Updated test baseline: `python user_service/manage.py test core.tests` (11/11).

### Expanded test coverage (valid + invalid + unauthorized)
- Added API tests for:
  - `users/me` unauthorized + profile update success
  - `users/set_password` success and wrong-current-password rejection
  - user delete permissions (self/admin allowed, unauthorized forbidden)
  - group edge-cases (duplicate create rejected, moderator admin-assignment denied, admin assignment allowed)
- Added unit tests for permission classes:
  - `IsAdminOrModerator`, `IsBuyerOrSeller`, `IsSellerOrReadOnly`, `IsSelfOrAdmin`, `IsOwnerOrReadOnly`
- Added model role-property tests:
  - no-group behavior, seller behavior, staff/superuser overrides
- Added cache utils tests:
  - invalidation pattern coverage
  - auth-fragment key generation behavior
- Test suite now: `python user_service/manage.py test core.tests` (30/30).

### Operational hardening
- Moved sensitive settings to environment-driven configuration:
  - `SECRET_KEY`, `DEBUG`, `ALLOWED_HOSTS`
  - DB connection settings (`POSTGRES_*`, `DB_HOST`, `DB_PORT`)
  - cache endpoint (`REDIS_URL`)
  - CORS/CSRF origin lists
- Added robust env parsing helpers (`env_bool`, `env_list`, `env_int`).
- Added production safety toggles and secure defaults:
  - `USE_HTTPS` gate for SSL redirect, secure cookies, proxy SSL header
  - HSTS controls (`SECURE_HSTS_*`)
  - secure headers (`X_FRAME_OPTIONS`, `SECURE_REFERRER_POLICY`, nosniff, etc.)
- Added hardening documentation and `.env` example to `user-service/README.md`.
- Revalidated tests after hardening changes: `python user_service/manage.py test core.tests` (33/33).

### Async roadmap alignment (email verification foundation)
- Added pluggable email provider abstraction in `core/emailing.py`.
- Implemented default `DjangoEmailProvider` with signed verification-link generation.
- Triggered verification email dispatch on user registration via `UserCreateSerializer.create`.
- Added switchable email behavior for local/dev:
  - file backend (default)
  - optional console backend via env
- Added future-proof provider switching through `EMAIL_PROVIDER_CLASS` setting.
- Added tests for provider dispatch and serializer-triggered verification flow.
- Revalidated tests: `python user_service/manage.py test core.tests` (35/35).

### Async roadmap alignment (queue fallback layer)
- Added queue-oriented dispatch entrypoint: `dispatch_verification_email()`.
- Added Celery-style task definition in `core/tasks.py` with retry policy (`max_retries=3`, backoff).
- Wired registration to dispatch through queue-aware path (`UserCreateSerializer.create`).
- Added env flags for queue mode:
  - `EMAIL_ASYNC_ENABLED`
  - `CELERY_BROKER_URL`, `CELERY_RESULT_BACKEND`
- Implemented graceful fallback to synchronous email send when async dispatch is unavailable/fails.
- Added tests for async enabled/disabled/fallback branches.
- Revalidated tests: `python user_service/manage.py test core.tests` (38/38).

### Async roadmap alignment (runtime scaffolding)
- Added Celery dependency to uv-managed `pyproject.toml`.
- Added Celery app bootstrap:
  - `user_service/user_service/celery.py`
  - safe import/export in `user_service/user_service/__init__.py`
- Added make targets for runtime operations:
  - `make celery-worker`
  - `make celery-beat`
- Added task-level smoke test for verification task execution path.
- Updated README with queue runtime commands and env config.
- Revalidated tests: `python user_service/manage.py test core.tests` (39/39).

### Tooling migration (uv)
- Migrated dependency management from `requirements.txt` to `pyproject.toml`.
- Removed `requirements.txt` and switched setup workflow to `uv sync --dev`.
- Updated `makefile` commands to run via `uv run ...`.
- Updated README to document uv-based dependency workflow.
- Revalidated tests via uv workflow: `make test-core` (39/39).

### Async roadmap alignment (e2e smoke command)
- Added end-to-end async smoke management command:
  - `python user_service/manage.py smoke_async_email --timeout=30`
- Command creates a temporary user, dispatches async verification email, and waits for generated file email output as success signal.
- Added Make target: `make smoke-async-email`.
- Updated README with worker+broker+smoke execution flow.
- Command registration validated: `uv run python user_service/manage.py help smoke_async_email`.

### Async roadmap alignment (real RabbitMQ queue semantics)
- Added RabbitMQ service to root `docker-compose.yml` (management image + healthcheck).
- Switched async dispatch to explicit queue publish with `apply_async(...)` and queue selection via `CELERY_TASK_DEFAULT_QUEUE`.
- Added Celery settings for queue behavior and local testing flags.
- Updated tests to validate queue publish call shape and failure handling semantics.
- Retained file email backend so worker-delivered messages are observable in local files.

### Async roadmap alignment (one-command local e2e smoke)
- Added Make target `make smoke-async-e2e` that:
  - starts RabbitMQ via root docker-compose,
  - launches local Celery worker,
  - runs async smoke command,
  - cleans up worker process automatically.
- Hardened startup flow with readiness checks:
  - wait for RabbitMQ health status before dispatch,
  - wait for Celery worker "ready" state before smoke run,
  - print worker log tail on startup failure for faster debugging.
- Fixed Celery CLI wiring in Make targets using explicit app module + Python path.
- Validated end-to-end flow: `make smoke-async-e2e` passed and generated file email output.

### Email verification completion (state transition)
- Added persistent user verification state: `users.User.is_verified` (default `False`).
- Added verification endpoint: `/api/auth/verify-email` (GET query token or POST JSON token).
- Verification token now performs real state transition:
  - before verification: `is_verified=False`
  - after valid token verification: `is_verified=True`
- Added migration: `users/migrations/0006_user_is_verified.py`.
- Added API tests for:
  - unverified default state on new user,
  - successful verification state transition,
  - invalid token rejection.
- Revalidated tests: `make test-core` (42/42).

---

## 🔜 Next (recommended order)

1. **Production integration criteria (from `specs.md`)**
   - Finalize shared auth claims contract consumed by all services.
   - Introduce versioned `user.*` domain events (`user.created`, `user.verified`, `user.role_changed`).
   - Implement outbox pattern for reliable user-service event publishing.
   - Add consumer contract tests for auth claims + event payload compatibility.

2. **Coverage quality improvements**
   - Add coverage report target for `manage.py test` workflow.
   - Gate CI on minimum coverage threshold for user-service.

3. **CI hardening**
   - Add CI job for lint + test-core.
   - Fail PRs on test or formatting regressions.

4. **Dockerfile**
   - Add dockerfile with necessery dependencies.
   - Add it to global docker-compose file.
