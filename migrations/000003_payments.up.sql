-- 000003_payments: the payment core.
--
-- Key invariants enforced here rather than in Go (docs/PAYMENT_LIFECYCLE.md):
--   * one order may have many attempts, but at most ONE non-terminal attempt
--   * a provider payment id maps to exactly one internal payment
--   * every provider call is recorded before it happens, with the exact body
--     needed to replay it (docs/RELIABILITY.md §2)

CREATE TABLE payments (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id      uuid NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    provider_account_id uuid NOT NULL REFERENCES provider_accounts (id),

    order_id            varchar(128) NOT NULL,
    attempt_number      integer NOT NULL DEFAULT 1 CHECK (attempt_number > 0),

    provider            varchar(32) NOT NULL,
    provider_payment_id varchar(64),

    amount_minor        bigint NOT NULL CHECK (amount_minor > 0),
    currency            varchar(3) NOT NULL DEFAULT 'RUB' CHECK (currency = 'RUB'),
    description         varchar(128) NOT NULL DEFAULT '',

    status              varchar(32) NOT NULL DEFAULT 'created'
                        CHECK (status IN ('created', 'creating', 'creation_unknown',
                                          'pending', 'succeeded', 'canceled')),
    checkout_url        text,
    return_url          text,

    metadata            jsonb NOT NULL DEFAULT '{}'::jsonb,

    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    completed_at        timestamptz,
    next_check_at       timestamptz,

    UNIQUE (application_id, order_id, attempt_number)
);

CREATE TRIGGER payments_set_updated_at
    BEFORE UPDATE ON payments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A provider payment belongs to exactly one internal payment.
CREATE UNIQUE INDEX payments_provider_payment_id_idx
    ON payments (provider, provider_payment_id)
    WHERE provider_payment_id IS NOT NULL;

-- The rule from docs/PAYMENT_LIFECYCLE.md §3: at most one attempt per order may
-- be non-terminal. Two concurrent creates cannot both win this index.
CREATE UNIQUE INDEX payments_one_active_attempt_idx
    ON payments (application_id, order_id)
    WHERE status NOT IN ('succeeded', 'canceled');

CREATE INDEX payments_application_created_idx ON payments (application_id, created_at DESC);
CREATE INDEX payments_order_idx ON payments (application_id, order_id);
CREATE INDEX payments_poll_idx ON payments (next_check_at)
    WHERE status NOT IN ('succeeded', 'canceled');

-- Records every interaction with the provider. Written before the call so a
-- lost connection is recoverable (docs/RELIABILITY.md §2).
CREATE TABLE payment_operations (
    id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_id               uuid NOT NULL REFERENCES payments (id) ON DELETE CASCADE,
    operation_type           varchar(32) NOT NULL
                             CHECK (operation_type IN ('create_payment', 'get_payment', 'create_refund')),

    -- Provider-facing idempotency key. Generated and stored before the first call.
    provider_idempotency_key varchar(64) NOT NULL UNIQUE,
    request_hash             bytea NOT NULL,
    -- Immutable request parameters, sufficient to replay the call unchanged.
    request_body             jsonb NOT NULL,

    status                   varchar(32) NOT NULL DEFAULT 'pending'
                             CHECK (status IN ('pending', 'in_progress', 'succeeded',
                                               'failed', 'unknown', 'needs_review')),
    attempt_count            integer NOT NULL DEFAULT 0,
    last_error               text,
    -- Deadline of the provider's idempotency guarantee, used by the NEEDS_REVIEW rule.
    idempotency_expires_at   timestamptz,
    started_at               timestamptz,
    completed_at             timestamptz,
    created_at               timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX payment_operations_payment_idx ON payment_operations (payment_id);
CREATE INDEX payment_operations_open_idx ON payment_operations (status)
    WHERE status IN ('pending', 'in_progress', 'unknown');

-- Protects the public API from repeated requests. Replaying a key with an
-- identical body returns the current state of the same payment; a different
-- body is a conflict (docs/PAYMENT_LIFECYCLE.md §4).
CREATE TABLE idempotency_requests (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id  uuid NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    idempotency_key varchar(64) NOT NULL,
    request_hash    bytea NOT NULL,
    payment_id      uuid REFERENCES payments (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (application_id, idempotency_key)
);
