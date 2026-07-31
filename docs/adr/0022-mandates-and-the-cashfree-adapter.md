# ADR-0022 — Mandates: a standing authority is not a payment, and the seam that could not hold one

- **Status**: Accepted
- **Date**: 2026-07-31
- **Deciders**: Payments
- **Issues**: [#13](https://github.com/tesserix/dwellm8/issues/13) (the spike), [#205](https://github.com/tesserix/dwellm8/issues/205) (this amendment), [#206](https://github.com/tesserix/dwellm8/issues/206) (the adapter)
- **Amends**: [ADR-0011](0011-payment-provider-adapter.md) — which is not superseded. The seam it argued for is right; it was incomplete, and one part of it was wrong in a way only a second provider could reveal.
- **Related**: [ADR-0003](0003-tenancy-and-row-level-security.md), [ADR-0007](0007-money-representation-and-rounding.md), [ADR-0009](0009-property-block-unit-model.md), [ADR-0012](0012-settlement-reconciliation-and-drift.md), [ADR-0015](0015-durable-workflow-standard.md), [`docs/payment-rails.md`](../payment-rails.md)

---

## Context

ADR-0011 ended with a list of what it had not decided, and the first item was the
mandate lifecycle — "the only reason `upi_autopay` currently looks like an
ordinary method". The #13 spike then established which rails carry Indian rent at
which amounts, and picked Cashfree as the head of the chain because one product
spans UPI Autopay, eNACH and physical NACH.

Writing that adapter found something the first one could not have: **the
`Adapter` interface cannot express Cashfree's webhook verification at all.**
Razorpay signs the raw body and hex-encodes. Cashfree signs
`x-webhook-timestamp + rawBody` and base64-encodes. `VerifyWebhook(payload
[]byte, signature string)` has nowhere to put the timestamp, and the timestamp is
not recoverable from the body, so a Cashfree adapter written against that
interface could verify nothing. Not a difficult integration — an impossible one.

That is the ordinary fate of a seam validated against one implementation. The
abstraction was drawn around Razorpay's shape while sincerely believing it was
drawn around "a payment provider", and it took a second provider to show which
was which.

The second half is the mandate itself. A payment and a standing authority
disagree about time: a payment is one attempt that resolves in minutes and never
moves backwards, and an authority lives for the length of a tenancy, is paused
and resumed on purpose, and produces many payments. Modelling the second as a
method on the first was defensible while nothing named a mandate; it stops being
defensible the moment something does.

---

## Decision

**A mandate is its own aggregate with its own lifecycle; mandates are a second
interface rather than five more methods on `Adapter`; `VerifyWebhook` takes the
whole delivery; and the rail-selection rule is executable Go over a rule table
whose numbers are data.**

### 1. The mandate aggregate

`money/domain/mandate` holds the authority. Three rails — `upi_autopay`,
`enach`, `nach_physical` — and seven states:

```
created → pending → active ⇄ paused
              ↘         ↘        ↘
          rejected    revoked   expired
```

Two things about it are deliberate and would be wrong if copied blindly from
ADR-0011.

**It is not forward-only.** Pausing and resuming is a product feature: a tenant
on a payment holiday must not have to re-authorise an authority nobody revoked.
`active ⇄ paused` is a cycle, on purpose, and the schema function permits it
while refusing `revoked → active`. Everything that carries correctness survives —
terminal states absorb, `from == to` is a permitted no-op so a redelivered
webhook needs no counter, and `ApplyConfirmed` is the only writer of `Status`.

**`rejected` and `expired` are different endings.** An owner asking why autopay
never started needs the difference between "your tenant's bank declined it" and
"your tenant never answered". The rails report them differently and so do we.

`unit_id` is `NOT NULL`, stricter than `payments`. An authority over a whole
property is not one any tenant gave, and the ADR-0009 assertion that requires
`is_delegated_unit()` in the policy of any unit-identifying table applies here
without being asked to.

Debits stay ordinary `collect.Payment` rows that name their `mandate_id`, so the
payment state machine, the idempotency index and the webhook contract are
reused rather than reimplemented. `collect.Method` gains `nach_debit`: a debit
under a NACH authority is neither a UPI method nor an offline one and previously
had nowhere to go.

### 2. `MandateAdapter`, a second interface

`Register / ConfirmMandate / Debit / Revoke / SupportsRail`, implemented only by
adapters that have mandates.

Widening `Adapter` was the obvious move and it is worse: `Offline` would have to
implement five methods whose only correct body is a panic. Cash does not sign a
standing authority. An interface nobody can wrongly satisfy is worth more than
one that is convenient to look up, and `Registry.MandateFor` skips offline by
interface *and* explicitly, because an offline "mandate" would be a standing
authority nobody granted.

`Registry.MandateBy(name)` resolves an existing authority by its own provider,
never by the chain — the same rule as payments, for a sharper reason. A mandate
lives for years, the chain will change under it, and asking today's head of the
chain about an authority it never registered returns "no such mandate", which is
indistinguishable from "the tenant cancelled".

### 3. The delivery, not the payload

`VerifyWebhook(w Webhook)`, where `Webhook` carries `Body`, `Signature` and
`Timestamp` — the timestamp verbatim and unparsed, because Cashfree signs it as
a string and formatting it back through a `time.Time` would change the bytes
that get hashed.

`Body` is the exact bytes read off the wire. Both providers sign the raw payload,
and a handler that decodes before verifying breaks both — silently, and only
against real traffic, because a test that builds the body and signs the same
bytes never notices. The test that catches it round-trips a body through
`encoding/json` and asserts the signature stops verifying.

Carrying the timestamp buys the thing the interface could not previously have: a
**replay window**. A signature over the body alone is valid forever, so a
captured delivery replays at any point in the future and verifies. `ErrStaleDelivery`
is returned rather than folded into "not verified", because a bad signature and a
four-hour-old genuine delivery are parked for different reasons and only one of
them is somebody attacking us.

### 4. The rail rule is code; the numbers are not

`mandate.Select(rent, caps, bank)` returns the rail, whether it is genuinely
unattended, the fallback method when no authority is possible, and the
rule-table row that decided it. Every threshold is an argument. There is no
default anywhere in the package, because the moment a ceiling has a default in
code somebody relies on it and the rule table stops being the source of truth.

The rule that is easy to get wrong is the middle one, and the test states it as
prose: UPI Autopay reaches ₹1,00,000, so the tempting answer for a ₹45,000 rent
is "UPI, it fits". It does fit, and every debit then requires the tenant's UPI
PIN within thirty minutes — the onboarding cost of a mandate paired with the
monthly failure rate of a manual payment. Above the AFA-free ceiling the rule
returns eNACH.

Rent is not in the AFA-exempt category set (insurance premiums, MF SIPs,
credit-card bills, the lending/investment MCCs), which is why the ordinary
₹15,000 ceiling is ours and the raised one is not.

### 5. Cashfree, and what a real adapter costs

Everything provider-specific is in one file: vocabulary translation, header
names, the signature scheme, the version pin. Four properties of it are worth
recording because each one is a decision somebody will otherwise re-litigate.

- **The API version is pinned and required.** Cashfree versions by date in a
  header. An unpinned client changes behaviour on their release schedule rather
  than ours, so startup fails without `x-api-version` — as it does without a base
  URL, credentials, or a webhook secret.
- **Their status vocabulary is wider than ours and translation refuses to
  guess.** An unknown status is an error, and the caller parks it. `USER_DROPPED`
  maps to `expired` rather than `failed`: nothing declined, and an owner reading
  "failed" would chase a tenant whose bank never saw a request.
  `BANK_APPROVAL_PENDING` maps to `pending`, because physical NACH sits there for
  a working week and a sweep treating it as stuck would cancel authorities that
  were about to activate.
- **`Revoke` is idempotent.** Revoking a revoked mandate succeeds. The
  alternative is a tenant's cancellation failing on a retry and reading, to them,
  as a refusal to let them cancel.
- **The order id is our idempotency key**, so Cashfree's uniqueness and ours
  become the same fact and a retry cannot create a second order at their end
  either. Their `x-idempotency-key` is sent where they honour it; the unique
  index on `(tenant_id, idempotency_key)` remains the guarantee we rely on,
  because Razorpay offers no general equivalent.

### 6. The float the arch guard caught

The first draft of the rupee boundary parsed Cashfree's `"12000.00"` into a
`float64`. ADR-0007's arch test rejected the build, naming five positions:

```
money/provider/cashfree.go:217:17: names float64 — money is int64 paise, and
    ADR-0007 §2 permits no float in this module
money/provider/cashfree.go:548:37: is the float literal 0.5 — …
```

That is the guard doing exactly what it was written for, on the first occasion
anybody had a reason to argue with it — and the reason was good, which is the
point. Parsing a decimal string is the obvious way to read a provider's amount,
and a float is how a rounding error gets from an aggregator into a ledger. Both
directions are now string and integer arithmetic; a third decimal place is
rounded half away from zero once, here, where it is tested, rather than wherever
the value is first added to something.

### 7. One live authority per unit

`mandates_one_active_per_unit_idx`, a partial unique index on
`(tenant_id, unit_id) WHERE status = 'active'`.

Two active mandates on one flat is a tenant debited twice on the first of the
month, and it is the kind of duplicate that looks correct from every screen: both
authorities are real, both were authorised by somebody, and nothing but this
index says the second must not exist. Partial, so a revoked authority does not
block the replacement a tenant changing banks needs.

### 8. What fails the build

- `money/domain/mandate` — the lifecycle table stated explicitly, terminal
  absorption, `ActivatedAt` set once across a pause, the ceiling check, the
  validation set, and the webhook rule including the assertion that no
  disposition moves a status.
- `money/domain/mandate` rails — the spike's two rents, both boundaries, every
  fallback being non-offline, physical NACH catching the bank that does nothing
  else, an impossible rule row refused, and the same rent routed differently by
  two rule rows so "a regulatory change is a row, not a release" is a test.
- `money/provider` — the Cashfree signature against eight wrong deliveries
  including a correctly hex-encoded one, the re-marshalled-body case, the replay
  window, the startup refusals, the version pin on every request, the
  vocabularies refusing to guess, idempotent revoke, transport failure versus
  client error, and the rupee boundary at its edges.
- `money/store` — the drift check between Go's lifecycle and
  `mandate_transition_allowed()` over all 49 ordered pairs, `paused → active`
  asserted against the database specifically, the vocabularies, and the two
  indexes that are rules.
- `platform/tenancy/isolationtest` — ADR-0003's five-part contract on `mandates`,
  and the one-live-authority index from a tenant session.

Measured, with the self-transition and the resume dropped from the schema
function: 20 pairs disagree and each is named.

---

## Alternatives considered

### A. Widening `Adapter` with the mandate methods — rejected

Fewer types, one place to look. Rejected because `Offline` would satisfy it by
writing five panics, and a panic in an interface implementation is a runtime
assertion standing in for a compile-time one. See §2.

### B. Keeping `VerifyWebhook(payload, signature)` and passing the timestamp out of band — rejected

A field on the adapter, or a context value. Rejected because it makes the
signature scheme a property of the adapter's mutable state rather than of the
delivery, and two deliveries in flight concurrently would race on it. The
delivery is the thing being verified; it should be the argument.

### C. A mandate as a payment with `status = 'authorised'` held open — rejected

Tempting, because an authorisation is also a thing that exists before money
moves. Rejected on lifetime and cardinality: an authorisation resolves in
minutes and produces one payment, an authority lives for years and produces
sixty. Every query over payments would then have to exclude the rows that are not
payments.

### D. A `mandate_events` table separate from `payment_events` — rejected

Symmetrical, and it doubles the inbox. The deduplication index, the park
vocabulary, the platform-only write policy and the nullable-tenant argument are
all identical, so a second table is a second copy of ADR-0011 §5 that will drift.
One inbox, a nullable `mandate_id` beside the nullable `payment_id`, and a CHECK
that a delivery names at most one of them.

### E. Hard-coding the AFA-free ceiling as a constant with a comment — rejected

`const AFAFreeCeiling = 1_500_000 // NPCI, review annually`. Rejected because the
comment is the part that decays. #13's second acceptance criterion is explicitly
that a regulatory change requires no code change, and a constant fails it by
construction.

### F. Falling back to UPI Autopay above the AFA-free ceiling instead of eNACH — rejected

It works, and it is what an integration written from the provider's feature
matrix would do. Rejected in §4: it has a mandate's onboarding cost and a manual
payment's failure rate, and it lets us describe as "autopay" something that asks
the tenant for a PIN every month.

---

## Consequences

**What is now true.** A standing authority is a first-class object with a
lifecycle the database enforces, and a tenant cannot hold two live ones on the
same flat. Cashfree can verify its webhooks, which it could not before. A
delivery cannot be replayed indefinitely. The rail rule is executable and its
numbers are data, so #13's two rents route correctly and a regulator moving the
ceiling is a row. Nothing in `money/domain` names Cashfree, and `grep -ril
cashfree internal/money/domain` returning nothing is the check.

**What this costs.** A second interface means `MandateFor` can fail at run time
in a way `For` could not, though only through configuration. The lifecycle now
exists in three places — Go, the schema function, and the ADR — and only the
first two are checked against each other. The rail rule needs a rule table that
does not exist yet: until it does, callers pass caps they got from somewhere, and
"somewhere" is the gap #74 has to close. And Cashfree's sandbox is not exercised
by any of this; every HTTP test runs against a recorded shape, so the first real
call will still find something.

**What is not decided.** Razorpay's adapter, which is now a file rather than a
question. Mandate amendment — whether Cashfree can raise a live authority's
ceiling, or whether an escalation means re-registration, which is the open
question in [`payment-rails.md`](../payment-rails.md) §9 and needs their
solutions team rather than their docs. The debit scheduler and the non-peak
windows (#74). Pre-debit notification records (#73). And the AFA question that
outranks all of them: whether the ₹15,000 threshold binds on the mandate ceiling
or the debit amount, which decides whether escalation headroom is free.
