# Security

Status: Stage 2 baseline. Mandatory from the first release; remaining features
land in Stages 3 and 8.

## 1. Provider secret storage

- YooKassa `secret_key` values are stored encrypted with authenticated
  encryption: AES-256-GCM.
- Ciphertext envelope: `version(1) | keyID(4) | nonce(12) | ciphertext+tag`.
  The nonce is random per encryption; the key id is derived from the key
  material, so it survives reordering of the key list during rotation.
- The master key (`MASTER_ENCRYPTION_KEY`) lives outside PostgreSQL — an
  environment secret or a dedicated secret manager. Compromising the database
  alone must not reveal provider keys.
- `MASTER_ENCRYPTION_KEY` is required in every environment: the service refuses
  to start without it, because it cannot store provider secrets safely.
- Rotation: set the new key as `MASTER_ENCRYPTION_KEY` and list retired keys in
  `MASTER_ENCRYPTION_PREVIOUS_KEYS` (comma-separated). Previous keys decrypt
  only; new writes always use the primary key, so old keys can be dropped once
  every row has been re-encrypted.
- Logs must never contain secrets, authorization headers, or full request bodies
  with sensitive data.

## 2. Application isolation

- Every request runs in the context of one `application_id`, resolved by the
  server from the API key. A client can never supply another application's id:
  `application_id` is not accepted in any request body or query parameter.
- All database access is filtered by the authenticated application.
- API keys have the form `app_live_` followed by 32 random bytes encoded as
  base64url (52 characters total). Keys are generated with `crypto/rand`.
- Only a SHA-256 hash and a 16-character public lookup prefix are stored. The
  full key is shown to the owner exactly once, at issuance.
- Comparison of a presented key against the stored hash uses
  `crypto/subtle.ConstantTimeCompare`.
- Keys can be revoked and rotated without deleting the application. Every
  authentication failure returns the same `UNAUTHORIZED` response, so a caller
  cannot probe whether a prefix exists, is revoked, or is expired.

## 3. Authentication layers

| Surface | Credential |
|---|---|
| Public API (`/v1/*`) | application API key (`app_live_…`), Bearer |
| Admin API (`/admin/*`) | provisional `ADMIN_TOKEN`, Bearer |
| Dashboard (later) | administrator session; never an application API key |
| Provider callbacks | provider payload validated against the provider API |

`ADMIN_TOKEN` is a deliberate Stage 2–5 placeholder until administrator sessions
with CSRF protection land (§11.4 of the plan). Two properties make it safe in
the meantime:

1. An application API key never grants admin access — the two credential types
   are checked independently.
2. When `ADMIN_TOKEN` is unset the admin API returns 503 and is never open.

The dashboard is protected by its own admin authentication and should be
reachable only over VPN or from a trusted address allowlist.

## 4. URL validation

- `return_url` must belong to the application's allowed domains; otherwise the
  service could be used to redirect buyers anywhere.
- Outgoing webhook destinations are validated before use.
- User-supplied webhook URLs must not target localhost, private/internal IP
  ranges, or infrastructure addresses.
- Outbound delivery must account for DNS rebinding and must not follow unsafe
  redirects. This matters because the service performs HTTP requests to
  administrator-provided addresses (SSRF risk).

## 5. Abuse limits

- Per-API-key request rate limits.
- Caps on unfinished payments per application.
- Limits on `metadata` size, string lengths, and incoming webhook body size.
- Admin surface protected against password brute force, CSRF and unauthorized
  configuration changes.

## 6. Card data

The service must never accept or store full bank card data. Card handling stays
with the payment provider.

## 7. Error hygiene

Client-facing errors use the normalized envelope in `api/openapi.yaml`. Responses
must never expose `secret_key`, database connection strings, stack traces, or
other internal detail. Internal causes are logged server-side with the request id
and surfaced to the client only as a safe code and message.

## 8. Transport

TLS terminates at Caddy. HSTS and security headers are set there. `/health/*` is
not exposed publicly. Health and metrics endpoints are for the platform, not the
internet.
