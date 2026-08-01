# ADR-0030 — The stay model: a guest is not a tenant, and one flat cannot be twice occupied

- **Status**: Accepted
- **Date**: 2026-08-01
- **Deciders**: Platform, Product, Compliance
- **Issues**: [#233](https://github.com/tesserix/dwellm8/issues/233)
- **Related**: [ADR-0009](0009-property-block-unit-model.md), [ADR-0010](0010-lease-lifecycle-state-machine.md), [ADR-0019](0019-public-listing-surface.md), [`docs/india-property-compliance.md`](../india-property-compliance.md) §4 and §6

---

## Context

Dwellm8 lets flats. A homestay sells nights, and the pull to reuse `leases` for
it is strong and wrong: a stay has a unit, a start, an end and money, so a
two-night booking looks like a very short tenancy and the schema would accept it.

`india-property-compliance.md` §4 already refuses that reading, and its reason is
not a modelling preference:

> A PG resident and a short-stay guest are **licensees or guests, not tenants**:
> they do not acquire tenancy protection, the lease lifecycle does not apply to
> them, and a platform that models them as tenants has both over-promised rights
> to the occupier and mis-stated the owner's position.

The consequences are concrete. A tenancy carries TDS under 194-I or 194-IB; a
stay carries none, because it is not rent — it is a supply of accommodation
service, taxed under GST by tariff band. A tenancy has a notice period, a lock-in
and a rent schedule; a stay has a cancellation policy and a nightly price. A
tenancy at twelve months attracts compulsory registration of the instrument; a
booking is not an instrument at all. Every one of those would be wrong, silently,
on a `leases` row with a two-night validity.

**And there is a problem underneath, which is the reason this ADR exists rather
than a one-line rule.** ADR-0010's promise — one flat, one tenancy — is a GiST
exclusion constraint on `leases`:

```sql
EXCLUDE USING gist (tenant_id WITH =, unit_id WITH =, validity WITH &&)
    WHERE (state IN ('active', 'in_notice', 'renewed', 'terminated', 'settled'))
```

A second constraint of the same shape on a `bookings` table would keep bookings
from colliding with bookings. **It would not stop a flat being let and booked
over the same nights**, because a PostgreSQL exclusion constraint cannot span two
tables. The failure is not theoretical and not rare: the first owner to list
their tenanted flat for a weekend produces it, and both records look correct in
isolation.

---

## Decision

**A booking is its own aggregate with its own state machine, and occupancy is a
third table both of them write to, carrying the one exclusion constraint that
makes "one flat, one occupier" true across the product.**

### 1. A guest is a guest

`bookings` has no TDS facts, no rent schedule, no notice period, no lock-in and
no renewal. It has nights, a nightly price, a cancellation policy and a guest.
The occupier's legal status is a column value nowhere — it is the table they are
in.

### 2. The state machine, and the one place it differs from a lease

```
held ──▶ confirmed ──▶ in_stay ──▶ completed
 │           │             │
 ▼           ▼             ▼
expired   cancelled     cancelled        (and confirmed ──▶ no_show)
```

`held` is the state ADR-0010 has no equivalent of, and the difference is the
whole point. ADR-0010 deliberately excludes `draft` and `pending_signature` from
the no-double-let constraint, because **two competing offers on one flat are
legitimate** — an owner preparing a renewal while the current tenancy runs is
normal.

That argument does not survive the move to nights. Two guests holding the same
weekend are not competing offers; they are two people about to pay for one bed.
So **a hold occupies the calendar**, and it expires on a timer rather than on
somebody's decision. The occupying states are `held`, `confirmed`, `in_stay`,
`completed` and `no_show` — a no-show still consumed the night, and the owner is
still owed for it.

`expired` and `cancelled` release the nights. `cancelled` is not a delete: the
money that moved is a reversing entry with a reason code (ADR-0006 §3), and the
booking stays readable, for the same reason a terminated lease does.

### 3. One occupancy table, one constraint

```sql
CREATE TABLE occupancy (
    tenant_id   uuid NOT NULL,
    unit_id     uuid NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('lease', 'booking')),
    source_id   uuid NOT NULL,          -- the lease or the booking
    validity    daterange NOT NULL,
    EXCLUDE USING gist (tenant_id WITH =, unit_id WITH =, validity WITH &&)
);
```

A lease that becomes a tenancy inserts a row here in the same transaction; a
booking that reaches `held` does the same. The exclusion is then a single
constraint over a single table, and it is the database — not a handler, and not a
trigger — that refuses the second occupier.

**Why not a trigger on each table checking the other.** Because it reads the
table it protects, so it is racy under exactly the concurrency it exists for.
This schema's assertion 14 already makes that argument about effective dating,
and it applies unchanged: two people booking the same weekend in the same
millisecond is the case, not the edge case.

**What it costs**, stated rather than discovered: `leases_no_double_let` moves.
The constraint that has enforced ADR-0010's central promise since it was written
is replaced by a row in `occupancy` and the exclusion there. That is a migration
on a live table with a real guarantee attached, it must be done with the
constraint held throughout, and the lease isolation contract has to be extended
to prove the promise still holds — not merely that the new table works.

`occupancy` is derived state with an enforcing constraint, which is a shape this
platform otherwise avoids (ADR-0006 §5: balances are derived, never stored). The
exception is argued rather than assumed: a balance is a sum that can be recomputed
from immutable rows, and a range exclusion is not a sum — PostgreSQL can only
enforce it over rows that exist. The mitigation is that the row is written in the
same transaction as its source and never independently, and that a reconciliation
check can prove the two agree.

### 4. Nightly is the primitive; monthly is a longer range

A monthly stay is a booking with a longer range and a different pricing rule, not
a lease. Serviced apartments and co-living are sold as accommodation, and the
occupier is a licensee.

**But there is a ceiling, and it is legal rather than technical.** A long enough
stay stops being a licence: §4 puts compulsory registration of the instrument at
twelve months and state-scoped, and an occupier in continuous possession acquires
protections a booking does not describe. So `bookings` carries a **maximum
nights**, sourced from the statutory rule tables (ADR-0023) and state-scoped, and
a stay that would exceed it is refused with the reason — it must be created as a
lease.

The number is not hard-coded here. It is a rule with an owner and a review date,
because it differs by state and it will change.

### 5. Listings are a sibling table, not more columns

`listings` (ADR-0019) is rent-shaped: `rent_minor`, `deposit_minor`, and states
ending in `let`. A homestay listing prices per night, varies by season, has a
minimum stay and needs a calendar. Bolting nullable nightly columns onto
`listings` would make every existing query carry a "which kind is this" branch,
and the public read policy — the one hole in this schema that does not fail
closed — would have to be reasoned about twice.

### 6. Publication is gated on evidence

§6 of the compliance document states the condition on which short-stay may ship
at all:

> If it ships, the listing flow must capture society permission and municipal
> consent as blocking evidence, not as a checkbox, because the platform is the
> party that made the letting easy.

So a homestay listing cannot reach `live` without society permission, municipal
consent and state tourism registration recorded as artefacts, and without the
owner's KYC and ownership proof. The gate is a database constraint on the
publication transition, not a validation in a handler — the same treatment
ADR-0024 gives the tax facts a tenancy cannot start without.

---

## Rejected alternatives

**Reuse `leases` with an `instrument_kind` column.** §4 leaves this open —
"their own state machine or an explicit decision that they reuse it with a
different instrument type" — so it was considered properly. It fails on the
lifecycle rather than on the data: the lease state machine has `pending_signature`,
`in_notice`, `renewed` and `settled`, and a booking has none of them. Every one
would become "not applicable when kind = booking", which is a state machine with
a second, undocumented state machine inside it. The TDS gate is the clincher: a
tenancy cannot activate without ADR-0024's two facts, and a guest has no
landlord-residency question to answer.

**Two independent exclusion constraints, one per table.** The version that looks
finished and is not. It stops booking-versus-booking and lease-versus-lease, and
leaves the cross case — which is the one an owner produces on their first
weekend.

**Denormalise bookings into `leases` for the constraint only.** Keeps one
constraint and puts rows in `leases` that are not leases, which every existing
lease query then has to exclude. The first one that forgets reports a two-night
booking as a tenancy, and the reports that read `leases` are the owner statements.

**Let the application check.** Rejected on ADR-0003's argument, which is the same
argument every time: the check that would be forgotten is the one that matters,
and its absence is invisible until two families have keys.

---

## Consequences

- `leases_no_double_let` migrates to `occupancy`. Live table, real guarantee,
  and the isolation contract must prove the promise across the move rather than
  prove the new table works.
- A hold occupies the calendar and therefore needs a reaper. An expiry that
  depends on a job that stopped running is a calendar that fills up with nothing.
  ADR-0028's periodic-jobs argument applies, and the reaper must be idempotent.
- The maximum-stay ceiling becomes a statutory rule row per state, with an owner
  and a review date, and a lease is what a longer stay must be created as.
- A guest is a party in the ledger with no tenancy behind them, so the resident
  scope (ADR-0029) needs a sibling: a guest reads their own booking and nothing
  else. The deny-by-default loop already closes every table to them; opening
  `bookings` is a deliberate act with its own policy.
- No TDS path is added, and that is a decision to record rather than an omission
  to notice later: a stay is not rent, and §194-O — whether the platform itself
  becomes an e-commerce operator — is a different question, open, and tracked on
  the issue.
