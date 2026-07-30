# ADR-0009 — Property, block and unit canonical model

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Platform
- **Issue**: [#10](https://github.com/tesserix/dwellm8/issues/10)
- **Related**: [ADR-0005](0005-owner-delegation-grants.md) (the scope this makes real), [ADR-0003](0003-tenancy-and-row-level-security.md), [ADR-0001](0001-modular-monolith-api.md), ADR-0007 (money, later), ADR-0020 (OpenFGA, later)

---

## Context

One model has to hold a standalone house, a four-flat building, a 240-flat
society tower with two wings, a shop, and a parking slot in the basement. Get the
hierarchy wrong and every module downstream carries a special case for the shape
it did not expect.

That is the stated problem. The one that turned out to matter more is a debt
ADR-0005 left behind. Scope was a closed vocabulary — `portfolio | property |
unit` — validated by nothing, because there were no properties and no units to
validate against. A `unit` scope row was insertable and meant nothing: it matched
no property, so it granted no access, and no test could tell the difference
between "correctly denied" and "silently broken". ADR-0005 said so in its
follow-up list and named this ADR.

So this is two decisions in one document: what the tree looks like, and what a
grant over part of it actually reaches. The second is where the surprises were.

Everything below was measured against PostgreSQL. Three things that looked right
in the design were wrong when run, and they are recorded as they happened.

---

## Decision

**One tree — property → block (optional) → unit → ancillary unit — owned by
whoever holds it as `tenant_id`. Ancillaries are units with a parent, not a
second table. Grant scope is validated against the grantor's own tree and
resolved to a property when it is written, and unit-bearing tables are judged at
unit granularity by a check that reads nothing.**

### 1. The tree

```sql
properties (id, tenant_id, code, name, kind,
            address_line1, address_line2, locality, city, district,
            state_code, pin, latitude, longitude, geocoded_at, geocode_source,
            municipal_tax_id, rera_id, society_registration_no, state)

blocks (id, tenant_id, property_id, code, name, floors)

units (id, tenant_id, property_id, block_id, parent_unit_id, unit_kind, code,
       floor, carpet_area_sqft, builtup_area_sqft, share_certificate_no,
       occupancy, electricity_consumer_no, water_connection_no, state)
```

`blocks` is optional, so a standalone house does not get a synthetic "Block A" —
inventing one is exactly the special-casing this model exists to avoid. A block
carries `floors` and nothing else of substance; it is a grouping, not a party to
anything.

`unit_kind ∈ flat | floor | room | shop | office | desk | parking | storage`
covers the shapes in the issue plus co-living (`room`) and managed workspace
(`desk`). `code` is unique per property, which is issue #10's validation
scenario, and `citext`, so `A-101` and `a-101` are the same flat rather than two.

Addressing is Indian and administrative rather than free-form: locality, city,
district, state, PIN, in the order every statutory form asks for them.
`state_code` is the ISO 3166-2:IN subdivision code and is deliberately *not*
called `state`, because `state` is this schema's word for a lifecycle. The
collision is unfortunate; the naming is the mitigation, and the GST numeric state
code is a lookup for ADR-0007 rather than a second column here.

Two checks are arithmetic rather than policy: `builtup_area_sqft >=
carpet_area_sqft`, because built-up includes carpet by definition and dues
computed from the wrong one are off by the walls; and a PIN may not begin with
zero, because no Indian PIN does, which makes a leading zero a transcription
error. `floor` is signed — a `> 0` check there would be a bug reported by every
tower with parking underneath it.

### 2. Ancillaries are units with a parent

A parking slot allotted to flat 1204 is a unit whose `parent_unit_id` is that
flat. An unallotted slot has none. This keeps one table, one id space, and one
answer to "what comes with this flat".

Three rules hold it in shape, and all three are declarative:

```sql
CHECK (parent_unit_id IS NULL OR unit_kind IN ('parking', 'storage'))
FOREIGN KEY (parent_unit_id, property_id)       REFERENCES units (id, property_id)
FOREIGN KEY (parent_unit_id, parent_is_ancillary) REFERENCES units (id, is_ancillary)
```

The last one is the odd-looking one. `parent_is_ancillary` is a generated column
that is always `false`, and `is_ancillary` is generated from `unit_kind`. A
foreign key from the constant-false column to `is_ancillary` therefore requires
the parent's `is_ancillary` to be false — so a slot cannot be parked on another
slot, without a trigger that could be dropped without the schema noticing.
Measured: `insert or update on table "units" violates foreign key constraint
"units_parent_primary_fkey"`.

What this model does *not* do is keep history: reassigning a slot is an `UPDATE`,
and who parked where last year is gone. That is a real limitation, chosen over
an effective-dated allotment table, and §Alternatives says why.

### 3. Tenancy is who holds the tree, not who owns the flat

`tenant_id` on all three tables is the organisation that holds the tree. For a
landlord's own flats that is the landlord. For a society it is the society: the
building, its wings and all 240 units live in the society's tenant, and an
individual flat owner is a separate organisation that reaches their flat through
a grant the society issues.

Ownership is therefore not tenancy, and this ADR does not model it. There is a
`share_certificate_no` on the unit and nothing that records a transfer on sale.

Tenant coherence inside the tree is enforced, not assumed. Every parent reference
is a composite foreign key that carries `tenant_id`:

```sql
CONSTRAINT units_property_fkey FOREIGN KEY (property_id, tenant_id)
    REFERENCES properties (id, tenant_id)
```

A plain reference to `properties(id)` would let a session hang a unit off another
organisation's property while keeping its own `tenant_id` on the row — and every
policy in the schema would then read that row as its own. Measured with the
composite key in place: `insert or update on table "units" violates foreign key
constraint "units_property_fkey"`.

### 4. What a grant over part of the tree reaches

This is the part that changed the delegation code, and the part worth reading
twice.

**Scope rows are validated when written, and resolved then too.** A trigger
checks that `scope_id` names a property or unit *belonging to the grantor*, and
stamps `scope_property_id` with the property the scope resolves to. It is not a
foreign key: `scope_id` is polymorphic, and the check that matters — that the
target is the grantor's — is one a foreign key cannot express. An owner scoping a
grant to a building they do not own is refused, and refused identically to
naming a row that does not exist, which is the correct answer to both.

The trigger is `SECURITY INVOKER`, so the lookup runs under the writer's own
row-level security. The grantor sees their own property and the scope is
accepted; anyone else sees nothing and is refused.

**`is_delegated()` no longer reads the tree at all.** Because the trigger has
already resolved a unit scope to its property, the property-level check is a
scope-table lookup:

```sql
AND (s.scope_kind = 'portfolio' OR s.scope_property_id = row_property)
```

The first design resolved the unit inside the function instead, with a subquery
against `units`. That version does not work, and the way it fails is worth
recording: the `units` policy calls the check, so the check reading `units` is a
policy consulting the table it governs. Measured — `stack depth limit exceeded
... CONTEXT: SQL function "is_delegated_unit_reading" during inlining`. Notably
*not* the `infinite recursion detected in policy` message PostgreSQL produces
when a policy names its own table directly: inlining a `STABLE` function gets
there first, and the error points at the function rather than at the policy that
is the cause. Anyone debugging that from the message alone would look in the
wrong place.

**A unit scope satisfies a property-scoped row.** A firm managing flat 1204 can
read the property and its blocks, because you cannot manage a flat without seeing
the tower it is in. This is a widening and should be read as one: at property
granularity, one granted unit reaches everything property-scoped in that
property. It stops at the property — the owner's other buildings stay invisible.

**Which is exactly why a unit-bearing table must be judged at unit
granularity.** `units`'s own policy uses a second check whose every argument
comes from the row:

```sql
is_delegated_unit(tenant_id, property_id, id, parent_unit_id, 'property.read')
```

It matches a portfolio scope, this property, this unit, or this unit's parent —
the last being the ancillary hop, so the slot allotted to a granted flat comes
with the flat. Passing `NULL` for the parent loses the hop and fails closed: the
row becomes unreachable, never over-reachable.

The alternative — satisfying assertion 5 with `is_delegated(tenant_id,
property_id, …)` and calling it done — is a defect that passes every assertion
ADR-0005 could write. Measured, with a mandate over flats 101 and 102 of a
five-unit property: `visible units [101 102 103 P-1 P-2], want [101 102 P-1]`. A
one-unit mandate reading the whole tower, and nothing about the request looking
wrong. CI plants precisely that and requires the contract to go red.

The six grant-level conditions from ADR-0005 §3 now live in one function,
`current_active_grant()`, which both scope checks call. Two copies of six
conditions is how one of them quietly stops checking who the grantee is.

### 5. Nothing in the tree can be deleted

The tree is the spine every lease, due, ticket and ledger entry hangs from. A
deleted unit orphans money; a deleted property orphans a grant scope that has no
foreign key to protect it. So `DELETE` is refused twice — the privilege is
revoked from `dwellm8_app`, and a `RESTRICTIVE` policy denies it — and correction
is `state = 'inactive'`. Measured: `permission denied for table units`, the
privilege lock firing before the policy is reached.

The table owner remains the escape hatch: a DBA at a `psql` prompt can still
delete, deliberately, and that is the only path.

### 6. The contract, and what fails the build

`isolationtest.RunPropertyScope` asserts the whole of §4 against a real
PostgreSQL: what a unit-scoped mandate sees (an exact set of codes, not a count —
a count of three passes when the three are the wrong three), that it sees the
building, that it cannot write what it can read, that a scope naming another
organisation's property is refused, that no unit can be deleted by either party,
that revocation closes the window, and that a duplicate code inside one property
is rejected while the same code in a sibling property is not.

`isolationtest.Run` — ADR-0003's five-part contract — now runs over `properties`
and `units` too. The delegated branch widens what a session can see, so the base
property that A cannot see B's rows has to be re-asserted rather than assumed to
have survived.

Two bootstrap assertions were added and one extended. Assertion 5 (a table with
`property_id` must pass it to a delegated check) now accepts
`is_delegated_unit()` as well, and it bit on `blocks` on first run, as ADR-0005
predicted it would. Assertion 6 requires any table identifying a unit — plus
`units` itself, whose unit column is `id` and which no column scan would find —
to use `is_delegated_unit()`.

Testing an assertion needed a detour worth writing down: replaying the schema
file cannot demonstrate that an assertion bites, because the replay re-creates
the policy it is about to judge and repairs the defect first. CI extracts the
assertion block and runs it alone against a schema broken underneath it.

`RunDelegated` — ADR-0005's per-table shape, where a firm writes a row into the
grantor's tenant — is deliberately not used for the tree. A firm under a scoped
mandate does not create the owner's buildings; `audit_events` remains its only
caller until a module lands a table a firm genuinely writes rows into, which
maintenance will.

---

## Alternatives considered

### A. Units owned by the flat owner, the society holding only the shell — rejected

`units.tenant_id` is the individual owner; `properties` and `blocks` belong to
the society.

- **For**: an owner's own flat and ledger need no grant at all, which is the
  common case for the owner-facing app.
- **Against**: one property then contains units belonging to hundreds of
  organisations, and every society-wide operation — dues, a notice, a general
  body resolution — becomes a cross-tenant read requiring 240 grants that no
  society will maintain. The composite foreign keys in §3 could not exist, so
  tenant coherence inside a property would be unenforceable.
- **Why rejected**: it optimises the case that a grant already handles and breaks
  the case that has no other mechanism.

### B. A `unit_ownership` table alongside society tenancy — deferred, not rejected

Tenancy as decided, plus an effective-dated row recording which organisation owns
which unit, with the share certificate and the transfer date.

- **For**: it is the honest model of a society, it survives a sale, and it makes
  "who owned this flat in March" answerable — which is the question a dues
  dispute turns on.
- **Against**: nothing yet reads it. Ownership currently affects who receives
  money, which does not exist until ADR-0007 and the money module.
- **Status**: this is the follow-up most likely to land next, and §3's
  "ownership is not tenancy" is written so that adding it does not move any row.

### C. A separate `unit_allotments` table for parking — rejected for now

Ancillary units stand alone; allotment is an effective-dated row.

- **For**: reassigning a slot keeps history, which `parent_unit_id` overwrites.
- **Against**: a second table and a second join for "what comes with this flat",
  and the ancillary hop in §4 becomes a subquery — inside a policy, which §4
  shows is where subqueries against the tree stop being free.
- **Why rejected**: nothing bills on parking history today. If it ever does, the
  parent column becomes the current row of the allotment table, and the hop is
  the only code that changes.

### D. A polymorphic foreign key on `scope_id` — rejected as impossible, then as undesirable

Split `scope_id` into `scope_property_id` and `scope_unit_id`, each with a real
foreign key.

- **For**: referential integrity from the database rather than a trigger, and no
  procedural code in the write path.
- **Against**: it does not express the check that matters — that the target
  belongs to the grantor — so the trigger would be needed anyway. It also
  rewrites ADR-0005's grant shape and the harness that tests it, to gain a
  guarantee that is strictly weaker than what replaced it.
- **Why rejected**: a constraint that admits a valid-looking wrong answer is
  worse than a trigger that admits none. (`scope_property_id` exists, but as the
  trigger's resolved output, not as a second user-supplied column.)

### E. Enforcing unit scope in application code — rejected

Keep `is_delegated()` at property granularity and filter units in Go.

- **For**: no recursion problem, no second function, no assertion 6.
- **Against**: it is ADR-0003's alternative A and ADR-0005's alternative C for the
  third time. The failure mode is a reporting query that forgets the filter, and
  it is silent and cross-organisation.
- **Why rejected**: the measurement in §4 is what this looks like when it goes
  wrong, and no reviewer would spot it in a diff.

### F. Deleting a unit, ever — rejected

- **For**: a mistyped unit is a mistake, and a mistake should be removable.
- **Against**: by the time a unit is wrong it may already have a lease, a
  receipt and a ledger entry pointing at it. Deleting it orphans money.
- **Why rejected**: `state = 'inactive'` costs a filter; a deleted unit costs a
  reconciliation nobody can complete.

---

## Consequences

**Good**

- One hierarchy covers a house, a tower, a shop, a co-living room and a parking
  slot. The differences are `unit_kind` and whether `block_id` is null.
- Area-based dues compute from the tree with no schema change: measured across a
  seeded 240-flat two-wing society, per wing, from `carpet_area_sqft`.
- Grant scope is now enforced at the granularity it is written at. "Two of the
  five units" means two.
- A grant can no longer name a row that does not exist, or one belonging to
  somebody else. Both are refused when the scope is written, not when it is used.
- Tenant coherence inside a property is a foreign key, not a review rule.

**Bad, and accepted**

- A unit scope widens to property granularity for property-scoped tables. A firm
  holding one flat can read the property row and its blocks. Bounded to that one
  property, and deliberate, but it is a widening and a reviewer should see it as
  one.
- Ownership of a flat inside a society is not modelled. Until alternative B
  lands, an owner's sight of their own flat depends on the society maintaining a
  grant — which inverts the usual direction, since the society is the grantor.
- Parking allotment has no history.
- Two scope checks now exist, and choosing the wrong one is a defect that looks
  correct. Assertion 6 catches the shape; it cannot catch
  `is_delegated_unit(tenant_id, property_id, id, NULL, …)` where the parent was
  meant, which merely fails closed.
- `is_delegated_unit()` runs a scope lookup per candidate row, as ADR-0005's
  consequences predicted. Indexed on `(grant_id, scope_property_id)`, but a
  delegated portfolio listing is measurably more work than an owner's own.
- Every external identifier — municipal tax id, electricity consumer number,
  RERA id — is unconstrained free text and not unique. Two flats genuinely can
  share a meter, and a municipal id genuinely is reassigned after a subdivision.
  Validation belongs to whichever module has a reason to care.

**Follow-up work this ADR creates**

- `unit_ownership` (alternative B), and with it the owner-side story that does
  not depend on a society-issued grant.
- The property module's Go bindings and the CRUD API. This ADR lands the schema,
  the policies and the contract; no handler reads a unit yet.
- Geocoding: `geocoded_at` and `geocode_source` exist, and nothing writes them.
- The first table to carry a `unit_id` — a lease, under ADR-0011 — is where
  assertion 6 first applies to something other than `units`, and where the
  ancillary hop's `NULL` case will need a decision rather than a default.
- ADR-0007's money model consumes `carpet_area_sqft` for area-based dues. The
  column is `numeric`, deliberately, and the rounding rule is that ADR's to make.
