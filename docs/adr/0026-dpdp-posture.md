# ADR-0026 — DPDP posture: consent as an artefact, and erasure as a partition

- **Status**: Accepted
- **Date**: 2026-07-31
- **Deciders**: Compliance, Platform
- **Issues**: [#20](https://github.com/tesserix/dwellm8/issues/20)
- **Related**: [ADR-0006](0006-chart-of-accounts-and-posting-rules.md), [ADR-0013](0013-kyc-data-handling.md), [ADR-0021](0021-demo-sandbox-architecture.md), [ADR-0024](0024-tds-decision-matrix.md), [`docs/data-retention.md`](../data-retention.md), [`docs/india-compliance.md`](../india-compliance.md) §6
- **Consumed by**: the rights endpoints (MVP 2), the erasure workflow, every collection point

---

## Context

Dwellm8 holds identity documents, financial records and occupancy history for people who
are **not its paying customers**. A tenant never chose this platform; their landlord did.
That asymmetry is the whole reason DPDP matters here more than it does in a product whose
subjects are its buyers.

The Act is often implemented as a policy page and an email address. What it actually
requires is a set of things the software must be able to *do* — state a purpose at
collection, record what was agreed to, stop when consent is withdrawn, produce what is
held, erase on request — and one thing it must be able to **refuse**, because financial
and tax records carry statutory retention periods that outlast any consent.

The refusal is the hard part, and it is where this platform is unusually exposed. Today's
work made it worse in a useful way: `tds_obligations`, `tds_certificates` and
`lease_tax_facts` are all no-delete by design, because they are the answer to a tax notice
years later. An erasure request now collides with them directly.

There is also a loose end. ADR-0013 gave `kyc_verifications` a `NOT NULL`
`consent_artefact_id` column referencing a table that **did not exist** — a promise rather
than a link, in the schema, since it was written.

---

## Decision

**Consent is an artefact with a purpose, a notice version and a language. Erasure is a
partition of a person's data into what goes, what is kept with a statute named against it,
and what waits — answered per organisation, and always explained.**

### 1. Consent is an object, and withdrawal is a timestamp

`consent_artefacts`: party, purpose, notice version, language, given, withdrawn, channel.
One live consent per person per purpose, enforced by a partial unique index.

A boolean column would not survive the first rewording of the notice: "the user agreed"
is not evidence of anything a year later. The **language** is recorded because DPDP §5(3)
gives the data principal the notice in English or an Eighth Schedule language, and a right
nobody records is a right nobody can check.

**Withdrawal is a timestamp, never a delete.** The record that consent was given and then
taken back is the evidence in both directions.

The `kyc_verifications` foreign key is added **`NOT VALID`**: existing rows carry ids
pointing nowhere, and validating against them would fail the bootstrap on a database that
is otherwise correct. New rows are checked from here; the backfill is its own reviewed
change.

### 2. Not everything is processed on consent, and saying otherwise is a lie

Purpose carries its own lawful basis. Rent is processed to perform the tenancy agreement;
a TDS deduction is processed because the Act requires it. Only marketing and support run
on consent alone.

So `Withdraw(purpose)` returns **whether the processing actually stops**, and a sentence.
A tenant withdrawing consent to their own rent ledger is told plainly that it does not
stop, that the basis is the agreement, and that it ends when the tenancy does. The
alternative — accepting the withdrawal in silence and continuing — is the design that
turns a compliance feature into a lie.

### 3. Erasure has three answers, not two

`Erase`, `Retain`, `Defer`, decided per **class** rather than per table: "the rent ledger"
is one answer to a data principal, eleven table names are not.

- **Erase** where nothing requires the data — contact and support.
- **Retain** where a statute does, with the statute, the anchor date and the expiry named
  in the sentence the requester receives. A retention with no citation is a refusal
  wearing a lab coat.
- **Defer** where something is unresolved: an open dispute, unsettled money, or an
  outstanding statutory obligation.

That third category is the story's failure scenario and the one products get wrong in both
directions — blindly executing and destroying the evidence in a live argument, or filing
the request under "cannot comply" and never telling anyone.

**Retention expires.** A class past its period is erased on request. The years are ceilings
on erasure, not a permanent exemption — and deliberately not a scheduled deletion job,
because an unattended job over financial records is a worse risk than holding them longer.

### 4. An outstanding certificate defers erasure

The least obvious deferral, and it follows from ADR-0024. A tenant may be finished with a
tenancy while the **landlord** is still owed a Form 16A or 16C for tax already taken from
their rent. Erasing the deduction record at the tenant's request would destroy a third
party's evidence for their own tax return.

So an outstanding obligation blocks, and the deferral names it.

### 5. An erasure is scoped to one organisation

The same person is a tenant of one organisation and an owner in another. A request made to
one is answered by one. Reaching across would hand an organisation the power to erase
another's records, which is a tenancy-isolation breach dressed as a privacy feature —
ADR-0003's boundary, applied to a right rather than to a query.

### 6. The matrix exists twice, and a test keeps them equal

[`docs/data-retention.md`](../data-retention.md) is where a compliance reviewer reads the
periods; `internal/platform/dpdp` is where an erasure request is answered by them. A
contract test parses the document and fails the build when the two disagree.

Same trade as ADR-0010's state machine, for the same reason: the dangerous drift is the
quiet one, where the document is amended after a review, the code is not, and every answer
cites a period nobody agreed to.

---

## Alternatives considered

### A. A `consented_at` column on each table that needs one — rejected

It cannot express purpose, cannot express which notice was shown, and cannot express
withdrawal. It also spreads the definition of consent across every table that collects
anything, so a change to the model is a migration of the whole schema.

### B. Erase everything and accept the statutory risk — rejected

It is a criminal-liability risk under the Income-tax Act for records the deductor is
required to hold, and it destroys third-party evidence (§4). "The user asked" is not a
defence for failing to retain what the law requires retained.

### C. Refuse erasure wherever any financial record exists — rejected

The opposite failure and the more common one. It makes the right meaningless for anyone
who has ever paid rent, when their prospect record, their enquiries and their support
conversations are erasable with nothing standing behind them.

### D. Anonymise in place instead of retaining — rejected, for now

Replacing the personal fields with a pseudonym inside the retention period is arguably a
better answer than either keeping or deleting: the financial shape survives, the person
does not. It is rejected *now* because the ledger's `party_id` is a foreign key into
records that must reconcile, and a half-done anonymisation is worse than either. Recorded
in `data-retention.md` §4 as the thing to revisit.

### E. Automatic deletion the day a period expires — rejected

A scheduled job running unattended over financial records, with no human in the loop, is a
larger operational risk than holding data slightly longer. Expiry makes a record
*erasable on request*; it does not delete it.

---

## Consequences

**What is now true.** Consent is an artefact with a version and a language, and
`kyc_verifications` finally points at something. An erasure request produces a per-class
answer with a statute and a date against every retained item, and a named blocker against
every deferred one. A withdrawal that changes nothing says so. A request to one
organisation cannot touch another's records. The retention periods cannot drift between
the document and the code.

**What this costs.** Every collection point now owes a notice version and a language, and
the notices themselves have to be written and translated — that is content work this ADR
creates and does not do. The `NOT VALID` foreign key means existing KYC rows still cite
consent artefacts that do not exist, and the backfill is outstanding. And the matrix is
now a thing that must be reviewed by somebody qualified rather than assembled by
engineering, which is the point but is also a dependency.

**What is not decided.** The rights *endpoints* are MVP 2 — this decides what they answer,
not how they are called. The grievance SLA needs a number and an owner, and has neither.
Whether the PMLA five-year period applies at all turns on whether Dwellm8 is a reporting
entity, which `india-property-compliance.md` §9 already flags as open. Crypto-shredding as
an alternative to retention (§D) is unbuilt. And **backups are untouched** — an erasure
that leaves the data in a restorable snapshot is not an erasure, and the rotation that
fixes it belongs to [#25](https://github.com/tesserix/dwellm8/issues/25).
