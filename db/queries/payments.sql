-- Queries for the payment core.

-- name: CreatePayment :one
INSERT INTO payments (
    application_id, provider_account_id, order_id, attempt_number, provider,
    amount_minor, currency, description, status, return_url, metadata
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetPayment :one
SELECT * FROM payments WHERE id = $1;

-- name: GetPaymentForApplication :one
SELECT * FROM payments WHERE id = $1 AND application_id = $2;

-- name: ListPaymentsForApplication :many
SELECT * FROM payments
WHERE application_id = sqlc.arg('application_id')
  AND (sqlc.narg('order_id')::varchar IS NULL OR order_id = sqlc.narg('order_id')::varchar)
ORDER BY created_at DESC
LIMIT sqlc.arg('limit')::int;

-- name: NextAttemptNumber :one
SELECT COALESCE(MAX(attempt_number), 0) + 1 AS attempt_number
FROM payments
WHERE application_id = $1 AND order_id = $2;

-- name: LockOrderForPayment :exec
-- Serializes payment creation per (application, order) so the "order already
-- paid" check and the insert cannot interleave with a concurrent success.
SELECT pg_advisory_xact_lock(
    hashtextextended(sqlc.arg('application_id')::text || ':' || sqlc.arg('order_id')::text, 0)
);

-- name: OrderHasSucceededPayment :one
SELECT EXISTS (
    SELECT 1 FROM payments
    WHERE application_id = $1 AND order_id = $2 AND status = 'succeeded'
) AS exists;

-- name: MarkPaymentCreating :exec
UPDATE payments SET status = 'creating' WHERE id = $1 AND status = 'created';

-- name: SetPaymentCreated :exec
UPDATE payments
SET status = 'pending',
    provider_payment_id = $2,
    checkout_url = $3,
    next_check_at = $4
WHERE id = $1;

-- name: SetPaymentCreationUnknown :exec
UPDATE payments
SET status = 'creation_unknown',
    next_check_at = $2
WHERE id = $1 AND status = 'creating';

-- name: SetPaymentCanceled :exec
UPDATE payments
SET status = 'canceled',
    completed_at = now(),
    next_check_at = NULL
WHERE id = $1 AND status NOT IN ('succeeded', 'canceled');

-- name: ApplyProviderStatus :exec
UPDATE payments
SET status = sqlc.arg('status')::varchar,
    completed_at = CASE
        WHEN sqlc.arg('status')::varchar IN ('succeeded', 'canceled') THEN now()
        ELSE completed_at
    END,
    next_check_at = sqlc.arg('next_check_at')
WHERE id = sqlc.arg('id') AND status NOT IN ('succeeded', 'canceled');

-- name: CountUnfinishedPayments :one
SELECT count(*) FROM payments WHERE status NOT IN ('succeeded', 'canceled');

-- name: CreatePaymentOperation :one
INSERT INTO payment_operations (
    payment_id, operation_type, provider_idempotency_key, request_hash,
    request_body, status, idempotency_expires_at
)
VALUES ($1, $2, $3, $4, $5, 'pending', $6)
RETURNING *;

-- name: StartPaymentOperation :exec
UPDATE payment_operations
SET status = 'in_progress', started_at = now(), attempt_count = attempt_count + 1
WHERE id = $1 AND status = 'pending';

-- name: CompletePaymentOperation :exec
UPDATE payment_operations
SET status = $2, completed_at = now(), last_error = $3
WHERE id = $1;

-- name: GetIdempotencyRequest :one
SELECT * FROM idempotency_requests
WHERE application_id = $1 AND idempotency_key = $2;

-- name: CreateIdempotencyRequest :one
INSERT INTO idempotency_requests (application_id, idempotency_key, request_hash, payment_id)
VALUES ($1, $2, $3, $4)
RETURNING *;
