-- 000001_init: baseline schema objects shared by every module.
--
-- Stage 1 only lays groundwork. Domain tables (applications, api_keys,
-- provider_accounts, payments, ...) are added in their own migrations.

-- pgcrypto provides gen_random_uuid() and digest() used across the schema.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- set_updated_at keeps the updated_at column honest for every table that
-- attaches this trigger. Defined once, reused everywhere.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
