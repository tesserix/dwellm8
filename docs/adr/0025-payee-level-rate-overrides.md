# ADR-0025 — Payee-level rate overrides: the rate deducted is not the rate the section says

- **Status**: Accepted
- **Date**: 2026-07-31
- **Deciders**: Compliance, Books
- **Issues**: [#87](https://github.com/tesserix/dwellm8/issues/87), [#215](https://github.com/tesserix/dwellm8/issues/215)
- **Related**: [ADR-0007](0007-money-representation-and-rounding.md), [ADR-0008](0008-effective-dating-and-temporal-queries.md), [ADR-0023](0023-statutory-rule-tables.md), [ADR-0024](0024-tds-decision-matrix.md)
- **Consumed by**: the payout run, the owner statement, Form 27Q and 16A generation

---

## Context

ADR-0024 chose the section. It did not produce a number anybody can deduct at,
because between the section's rate and the rent that actually arrives there are
four more provisions, and every one of them turns on a fact about the **payee**
rather than about the payment:

| Provision | Turns on | Effect |
|---|---|---|
| §197 | An Assessing Officer's certificate | Replaces the rate, downwards, possibly to nil |
| §206AA | Whether a PAN has been furnished | Floors the rate at 20% |
| §206AB | Whether the payee was a specified non-filer | Floors it at twice the rate, or 5% |
| Surcharge and cess | Being a non-resident, and how much they receive | Raises it above 100% of the section rate |

Two of these are why the platform's numbers were unusable in the one case that
matters most. §195 has no rate in the registry, deliberately (ADR-0024 §5) — and
the reason a real NRI landlord's deduction is nevertheless computable in practice
is that they hold a §197 certificate. Without modelling certificates, "we cannot
compute §195" is permanent rather than a gap waiting on data.

The other two produce the opposite failure. An NRI landlord with no Indian PAN is
at 20% before surcharge, not at the treaty rate — and a platform that quietly
deducted the section rate would under-deduct on the tenant's liability, every
month, invisibly.

---

## Decision

**The rate deducted is resolved as an ordered sequence of overrides over the
section's rate, each recorded with the provision and the row that produced it. A
§197 certificate is tenant data and lives beside the registry; §206AA and §206AB
are statutory numbers and live in it.**

### 1. A certificate is tenant data, not a rule

`tds_certificates` is a new table: `party_id`, `section`, `certificate_number`,
`rate_bps`, a bounded validity, `issued_on`. Tenant-scoped, row-level security,
no delete.

It is emphatically **not** a row in `statutory_rules`. That table has no runtime
writer by design (ADR-0023 §2) and assertion 18 fails the build if it acquires
one; a certificate is entered by a user, about one landlord, from a document an
officer issued. Putting it in the registry would mean handing back the `INSERT`
that ADR-0023 revoked, in order to store something that is not a statutory
parameter at all.

`valid_to` is `NOT NULL`, alone among the effective-dated tables here. A
certificate is issued for a period; an open-ended one would go on lowering a
deduction for years after the determination behind it lapsed, and the failure
would look exactly like a correct low rate.

A certificate **replaces** the rate rather than flooring it, and suppresses
surcharge and cess: the officer determined the rate to deduct at, not a base to
build on. That is the professional reading and it is the one place in this ADR
where a reasonable practitioner might differ, so it is stated here and flagged in
[#215](https://github.com/tesserix/dwellm8/issues/215) rather than buried in a
branch.

### 2. §206AA and §206AB are floors in the registry, and the zero value deducts more

Both are ordinary statutory rows: `tds.206aa_no_pan_floor` at 20% and
`tds.206ab_non_filer_floor` at 5%. The arithmetic around them — "the higher of",
"twice the rate in force" — is code, because it is arithmetic and not a number.

**`PayeeProfile`'s zero value means no PAN on file**, and therefore deducts at
20% rather than at the section rate. A field nobody filled in has to fail in the
direction that over-deducts: the money is recoverable by the landlord on
assessment, whereas an under-deduction is the tenant's interest and penalty,
discovered late.

Two consequences worth stating:

- **Rule 37BC** takes a *non-resident* out of §206AA where they furnish name,
  address, email, phone, TRC and TIN. It does nothing for a resident, and the
  resolution ignores it for one rather than pretending the flag is general.
- **§206AA is a floor over a known rate, not a rate.** Where the section's rate is
  a gap — §195 — a missing PAN does not make 20% the answer, because "the higher
  of 20% and an unknown number" is not 20%. The assessment still fails, and that
  is deliberate.

### 3. §206AB is the effective-dating case that could not have been invented

It was inserted in 2021 and **omitted** by the Finance (No. 2) Act 2024 with
effect from 1 October 2024. The row is bounded at that date rather than deleted,
so a deduction recomputed for August 2024 still resolves the floor that applied to
it, and one dated after simply finds no rule and applies nothing.

A constant would have been deleted in the 2024 cleanup and every historical
recomputation would have silently changed. This is ADR-0023's whole argument,
happening.

### 4. Surcharge and cess, and why the effective rate is in no table of sections

Both apply only to a payment to a **non-resident** — a resident's non-salary TDS
is the section rate flat, and adding 4% of cess to it is a common error in the
expensive direction.

Surcharge is a slabs rule, per payee form, selected on the year's aggregate to
that payee: a non-resident individual is on a five-band ladder from nil to 37%, a
foreign company on a three-band one from nil to 5%. Cess is 4% on tax and
surcharge together. So a ₹60,000-a-month NRI landlord and a ₹8,00,000-a-month one
are deducted at different rates from the same section, which is exactly the fact a
table of sections cannot express.

Composition is on the **rate**, not on the money: 10% raised by a 10% surcharge is
11%, then 11.44% with cess. The one division involved rounds half away from zero
for ADR-0007's reason, and the money module still performs the single
multiplication that turns a rate into paise.

### 5. Every step is recorded

`EffectiveRate` carries the section's own rate, the rate deducted, and an ordered
`Applied` trail — the provision, the rate either side, the registry row where one
supplied the number, and a sentence. A landlord who receives less rent than the
agreement says will ask why, and "20%, because no PAN was furnished, floored by
§206AA" is an answer that a deductor can defend and a spreadsheet cannot produce.

---

## Alternatives considered

### A. Hold §197 certificates in `statutory_rules` with a `party_id` column — rejected

It is the shortest path and it destroys the registry's central property. The table
would need a runtime writer, which ADR-0023 revoked and assertion 18 enforces, and
a tenant-scoped column on reference data would put one organisation's certificate
one policy mistake away from lowering another's deduction.

### B. Store the effective rate on the lease — rejected

It depends on the date, the payee's PAN status, a certificate that may be issued
or may lapse mid-year, and the year's aggregate to that payee. Every one of those
changes without the lease changing. ADR-0024 §D rejected storing the *section* for
the same reason and this is the stronger case.

### C. Default `PayeeProfile` to "PAN on file" — rejected

It reads as the friendly default and it under-deducts silently for every payee
whose record is incomplete. The direction of a default is a decision, and the safe
direction here is the one that costs the landlord a refund claim rather than
costing the tenant a penalty.

### D. Apply surcharge and cess to residents too — rejected

Wrong, and expensively so in the direction users notice: it would shave 4% off
every resident landlord's rent for no statutory reason.

### E. Stack surcharge and cess on a §197 rate — rejected, with a caveat

A certificate states the rate at which tax is to be deducted, so surcharge and
cess are not added on top. This is the position taken and it is the one item here
a practising CA should confirm; §215 owns that.

### F. Model "twice the rate" as a registry row — rejected

It is not a number a Finance Act sets, it is arithmetic in the section's own text.
A `multiplier` column would invite every future statutory formula into the value
shape, which §1 of ADR-0023 already refused for the same reason.

---

## Consequences

**What is now true.** A §195 deduction is computable where the landlord holds a
certificate, which is how the real ones are done. A payee with no PAN on file is
deducted at 20% rather than at the section rate, and the platform says which
provision did it. A recomputation of an August 2024 deduction still sees §206AB,
which no longer exists. The effective rate on a payment to a non-resident includes
surcharge and cess, and the owner statement can show the four steps that produced
it.

**What this costs.** `Assess` now takes a payee profile, so every caller must say
what it knows about the landlord — deliberately, because the alternative is a
default that under-deducts. Certificates are data somebody has to enter and keep
current, and an expired certificate silently returns the deduction to the section
rate, which is correct and will surprise the first landlord it happens to. The
surcharge rows are seeded `needs_bare_act_check` like everything else in the
registry, so none of this may block until [#215](https://github.com/tesserix/dwellm8/issues/215)
is done.

**What is not decided.** Whether the §115BAC 37% surcharge cap reaches a
non-resident's rent is the sharpest open question in the seeded data. Certificate
*capture* — the form, the document upload, the expiry reminder — is
[#87](https://github.com/tesserix/dwellm8/issues/87)'s. Nothing here reads a PAN
or a filer status from anywhere: the profile is assembled by the caller, because
identity data belongs to the identity module and ADR-0001 §3 keeps this package
out of its tables. And the §194-IB proviso that caps a §206AA deduction at the
last month's rent is an amount rule rather than a rate rule, so it belongs to
whatever computes the deduction, not here.
