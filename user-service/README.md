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

##### JWT quick usage examples

Create tokens:

```bash
curl -X POST http://127.0.0.1:8000/api/auth/jwt/create/ \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"StrongPass123!"}'
```

Refresh access token:

```bash
curl -X POST http://127.0.0.1:8000/api/auth/jwt/refresh/ \
  -H "Content-Type: application/json" \
  -d '{"refresh":"<refresh_token>"}'
```

Verify token:

```bash
curl -X POST http://127.0.0.1:8000/api/auth/jwt/verify/ \
  -H "Content-Type: application/json" \
  -d '{"token":"<access_token>"}'
```

Call a protected endpoint:

```bash
curl http://127.0.0.1:8000/api/users/ \
  -H "Authorization: Bearer <access_token>"
```

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

## Environment Configuration (Operational Hardening)

Key environment variables:

- `SECRET_KEY` (required when `DEBUG=false`)
- `DEBUG` (`true/false`)
- `ALLOWED_HOSTS` (comma-separated)
- `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `DB_HOST`, `DB_PORT`
- `REDIS_URL`
- `CORS_ALLOWED_ORIGINS` (comma-separated)
- `CSRF_TRUSTED_ORIGINS` (comma-separated)
- `CORS_ALLOW_CREDENTIALS` (`true/false`)
- `USE_HTTPS` (`true/false`)
- `SECURE_HSTS_SECONDS`, `SECURE_HSTS_INCLUDE_SUBDOMAINS`, `SECURE_HSTS_PRELOAD`
- `X_FRAME_OPTIONS`, `SECURE_REFERRER_POLICY`

Example local `.env`:

```env
SECRET_KEY=replace-me
DEBUG=true
ALLOWED_HOSTS=127.0.0.1,localhost

POSTGRES_DB=kazna_user_service
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
DB_HOST=127.0.0.1
DB_PORT=5432

REDIS_URL=redis://127.0.0.1:6379/1

CORS_ALLOWED_ORIGINS=http://localhost,http://127.0.0.1
CSRF_TRUSTED_ORIGINS=http://localhost,http://127.0.0.1
CORS_ALLOW_CREDENTIALS=true

USE_HTTPS=false
```

Production baseline:
- set `DEBUG=false`
- set a strong `SECRET_KEY`
- set `USE_HTTPS=true` behind reverse proxy/load balancer
- set restrictive `ALLOWED_HOSTS`, CORS and CSRF origin lists
