# ADR-0006 — Chart of accounts and ledger posting rules

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Payments
- **Issue**: [#7](https://github.com/tesserix/dwellm8/issues/7)
- **Related**: [ADR-0003](0003-tenancy-and-row-level-security.md) (the exemption this ADR found already granted), [ADR-0009](0009-property-block-unit-model.md) (where money sits, and what a grant reaches), [ADR-0002](0002-event-backbone-and-outbox.md) (idempotent consumers), ADR-0007 (money representation, next), ADR-0011 (payment provider, later)

---

## Context

The ledger is the subsystem that decides whether this product is trustworthy.
Rent, late fees, deposits, adjustments, owner payouts, refunds, GST, TDS and
gateway settlements all have to be postings against a fixed chart of accounts,
with balances derived — because the alternative, a stored `amount_due` that some
service updates, is the defect every rental product has and nobody can prove they
do not have.

Two things were known going in. Money is `amountMinor` int64 (ADR-0007 will fix
the full standard; this ADR needs only the type). And a management firm reaches
an owner's units through a grant, at unit granularity, since ADR-0009 — so an
owner's money has to be reachable the same way and no further.

Everything below was measured against PostgreSQL 16, which is what CI and prod
run. Four things came out differently from the design, and one of them was a
defect in the tenancy model that has nothing to do with the ledger — it switched
tenant isolation off for any session connecting as the schema's owner, in every
environment whose roles the bootstrap job created. They are recorded as they
happened, production included, where the answer was better than expected and is
written down rather than assumed.

---

## Decision

**One platform-wide chart of accounts; every money event posts through a
declared template; entries and postings are append-only and correction is a
reversing entry with a reason code; every balance is a sum over the postings, and
what a firm can see of it is judged at unit granularity.**

### 1. The chart is one list, not one per organisation

```sql
ledger_accounts (code, name, account_type, normal_side, party_kind, description)
```

No `tenant_id`. A landlord with a private idea of what `deposit_liability` means
is a landlord whose statements cannot be compared, consolidated or audited, and
the first consolidated report across a management firm's portfolio would have to
reconcile fourteen charts that agree on nothing. Organisations get their own
postings, never their own accounts.

`normal_side` is a generated column, `debit` for assets and expenses and `credit`
for the rest, and Go derives it the same way from the same account type. An asset
with a credit normal balance is not a preference, it is a typo, and every report
that assumes the sign would then be silently backwards.

`party_kind` is whose balance the account is kept per: a receivable without a
tenant cannot be chased and a payable without an owner cannot be paid. It is
enforced on the posting — `CHECK ((party_kind = 'none') = (party_id IS NULL))`
in the schema, and the equivalent in `Entry.Validate()`.

### 2. Fourteen accounts, where the issue listed thirteen

The issue's list is implemented as written, plus `tenant_advance`.

The addition is what issue #7's own edge case requires. "A payment that arrives
before its invoice is handled as an advance, not an error" — and an advance
posted as a credit to `tenant_receivable` is a receivable with a credit balance,
which misstates both sides of the balance sheet and which no ageing report knows
how to bucket. Money held against a charge that does not exist yet is a
liability. So the payment template splits: what settles an existing debt goes to
the receivable, and the remainder goes to `tenant_advance`.

That single decision covers three of the four cases the issue names — partial
payment, overpayment, and payment-before-invoice are the same arithmetic with
different inputs, and there is one template rather than three.

### 3. Immutable, and the reversal is the only correction

`journal_entries` and `ledger_postings` have `UPDATE` and `DELETE` revoked from
`dwellm8_app`, and a `RESTRICTIVE` policy denying each. Two locks, for the reason
ADR-0009 gives about the tree: the privilege stops the statement before the
policy is reached, and the policy is what survives a future `GRANT` that hands
the privilege back. `UPDATE` being revoked as well as `DELETE` is what makes this
table different from every other one in the schema — a corrected amount that
leaves no trace is indistinguishable from a corrected amount that was theft.

A reversal is an entry whose every line is the original's opposite side. **The
link lives on the reversing entry**, not the original:

```sql
reverses_entry_id uuid REFERENCES journal_entries(id),
reversal_reason   text CHECK (... IN ('duplicate', 'wrong_amount', ...))
```

Recording the reversal on the original would be an `UPDATE` of an immutable
table, which is the mechanism disproving itself. And a partial unique index on
`reverses_entry_id` means an entry is reversed once: a second reversal doubles
the correction and lands on the wrong side of the original, which is a defect
that looks like activity.

The reason vocabulary is closed. Every reason becomes "adjustment" the moment the
field is free text, and the whole value of a reversing entry is that it says what
went wrong.

### 4. Templates are data; the arithmetic is Go

`posting_templates` and `posting_template_lines` hold, for each of the twelve
money events, which account each line posts to, on which side, and which of the
event's amounts it takes (`gross`, `net`, `tax`, `tds`, `principal`, `advance`).
The rule is therefore inspectable in the database a dispute is being argued
against, rather than only in a binary.

The engine is out of scope per the issue, and what landed instead is the smallest
thing that makes the templates real: `internal/money/domain` holds the same
templates as data and one function, `apply()`, that turns a template plus a set
of amounts into postings. Every event in the product goes through the same
arithmetic, so there is no per-event opportunity to get a side wrong. What each
constructor decides is only *which amounts exist* — that a payment beyond the
outstanding balance becomes an advance, that a payer deducting TDS pays the net
and the deduction stays receivable.

Two copies of the rule is a deliberate choice and it has a price, paid in
`internal/money/store`: a contract test compares the Go chart and templates
against the database's, in both directions, and fails on any difference. Without
it the failure mode is not a crash — it is an account the module posts to and the
reports have stopped summing.

Optional lines are omitted when their amount is zero rather than posted as zero.
Rent to a residential tenant is GST-exempt, so a zero GST posting would be the
commonest row in the table and would mean nothing.

**Double entry is enforced by a deferred constraint trigger**, and the timing is
the part worth knowing:

> `tenancy: commit: ERROR: journal entry 11111111-… does not balance: debits
> 2500000, credits 2000000 (SQLSTATE 23514)`

It has to be deferred — an entry is unbalanced between its first line and its
last, so a non-deferred trigger would reject every entry ever written. The
consequence is that the violation surfaces from `COMMIT`, not from the `INSERT`
that caused it. **Code that checks the error of each `Exec` and ignores the error
of `Commit` will believe an unbalanced entry was written.** `tenancy.Scoped`
returns the commit error, and the contract test asserts that the failure arrives
from the commit rather than earlier — if it ever arrives earlier, the check has
stopped being deferred and no multi-line entry can be written at all.

A second deferred trigger runs from the other end: an entry with no postings at
all never fires the first one, and is a header that appears in every statement
and totals in none.

### 5. Balances are derived, and here is what that costs

There is no stored balance anywhere in this schema. `ledger_balances` is a view
that groups the postings by tenant, place, account and party and sums
`signed_minor` — a generated column, `+amount` for a debit and `-amount` for a
credit, so the sign convention lives in one place instead of in every query that
asks what a tenant owes.

Measured against 48,000 postings across 1,000 units and 24 months, as
`dwellm8_api` with `app.tenant_id` set, so the row-level security predicate is in
the plan:

| Query | Plan | Time |
|---|---|---|
| One unit's ledger (48 of 48,000 rows) | Index scan on `ledger_postings_unit_idx` | **0.16 ms** |
| The same through `ledger_balances` | Same index scan, aggregate on top | **0.15 ms** |
| One party's balance for one account | Index scan on `ledger_postings_party_idx` | **0.04 ms** |
| Whole portfolio, per account | Parallel sequential scan, 48,000 rows | **34 ms** |

So the two queries the product actually makes on a screen — a tenant's statement
and a unit's ledger — are index scans and stay index scans, and the view costs
nothing over the raw sum because the predicate pushes into the same index. The
portfolio-wide aggregate is a full scan and grows linearly; at ten times this
size it is a third of a second.

The sanctioned fix, when that day arrives, is **not** a stored balance column. It
is an append-only snapshot written by the period close (issue #190): a closing
balance per account per period, from which any later balance is the snapshot plus
the postings after it, and which can be discarded and rebuilt from the postings
at any time. A cache that can be recomputed is a different thing from a number
that is authoritative because somebody remembered to update it.

`security_invoker = true` on the view is load-bearing, and it is why this schema
now requires PostgreSQL 15 or later. Without it a view executes with its owner's
privileges: `current_user` inside the view becomes the owner, so
`is_platform_session()` is false even for a platform session and the delegated
branch is judged against a role that holds no grant. The view would not leak — it
would silently under-report, which is the harder failure to notice. Assertion 10
fails the bootstrap if the option is ever lost.

### 6. What a firm sees of an owner's money

`ledger_postings` carries `property_id`, `unit_id` and a denormalised
`unit_parent_id`, and its policy is ADR-0009's unit-granularity check with one
addition:

```sql
OR (property_id IS NOT NULL
    AND is_delegated_unit(tenant_id, property_id, unit_id, unit_parent_id, 'money.read'))
```

`property_id` is nullable, because a GST remittance belongs to the organisation
and not to a building. A `NULL` property passed to `is_delegated_unit()` matches a
portfolio scope, which would hand a firm the owner's statutory position — so the
delegated branch requires a property outright. A firm sees the money of the units
it manages and nothing about the organisation that owns them. The contract
asserts exactly that: a mandate over flat 101 sees 101's postings and the parking
slot allotted to it, and not the flat next door's, and not the organisation's own.

`unit_parent_id` is the ancillary hop, stamped by a `BEFORE INSERT` trigger
reading `units`. It is denormalised rather than resolved in the policy for
ADR-0009 §4's reason: a policy that reads a table cannot be the policy on that
table, and the check takes the parent as an argument precisely so it reads
nothing. The trigger is `SECURITY INVOKER`, so a writer who cannot see the unit
gets a `NULL` parent and loses the hop — which fails closed. `SECURITY DEFINER`
would be worse than useless: the function's owner is the table owner, which
`FORCE ROW LEVEL SECURITY` applies to and which sets no `app.tenant_id`, so it
would see nothing at all.

Planting the tempting defect — the same check at property granularity, which
satisfies every assertion in the schema — produces: `visible postings
[scope-granted scope-parking scope-sibling], want [scope-granted scope-parking]`.
A firm managing one flat reading the whole tower's rent roll, and nothing about
the request looking wrong. CI plants it and requires the contract to go red.

### 7. The platform exemption was already granted, and nothing said so

This was found while asserting that the ledger's isolation was real, and it is
the most consequential thing in this ADR.

`is_platform_session()` was `pg_has_role(current_user, 'dwellm8_platform',
'member')`. Measured on PostgreSQL 16, as the role that owns these tables:

```
owner   member=t usage=f is_platform_session=t rows_visible=161
api     member=f usage=f is_platform_session=f rows_visible=0
```

161 postings across every organisation in the database, from a connection that
should have seen none.

From PostgreSQL 16, a `CREATEROLE` role is **automatically granted every role it
creates** — `WITH ADMIN TRUE, INHERIT FALSE, SET FALSE`. The bootstrap job has
`CREATEROLE` and creates `dwellm8_platform`, so it holds a bookkeeping membership
of it, and `'MEMBER'` answers true for a membership whose privileges are
explicitly not inherited. ADR-0009 §6 argued at length against granting the owner
membership of `dwellm8_platform`. The server grants it by itself.

The fix is one word: `'USAGE'` asks whether the role's privileges are in force for
this session, which is what the exemption actually means. It answers false for
the admin-only grant and stays true for `dwellm8_platform` itself. After the
change, the owner sees 0 postings and the platform role still sees all 161.

**What this was worth in production, measured rather than assumed.** Two things
in ADR-0009 §6 have since stopped being true, and both were checked against the
running cluster before this paragraph was written:

- The API does *not* connect as the owner. Its `DB_USER` comes from
  `dwellm8-postgres-api-credentials`, whose username is `dwellm8_api` — a
  dedicated role, exactly as the schema intends. ADR-0009 §6 recorded the CNPG
  app credentials, and the deployment has moved on.
- On the production database (16.4), `dwellm8` holds the automatic grant for the
  eight module roles and for `dwellm8_app`, and **not** for `dwellm8_api` or
  `dwellm8_platform` — those two predate the bootstrap creating them and were
  made by a superuser. So `is_platform_session()` was false for the owner there,
  and production was not exposed.

It was one role-creation away from being exposed. Any environment where the
bootstrap job creates `dwellm8_platform` itself — which is what the schema file
says to do, and what every fresh environment and every CI run does — has the
grant, and had the exemption. That is where it was found.

Assertion 11 fails the bootstrap if any table owner ever inherits the platform
role, and CI plants the inherited grant and requires the assertion to fire. Every
policy in the schema read correctly throughout; nothing about the defect was
visible in a diff, which is the argument for assertions that ask the running
server rather than for review.

### 8. What fails the build

`isolationtest.RunLedger` asserts §§3–6 against a real PostgreSQL: that an
invoice and its matching payment net the receivable to zero and leave the money
in clearing rather than in the bank; that the view agrees with the sum it claims
to compute; that an unbalanced entry, a one-line entry and an empty entry are all
refused; that no posting or entry can be edited, backdated or deleted; that the
reversal nets it out and a second reversal is refused; that a redelivered event
produces one entry; that a posting cannot name a unit outside the property it
claims; and the whole of §6's delegated visibility, including revocation.

`isolationtest.Run` — ADR-0003's five-part contract — runs over both ledger
tables. Both inserts write a whole balanced entry, because a single posting
cannot be written at all; that is the only place in the harness where the
one-INSERT shape does not fit.

`internal/money/domain` tests the arithmetic without a database, including issue
#7's primary scenario and all four payment cases. `internal/money/store` is the
drift check between the Go copy of the rule and the database's.

Five new bootstrap assertions: the ledger must be immutable (7), money must be an
integer of minor units (8), every template must post to both sides (9), the
balances view must be `security_invoker` (10), and no table owner may inherit the
platform role (11).

Two of them were wrong when first written, which is the argument for testing an
assertion rather than reading it:

- Assertion 8 matched money by column name and failed on
  `posting_template_lines.amount_role`, which holds the word `gross`. It was
  right that the name looked like money and wrong that the column was; it now
  tests the type and keeps a narrower rule about the name.
- Assertion 7 used `cmd` as a subquery alias, so `p.cmd = cmd` bound to
  `pg_policies`' own column, the condition became `p.cmd = p.cmd`, and the
  assertion passed as long as *either* deny policy existed. Dropping
  `ledger_postings_no_update` and running the assertion block came out green.
  Renaming the alias to `required_cmd` fixed it, and CI now plants exactly that
  defect.

---

## Alternatives considered

### A. A chart of accounts per organisation — rejected

Each tenant seeds and edits its own accounts, with a platform-supplied default.

- **For**: a society's books genuinely differ from a landlord's, and a chartered
  accountant will ask for their own account codes on day one.
- **Against**: every consolidated view — a management firm's portfolio, platform
  revenue, a regulator's extract — becomes a mapping exercise across N charts,
  and the mapping is the thing that silently rots. A posting's meaning would stop
  being knowable from the posting.
- **Why rejected**: the customisation people actually want is reporting labels
  and groupings, which can sit on top of a fixed chart. Sub-accounts under a
  fixed parent remain available if that turns out to be wrong.

### B. A stored balance, maintained by the posting engine — rejected

`unit_balances(tenant_id, unit_id, account_code, balance_minor)`, updated in the
same transaction as the postings.

- **For**: the portfolio-wide query in §5 becomes a lookup instead of a 34 ms
  scan, and it stays that way at a hundred times the size.
- **Against**: it is a second source of truth for the one number the product must
  never be wrong about. Every path that writes a posting must remember to update
  it; a concurrent update needs a lock the postings do not need; and when the two
  disagree — which is when, not if — there is no rule for which one wins.
- **Why rejected**: the measured numbers say it is not needed yet, and the
  rebuildable period-close snapshot in §5 buys the same speed without the second
  authority. This is the guardrail in `docs/backlog.md` stated as a decision:
  balances are derived from postings, with no stored-and-mutated amount due.

### C. `numeric(14,2)` for amounts — rejected

- **For**: PostgreSQL's `numeric` is exact, so this is not the float mistake; it
  reads naturally in `psql`; and it makes GST arithmetic look like the tax
  formula it implements.
- **Against**: it is exact in the database and inexact everywhere else. Go has no
  decimal type in the standard library, every JSON encoder in the chain turns it
  into a float64, and the analytics extract is the place that would find out. The
  guardrail exists because that path has burned this product's category before.
- **Why rejected**: `bigint` of paise is exact in every one of those hops, and
  assertion 8 now fails the bootstrap on any inexact column with a money-shaped
  name.

### D. Correction by `UPDATE` with an audit trail — rejected

Let an operator amend a posting, and write the before and after to
`audit_events`.

- **For**: it is what every operator asks for, and the audit trail does record
  what changed.
- **Against**: the audit trail is a different table with different retention and
  a different access path, and reconstructing a balance as at a past date then
  means replaying an audit log rather than summing a ledger. Anybody with the
  privilege to write the posting can write the audit row.
- **Why rejected**: a reversing entry gives the same operator the same outcome
  and leaves the arithmetic self-contained. The cost is two rows where one felt
  natural, and a reason code the operator has to pick.

### E. Templates in Go only — rejected

Drop `posting_templates`; the Go catalogue is the rule.

- **For**: one copy, no drift check, no contract test in `money/store`.
- **Against**: when a dispute is argued three years from now, the question is
  what the rule was at the time, and the answer would be a git tag rather than a
  row. Nothing in the database would say why `deposit_liability` was credited.
- **Why rejected**: the drift check costs one test file and makes the rule
  legible where the evidence lives. The versioning column is there for the same
  reason, though nothing selects a version yet.

### F. A separate `advance_received` event rather than a split payment — rejected

Two templates: one for a payment against an invoice, one for an advance.

- **For**: each template is simpler, and "was this an advance" is answerable from
  the entry kind.
- **Against**: the caller then has to decide which event it is, and that decision
  is exactly the arithmetic being avoided — a payment of 30,000 against 25,000
  outstanding is both. It would also make an overpayment two entries for one
  bank movement.
- **Why rejected**: one template with an optional line on each side handles all
  four cases in issue #7, and the entry kind stays a description of what happened
  in the world rather than of what the software concluded.

---

## Consequences

**Good**

- Every rupee is a posting against a named account, and every balance in the
  product is a sum. There is nowhere for an unexplainable number to live.
- An entry that does not balance cannot be committed, by any path, including a
  `psql` prompt.
- Nothing in the ledger can be edited or deleted by the role the application
  connects as, and the correction mechanism carries a reason code from a closed
  list.
- A firm's sight of an owner's money is bounded by the same grant that bounds its
  sight of the units, at the same granularity, and closes on revocation.
- Redelivered provider events cannot double an owner's income: the idempotency
  key is a unique index rather than a convention.
- A tenant statement and a unit ledger are index scans at 48,000 postings, and
  the view costs nothing over the raw sum.
- The chart and the templates exist in the database, so the rule is legible where
  the evidence is, and a drift between it and the code fails a build.
- The tenancy defect in §7 is closed, and an assertion now watches for it. That
  finding is worth more than the ledger.

**Bad, and accepted**

- The balance rule fires at `COMMIT`. An error handler that only inspects
  statement errors will report success on an entry that was rejected. This is
  inherent to a deferred constraint and the only mitigation is that
  `tenancy.Scoped` returns the commit error and the contract asserts it.
- A data migration that writes ledger rows inside ADR-0009 §6's `NO FORCE` /
  `FORCE` window must `SET CONSTRAINTS ALL IMMEDIATE` before restoring `FORCE`.
  Measured: `ERROR: cannot ALTER TABLE "journal_entries" because it has pending
  trigger events`. Found while seeding the performance fixture, which is a
  fortunate place to find it.
- The rule exists twice, in SQL and in Go. The contract test is the mitigation
  and it only runs where a database is available; a laptop run without one skips
  it.
- Every posting of an entry shares one property and unit, so a payout batch
  spanning three buildings is three entries. Issue #189 is where that stops being
  free.
- A grant carrying `money.collect` but not `money.read` cannot write an entry at
  all: the deferred "entry has postings" check reads back the postings it just
  wrote, under the writer's own row-level security. Write-only money access is
  not a shape this product supports, and the failure is loud rather than silent.
- The portfolio-wide aggregate is a full scan and grows linearly. The number is
  in §5 and the fix is named, but it is not built.
- `society_dues_receivable` and `sinking_fund` are in the chart and no template
  posts to them. Society billing is a later epic; the accounts are there so that
  work does not start by amending the chart.
- The schema now requires PostgreSQL 15 or later, and says so by failing the
  bootstrap rather than by misbehaving.

**Follow-up work this ADR creates**

- ADR-0007, immediately: rounding, where rounding is permitted, largest-remainder
  allocation, and the serialisation rule. `Minor` is deliberately the smallest
  type this ADR needed, and proration does not exist until that ADR does.
- The posting engine and the money module's service interface. This ADR lands the
  accounts, the templates, the rules and the contract; no handler posts an entry
  yet.
- The period close and its rebuildable snapshot (issue #190), which is also where
  the portfolio-wide query stops being a full scan.
- TDS and GST rate resolution from versioned, state-scoped rule tables (issues
  #18, #19). The templates have the lines; nothing computes the amounts.
- A statement renderer, which is where `occurred_on` versus `posted_at` first
  matters to somebody outside the database.
- ADR-0009 §6 is now wrong about which role the API connects as (§7 records what
  it is). The correction belongs in a superseding ADR rather than in an edit, and
  it changes that section's conclusion about how much the owner exemption would
  have cost.
