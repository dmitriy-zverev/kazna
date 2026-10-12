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
- Every application also receives a webhook signing secret (32 random bytes,
  base64url) used for HMAC-SHA256 over outgoing events. It is stored encrypted
  with the same cipher as provider secrets, returned once at creation, and can
  be rotated through the admin API.
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

Outbound URLs (`webhook_url`, `default_return_url`) are validated when they are
stored, in `internal/security.ValidateOutboundURL`:

- Scheme must be `https`. In development and staging `http` is also accepted so
  a local webhook sink can be registered.
- Embedded credentials (`https://user:pass@host/…`) are rejected.
- In production the host must not be `localhost`, `*.localhost`, `*.local`,
  `*.internal`, or a single-label name such as `webhook-sink`.
- In production every resolved address must be public: loopback, private
  (10/8, 172.16/12, 192.168/16), link-local (169.254/16, including the cloud
  metadata endpoint), unspecified and multicast addresses are rejected.
- A hostname that cannot be resolved is rejected, because its destination cannot
  be verified.

`APP_ENV=development` and `APP_ENV=staging` relax the host and scheme rules;
`APP_ENV=production` does not. This split is what the independent verification
harness expects (docs/VERIFICATION_HARNESS.md).

Still outstanding:

- **Per-application domain allowlisting for `return_url`.** The plan (§12.3)
  requires the return URL to match the application's allowed domains. The
  allowlist needs a management surface, so it lands with the dashboard. Until
  then a return URL only has to be a well-formed public URL.
- **Re-validation at delivery time.** Checking at write time does not close the
  DNS-rebinding window: a name that resolved publicly can later resolve
  internally. The delivery worker must resolve and re-check immediately before
  each request and must not follow redirects to unsafe addresses. This lands
  with the delivery worker in Stage 5.

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
