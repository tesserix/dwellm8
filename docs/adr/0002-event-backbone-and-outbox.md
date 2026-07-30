# ADR-0002 — Event backbone, outbox and delivery guarantees

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Platform
- **Issue**: [#2](https://github.com/tesserix/dwellm8/issues/2)
- **Related**: [ADR-0001](0001-modular-monolith-api.md) (modules and their events), ADR-0015 (Temporal workflows, later)

---

## Context

Rent collection, ledger postings, notifications and document workflows all fan
out from domain events. ADR-0001 names the events each module publishes but
says nothing about how they survive.

HomeChef settled the question the expensive way. An event published from a
request handler — after the transaction commits, before the process is
guaranteed to live — is lost when the pod rolls. It rolls during every deploy.
The symptom was not an error; it was a notification that never arrived and a
ledger that was quietly short, discovered days later.

For Dwellm8 the same failure is a rent receipt that never reaches a tenant, or
a payout that never releases. The outbox is not an optimisation here, and it is
not optional.

---

## Decision

**Every domain event is written to a transactional outbox in the same
transaction as the state change that caused it. A relay publishes from the
outbox to NATS JetStream at least once. Every consumer is idempotent.**

There is no other way to publish an event. A `nats.Publish` in a request
handler is a review failure.

### 1. The envelope

Every event carries the same envelope. Consumers may rely on all of it.

```json
{
  "id":            "01J8XQ...",              // ULID, the deduplication key
  "type":          "money.payment.received",  // module.aggregate.past-tense-verb
  "version":       1,                          // integer, only ever incremented
  "tenant_id":     "uuid",                     // the organisation; never absent
  "occurred_at":   "2026-08-04T09:14:22.481Z", // when the fact happened
  "actor": { "kind": "user|system|provider|support", "id": "uuid|null" },
  "subject":       { "kind": "payment", "id": "uuid" },
  "correlation_id":"01J8XQ...",              // the whole causal chain
  "causation_id":  "01J8XQ...",              // the single event that caused this one
  "data":          { }                         // the payload, module-defined
}
```

- `id` is a ULID, generated when the outbox row is written. It is stable across
  retries, which is what makes deduplication possible at all.
- `tenant_id` is mandatory on every event, with no exception for platform-level
  facts — those carry the platform organisation.
- `occurred_at` is when the fact happened, not when it was published. The gap
  between them is the relay's lag, and it is a metric.
- `correlation_id` survives a whole causal chain — a tenant tapping *pay*
  through to the owner's payout. `causation_id` is the immediate parent.

Money in a payload follows the standard everywhere else in the product:
`amount_minor` as an integer and `currency` beside it. No floats in an event,
ever, including in analytics extracts downstream.

### 2. Naming

`<module>.<aggregate>.<past-tense-verb>` — lower case, dot separated, and a
fact rather than a command. `money.payment.received`, not `money.receive_payment`.

Subjects mirror the type with the tenant appended, so a consumer can filter to
one organisation without deserialising:

```
dwellm8.<module>.<aggregate>.<verb>.<tenant_id>
```

### 3. The outbox

One table, owned by the `platform` schema and writable by every module —
the single exception to ADR-0001's one-writer rule, because the outbox is
infrastructure rather than domain.

```sql
CREATE TABLE outbox (
    id              text PRIMARY KEY,            -- the event ULID
    tenant_id       uuid NOT NULL,
    type            text NOT NULL,
    version         int  NOT NULL DEFAULT 1,
    subject_kind    text NOT NULL,
    subject_id      text NOT NULL,
    correlation_id  text NOT NULL,
    causation_id    text,
    actor_kind      text NOT NULL,
    actor_id        uuid,
    occurred_at     timestamptz NOT NULL,
    payload         jsonb NOT NULL,
    published_at    timestamptz,                 -- NULL until the relay confirms
    attempts        int NOT NULL DEFAULT 0,
    last_error      text,
    next_attempt_at timestamptz NOT NULL DEFAULT now()
);
```

The rule: **the state change and the outbox insert are one transaction.** If
the transaction rolls back, the event never existed. If it commits, the event
will be published — eventually, whatever happens to the process next.

### 4. The relay

A goroutine in the same process, with a second one as a safety net:

1. Claim a batch of unpublished rows where `next_attempt_at <= now()`, using
   `FOR UPDATE SKIP LOCKED` so replicas do not fight.
2. Publish to JetStream with `Nats-Msg-Id` set to the event `id`, which gives
   server-side deduplication inside the stream's duplicate window.
3. On ack, set `published_at`. On failure, increment `attempts`, record the
   error and back off — 1s, 2s, 4s, capped at 5 minutes.
4. A sweeper re-claims rows stuck in flight longer than two minutes, because a
   pod that dies between publish and ack leaves exactly that.

Publishing is at-least-once. It is never exactly-once, and no consumer may
assume otherwise.

**When JetStream is unreachable**, rows accumulate. Nothing is lost and nothing
blocks a user's request — the write already committed. Alerts:

| Condition | Severity |
|---|---|
| Oldest unpublished row older than 60s | P3 |
| Oldest unpublished row older than 5 min | P2 |
| Oldest unpublished row older than 15 min, or backlog above 10,000 | P1 |
| Any row with `attempts >= 10` | P2, and it stops being retried automatically |

### 5. Streams and consumers

One stream per module, subjects wildcarded beneath it:

| Stream | Subjects | Retention | Max age |
|---|---|---|---|
| `DWELLM8_MONEY` | `dwellm8.money.>` | Limits | 30 days |
| `DWELLM8_LEASE` | `dwellm8.lease.>` | Limits | 30 days |
| `DWELLM8_MAINTENANCE` | `dwellm8.maintenance.>` | Limits | 14 days |
| `DWELLM8_IDENTITY`, `_PROPERTY`, `_COMMUNITY`, `_DISCOVERY`, `_NOTIFY` | as above | Limits | 14 days |

Money keeps the longest window because it is the one anybody will ever want to
replay for an audit.

Consumers are **durable, explicit-ack, pull** — named
`<module>-<purpose>`, for example `notify-payment-receipt`. Pull rather than
push, because a consumer that cannot keep up should build a visible backlog
rather than be flooded. `max_deliver: 5`, `ack_wait: 30s`.

### 6. Idempotency, which is the consumer's job

At-least-once means every consumer will see a duplicate eventually — on a
relay retry, a redelivery, or a replay. Each consumer keeps a processed-event
table keyed on `(consumer_name, event_id)` and writes it **in the same
transaction as its effect**. Seen it before, skip.

`money` is stricter. A ledger posting carries the event id in its idempotency
key, and the unique index on that key is what actually prevents a double
posting — not the consumer's memory. **A replay of an entire stream must not
double-post**, and that is a property of the ledger's constraint rather than
the discipline of the code.

### 7. Poison messages

After `max_deliver` failures a message lands on `DWELLM8_DLQ`, tagged with the
consumer, the error and the delivery count. Dead-letter is a P2 alert with a
named owner. Messages are replayed from the DLQ deliberately, after the cause
is fixed — never automatically, because automatic DLQ replay is how a poison
message becomes a loop.

### 8. Versioning

Additive changes only. New optional field, same version. Anything else — a
field removed, renamed, its meaning changed, its type narrowed — is a new
`version` and a new subject suffix (`.v2`), published **alongside** the old one
until every consumer has moved. Producers may not break consumers; that is the
whole point of publishing events instead of calling each other.

---

## Alternatives considered

### A. Publish directly from the handler after commit — rejected

- **For**: no table, no relay, no lag.
- **Against**: the window between commit and publish is unprotected. A pod roll
  inside it loses the event silently. HomeChef proved this in production.
- **Why rejected**: the failure is silent and the loss is permanent.

### B. Change data capture from the WAL (Debezium) — rejected for now

- **For**: no application discipline needed; nothing can forget the outbox.
- **Against**: another stateful component to run; events become table diffs
  rather than domain facts, so consumers reconstruct intent from column
  changes; schema changes become event changes.
- **Why rejected**: a domain event should say *rent received*, not *four
  columns changed*. Worth revisiting if outbox discipline decays.

### C. Two-phase commit between PostgreSQL and NATS — rejected

- **For**: exactly-once in theory.
- **Against**: NATS does not participate in XA, and distributed transactions
  bring an availability cost that dwarfs the problem.
- **Why rejected**: not available, and not desirable if it were.

### D. Outbox with a separate relay deployment — deferred

- **For**: a slow relay cannot starve the API's goroutines; independently scalable.
- **Against**: a second deployment, a second thing to watch, for a load that is
  currently trivial.
- **Why deferred**: the relay is behind an interface, so extracting it later is
  a deployment change, not a rewrite.

---

## Consequences

**Good**

- No domain event is lost, including across a deploy, a crash, or a ten-minute
  NATS outage.
- Consumers can be added without touching a producer.
- A stream replay is safe, because the ledger's idempotency key — not the
  consumer's memory — is what prevents a double posting.

**Bad, and accepted**

- Events are delayed by the relay's poll interval; this is not a synchronous bus.
- Every consumer carries idempotency bookkeeping. It is boilerplate, and it is
  the price of at-least-once.
- The outbox table grows and needs pruning — published rows older than 7 days,
  by a job that must never delete an unpublished one.

**Follow-up work this ADR creates**

- The outbox table and relay in `internal/platform/outbox`, with a test that
  kills the relay mid-publish and asserts nothing is lost.
- The four backlog alerts, in the same PrometheusRule as the database alerts.
- The event catalogue as a versioned document, generated from the code rather
  than hand-maintained.
- ADR-0006 for the ledger's posting rules, where the idempotency key this ADR
  relies on is actually defined.
