# ADR-0011 — Payment provider adapter, idempotency and the webhook contract

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Payments
- **Issue**: [#12](https://github.com/tesserix/dwellm8/issues/12)
- **Related**: [ADR-0006](0006-chart-of-accounts-and-posting-rules.md) (what a captured payment posts), [ADR-0007](0007-money-representation-and-rounding.md) (the amount), [ADR-0009](0009-property-block-unit-model.md) (what a firm may collect against), [ADR-0003](0003-tenancy-and-row-level-security.md) (the isolation the inbox bends and does not break), ADR-0012 (settlement reconciliation, next), ADR-0015 (durable workflows, later)

---

## Context

Razorpay is the primary aggregator and the architecture may not depend on it.
That is the stated constraint, and it is the smaller half of the problem.

The larger half is that a payment provider is an unreliable narrator. It tells
you things out of order, it tells you the same thing five times, it tells you
about payments you have never heard of, and occasionally something that is not
the provider tells you something very convenient. Every rental product that has
lost money to a payments bug lost it to one of those four, and none of them are
fixed by choosing a better provider.

So this ADR settles three things before any collection code exists: the shape of
the adapter, the idempotency contract, and what a webhook is permitted to cause.
The third is the one that matters. A webhook is a hint that something may have
changed. It is not evidence that it did, and a system that treats it as evidence
will eventually credit a tenant for a payment that never cleared.

What was already fixed going in: money is `int64` paise with a ceiling
(ADR-0007), a captured payment posts into gateway clearing rather than the bank
(ADR-0006 §4), and a management firm reaches an owner's units through a grant at
unit granularity (ADR-0009). The last of those turned out to constrain the
payments table more than the payments design did.

---

## Decision

**One canonical payment model that names no provider; a client idempotency key
that is enforced by a unique index rather than by a handler; every webhook
deduplicated, recorded, and permitted only to trigger a confirmation against the
provider's own API; and a forward-only state machine enforced in Go and in
PostgreSQL.**

### 1. The canonical model, and the seam

`collect.Payment` is what a provider was asked to do and what it said back. It
is deliberately not the ledger: the ledger records what is true about the money,
this records an attempt to collect it, and the two meet at `entry_id` only once
the payment reaches a state that justifies a posting.

The method vocabulary is closed and spans everything the product takes:
`upi_collect`, `upi_intent`, `upi_autopay`, `card`, `netbanking`, and three
offline methods — cash, cheque, transfer. Offline is a first-class method rather
than the absence of one, for the reason in §6.

The seam is `internal/money/provider`. An adapter translates one provider's
vocabulary into the canonical one and back; nothing in `money/domain` names an
aggregator; and a second aggregator is a new file there plus a line of
configuration. `Registry` mirrors the email-provider engine already used in the
org: a named set, an ordered chain from configuration, and a fallback that is
always registered.

Two details are worth stating because they are the ones that decay:

- **`Registry.By(name)`, not the chain, resolves an existing payment.** A payment
  created under Razorpay is confirmed and reconciled against Razorpay forever, no
  matter what the chain says after a migration. Resolving by chain would silently
  ask the wrong provider about a payment it has never heard of, and that answer
  is indistinguishable from "this payment failed".
- **A duplicate registration panics at startup, and an unknown name in the chain
  fails at startup.** A deployment that quietly used a different provider than
  its configuration names is the worst outcome available here, and the typo that
  causes it (`razropay`) is discovered at 9am on the first of the month
  otherwise.

Razorpay's own HTTP client is not in this ADR. What is here is the contract it
must satisfy, the registry that selects it, the signature verification every
adapter needs, and the offline adapter — which is not a stub.

### 2. Idempotency is an index, not a handler

Every collection request carries a client key. It is stored on the payment, and
`payments_idempotency_idx` is `UNIQUE (tenant_id, idempotency_key)`.

That index *is* the guarantee. Three retries produce one row because the second
and third lose a race against a unique constraint — not because a handler
remembered to check first, which is a check that is correct in review and wrong
under concurrency. Measured: a second insert with the same key fails with
`duplicate key value violates unique constraint "payments_idempotency_idx"`.

**The key does not expire.** This is a deliberate difference from the provider's
own idempotency keys, which typically last 24 hours. A retry a month later — a
replayed queue, a reconciliation job, an operator repeating a request — must not
create a second collection, and an expiring key is exactly the mechanism that
would let it. The row is never deleted, so the key is never free.

The provider's key is passed through as well where the provider supports one.
The two guarantees are different and both are wanted: theirs stops a duplicate
order at their end, ours stops a duplicate row at ours, including for a provider
that offers nothing.

### 3. A forward-only state machine, in both places

Eight states. `created → attempted → authorised → captured → settled`, with
`failed`, `expired` and `cancelled` as endings, and terminal states absorbing.

`authorised` has two endings on purpose. An authorisation that the bank declines
at capture is `failed`; one released deliberately is `cancelled`. The provider
reports them differently, and an owner asking why a collection did not happen
needs the difference between "the bank declined" and "we let it go".

**`from == to` is a permitted no-op, and that is the whole deduplication
design.** The fifth delivery of an event asks for the state the payment is
already in; that is allowed and changes nothing. There is no delivery counter
anywhere in the system, because there is nothing for one to protect.

The machine exists in Go, so a handler can decide without a round trip, and as
`payment_transition_allowed()` in the schema, so an out-of-order update is
refused even on a path that never went through Go. The contract test evaluates
the SQL function over all 64 ordered pairs and fails on any disagreement.
Measured, with the self-transition dropped from the function: 17 pairs disagree
and each is named. Measured against PostgreSQL 16, on the trigger:

```
attempted -> attempted   accepted (the redelivered webhook)
attempted -> settled     ERROR: payment ... cannot go from attempted to settled
attempted -> created     ERROR: payment ... cannot go from attempted to created
```

`payments_captured_has_entry` is the other half: a payment cannot be `captured`
or `settled` with no `entry_id`. Money the provider has and the ledger does not
know about is the exact shape of the defect this whole subsystem exists to
prevent, and it is a `CHECK` rather than a convention. Measured: capturing
without an entry fails on that constraint by name.

### 4. A webhook is advisory, and the design makes that structural

`Decide(delivery, current)` returns one of three dispositions: **Confirm**,
**Ignore**, **Park**. There is no fourth, and none of the three writes a status.
The only affirmative outcome is *go and ask the provider*, and `ApplyConfirmed`
— the only writer of `Status` — takes a confirmed status rather than a claimed
one. "A webhook alone never moves money" is therefore a property of the types,
not a discipline somebody has to remember at review time.

The order of checks inside `Decide` is load-bearing:

1. **Signature first.** An unverified delivery is not inspected further and the
   payment it names is not looked up. Its contents are an attacker's if the
   signature failed, and the park reason reported for an unsigned delivery is the
   signature — never "unknown payment" — so a prober learns nothing about which
   payments exist.
2. **Unknown claim → parked as unsupported.** A status this system has no state
   for is not guessed at.
3. **Unknown payment → parked, not dropped.** The provider believes it collected
   money against something this system has never seen. Dropping loses that;
   guessing an organisation for it is a cross-tenant write.
4. **Same state → ignored.** The redelivery case.
5. **Not a legal transition → parked as stale.** The out-of-order case. Parked
   rather than ignored, because it is also what a replay attack looks like and
   the two deserve the same scrutiny.

Deduplication is `UNIQUE (provider, provider_event_id)` on `payment_events`, so
the fifth delivery conflicts and is discarded without any handler being careful.
Every delivery is stored, verified or not: an unverified one is evidence of
something, possibly of an attack. `payment_events_unverified_is_parked` makes it
structurally impossible to attribute an unverified delivery to a payment.

Signature verification is HMAC-SHA256 with a constant-time compare via
`crypto/subtle`. A byte-by-byte comparison leaks the correct signature one
request at a time to anybody willing to measure. An empty secret verifies
nothing, so a deployment that forgot to configure one rejects every delivery
rather than trusting every delivery.

### 5. The inbox is the one table that may belong to nobody

`payment_events.tenant_id` is nullable, and that is "parked, not dropped" made
structural rather than procedural. A delivery for an unknown payment has no
organisation to attribute it to, so it has none, and the policy makes it visible
only to a platform session — the reconciliation sweep.

The consequence, stated because it is a real architectural constraint rather
than an implementation detail: **webhook ingestion is a platform-role path.** The
handler runs before it knows whose money this is, so it cannot run inside a
tenant-scoped session. `payment_events`'s `WITH CHECK` is `is_platform_session()`
alone: no organisation can write to the inbox at all. Asserted from both sides —
neither harness organisation can read a parked event or forge one, and the
platform session sees it.

`payments` itself is ordinary ADR-0003/0009 territory and gets the five-part
contract. One thing about it is not ordinary: `property_id` is `NOT NULL`, unlike
`ledger_postings`, because every collection is against something a tenant
occupies and there is no organisation-level collection the way a GST remittance
is an organisation-level posting. That lets the delegated branch of the policy be
unconditional instead of having to guard against a missing property.

**The ADR-0009 assertions caught this table without being changed.** Assertion 6
is column-driven — it finds every table with a `unit_id` and requires
`is_delegated_unit()` in its policy — so writing the payments policy at property
granularity, which is the plausible mistake, failed the bootstrap:

```
ERROR: table(s) identifying a unit whose policy does not use is_delegated_unit():
       payments — a one-unit mandate would read every unit in the property
```

That was checked deliberately rather than assumed, and CI now plants it, because
a guard that only covers the tables its author had in mind decays with every
migration.

### 6. Offline is a method, not a fallback

When the aggregator is down the tenant still pays. The alternative to recording
that is a caretaker's notebook, so `offline` is a registered adapter, always
present, and the last link of every chain.

It is emphatically **not** an automatic fallback. `Registry.For` refuses to
resolve an online method to the offline adapter even when offline is first in the
chain. Offline means a person asserted that money arrived; choosing it because an
API call failed would record a receipt nobody witnessed. `ErrUnavailable` is
distinguished from every other error so the layer above — which has a human in
it — can offer offline recording as a choice. The degradation is a decision, not
a code path.

Offline's `Confirm` reports `captured`, because by the time anybody records cash
the money has arrived and the person recording it is the evidence. Its
`VerifyWebhook` is false for every input: offline payments generate no webhooks,
so anything claiming to be one is not from a provider that does not exist.

### 7. What fails the build

- `internal/money/domain/collect` — the state machine table stated explicitly,
  terminal absorption, the five-delivery out-of-order scenario, and the assertion
  that no delivery in any shape moves a status.
- `internal/money/provider` — the retry scenario, chain resolution, the refusal
  to fall through to offline, signature verification against six wrong
  signatures, and the startup failures.
- `internal/money/store` — the drift check between Go's transition table and the
  schema's function over all 64 pairs, the method/status/park-reason
  vocabularies, and the existence and shape of the idempotency index.
- `internal/platform/tenancy/isolationtest` — ADR-0003's five-part contract on
  `payments`, and the parked-webhook visibility test from both sides.

CI plants two failures and expects red: the schema's transition function with the
self-transition removed, and the payments policy weakened to property
granularity. They join the ADR-0003, -0005, -0006, -0007 and -0009 guards.

---

## Alternatives considered

### A. Applying the webhook's status directly — rejected

The obvious design, and it is what most integrations do. Rejected because it
makes the provider's message the system of record for money. Webhooks arrive out
of order routinely and are forgeable if a secret ever leaks; the confirmation
call costs one HTTP request per state change, on an event that happens a handful
of times per payment. It is the cheapest insurance in the system.

The state machine would catch the worst of it — a backwards transition is refused
either way — but "captured" arriving for a payment that actually failed is a
forward transition, and nothing but asking the provider distinguishes it.

### B. Idempotency in the handler rather than in an index — rejected

Check for an existing payment with this key, and create one if absent. Correct in
review, wrong under concurrency: two retries arriving together both find nothing
and both create. The unique index is the only version of this that is true when
two requests race, which is precisely when a retry storm happens.

### C. Dropping a webhook for an unknown payment — rejected

Tempting, because it is usually a test event or another environment's traffic.
Rejected because the residual case is the provider believing it collected money
this system cannot account for, which is the one webhook that must never be lost.
The nullable `tenant_id` is what makes keeping it possible without a cross-tenant
write.

### D. Attributing an unknown webhook by looking up the amount or the payer — rejected

An attempt to rescue the unknown-payment case by matching on something other than
the provider's id. Rejected as a cross-tenant read that is also unreliable: two
tenants in the same tower paying the same rent on the same day is not a rare
event, it is the first of the month.

### E. A status rank column with a `>=` check instead of a transition table — rejected

Simpler, and it enforces monotonicity in one line. Rejected because monotonicity
is not the rule: `created → failed` is a legal move to a lower-ranked state, and
`attempted → settled` is an illegal move to a higher-ranked one. A rank permits
the second and forbids the first, which is exactly backwards on both.

### F. Automatic fallback to offline recording when the provider is down — rejected

See §6. It converts an infrastructure failure into a financial assertion nobody
made. The provider being unreachable is distinguishable (`ErrUnavailable`) so
that a human can be offered the choice, which is a different thing from making it
for them.

### G. One table for payments and webhook deliveries — rejected

A single `payment_events` table with the payment as a projection over it. It is a
clean event-sourced shape and it fails on the tenancy model: deliveries may
belong to no organisation and payments always belong to one, so the table would
need a nullable `tenant_id` and the payment rows would be one policy mistake away
from being globally readable. Two tables let `payments` have the ordinary
strict policy and confine the nullable-tenant exception to the inbox, where it is
visible and small.

### H. Storing the provider's raw payload only, without a normalised status — rejected

Keeps the adapter thin and defers all interpretation. Rejected because the
interpretation then happens in every reader, at query time, forever — including
in reports written two years later by somebody who has never read the provider's
documentation.

---

## Consequences

**What is now true.** No domain code names an aggregator, and a second one is a
file plus a configuration line. A retried collection request produces one
payment because of an index. A webhook cannot move money in any code path,
because no function that reads a webhook can write a status. Deliveries are
deduplicated, kept whether or not they were acted on, and never attributed to an
organisation on a guess. A payment cannot be captured without a ledger entry, in
Go and in PostgreSQL. And the payments table was policed by an assertion written
before it existed.

**What this costs.** Every state change costs an extra call to the provider, and
a provider whose confirmation API is slower or less available than its webhooks
will make collections feel slower. Webhook ingestion needs the platform role,
which is a wider connection than a request handler would otherwise use and has to
stay confined to that one path. Parked events accumulate and need a sweep with
somebody's attention on it — ADR-0012's job, and the reason the parked index
exists. And the transition table lives in two places; the drift check is real but
it is a check, not an impossibility.

**What is not decided.** Razorpay's client itself. Mandate lifecycle for UPI
autopay — creation, amendment, revocation — which is its own story and is the
only reason `upi_autopay` currently looks like an ordinary method. Payout rails,
which are MVP 3. The retry, timeout and dead-letter policy around the
confirmation call, which belongs with ADR-0015's durable workflow standard rather
than here. And settlement reconciliation — matching captured payments against the
provider's settlement file — which is ADR-0012 and is what the `settled` state
and the clearing balance exist for.
