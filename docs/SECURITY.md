# Security

Status: Stage 1 baseline. Mandatory from the first release; features land in
Stages 2, 3 and 8.

## 1. Provider secret storage

- YooKassa `secret_key` values are stored encrypted with authenticated
  encryption: AES-256-GCM.
- The master key (`MASTER_ENCRYPTION_KEY`) lives outside PostgreSQL — an
  environment secret or a dedicated secret manager. Compromising the database
  alone must not reveal provider keys.
- In production, `MASTER_ENCRYPTION_KEY` is required; startup fails without it.
- Master key and provider keys support rotation.
- Logs must never contain secrets, authorization headers, or full request bodies
  with sensitive data.

## 2. Application isolation

- Every request runs in the context of one `application_id`, resolved by the
  server from the API key. A client can never supply another application's id.
- All database access is filtered by the authenticated application.
- API keys are generated with a cryptographically secure RNG, shown to the owner
  exactly once, and stored only as a hash. Lookup uses a public prefix/id.
- Keys can be revoked and rotated without deleting the application.

## 3. Authentication layers

| Surface | Credential |
|---|---|
| Public API | application API key (`app_live_…`), Bearer |
| Dashboard | separate administrator session; never an application API key |
| Provider callbacks | provider payload validated against the provider API |

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
