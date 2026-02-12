# User Service

User management service for the Kazna platform.

## Tech Stack

- Django + Django REST Framework
- PostgreSQL
- Djoser + SimpleJWT (JWT auth)
- Redis (caching)
- drf-spectacular (OpenAPI/Swagger)

## Current Status

### ✅ Implemented

#### Authentication and users
- Custom `User` model (`users.User`) with profile fields.
- Registration and auth flows via Djoser.
- JWT authentication enabled (`/api/auth/jwt/create`, `/api/auth/jwt/refresh`, `/api/auth/jwt/verify`).
- Password change endpoint via custom `set_password` action.

#### Roles and permissions
- Role model (`Group`) with `buyer`, `seller`, `moderator`, `admin`.
- Role helper properties on user (`is_admin`, `is_moderator`, etc.) with safe handling.
- Custom permissions in API (including object-level `IsSelfOrAdmin`).
- Global API default permission is authenticated access, with public registration only.

#### API and docs
- User and group viewsets under `/api/`.
- OpenAPI schema and docs endpoints:
  - `/api/schema/`
  - `/api/schema/swagger-ui/`
  - `/api/schema/redoc/`

#### Caching
- Redis configured as Django cache backend.
- Read caching for user/group list/detail and `users/me`.
- Targeted cache invalidation for changed entities (pattern-based, no global clear).

#### Quality baseline
- Regression test suite in `core/tests.py` covering:
  - access control,
  - JWT auth endpoint,
  - cache invalidation behavior.

## Future Improvements

### 1) Cache architecture cleanup
- Move cache keys/patterns into shared utilities.
- Remove duplication between viewsets and signal handlers.

### 2) Auth and security hardening
- Move all sensitive settings to environment variables (`SECRET_KEY`, `DEBUG`, hosts, CORS).
- Add stricter production defaults.
- Expand JWT negative-path tests (expired/invalid tokens).

### 3) Permissions matrix expansion
- Add explicit tests for all role combinations on all sensitive endpoints.
- Ensure least-privilege policy remains enforced as endpoints grow.

### 4) Async capabilities
- Add Celery + RabbitMQ skeleton for background tasks:
  - email verification,
  - async profile-related events.

### 5) CI workflow
- Add CI checks for `pre-commit` + `core.tests` on pull requests.

## Developer Commands

From `user-service/`:

- `make help` – list available targets
- `make install-hooks` – install pre-commit hooks
- `make run` – run dev server
- `make test-core` – run core tests
- `make check` – lint + core tests
