# ADR-0023 — Statutory rule tables: a rate change is a row, a gap is an error

- **Status**: Accepted
- **Date**: 2026-07-31
- **Deciders**: Compliance, Books
- **Issues**: [#18](https://github.com/tesserix/dwellm8/issues/18)
- **Related**: [ADR-0006](0006-chart-of-accounts-and-posting-rules.md), [ADR-0007](0007-money-representation-and-rounding.md), [ADR-0008](0008-effective-dating-and-temporal-queries.md), [`docs/india-compliance.md`](../india-compliance.md), [`docs/india-property-compliance.md`](../india-property-compliance.md) §1
- **Consumed by**: [#19](https://github.com/tesserix/dwellm8/issues/19) (the TDS decision matrix), the GST epic, the lease builder's deposit cap

---

## Context

Every number this product computes tax and caps with moves, and each moves on a
different clock. GST rates move with a Council notification. TDS thresholds move
with a Budget — the §194-I annual threshold went from ₹2,40,000 to ₹6,00,000 with
the Finance Act 2025, and the §194-IB rate from 5% to 2% part-way through a
financial year. Deposit caps move with a state amendment and disagree between
states while they do. Stamp duty is a state scale that is progressive in some
states and flat in others.

A constant in a service is wrong within a year of shipping and does not know it.
That is the ordinary framing and it understates the problem: the failure is not
that the number is stale, it is that **every past computation silently changes
with it**. Correct a hardcoded rate and last March's invoice recomputes at this
March's rate. There is then no way to explain a document to the person who
received it, which is the thing a tax computation exists to be able to do.

`india-property-compliance.md` §1 already states the governance model — a
registry entry with a citation, an owner, a verification status and a review
date. This ADR is that model made structural: what the table is, how a resolution
works, and what happens when there is no row.

---

## Decision

**Every statutory parameter is a row in `statutory_rules`, effective-dated per
ADR-0008 and scoped to a jurisdiction. Resolution is as-of a date the caller
states, falls back from a state to the central rule and says which it used, and
fails with a named gap rather than a default.**

### 1. One table, four value shapes

`statutory_rules` holds a rate (basis points), an amount (minor units), a count
(months) or a scale (`statutory_rule_slabs`), and a CHECK requires the column that
matches the kind and forbids the others. Not one polymorphic `numeric value`: a
rate read as an amount is 18 paise of GST on a ₹25,000 rent, and nothing about the
resulting invoice looks wrong. ADR-0007's argument about money applies unchanged —
`rate_bps` is an integer because 18% in floating point does not add up either.

Slabs are a child table rather than JSON, so every bound is a `bigint` of minor
units that the schema's money assertion can see, and a band is addressable by a
query rather than by a path expression.

### 2. Reference data with no runtime writer

No `tenant_id` and no row-level security, exactly like the chart of accounts
(ADR-0006 §2) and for a stronger version of the same reason: an organisation with
a private idea of the TDS rate is an organisation deciding its own tax. `INSERT`,
`UPDATE` and `DELETE` are revoked from `dwellm8_app`, so the schema file is the
only author and a rate change is a reviewed commit.

Assertion 18 asserts the privilege rather than trusting the `REVOKE`, because a
blanket `GRANT ... ON ALL TABLES` three sections above would silently hand it back.

### 3. Effective dating, and the shape of a change

The ADR-0008 pattern unchanged: `valid_from`, `valid_to`, a generated `validity`
daterange, and an exclusion constraint over (type, jurisdiction, key, validity)
that makes one rule in force at a time a property of the database. A rate change
is a new row whose `valid_from` is the old row's `valid_to`; a rate that was
recorded wrongly is an ADR-0008 correction that retires the wrong row rather than
editing it.

The seeded §194-I threshold is the worked example: `[2020-04-01, 2025-04-01)` at
₹2,40,000 and `[2025-04-01, )` at ₹6,00,000. A deduction computed for March 2025
resolves the first, in November and in five years, because the row that was true
then is still there and still bounded.

**A computation stamps the rule id it used.** Resolving the same query again is
not reproducibility — the registry can be corrected. The artefact recording which
row produced it, is.

### 4. Governance columns, and the one rule that is a CHECK

`statute_ref`, `source_url`, `verification_status`, `verified_by`, `verified_on`,
`owner`, `review_due`, `enforcement` — the columns
`india-property-compliance.md` §1.1 specifies. Two of them interact and that
interaction is a constraint rather than a review rule:

> **An unverified rule may never carry `enforcement = 'block'`.**

A deposit cap enforced from a blog post is worse than no cap, because it is wrong
with authority: it refuses a lawful tenancy, in the product's voice, citing
nothing. The Karnataka 2025 amendment is the live case — reported by secondary
sources, unverified, so it records and does not block.

### 5. Jurisdiction, and what falls back to what

`IN` is the central rule, held once. A state row overrides it. Resolution tries
the state, then `IN`, and returns which one answered — because "Karnataka
legislated" and "Karnataka did not, so the central rule stands" are different
facts, and an invoice that records which applied can be explained later.

There is deliberately **no national deposit cap row**. The Model Tenancy Act is a
model adopted state by state, not central law, so a national row would assert a
cap that binds nobody, and an unlisted state must be a gap rather than two months.

### 6. A gap is an error that names itself

No default rate, no nearest date, no most-recent-anywhere. `Resolve` returns a
`*Gap` naming the type, the key, the jurisdiction and the date, and recording
whether the central rule was tried too. A calculation that proceeds with a number
nobody authorised is worse than one that stops: the first produces a document, the
second produces a ticket.

### 7. Review is a report, not an exception

`statutory_rules_review_due` is a view — live rules at or within thirty days of
their review date, `days_overdue` negative while the review is still ahead. Derived
rather than a stored flag, for the reason ADR-0010 §6 gives about expiring leases:
a flag needs a job to maintain it, and a rule that should be flagged and is not is
exactly the silence this table exists to break.

An overdue rule still resolves. The operational answer to a missed review is an
alert with an owner on it, not a service that stops billing rent.

---

## Alternatives considered

### A. Constants in the service with a `// review annually` comment — rejected

The comment is the part that decays, and the story's first acceptance criterion —
recomputing an old invoice at the old rate — is unreachable by construction.
ADR-0022 §E rejected the same shape for the AFA ceiling.

### B. A configuration file or Helm values — rejected

It moves the number out of the code and keeps every other defect: no effective
dating, so history is lost on deploy; no citation or owner; and the value in force
is whatever the last rollout carried, which is not answerable as of a date.

### C. One row per state, including for central rules — rejected

Twenty-eight copies of the GST rate is twenty-eight rows to change on notification
day, and the one that is missed is the one that computes wrongly for months. The
fallback costs one lookup and makes "this state has legislated" visible in the
answer.

### D. A generic `numeric value` column with a unit string — rejected

It is the schema-level version of the defect §1 describes, and it defeats the
money assertion: a `numeric` column named `value` passes every guard this schema
has and holds a rupee amount in floating point.

### E. Bitemporal rows — rejected, for now

"What did we believe the rate was last Tuesday" is answerable from the retired
correction rows by hand, and is not a query the product has. ADR-0008 drew the
same line and this follows it rather than opening a second model.

### F. Resolving against `now()` inside the database — rejected

A function that reads the clock cannot be tested at a boundary, and every
interesting bug in effective dating is at a boundary. The date is an argument,
here as in `platform/effective`, `recon` and `workflow`.

---

## Consequences

**What is now true.** A rate change is a row with a citation, an owner and a
review date, and it does not touch a line of Go. A recomputation of an old invoice
resolves the old rule, and the artefact can say which row it used. An unlisted
state fails loudly instead of quietly inheriting a cap that does not bind it. An
unverified rule cannot block, and the database refuses one that tries. The
registry has no runtime writer, and a build fails if it acquires one.

**What this costs.** The vocabulary now exists in two places — the CHECK
constraints and the Go constants — and the store's contract test is the price of
that; it fails the build in either direction. Every seeded row is
`needs_bare_act_check` or worse, so nothing here may gate anything a user sees
until a practising CA has signed it off, and that is a real backlog item rather
than a formality. The registry is loaded whole and held in memory, so a change
reaches a running process on reload rather than immediately.

**What is not decided.** The GST and TDS calculations themselves (#19 and the GST
epic own them) — this decides only where the numbers come from. Stamp duty scales
per state are modelled and unseeded: `e-stamping-by-state.md` has the channel
detail and the duty bases are not yet verified. The rail-selection caps that
ADR-0022 §4 needs are not statutory rates and are not here yet; whether NPCI's
ceilings should join this table or sit beside it is #74's question. And there is
no admin surface: a rate change today is a pull request against the schema, which
is the correct starting point and will not stay sufficient once Compliance owns
the rows rather than Engineering.
