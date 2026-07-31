# ADR-0029 — The resident scope: a renter is not a small organisation

- **Status**: Accepted
- **Date**: 2026-08-01
- **Deciders**: Platform, Product
- **Issues**: [#51](https://github.com/tesserix/dwellm8/issues/51)
- **Related**: [ADR-0001](0001-modular-monolith-api.md), [ADR-0003](0003-tenancy-and-row-level-security.md), [ADR-0006](0006-chart-of-accounts-and-posting-rules.md), [ADR-0011](0011-payment-provider-adapter.md), [ADR-0027](0027-identity-gip-tenants.md), [`docs/threat-model.md`](../threat-model.md)

---

## Context

Every boundary in this platform is drawn between organisations. ADR-0003 puts
`app.tenant_id` on the session and lets PostgreSQL filter; ADR-0005 opens a
scoped, revocable hole in it for a managing firm; ADR-0027 resolves a sign-in to
one organisation and refuses when the answer is several. That is the right
boundary for a landlord, a manager and a vendor, and it is enforced by row-level
security rather than by anybody remembering a `WHERE` clause.

A tenant does not fit it, in two separate ways.

**A renter scoped to their landlord's organisation reads every other tenant of
that landlord.** Their rent, their arrears, their move-out settlement, their
party id. This is a worse disclosure than the cross-organisation one the whole
schema is arranged to prevent, and it is invisible: every policy still reads
correctly, every query is scoped, and the screen renders. A management firm with
three hundred flats has three hundred tenants who can each read the other two
hundred and ninety-nine.

**A renter with two landlords has no single organisation.** ADR-0027's resolver
answers `409 Conflict` for a person with several memberships and requires them to
choose which they are acting as. That is right for a manager who is also a
landlord. For a tenant with a flat in Pune and another in Bengaluru it is
nonsense: the answer is both, they are not "acting as" anybody, and the two
landlords must never learn of each other.

There is a third problem underneath both. A renter's identity does not exist at
sign-up. Every other surface onboards forwards — somebody signs in and an
organisation is created for them — and `identity/store.KindFor` explicitly
refuses to create one for Live, because a tenant belongs to their landlord's
organisation. A tenant arrives backwards: their landlord types a mobile number
into a lease weeks before that person has ever seen this product, and the SMS
reminder is the first thing they encounter.

---

## Decision

**A second session setting, `app.resident_party_id`, narrows a request from "this
organisation" to "this renter". Every row-level-secured table is denied to a
resident session by default; nine are opened with an explicit, restrictive
narrowing. A renter with several landlords is several scoped reads, never one
query across them.**

### 1. The narrowing is the database's

`tenancy.Scoped` writes three settings on every transaction — tenant, grant and
resident — and writes all three unconditionally. The conditional version leaves
one request's renter in place for the next request that picks the connection up,
which fails towards disclosure and does so intermittently.

Every resident policy is `RESTRICTIVE` and every predicate begins `NOT
is_resident_session() OR …`, so an owner, a manager, a delegated firm and the
platform session are unaffected. This section of the schema can only narrow.

The alternative was a `WHERE party_id = $me` in each of the twenty-odd queries a
tenant surface issues. ADR-0003's argument applies unchanged and is stronger here:
the filter that would eventually be forgotten is the one separating two tenants of
the same landlord, and forgetting it produces no error.

### 2. Deny by default, opened by allowlist

A loop at the foot of the resident section puts a restrictive deny on every table
with row-level security that is not on a nine-name allowlist. It is
column-driven rather than a list for the reason assertions 6 and 16 already give:
a guard covering the tables its author had in mind decays with the next
migration, and here the decay is a renter reading a table nobody remembered.

Opened: `leases`, `lease_parties`, `rent_schedule`, `journal_entries`,
`ledger_postings`, `payments`, `units`, `properties`, `organisations`. Everything
else — mandates, KYC, ownership, the audit trail, settlement, workflows — is
closed, including tables this schema does not yet have.

Assertion 19 fails the bootstrap if any table has no opinion, and separately if
any resident policy is `PERMISSIVE`. A permissive one would be an `OR` against
the organisation policy, so instead of narrowing a renter it would widen everybody
else to the renter's rows.

### 3. A payment is the one thing a renter writes

`payments_resident_scope` constrains both halves: the payer must be them and the
lease must be theirs. `resident_holds_lease()` is false for a NULL lease, so a
renter's payment always names the tenancy it pays — a payment attached to nothing
would post against nothing and appear on no statement.

The collection itself goes through `money/service.Payments.Collect`, unchanged. A
tenant paying their own rent is the same act as a manager taking it, and giving it
a second code path would give it a second idea of idempotency, a second webhook
path and a second set of ways to be wrong.

### 4. Two organisations, one renter, never one query

`identity/store.Residencies` is the only query in this product that deliberately
spans organisations on a customer's behalf. It runs on the platform pool for the
reason ADR-0011 §5 gives for the webhook inbox — the request arrives knowing a
person and not an organisation — and it returns nothing but lease ids and
landlord names.

Everything after it is one scoped read per tenancy. That is more round trips and
it is the only safe shape: no result set in this product spans two landlords, so
there is no join that can leak one into the other's row.

### 5. The identity exists before the sign-in

`identity_principals` gains a partial unique index on `phone` where
`surface = 'live'`, and a Live principal is created when a landlord names a
mobile number on a lease — with `gip_uid` set to the reservation marker
`phone:+91…`, which no Google uid can collide with. The first sign-in *claims*
that row: the placeholder uid is replaced by the real one and the party id is
unchanged, which is what keeps six months of receipts theirs.

Without the index, the second landlord to enter the same number mints a second
party id and the renter signs in to find one of their two flats.

The claim is conditioned on the placeholder uid, so it can only ever take over a
reservation. A second person verifying a recycled number does not inherit the
first one's tenancies.

### 6. It is a surface, not a ninth module

`internal/surface/resident` composes identity, lease and money. It owns no table,
no rule and no domain. Putting the composition inside any of the three would make
that module depend on the other two, which is the coupling ADR-0001 exists to
prevent; a surface above all three depends downwards only.

Two arch tests hold the line: a surface reaches a module through its `service`
package and never its store, and a surface directory may not contain `domain/`,
`store/` or `events/`. The moment it needs one it has become a module and has to
argue for itself here.

### 7. A denied attempt is logged and discloses nothing

A lease that is not in the session's residency list is answered `404`, not `403`.
`403` confirms the tenancy exists, which is the whole of what somebody changing an
identifier in a URL is trying to learn.

The attempt is written to the application log with the party that made it, the
identifier requested and the path — not to `audit_events`. That is deliberate:
`audit_events.tenant_id` is `NOT NULL` by design, and a denied attempt belongs to
no organisation. Attributing it to one would put a false record in some
landlord's audit trail.

---

## Rejected alternatives

**Give each renter their own organisation.** It fits the existing model exactly
and is wrong in every direction: the lease, the money and the property belong to
the landlord's organisation, so either the tenant's organisation holds nothing
and is decorative, or the ledger has to be split across two — and a receivable
whose two sides live under different tenants cannot be summed by any policy in
this file.

**A delegation grant from the landlord to the tenant (ADR-0005).** Closer, and it
was the first design. A grant is scoped to a property and carries permissions,
which is nearly right. It fails on two counts: a grant's grantee is an
*organisation*, so this reintroduces the problem above; and grant scope is
property-granularity with a unit hop, which would let a tenant read every tenancy
on their own flat's history rather than their own.

**Filter in the application.** Rejected on ADR-0003's own argument. The specific
failure it invites is a new endpoint written six months from now by somebody who
scopes the organisation correctly and does not know the resident setting exists.
Deny-by-default means that endpoint returns nothing rather than everything.

**A signed link with no sign-in.** Tempting for the reminder flow: the SMS carries
a token, the page opens, no OTP. It was rejected because the link outlives the
reason for it — it sits in an SMS thread on a phone that gets sold, lent or
backed up, and it grants a person's whole payment history to whoever holds it. The
story asks for OTP and the story is right.

**Our own OTP.** Identity Platform already verifies a phone number by OTP, and
ADR-0027's verifier already trusts the result. A second OTP system would be a
second SMS bill, a second rate limiter, a second set of ways to be wrong, and a
credential the API would have to be taught to accept.

---

## Consequences

- One extra `set_config` per transaction, on every request in the product. It is
  a parameter on a statement already being issued.
- A renter's tenancy list is N+1 round trips for N landlords. N is one for almost
  everybody and two for the case this ADR is about.
- `lease_parties` and `rent_schedule` gained a plpgsql guard on their delegated
  read branch. The resident policy on `leases` reads `lease_parties`, and
  `lease_parties`' own policy read `leases`; the two recursed until PostgreSQL
  gave up. Measured: `stack depth limit exceeded (SQLSTATE 54001)` on the first
  tenant-view query. plpgsql is what makes the guard load-bearing — statements
  run in order, so the correlated subquery is never reached in a resident
  session, where the SQL planner is free to evaluate it first.
- The rate limiter gained a third bucket. The tenant surface's callers are people
  rather than organisations and send no organisation header, so `ByTenant`
  returns `""` for every one of their requests and would limit none of them.
- Two CI steps plant defects and require a red build: one removes the renter
  narrowing from `leases` and `lease_parties`, one drops four deny policies.
  Weakening only `resident_holds_lease` is *not* a usable planted defect — the
  policy on `lease_parties` catches it, which is the two locks working and would
  make the step pass while proving nothing.
