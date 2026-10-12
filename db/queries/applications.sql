-- Queries for applications, API keys and provider connections.

-- name: CreateApplication :one
INSERT INTO applications (name, slug, default_return_url, webhook_url)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetApplication :one
SELECT * FROM applications WHERE id = $1;

-- name: GetApplicationBySlug :one
SELECT * FROM applications WHERE slug = $1;

-- name: ListApplications :many
SELECT * FROM applications ORDER BY created_at DESC;

-- name: CreateAPIKey :one
INSERT INTO api_keys (application_id, key_prefix, key_hash, name, permissions, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAPIKeyByPrefix :one
SELECT * FROM api_keys WHERE key_prefix = $1;

-- name: ListAPIKeys :many
SELECT * FROM api_keys WHERE application_id = $1 ORDER BY created_at DESC;

-- name: RevokeAPIKey :execrows
UPDATE api_keys
SET revoked_at = now()
WHERE id = $1 AND application_id = $2 AND revoked_at IS NULL;

-- name: TouchAPIKeyLastUsed :exec
UPDATE api_keys
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: CreateProviderAccount :one
INSERT INTO provider_accounts (provider, shop_id, encrypted_secret_key, environment)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetProviderAccount :one
SELECT * FROM provider_accounts WHERE id = $1;

-- name: GetProviderAccountByShop :one
SELECT * FROM provider_accounts
WHERE provider = $1 AND shop_id = $2 AND environment = $3;

-- name: AttachProviderAccount :exec
INSERT INTO application_providers (application_id, provider_account_id, is_default)
VALUES ($1, $2, $3)
ON CONFLICT (application_id, provider_account_id)
DO UPDATE SET is_default = EXCLUDED.is_default;

-- name: ListProviderAccountsForApplication :many
SELECT pa.*, ap.is_default
FROM provider_accounts pa
JOIN application_providers ap ON ap.provider_account_id = pa.id
WHERE ap.application_id = $1
ORDER BY pa.created_at DESC;

-- name: ClearDefaultProviderForApplication :exec
UPDATE application_providers
SET is_default = false
WHERE application_id = $1 AND is_default;

-- name: GetDefaultProviderAccountForApplication :one
SELECT pa.*
FROM provider_accounts pa
JOIN application_providers ap ON ap.provider_account_id = pa.id
WHERE ap.application_id = $1
  AND ap.is_default
  AND pa.status = 'active'
LIMIT 1;

-- name: UpdateProviderAccountStatus :exec
UPDATE provider_accounts SET status = $2 WHERE id = $1;
