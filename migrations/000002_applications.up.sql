-- 000002_applications: client applications, API keys and provider connections.
--
-- Isolation rule (docs/SECURITY.md §2): every row that belongs to an
-- application carries application_id, and all access is filtered by it.

CREATE TABLE applications (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name               varchar(255) NOT NULL,
    slug               varchar(64) NOT NULL UNIQUE,
    status             varchar(16) NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'disabled')),
    default_return_url text,
    webhook_url        text,
    -- HMAC secret used to sign outgoing events (docs/RELIABILITY.md §6).
    webhook_secret_encrypted bytea,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER applications_set_updated_at
    BEFORE UPDATE ON applications
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE api_keys (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id uuid NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    -- Public lookup prefix of the key. The key itself is never stored.
    key_prefix     varchar(16) NOT NULL UNIQUE,
    key_hash       bytea NOT NULL,
    name           varchar(255) NOT NULL DEFAULT '',
    permissions    text[] NOT NULL DEFAULT '{}',
    expires_at     timestamptz,
    last_used_at   timestamptz,
    revoked_at     timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX api_keys_application_id_idx ON api_keys (application_id);

CREATE TABLE provider_accounts (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider             varchar(32) NOT NULL,
    shop_id              varchar(64) NOT NULL,
    -- AES-256-GCM ciphertext; the plaintext secret never leaves the process.
    encrypted_secret_key bytea NOT NULL,
    environment          varchar(16) NOT NULL
                         CHECK (environment IN ('test', 'production')),
    status               varchar(16) NOT NULL DEFAULT 'active'
                         CHECK (status IN ('active', 'needs_attention', 'disabled')),
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider, shop_id, environment)
);

CREATE TRIGGER provider_accounts_set_updated_at
    BEFORE UPDATE ON provider_accounts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- One YooKassa account may serve several applications (docs/TECHNICAL_PLAN.md §4.1).
CREATE TABLE application_providers (
    application_id      uuid NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    provider_account_id uuid NOT NULL REFERENCES provider_accounts (id) ON DELETE CASCADE,
    is_default          boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (application_id, provider_account_id)
);

-- At most one default provider account per application.
CREATE UNIQUE INDEX application_providers_one_default_idx
    ON application_providers (application_id)
    WHERE is_default;
