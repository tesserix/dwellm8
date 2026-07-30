# ADR-0021 — Demo sandbox: sessions, isolation and lifetime

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Platform, Demo
- **Issue**: [#162](https://github.com/tesserix/dwellm8/issues/162)
- **Related**: [ADR-0003](0003-tenancy-and-row-level-security.md) (the isolation a sandbox reuses, and the promise it never kept), [ADR-0006](0006-chart-of-accounts-and-posting-rules.md) (the immutability this relaxes, and how far), [ADR-0011](0011-payment-provider-adapter.md) (the adapter a demo may use), [ADR-0013](0013-kyc-data-handling.md) (why the token is hashed), [ADR-0015](0015-durable-workflow-standard.md) (the operations a demo may not run)

---

## Context

Anyone can start a demo with no account, so the sandbox is an unauthenticated,
write-capable surface exposed to the whole internet. Left undesigned it becomes one of
two things: a shared mess where visitors edit over each other, or an abuse and cost
problem.

There was already a promise waiting to be kept. `organisations.is_sandbox` has existed
since ADR-0003 carrying this comment:

> A sandbox organisation holds demonstration data (M19). Nothing in it may ever cause a
> side effect: no money moves, no message is sent.

Nothing enforced any part of that. The column was a boolean with a docstring.

And there was a conflict nobody had noticed. Every table in this schema carries a
`RESTRICTIVE ... FOR DELETE USING (false)` policy — twenty-one of them — because the
whole product is an append-only financial record. **An expired demo sandbox could
therefore never be cleaned up.** The story's own edge case, "an expired sandbox must leave
no orphaned records", was impossible against the schema as it stood.

---

## Decision

**One ephemeral organisation per session, keyed by a hashed opaque token with a sliding
window and a hard ceiling; the side-effect ban made structural; and deletability granted
to exactly one new role, scoped to sandboxes, because the two rules hold each other up.**

### 1. The two halves, and why neither is safe alone

This is the whole ADR:

> A demo is purgeable **because** nothing real may originate in one.
> Nothing real may originate in one **because** it is purgeable.

Weaken either and the other stops being true. A sandbox that could reach Razorpay would
hold real payments, and dropping it would destroy a real financial record. A sandbox that
could not be dropped would accumulate forever, which is the cost problem the story is
about.

`sandbox_purge_permitted(tenant_id)` is the first half in code and
`payments_sandbox_provider` is the second, and the ADR's job is to keep them in step.

### 2. Session identity without an account

An opaque 256-bit token on the device, **stored hashed and never in the clear** — the
same reasoning as ADR-0013's identifiers: a copy of the database would otherwise hand out
live sessions.

`sha256` rather than a slow hash, deliberately. This is a random 256-bit value, so there
is no dictionary to defend against and bcrypt would only make every request slower.

Not a JWT. A signed token cannot be revoked without a server-side record, and a demo
session is exactly the thing you need to revoke — for abuse, for cleanup, for a cap.
Having the record is the point, so the token may as well be opaque.

**Two expiries.** `expires_at` slides on use, so a visitor returning after several days
resumes rather than silently receiving a fresh sandbox with their work gone — the story's
edge case. `hard_expires_at` does not, because a sliding window with no ceiling is a
session that never expires, and the storage cost of one that never expires is unbounded.

### 3. Isolation, and the side-effect ban made structural

One sandbox organisation per session, `UNIQUE` on `demo_sessions.tenant_id`, never
shared. Two visitors editing the same demo is the failure that makes a sandbox worthless.

`demo_sessions_sandbox_only` refuses a session pointing at an organisation that is not a
sandbox — otherwise the token is a way to reach real data with no account at all, which
is the entire risk of an unauthenticated surface.

Then the promise from ADR-0003, kept:

- **`payments_sandbox_provider`** — a payment in a sandbox organisation may name only the
  `sandbox` adapter. Not "no payments": a demo of a rent platform has payments in it. The
  rule is that none of them reaches a real provider, so a visitor clicking through cannot
  create an order at Razorpay however the code above is wired.
- **`workflow_runs_sandbox_ban`** — no durable operation may originate in a sandbox at
  all. Every operation on ADR-0015's list moves money, files a document or pays a
  government gateway; there is none a demo may legitimately run.

Both are triggers rather than policies, because they must hold for a tenant session as
well as a platform one.

### 4. Deletability, and what it cost

Twenty-one `USING (false)` policies became `USING (sandbox_purge_permitted(tenant_id))`.
That predicate requires **both** that the row belongs to a sandbox organisation and that
the session is the purge job — and the purge job is a *third role*, `dwellm8_purge`, not
the platform role.

The third role matters and it was not the first design. This schema protects its history
with two locks: the `DELETE` privilege is revoked *and* a `RESTRICTIVE` policy refuses
the delete. Granting `DELETE` back to `dwellm8_platform` would have removed one of those
locks for onboarding, support and reporting alike. A dedicated role keeps both locks
intact everywhere except the one job that needs them open — and even there the policy
only reaches a sandbox.

**The purge job scopes itself to one organisation at a time**, exactly as a request does.
It is deliberately *not* platform-exempt, so visibility comes from ordinary tenancy and
deletability from the policy. Measured:

```
purge role, scoped to the sandbox      DELETE 1
purge role, scoped to a real org       DELETE 0   (it can see the row; the policy refuses)
platform role                          ERROR: permission denied for table properties
tenant session                         ERROR: permission denied for table properties
```

**Two assertions had to be relaxed, and this is the honest cost of the ADR.**

Assertion 7 said no ledger row may ever be deleted. It now says no ledger row *of a real
organisation* may ever be deleted, and the `DELETE` predicate may be
`sandbox_purge_permitted()` and nothing else. `UPDATE` stays absolute — there is no reason
to edit a posting even in a demo. Assertion 4 got the same treatment for the delegation
tables, where the grantee still cannot erase anything because the predicate requires a
purge session *and* a sandbox.

Both relaxations rest entirely on §3. If the side-effect ban is ever weakened, both must
go back to `false`, and that dependency is written into the assertions themselves.

Assertion 7 is what caught the change — ADR-0006 wrote it, and it failed the moment I
weakened what it was protecting. That is the guard working, and it is why the relaxation
is a considered edit with a paragraph rather than something that slid through.

**Assertion 16** is the new one, and it points the other way: every tenant-scoped table's
no-delete policy must permit the purge. Column-driven, so a table added by a future ADR is
covered before anybody writes a test for it — which matters because the failure is
invisible from the new table. It shows up months later as a demo sandbox that cannot be
cleaned up, as storage that only grows. It caught two tables during this ADR whose policy
was formatted differently and which the rewrite had missed.

### 5. Cost bounds

`demo_sessions_cap` refuses a new session beyond 500 live ones.

Rate limiting belongs at the edge and is not in this schema — but edge rate limiting fails
open under a distributed attack, and "the cost stays within the stated cap" is only true
if something states it and something enforces it. The cap is the floor beneath the edge,
and it fails closed.

It is a constant in the schema rather than a configuration value, deliberately: a number
an operator can raise under pressure at 3am is a number that gets raised. Changing it is a
schema change with a review.

Existing sessions are unaffected by a creation flood, which is the story's failure
scenario — the cap is on `INSERT`, and nothing about a rejected creation touches a live
session.

`origin_bucket` is coarse on purpose. A `/24` or a hashed prefix is enough to rate-limit
on, and storing the address would build a log of who visited a marketing site.

### 6. How the authorisation model sees a demo

The story asks that the sandbox be checked like everything else rather than bypassing it,
and today it is: a demo session is an ordinary tenant-scoped session on its own
organisation. `current_tenant_id()` is the sandbox, every policy applies unchanged, and
there is no demo-shaped hole anywhere.

That is the answer under the current model. ADR-0020 will introduce OpenFGA, and the
constraint it inherits is this one: a demo subject is a subject, not an exemption.

### 7. Conversion

Nothing carries over, and that follows from §3 rather than being a product decision. A
sandbox contains no real payment, no real document and no real verification — by
construction — so there is nothing that *could* carry. Conversion creates a fresh
organisation and the visitor starts with real data.

The alternative, migrating a sandbox organisation to a real one by clearing
`is_sandbox`, is rejected in D below.

### 8. What fails the build

- `internal/platform/tenancy/isolationtest` — the purge deleting from a sandbox and
  deleting nothing from a real organisation while being able to *see* it, the platform
  role and a tenant session both refused by the missing privilege, a sandbox payment
  naming a real provider refused, the sandbox adapter accepted, a real organisation
  unaffected, a durable operation refused in a sandbox, a demo session refusing to point
  at a real organisation, the token stored as a 32-byte hash, and one session per sandbox.

CI plants four failures and expects red.

---

## Alternatives considered

### A. A schema or database per sandbox — rejected, and it is the closest call

`CREATE SCHEMA demo_abc123`, seed it, and purge with `DROP SCHEMA ... CASCADE`. Perfect
isolation, no policy changes, no relaxed assertions, and cleanup is one statement.

Rejected because the demo would stop being the product. Every migration would have to run
against N live sandbox schemas, or a sandbox would be running an older schema than the
thing it is demonstrating — and a demo that diverges from the product is a demo that
misleads. Connection pooling across hundreds of schemas is its own problem.

It is the right answer if sandbox volume ever makes row-level purging slow, and the cost
of switching later is a purge job rewrite rather than a data model change.

### B. Purge by granting DELETE to the platform role — rejected

The obvious implementation, and it was the first one here.

Rejected because it removes one of this schema's two locks for every platform operation,
not just the purge. Onboarding, platform reporting and audited support sessions all use
that role, and none of them should be able to delete anything. A third role costs one
`CREATE ROLE` and keeps the property everywhere else.

### C. Never purge; let sandbox data accumulate — rejected

Zero schema changes, and honest about the append-only design.

Rejected because it fails the story's edge case outright and turns an unauthenticated
surface into unbounded storage growth. The cap in §5 would bound live sessions but not
their data, so the database would grow with every visitor forever.

### D. Convert a sandbox by clearing `is_sandbox` — rejected

It looks like the cheapest conversion path: the visitor keeps their demo data and it
becomes real.

Rejected because everything in that organisation was created under the side-effect ban.
The payments name the sandbox adapter and settled nothing, the ledger balances against
money that never moved, and no document was ever stamped. Clearing the flag would turn
fiction into a financial record — and would silently make the organisation
non-purgeable, so a mistake could never be undone.

### E. A signed token (JWT) instead of a stored hash — rejected

Stateless, no table, no lookup.

Rejected because the record is the point. A demo session must be revocable for abuse,
countable for the cap, and expirable for cleanup — all of which need server-side state,
and once the state exists a signed token buys nothing but a way to be inconsistent with
it.

### F. A configurable session cap — rejected

Normal practice, and it would let the cap be tuned without a deploy.

Rejected because the cap exists to bound cost under attack, and the moment it binds is the
moment somebody is under pressure to raise it. A constant makes raising it a change with a
review, which is exactly the friction wanted.

---

## Consequences

**What is now true.** `is_sandbox` means something: a demo cannot reach a payment
provider, cannot start a durable operation, and cannot be pointed at by a session unless
it really is a sandbox. An expired sandbox can be cleaned up, by one role, scoped to
sandboxes, with every other session keeping both locks. A returning visitor resumes.
Session creation is bounded by a number in the schema. The token is never stored in the
clear. And a table added by a future ADR is covered by assertion 16 before anybody thinks
about demos.

**What this costs.** Two immutability assertions are weaker than they were: the ledger's
and the delegation tables', both scoped to sandboxes and both dependent on the
side-effect ban staying true. That dependency is written down and asserted, and it is
still a coupling between two ADRs that a future change could break. `dwellm8_purge` is a
fourth database role to provision, rotate and keep out of application configuration —
and a role with `DELETE` on twenty-two tables is a role worth watching. The cap is a
constant, so raising it needs a deploy, which will be inconvenient exactly once. And the
side-effect ban is enforced for payments and workflows and *not* for notifications,
because there is no notification table yet — the promise says "no message is sent" and
that half is currently kept by nothing.

**What is not decided.** The seed template itself, and the cost per session it implies —
`template` is recorded so a change is visible, and nothing yet reads it. The cleanup job's
schedule and its own durability (it is a natural ADR-0015 operation, and it is not on the
list because it originates in no organisation). Edge rate limiting and bot protection,
which belong with the web topology in ADR-0018. Media and object-storage cleanup, which
the story names and which no table here reaches. And the notification half of the
side-effect ban, which lands with the notify module.
