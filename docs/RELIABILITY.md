# Reliability

Status: Stage 1 baseline. Defines recovery, retry and delivery rules. Implemented
across Stages 4–5.

## 1. Principle

A confirmed payment must never be lost, and an unconfirmed one must never be
treated as final. Every step that changes money-related state is recoverable
from persisted data.

## 2. Operation records

Before any external call, a row is written to `payment_operations` with:

- the operation type,
- the provider idempotency key,
- a hash and a copy of the immutable request parameters,
- attempt count and last error.

If the connection drops mid-call, the record already exists and the worker can
recover it. The stored request parameters are sufficient to replay the call
unchanged. No secrets or customer PII are stored here.

## 3. Status tracking without webhooks

The service works correctly when no YooKassa webhook is configured.

Adaptive polling schedule (initial values, tuned per provider limits):

| Age of an unfinished payment | Poll interval |
|---|---|
| first 5 minutes | 15–30 s |
| 5–30 minutes | ~1 minute |
| 30 minutes – 2 hours | 5 minutes |
| older | 10–15 minutes |

Polling stops only on a confirmed terminal state. A payment is never marked
`canceled` merely because some minutes have passed — some methods settle slowly.
Stuck operations move to a diagnostics queue.

## 4. Retry policy

- Retryable: connection errors, timeouts, HTTP 429, HTTP 5xx.
- Terminal (do not retry blindly): HTTP 400/422 validation errors.
- Auth failure (HTTP 401/403): the connection moves to a state requiring
  administrator action; retrying forever is pointless.
- HTTP 429 uses exponential backoff with jitter.
- Concurrent requests per provider account are capped.

## 5. Transactional outbox

Status change and event creation commit in one transaction:

```sql
BEGIN;
UPDATE payments SET status = 'succeeded'
WHERE id = $1 AND status NOT IN ('succeeded', 'canceled');
INSERT INTO outbox_events (...);
COMMIT;
```

It is impossible to persist a new status without the matching event. The insert
runs only after a confirmed legal transition, guarded against duplicates.

## 6. Event delivery

- The delivery worker claims events with `FOR UPDATE SKIP LOCKED`.
- Events are POSTed to the application's registered URL with an HMAC-SHA256
  signature over the body plus a timestamp.
- The system guarantees **at-least-once delivery, not exactly-once**. Consumers
  must tolerate duplicates and should store processed `event_id`s.
- Failed deliveries retry with increasing backoff. After prolonged failure the
  delivery is marked `delivery_failed` and an administrator alert is raised; the
  event stays available for resend.

## 7. Incoming webhooks

1. Validate format, identify the provider connection, persist the raw event.
2. Return HTTP 200 to YooKassa only after durable storage.
3. The worker re-fetches the payment from the provider API, verifies shop id,
   amount, currency and application ownership.
4. Update state only if it actually changed; a repeated notification causes no
   repeated business change.
5. YooKassa redelivers unacknowledged notifications for up to 24 hours, so
   persistence plus reconciliation matter even with webhooks enabled.

## 8. Restart and recovery

- On restart the worker resumes unfinished operations and undelivered events
  from PostgreSQL; nothing lives only in memory.
- After restoring from a backup the service reconciles unfinished payments with
  YooKassa and rebuilds the delivery queue.

## 9. Observability

Endpoints: `GET /health/live`, `GET /health/ready`, `GET /metrics`.

Track: payments created, provider errors, operation duration, unfinished
payments, outbox queue length, time of last successful background run. Alert on
payments that stay in an unknown state too long.
