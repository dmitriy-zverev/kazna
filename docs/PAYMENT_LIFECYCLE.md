# Payment lifecycle

Status: Stage 1 baseline. Defines the internal state machine, allowed
transitions and idempotency rules. Implementation lands in Stage 3.

## 1. States

| State | Meaning | Terminal |
|---|---|---|
| `created` | Local operation record exists; provider not contacted yet | no |
| `creating` | Provider call in flight | no |
| `creation_unknown` | Provider may have processed the request; result not confirmed | no |
| `pending` | Payment exists at the provider, awaiting completion | no |
| `succeeded` | Payment confirmed by the provider | yes |
| `canceled` | Provider confirmed cancellation or definitive failure | yes |

`waiting_for_capture` is intentionally absent: MVP uses one-stage payments.

## 2. Transitions

```
created ──▶ creating ──┬──▶ pending ──┬──▶ succeeded
                       │              └──▶ canceled
                       └──▶ creation_unknown ──▶ pending
                                              └──▶ canceled
```

Rules enforced by a dedicated state machine, not by ad-hoc `if`s:

1. Only the listed transitions are allowed.
2. `succeeded` and `canceled` are terminal and never leave their state.
3. A late webhook must never move `succeeded` back to `pending`.
4. External provider statuses never drive internal transitions directly; a
   mapper translates them and the state machine validates the result.

## 3. One active attempt per order

- A single order may have several attempts over time (`attempt_number` 1, 2, …).
- At most **one non-terminal attempt** may exist per `(application_id, order_id)`.
- A new attempt is created only after the previous one is terminal, and only
  through `POST /v1/orders/{order_id}/payment-attempts`.
- A database-level partial unique index enforces this; the Go check alone is not
  sufficient because two concurrent requests can both pass it.

## 4. Idempotency

Two independent layers with separate keys:

| Layer | Key | Guarantee |
|---|---|---|
| Application → this service | client `Idempotency-Key` header (≤ 64 chars) | Request replay returns the stored result |
| This service → YooKassa | server-generated UUID stored before the first call | Provider treats replays as the same operation |

Rules:

1. Same key + identical body → return the stored result, no new provider call.
2. Same key + different body → `409 IDEMPOTENCY_CONFLICT`.
3. The provider idempotency key is generated and persisted **before** the first
   external call, together with the exact request body needed to replay it.
4. A retry reuses the same key and byte-identical body.
5. YooKassa honors idempotency keys for 24 hours. After that window a replayed
   key may be treated as new.

## 5. The 24-hour rule

If the outcome of a create operation cannot be established before the provider
idempotency window expires, the operation moves to `NEEDS_REVIEW`.

**Automatically retrying a payment creation after the idempotency window has
expired is forbidden** until it is established whether the original operation
existed. This is a safety requirement, not an optimization.

## 6. Money representation

- All amounts are integers in minor units (kopecks for RUB).
- The API accepts and returns minor units.
- Conversion to the YooKassa string format (`"499.00"`) happens only inside the
  provider adapter.

## 7. Status source of truth

The provider is authoritative for payment outcome. Our database is authoritative
for identity, ownership and delivery state. Reconciliation (see
`docs/RELIABILITY.md`) keeps the two aligned.
