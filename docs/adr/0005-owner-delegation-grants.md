# ADR-0005 — Owner delegation grant model

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Platform
- **Issue**: [#6](https://github.com/tesserix/dwellm8/issues/6)
- **Related**: [ADR-0003](0003-tenancy-and-row-level-security.md) (the tenancy boundary this crosses), [ADR-0001](0001-modular-monolith-api.md), ADR-0009 (property and unit model, later), ADR-0020 (OpenFGA, later)

---

## Context

A management firm manages units it does not own. A chartered accountant sees an
owner's ledger and nothing else. A caretaker closes maintenance tickets for one
building. A society committee changes every year. All four are the same
question: how does one organisation reach another organisation's rows, and how
does the owner take that back?

ADR-0003 made the organisation the tenancy boundary and deferred exactly one
thing — §7 said cross-organisation access traverses "an explicit, revocable
grant", named this ADR, and stopped there. This is that mechanism.

The requirement that shapes everything else is revocation. An owner who ends a
mandate expects the firm to lose access now, not at the next login, and expects
to still be able to answer "what did they have access to, and when" a year
later. Those two pull in opposite directions: the fastest way to remove access
is to delete the row, and deleting the row destroys the answer.

Everything below was reproduced against PostgreSQL before it was written down.
Two of the claims that seemed obvious were wrong, and are recorded as they
happened rather than quietly corrected.

---

## Decision

**Cross-organisation access is an effective-dated, scoped, permissioned grant
row owned by the grantor. A request declares which grant it is acting under;
PostgreSQL decides whether that grant is real. Grants are never deleted, never
transitive, and every access made under one is stamped with the grant id.**

### 1. The grant

```sql
delegation_grants (
    id, tenant_id,          -- tenant_id is the GRANTOR: the owner
    grantee_org_id,         -- the firm
    permissions text[],     -- a closed vocabulary
    valid_from, valid_to,   -- effective dating
    revoked_at, revoked_by, revoked_reason,
    created_at, created_by)

delegation_grant_scopes (grant_id, tenant_id, scope_kind, scope_id)
    -- scope_kind ∈ portfolio | property | unit
```

Scope is enumerated rows, not a predicate, so "two of the five units" is a join
and can be shown in a UI without evaluating anything. `scope_id` is not yet a
foreign key: properties and units arrive with ADR-0009, which adds it.

`permissions` is a closed set checked by the table — `property.read`,
`money.collect`, `maintenance.write` and so on — so a typo is a constraint
violation rather than a permission nobody holds. Two things are deliberately
absent from that vocabulary. There is no `identity.*`: a grant never confers
control of the owner's account, users or other grants. And there is no
permission for onward delegation, because §5 makes it an operation that does
not exist rather than one that was withheld.

A grant is active when `revoked_at IS NULL AND now() >= valid_from AND (valid_to
IS NULL OR now() < valid_to)`. Every one of those clauses is evaluated by the
database on every row, not cached in a session.

### 2. It lives in the grantor's tenant, and only the grantor writes it

The grant is the owner's decision and the owner's to end, so `tenant_id` is the
grantor. But the firm must be able to read the mandate it holds. That makes this
the first policy in the schema whose `USING` and `WITH CHECK` genuinely differ:

```sql
CREATE POLICY delegation_grants_access ON delegation_grants
    USING (tenant_id = current_tenant_id()          -- the owner
           OR grantee_org_id = current_tenant_id()  -- the firm
           OR is_platform_session())
    WITH CHECK ((tenant_id = current_tenant_id() AND current_grant_id() IS NULL)
           OR is_platform_session());
```

ADR-0003 predicted this case — "the day the two need to differ, when the
fallback would quietly be wrong" — and it arrived with the very next table.

The policy makes no call to `is_delegated()`. The policy governing the grants
table must not consult the grants table.

### 3. How a request declares a grant

```
request → token verified → membership → tenant_id (the FIRM's own organisation)
        → the firm selects a mandate → grant id in the request context
        → SET LOCAL app.tenant_id, app.grant_id
```

The tenant stays the firm. Acting under a grant is a window, not a costume —
there is no point at which the API pretends to be the owner, which is why
`tenancy.GrantID` is a distinct Go type from `tenancy.ID` and cannot be assigned
to one.

`app.grant_id` is a claim. The check is `is_delegated(row_tenant, row_property,
permission)`, and it re-derives everything on every row: the grant must exist,
name the current tenant as its grantee, name the row's owner as its grantor, be
active now, carry the permission the policy asked for, and cover the property
the row belongs to. Six conditions, none of them taken from the session.

It is not `SECURITY DEFINER`, deliberately. The lookup runs under the caller's
own row-level security, so a session quoting somebody else's grant id finds
nothing — the policy in §2 hides the row the check would need. That is a second
refusal, independent of the first, and it was worth having: it is the one that
holds if the permission logic is ever wrong.

### 4. The policy template for a delegable table

```sql
CREATE POLICY <t>_tenant_isolation ON <t>
    USING      (tenant_id = current_tenant_id() OR is_platform_session()
                OR is_delegated(tenant_id, property_id, '<module>.read'))
    WITH CHECK (tenant_id = current_tenant_id() OR is_platform_session()
                OR is_delegated(tenant_id, property_id, '<module>.write'));
```

The permission is named in the policy, so what a table requires is readable at
the table. Read and write ask for different permissions, which is the second
reason `USING` and `WITH CHECK` must be stated separately: a read-only mandate
must let a firm see a row it may not change.

Passing `NULL` for `row_property` means "this row is not property-scoped". That
is right for an audit entry and catastrophic for a lease — it widens a two-unit
mandate to the whole portfolio. A bootstrap assertion therefore fails any table
that has a `property_id` column whose policy does not pass it, which will bite
the first time a module lands one under ADR-0009.

### 5. Grants are neither transitive nor deletable

`current_grant_id() IS NULL` in the `WITH CHECK` above means a session acting
under a grant cannot write a grant at all. A firm cannot pass an owner's units
on to a sub-contractor, and does not need a permission withheld to stop it.

Deletion is refused twice: the privilege is revoked from `dwellm8_app`, and a
`RESTRICTIVE` policy denies `DELETE` outright. The second lock is not decoration.
Measured, as the grantee, with the privilege held and the policy absent:
`DELETE 1` — a firm can destroy the grant and, with it, the record of everything
it was ever allowed to do. With the policy present: `DELETE 0`, silently, for
both the grantee and the owner.

That experiment nearly lied. The first attempt was refused by a foreign key from
`delegation_grant_scopes`, which looked like the design working and was actually
a scope row happening to exist. Repeating it against a scope-less grant gave the
real answer.

### 6. Revocation, and the rule for work in flight

Revocation is `UPDATE delegation_grants SET revoked_at = now()`. Nothing is
deleted, so the history of what was permitted survives the mandate — and the
firm keeps sight of the revoked grant, because its own past audit entries are
otherwise unexplainable.

How immediate "immediate" is depends on the isolation level, which was measured
rather than assumed. With another session revoking mid-transaction:

| Reader's transaction | Access before | Access after the revoke commits |
|---|---|---|
| READ COMMITTED | yes | **no**, from the next statement |
| REPEATABLE READ | yes | yes, until the transaction ends |

`tenancy.Scoped` therefore pins READ COMMITTED explicitly rather than inheriting
a default that someone could change.

Expiry by `valid_to` behaves differently again, and this one is a genuine trap:
`now()` is transaction-start time, so a grant that expires two seconds into an
open transaction is still honoured three seconds later inside it — confirmed,
with `now() = statement_timestamp()` returning false as the explanation. A new
transaction denies at once. Expiry is thus bounded by transaction lifetime,
which is bounded by `statement_timeout` and
`idle_in_transaction_session_timeout`; revocation is not, and revocation is what
an owner reaches for when something is wrong.

**The documented rule for in-flight work.** Money that a tenant has already paid
belongs to the owner, and its settlement is not gated on anybody's grant: the
remaining steps of a collection — reconciliation, ledger posting, payout release
— run as the owner's own work, not under the firm's mandate. Revocation
therefore cannot strand a tenant's payment, and the tenant sees nothing at all,
because their agreement is with the owner and never was with the firm. What the
firm loses, from the next statement, is initiation and visibility: no new
collection may be started, and the owner's rows stop being readable. A
collection that had not yet taken money is simply abandoned.

### 7. The audit trail of delegated access

`audit_events` gains `actor_org_id` and `grant_id`. An access made under a grant
is written **into the owner's tenant** — it is the owner's record of who looked
at their data — stamped with the firm that looked and the grant they used.

This is a deliberate widening and should be read as one: a delegated session can
write into another organisation's table. It is bounded by the `WITH CHECK`,
which requires the row to name the acting firm and the grant in use. So a firm
can add to the owner's trail and cannot forge an entry in the owner's name;
both were tested. The permission `audit` is implied by every grant rather than
being grantable, because access that cannot be recorded is access without a
trace, and no owner should be able to switch that off by accident.

The firm reads back only the rows carrying its own grant id. A mandate is a
window onto the access it made, not onto the owner's history.

### 8. The contract, and what fails the build

`isolationtest.RunGrantModel` asserts the grant object itself, once: that a
grant reaches the properties it names and no others, that a permission it does
not carry is refused, that quoting another organisation's grant confers nothing,
that a grantee cannot widen, revoke, re-delegate or delete, and that revocation
ends the reach while keeping the record.

`isolationtest.RunDelegated` asserts one delegable table, and every module with
one calls it — the same shape as ADR-0003's five-part contract. `audit_events`
is the first.

Three assertions run at bootstrap and fail the job rather than shipping: the two
delegation tables must carry a `RESTRICTIVE` deny-delete policy, any table with
a `property_id` must pass it to `is_delegated()`, and the ADR-0003 assertions
continue to cover the rest. CI additionally plants a real defect — an
`is_delegated()` that stops checking who the grantee is — and requires the
contract to go red.

---

## Alternatives considered

### A. Membership in two organisations — rejected

Give the firm's users a membership row in the owner's organisation and let
existing tenancy do the rest.

- **For**: no new mechanism at all; every policy already written keeps working.
- **Against**: it is indistinguishable from the owner. Scope cannot be
  expressed, so a two-unit mandate becomes the whole portfolio; revocation
  removes the membership and leaves nothing to explain what happened; and the
  audit trail records the owner's organisation doing things the owner never did.
- **Why rejected**: it deletes exactly the three properties the issue asks for.

### B. Copying or sharing rows — rejected

Materialise the delegated units into the firm's tenant, or let a row carry two
tenant ids.

- **For**: reads are trivial and fast; no policy work.
- **Against**: two copies of a lease diverge, and the one the tenant is paying
  against is now a coin toss. A second `tenant_id` breaks the invariant every
  policy and every index depends on.
- **Why rejected**: ADR-0003 §7 already ruled this out; restating it here so the
  reason travels with the mechanism.

### C. Enforcing scope in application code — rejected

Keep grants as data, but filter in Go.

- **For**: easier to express complicated rules; no SQL to debug.
- **Against**: it is ADR-0003's alternative A wearing a different hat. One
  reporting query written under deadline forgets the join, and the failure is
  silent and cross-organisation.
- **Why rejected**: the database is the only layer that cannot be bypassed by
  forgetting.

### D. A privileged "manager" role — rejected

Let firms connect as a role exempt from RLS and filter by grant in the query.

- **For**: simple policies; one code path.
- **Against**: the exemption is invisible at the point of review, and a firm's
  bug becomes every owner's disclosure. ADR-0003 rejected `BYPASSRLS` for the
  same reason and had to be dragged back from it once already.
- **Why rejected**: an exemption granted per connection cannot be scoped per row.

### E. Deleting the grant on revocation — rejected

- **For**: the fastest possible revocation, and no "is it still active" logic.
- **Against**: it destroys the answer to "what were they allowed to do in
  March", which is the question that gets asked in a dispute, and orphans every
  audit row pointing at the grant.
- **Why rejected**: revocation must remove access, not evidence.

### F. `clock_timestamp()` instead of `now()` — rejected

Would make `valid_to` expiry immediate mid-transaction, closing the gap in §6.

- **For**: no window at all, however small.
- **Against**: it is `VOLATILE`. Inside a policy it would be re-evaluated
  unpredictably, and a single statement could see a row and then not see it —
  torn reads inside one transaction, which is a worse failure than a
  second-long window on a scheduled expiry.
- **Why rejected**: revocation is the urgent path and is already immediate;
  expiry is scheduled, and a transaction-consistent view is worth more.

---

## Consequences

**Good**

- One mechanism serves owners, firms, accountants, caretakers and societies.
  The differences between them are rows in `permissions` and scope, not code.
- An owner can always answer "who had access to what, and when", because
  nothing that answers it can be deleted.
- A grant cannot be widened, borrowed or passed on by the party holding it. All
  three are refused by the database, not by a handler.
- Revocation takes effect from the next statement, and the tenant is untouched
  by it.

**Bad, and accepted**

- `is_delegated()` runs a subquery per candidate row. It is `STABLE` and both
  lookups are indexed, but a delegated portfolio query is measurably more
  expensive than an owner's own. If this bites, the fix is a materialised
  reachable-property set per grant, not a weaker check.
- Every delegable table's policy is longer and must name its permissions
  correctly. The assertion catches an omitted `property_id`; it cannot catch
  `money.read` where `money.payout` was meant.
- A delegated session can write into another organisation's `audit_events`.
  Bounded and stamped, but it is a real widening of ADR-0003's model and should
  be reviewed as one.
- Expiry by `valid_to` is bounded by transaction lifetime rather than instant.

**Follow-up work this ADR creates**

- The grant lifecycle API and the owner-facing revoke path (MVP 1), including
  writing `revoked_by` and `revoked_reason` from the request context.
- The money module's implementation of §6's in-flight rule, when collections
  exist (ADR-0007, ADR-0012).
- `property_id` and the first genuinely property-scoped policy under ADR-0009,
  which is the point at which the scope machinery stops being tested only
  through `is_delegated()` directly.
- Enforcing that a delegated request writes an audit row at all. Today the
  policy constrains what such a row must say; nothing yet requires one to exist.
  That is a review rule until there is a linter, and it is the weakest link in
  this ADR.
- ADR-0020 will move the permission decision to OpenFGA. It replaces §3's
  vocabulary and §4's template, not §1's grant object or §5's guarantees — the
  row model is chosen to survive that.
