# ADR-0003 — Organisation tenancy model and row-level security standard

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Platform
- **Issue**: [#3](https://github.com/tesserix/dwellm8/issues/3)
- **Related**: [ADR-0001](0001-modular-monolith-api.md), [ADR-0002](0002-event-backbone-and-outbox.md), ADR-0005 (delegation grants, later)

---

## Context

The organisation is the tenancy boundary. A landlord account, a management firm
and a society are all organisations, and a management firm reading an owner's
unit must traverse a grant rather than a shortcut.

Retrofitting this is brutal, so it is decided before the first table exists —
and everything below was reproduced against a real PostgreSQL before being
written down, because three plausible-looking arrangements turned out to be
wrong.

---

## Decision

**Every tenant-scoped table carries `tenant_id`, forces row-level security,
and constrains writes as well as reads. The API connects as a role that owns
nothing. A request sets the tenant from the verified token, never from a
header.**

### 1. The column

`tenant_id uuid NOT NULL REFERENCES organisations(id)` on every table holding
customer data. No exceptions, including tables that "obviously" belong to one
organisation — the exception is what a later join gets wrong.

### 2. The policy template

```sql
ALTER TABLE <t> ENABLE ROW LEVEL SECURITY;
ALTER TABLE <t> FORCE  ROW LEVEL SECURITY;

CREATE POLICY <t>_tenant_isolation ON <t>
    USING      (tenant_id = current_tenant_id() OR is_platform_session())
    WITH CHECK (tenant_id = current_tenant_id() OR is_platform_session());
```

Three things in that template are load-bearing, and each was learned the hard
way.

**`FORCE`, not merely `ENABLE`.** A table's owner bypasses its own policies.
The bootstrap job creates these tables as `dwellm8`, and the API originally
connected as `dwellm8` — so every policy was decorative for exactly the role
that mattered. Measured on a real instance: the owner saw 2 of 2 rows with
`ENABLE`, and 1 of 2 with `FORCE`.

**`WITH CHECK`, not only `USING`.** `USING` filters what a statement can see;
`WITH CHECK` filters what it can write. With only the first, a session scoped
to organisation A can insert a row belonging to organisation B and then be
unable to read it back — worse than either failing or succeeding cleanly.

**`current_tenant_id()`, not `current_setting(...)::uuid`.**
`current_setting('app.tenant_id', true)` returns NULL when never set but an
empty string after `RESET`, and `''::uuid` raises. That surfaces as a 500 on
reused pooled connections and only on reused ones. The function coerces `''`
to NULL, so an unset tenant denies quietly:

```sql
CREATE OR REPLACE FUNCTION current_tenant_id() RETURNS uuid
    LANGUAGE sql STABLE PARALLEL SAFE AS
$$ SELECT nullif(current_setting('app.tenant_id', true), '')::uuid $$;
```

### 3. Two roles, and no `BYPASSRLS`

| Role | Owns | Bypass | Used for |
|---|---|---|---|
| `dwellm8` | Every table | No (FORCE covers it) | Migrations and the schema bootstrap only |
| `dwellm8_api` | Nothing | No | Every request |
| `dwellm8_platform` | Nothing | No — exempt via policy | Onboarding an organisation, platform reporting, audited support |

Some operations genuinely cannot be tenant-scoped: creating an organisation
compares the policy against a row that does not exist yet. That needs a real
exemption. It is written **into the policies** through `is_platform_session()`
rather than granted as the `BYPASSRLS` role attribute, for two reasons: the
attribute requires superuser, which CNPG rightly withholds, and an attribute is
invisible in the policy a reviewer is actually reading.

Every platform-session operation writes to `audit_events`. An exemption that
leaves no trace is a back door.

### 4. Where the tenant comes from

From the verified token, resolved to a membership, and from nowhere else.

```
request → GIP token verified → subject → membership lookup
        → tenant_id in the request context → SET LOCAL app.tenant_id
        → every query in that transaction is scoped
```

A client-supplied `X-Tenant-Id` header is ignored, and its presence is logged
as a suspicious event. A user who belongs to several organisations chooses
explicitly, and the choice is validated against their memberships on every
request rather than trusted from the client.

`SET LOCAL` inside the transaction, never `SET`. A pooled connection outlives
the request; a plain `SET` leaks the last request's tenant to the next one that
picks up that connection, which is the worst possible failure — intermittent,
and wrong in favour of disclosure.

### 5. The SDK binding

`internal/platform/tenancy` is the only sanctioned way to reach the database:

```go
// Scoped runs fn inside a transaction with app.tenant_id set. There is no
// exported way to get a *sql.Tx without a tenant, which is the point.
func Scoped(ctx context.Context, db *sql.DB, fn func(context.Context, *sql.Tx) error) error
```

A repository that takes a raw `*sql.DB` fails the boundary test in
`internal/platform/arch`, alongside ADR-0001's module import rules.

### 6. The tenant-isolation test contract

Every module's store package satisfies the same five, or it is not done:

1. Two organisations with rows in the table; scoped to A, the query returns
   only A's rows.
2. With no tenant set, the query returns **zero rows** and does not error.
3. A write naming another organisation's `tenant_id` is refused by the database,
   not by application code.
4. The owner role is filtered too — the FORCE test.
5. A platform session sees across organisations, and the access appears in
   `audit_events`.

Three of these are asserted at bootstrap as well, in
`003_tenancy_assertions.sql`: a table with RLS enabled but not forced, a role
holding `BYPASSRLS`, or a policy without `WITH CHECK` fails the schema job
rather than shipping.

### 7. Cross-organisation access

A management firm reading an owner's unit traverses an explicit, revocable
grant — never a second `tenant_id`, never a shared row. The grant model itself
is ADR-0005; this ADR fixes only that the traversal is explicit and appears in
the audit trail.

---

## Alternatives considered

### A. Application-level filtering, no RLS — rejected

- **For**: simple, portable, no policy to debug.
- **Against**: one forgotten `WHERE tenant_id = ?` in one query is a
  cross-organisation disclosure, and the code path most likely to forget is the
  reporting query written under time pressure.
- **Why rejected**: the failure is silent, and the blast radius is other people's rent.

### B. A schema or a database per organisation — rejected

- **For**: isolation by construction; trivially explainable.
- **Against**: thousands of schemas, migrations multiplied by tenant count,
  connection-pool fragmentation, and cross-tenant reporting becomes a union
  over N schemas. Onboarding turns into DDL.
- **Why rejected**: the operational cost is enormous at our tenant count and
  grows with success.

### C. RLS with `BYPASSRLS` for platform work — rejected

- **For**: the standard PostgreSQL answer.
- **Against**: needs superuser to grant, which CNPG withholds; and the
  exemption becomes invisible at the point of review.
- **Why rejected**: replaced by a policy-level exemption that says who is
  exempt, in the policy.

### D. `SET` rather than `SET LOCAL` — rejected

- **For**: marginally fewer statements.
- **Against**: leaks the tenant across pooled connections.
- **Why rejected**: intermittent cross-tenant disclosure is the worst bug this
  system could have.

---

## Consequences

**Good**

- A forgotten filter returns zero rows instead of another organisation's data.
- Isolation is enforced twice, independently: the API role owns nothing, and
  FORCE covers the owner.
- Three classes of mistake fail the schema bootstrap rather than shipping.

**Bad, and accepted**

- Every query pays a policy evaluation. `current_tenant_id()` is `STABLE`, so
  it is evaluated once per statement, but it is not free.
- Every database access must go through `Scoped`, which is more ceremony than
  a bare query.
- Platform-session code is a small, privileged surface that needs review
  attention out of proportion to its size.

**Follow-up work this ADR creates**

- `internal/platform/tenancy` with `Scoped`, and the arch test that fails a
  repository taking a raw `*sql.DB`.
- The five-part isolation contract as a reusable test helper, so a module
  satisfies it in a few lines (issue #4).
- Audit-trail writes on every platform-session path, enforced in review until
  there is a linter for it.
