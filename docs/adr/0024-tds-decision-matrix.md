# ADR-0024 — The TDS decision matrix: two facts at lease creation, and a section that cannot be guessed

- **Status**: Accepted
- **Date**: 2026-07-31
- **Deciders**: Compliance, Books
- **Issues**: [#19](https://github.com/tesserix/dwellm8/issues/19)
- **Related**: [ADR-0006](0006-chart-of-accounts-and-posting-rules.md), [ADR-0007](0007-money-representation-and-rounding.md), [ADR-0008](0008-effective-dating-and-temporal-queries.md), [ADR-0010](0010-lease-lifecycle-state-machine.md), [ADR-0023](0023-statutory-rule-tables.md), [`docs/india-compliance.md`](../india-compliance.md) §5
- **Consumed by**: the payout run, the owner statement, the reminder schedule, form generation (MVP 3)

---

## Context

Three sections of the Income-tax Act 1961 can govern tax deducted at source on
rent, and they agree about almost nothing:

| | §194-I | §194-IB | §195 |
|---|---|---|---|
| Deductor | Anyone not below | An individual or HUF **not** liable to audit under §44AB | Anyone paying a non-resident |
| Threshold | The year's rent to that landlord | The month's rent | **None** — from the first rupee |
| Timing | Every payment or credit | Once, at year end or lease end, on the whole period | Every payment or credit |
| TAN | Required | **Not** required | Required |
| Filing | Challan, quarterly 26Q, Form 16A | Form 26QC, Form 16C | Challan, quarterly **27Q**, Form 16A, 15CA/15CB |

Which one applies is settled by two facts: what kind of payer the tenant is, and
whether the landlord is a resident. Both are known at lease creation and neither
is derivable from anything the product already holds — a company name does not
tell you whether an individual landlord spends 182 days a year in India, and a
tenant's own legal form does not tell you whether they are liable to audit.

The failure this ADR is written against is not "the wrong rate". It is a tenancy
that runs for nine months before anybody asks. The deduction was due monthly, the
interest under §201(1A) runs from the date it was due, the penalty is the
deductor's, and the deductor is the tenant — who was never told. §195 is where
this is worst, because there is no threshold to fall below: a ₹18,000 rent to an
NRI landlord is deductible from the first rupee, and nothing about the payment
looks different from any other.

ADR-0023 built the registry the numbers come from and explicitly left the
calculation to this story. What is decided here is the **choice**: which section,
on what facts, producing what obligations.

---

## Decision

**The deductor class and the landlord's residency are captured before a tenancy
starts, held as an effective-dated history, and select one of three paths. The
section is chosen without touching the registry; the numbers are resolved as of a
date and fail loudly when absent. A section 195 tenancy cannot start until the
deductor has acknowledged the obligation.**

### 1. Residency first, then the deductor — in that order

A non-resident landlord puts **every** deductor on §195, including the salaried
tenant who would otherwise be on §194-IB and the company that would be on §194-I.
Only then does the deductor class separate §194-IB from §194-I.

Read the other way round — decide on the deductor, adjust for residency — the
matrix produces a §194-IB deduction at 2% above ₹50,000 a month on a payment
abroad that owed the treaty rate from the first rupee. The order is the decision,
so it is written as one branch and not as two independent lookups.

The deductor class is four values, not two. "Individual or business" is the wrong
cut: the line the Act draws is audit liability under §44AB, so a doctor with a
large practice and a salaried tenant are both individuals and are not on the same
path. `government` is held apart from `business` because its deposit is a book
entry reported on Form 24G, and a product that promises it a challan number is
promising a reference that will never exist.

### 2. Selection is free; arithmetic needs the registry

`Select` answers the section, the basis, the timing, the artefacts, the TAN
requirement and whether an acknowledgement is owed — without a rate, a threshold
or a database. That is what lets the lease screen say *"section 195, from the
first rupee, and the liability is yours"* for a landlord whose rate nobody has
verified.

`Assess` is what resolves numbers, and it takes the date as an argument for
ADR-0023's reason: the §194-I threshold rose on 1 April 2025 and the §194-IB rate
fell on 1 October 2024, so a tenancy that spans either is assessed with different
numbers on each side of it, from the same code. The assessment carries the rule
ids it used, so what was deducted can be explained years later.

Thresholds are **exceeded**, not reached, as both provisos say. Rent exactly at
the threshold deducts nothing.

### 3. No money is computed in the matrix

`Assess` returns a rate in basis points and whether the threshold is crossed. It
does not multiply. ADR-0007 permits one rounding primitive and it lives under
`internal/money`; a second one in `platform/statutory/tds` would be a second
answer to which way a half-paisa goes. The money module applies the rate the
matrix selects, through the existing `payment_with_tds` template — debit
`gateway_clearing` with the net, debit `tds_receivable` with the deduction, credit
`tenant_receivable` with the gross — so the deduction reaches the owner statement
as an asset the owner will recover from the government rather than as money lost,
and the payout reconciles.

### 4. The facts are a history, not two columns

A landlord who moves abroad in October was a resident in April, and both are true
of the same tenancy in the same financial year. April's rent was deducted at 10%
under §194-I, deposited and certified that way; overwriting a column would restate
a deduction that was correct. So `lease_tax_facts` is ADR-0008's shape — half-open
intervals, an exclusion constraint, corrections that retire rather than edit — and
`History.PathOn(date)` answers per payment.

`Changes()` reports the dates the section changes inside the tenancy, so the
owner statement and the payout run learn about a split year before they meet it.

### 5. Section 195 has no rate row, on purpose

The registry seeds §194-I and §194-IB and deliberately holds **no §195 rate**. The
rate on a payment to a non-resident is the Act's or a treaty's, read with that
landlord's tax residency certificate and their Form 10F — it is not one number and
it is not ours to choose. So `Assess` on a §195 path returns ADR-0023's named gap.

This is the ADR's sharpest edge and it is deliberate: the platform will say the
section applies, say it starts at the first rupee, say who carries it, and refuse
to compute the deduction. A plausible number here would be wrong with authority,
which §4 of ADR-0023 already rejected for caps.

### 6. Acknowledgement is a gate on the tenancy, not on the draft

A draft may record that the landlord is an NRI before the tenant has been shown
what it costs them. The tenancy may not start until they have: `Activate` refuses,
and a deferred constraint trigger refuses the same thing for a write that never
went through Go.

Deferred to commit rather than immediate, because the facts carry a foreign key to
the lease they belong to and the two are written together. An immediate trigger
enforces the identical rule and rejects every legitimate write — it is the version
of this guard that looks tidier and is inert in the worst way, so CI plants it.

The acknowledgement lives on the facts it was given against. The first set may be
acknowledged before the tenancy starts — that is what signing is — but a set that
*supersedes* another may not be acknowledged before it arose, because copying
April's date onto October's row is exactly how a tenant would be recorded as
having accepted an obligation nobody told them about.

### 7. Joint owners are assessed per payee, on an exact split

Where shares are definite and ascertainable, the threshold is tested per
co-owner: two owners of one flat each receive half the rent, and half may be below
a threshold the whole is above. Residency is per co-owner too — a couple with one
partner abroad is a §194-I payment to one and a §195 payment to the other, in the
same month out of the same rent.

That treatment is also the shape of the most common evasion, so `Apportion`
accepts nothing vaguer than an exact split: shares totalling 10,000 basis points
and amounts totalling the rent to the paisa. A remainder means somebody rounded,
and a rounded share is not an ascertainable one. The rent is not divided here
either — the caller passes the division ADR-0007's allocator already made.

### 8. The obligation stays with the deductor

Stated once, in one constant, used in the terms of service, on the lease screen
and in the package's own documentation: the deduction, deposit, return and
certificate are the deductor's. Dwellm8 computes, reminds, records and produces
references; it does not deduct, deposit or file for anyone. Facilitation is a
legal position, not a disclaimer, and three teams paraphrasing it differently is
how it stops being true.

---

## Alternatives considered

### A. Ask for the facts at the first payout — rejected

It is the behaviour this ADR exists to prevent. By the time a payout run needs the
section, the rent has been paid for months, the deduction was due monthly and the
interest runs from each due date. The cost of asking early is a two-field form;
the cost of asking late is a liability with someone else's name on it.

### B. Derive residency from the landlord's address or bank account — rejected

Residency is a day-count test under §6, not a postcode. An NRE account is
suggestive and not determinative, and an Indian address proves nothing about where
its owner spent the year. A derived answer would be wrong quietly and would carry
the platform's authority; a declared one is wrong loudly and carries the
declarant's name and date.

### C. Two columns on `leases`, overwritten when residency changes — rejected

It destroys the fact that made last April's deduction correct. §4.

### D. Store the selected section on the lease — rejected

It is derived from the facts and the date, so storing it creates a second source
of truth that goes stale the moment either changes — ADR-0010 §6's argument about
`expiring` as a stored state, applied to a section. `PathOn` is a pure function of
data the lease already holds.

### E. Pick a default §195 rate — 20%, or 30%, or the Act's residuary rate — rejected

Every candidate is defensible in some case and wrong in most, and all of them
produce a challan. §5. The gap costs a support conversation; the default costs a
short deduction the tenant discovers when the assessing officer does.

### F. Let the matrix compute the deduction amount — rejected

A second rounding implementation, in a package that already refuses to hold money.
§3.

### G. Test the threshold per property rather than per payee — rejected

The threshold is per payee: two flats let by one owner to one tenant are one
threshold. Testing per property under-deducts on exactly the portfolio landlords
this product is built for, and it is the classic implementation error, so
`Rent.AnnualMinor` is documented as the aggregate to the landlord.

---

## Consequences

**What is now true.** Every tenancy knows its TDS section before its first rupee
of rent, and a lease cannot go live without one — in Go and in PostgreSQL. A §195
tenancy cannot start until the tenant has accepted an obligation that is theirs.
Residency that changes mid-lease splits the tenancy instead of restating it. Joint
owners are assessed on their own shares, and only on a split that adds up. A
deduction carries the rule ids it was computed from.

**What this costs.** Two more questions on the lease form, and they are questions
some users will not know the answer to — "am I liable to audit under §44AB" is a
real question with a real answer, and the UI will have to explain it rather than
present a dropdown. Every existing fixture that lets a flat now has to record the
facts, which is the guard working and was a genuine change to a dozen tests. And
§195 leases are, today, a path the platform can describe and cannot compute — that
is a support burden until a practising CA signs off a rate model.

**What is not decided.** Form generation — 26QC, 16C, 16A, 27Q, 15CA/15CB — is
MVP 3 and this only names the artefacts each path owes. The reminder schedule that
turns `Timing` into due dates is the notify module's. Lower or nil deduction
certificates under §197, which change the rate for a specific landlord and are the
common NRI answer to §195, are not modelled: they are a per-landlord rate override
with their own validity window, and whether that belongs in `statutory_rules` or
beside it is the next question this area has to answer. Nothing here files
anything, and §8 says so on purpose.
