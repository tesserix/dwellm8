# TDS rows — verification findings and proposed corrections

Working paper for [#215](https://github.com/tesserix/dwellm8/issues/215). It records what
each seeded TDS row in `statutory_rules` is believed to say, where that belief comes from,
and what a bare-act check has to settle before the row may gate anything a user sees.

**Nothing here sets `verification_status = 'verified'`.** That column names a human who
read the section, and this document is not one. Its purpose is to make the reviewer's job
an hour rather than a week, and to say plainly which rows are *believed correct*, which
are *believed correct but reasoned from a secondary source*, and which are **open
questions the platform is currently guessing about**.

Read with [`india-compliance.md`](india-compliance.md) §5,
[ADR-0023](adr/0023-statutory-rule-tables.md), [ADR-0024](adr/0024-tds-decision-matrix.md)
and [ADR-0025](adr/0025-payee-level-rate-overrides.md).

---

## 1. How to read the confidence column

| | Meaning |
|---|---|
| **A** | The provision is stable, long-standing and quoted consistently across primary and secondary sources. A bare-act check should confirm it in minutes. |
| **B** | Believed correct, but the value or its date comes from an amending Finance Act whose commencement wording must be read, not assumed. |
| **C** | **Open.** The platform has taken a position and it may be wrong. Do not let these block anything. |

---

## 2. The rate and threshold rows

| Rule key | Value | Provision | Confidence | What the check must settle |
|---|---|---|---|---|
| `tds.194i_land_and_building` | 10% | §194-I(b) | **A** | That the plant-and-machinery rate under §194-I(a) is genuinely out of scope — a furnished let with equipment is arguably split, and the product treats it all as land and building |
| `tds.194i_annual` | ₹2,40,000 → ₹6,00,000 from 1 Apr 2025 | §194-I proviso, Finance Act 2025 | **B** | The commencement date. The registry boundary is `2025-04-01`; if the Act says the amendment applies to payments *credited* on or after a different date, one month is priced wrongly on each side |
| `tds.194ib_individual_huf` | 5% → 2% from 1 Oct 2024 | §194-IB(1), Finance (No. 2) Act 2024 | **B** | Same, and harder — see §3 below, because §194-IB deducts once for the whole year |
| `tds.194ib_monthly` | ₹50,000/month | §194-IB(1) | **A** | Whether "rent" for the threshold includes maintenance and amenity charges billed separately, which is how most Bengaluru lets are structured |
| `tds.194ia_immovable_transfer` | 1% | §194-IA | **A** | — |
| `tds.194ia_consideration` | ₹50,00,000 | §194-IA(2) | **B** | The 1 Oct 2024 aggregation amendment: the threshold is tested on the *aggregate* consideration across all buyers and sellers, and the registry holds one number with no aggregation rule beside it |
| `tds.206aa_no_pan_floor` | 20% | §206AA(1)(iii) | **A** | — |
| `tds.206ab_non_filer_floor` | 5%, bounded at 1 Oct 2024 | §206AB, omitted by the Finance (No. 2) Act 2024 | **B** | That the omission was effective 1 Oct 2024 and was an omission rather than a suspension — the row's upper bound depends on it |
| `tds.cess.health_and_education` | 4% | Finance Act 2018 | **A** | That it applies to TDS on a non-resident's rent and not only to salary and to assessed tax |
| **§195 rate** | **No row** | §195 read with the DTAA | — | Nothing to verify. ADR-0024 §5 |

---

## 3. The four questions that actually need a professional

These are not verification chores. Each is a position the platform has taken, and each
changes a number a user sees.

### 3.1 The §194-IB rate change against a once-a-year deduction

§194-IB deducts **once**, in the last month of the financial year or of the tenancy, on the
whole period's rent. The rate fell from 5% to 2% on 1 October 2024. For a tenancy running
April 2024 to March 2025, deducted in March 2025, the question is whether the deduction is:

- 2% on the whole year, because that is the rate in force when the liability arose; or
- 5% on April–September and 2% on October–March, apportioned; or
- 5% on the whole year, because the rate is fixed by reference to the period.

The platform currently resolves **the rate in force on the assessment date**, which gives
the first answer. It is the most defensible and it is not obviously right. CBDT circular
guidance on the amendment should settle it.

### 3.2 Whether the 37% surcharge band survives §115BAC for a non-resident

The seeded non-resident individual surcharge ladder is nil / 10% / 15% / 25% / 37%. Under
the new regime in §115BAC the highest surcharge is capped at 25% for a resident individual.
Whether that cap reaches a non-resident's rental income, and whether it reaches TDS at all
as opposed to assessed tax, decides a 12-point swing on a landlord earning over ₹5 crore.

Rare, and enormous when it happens. The row is `warn` and must stay so until this is
answered.

### 3.3 Whether surcharge and cess stack on a §197 certificate rate

ADR-0025 §1 takes the position that a certificate states the rate to deduct at, so nothing
is added on top. The alternative reading — that the certificate fixes the base rate and
surcharge and cess apply to it — produces a materially higher deduction on exactly the
landlords who went to the trouble of obtaining a certificate.

Both readings are held by practitioners. The certificate's own wording usually settles it
per certificate, which suggests the model may eventually need a flag on the row rather than
a global rule.

### 3.4 Whether maintenance and amenity charges are "rent"

Most Indian residential lets bill rent and maintenance separately, often to different
entities. If maintenance is rent for §194-I and §194-IB, the thresholds are crossed
earlier and by more tenancies than the platform currently thinks; if it is not, splitting
the bill is a lawful way under the threshold and the product should say so.

This one affects the **threshold test**, not the rate, and therefore decides whether TDS
applies at all for a large band of ordinary tenancies. It is the highest-value question on
this page.

---

## 4. What is not modelled at all

| Gap | Why it matters | Where it would go |
|---|---|---|
| §194-IB's proviso capping a §206AA deduction at the last month's rent | Without it, a no-PAN landlord's final-month deduction can exceed the payment | An amount rule, so in whatever computes the deduction — not in the rate resolution |
| Aggregation across buyers and sellers for §194-IA | The 1 Oct 2024 amendment is the reason a ₹50 lakh threshold is not a per-party threshold | Beside the threshold row, as a rule about how it is tested |
| Lower deduction certificates under §195(2)/(3) as distinct from §197 | A different application route to the same outcome | The `tds_certificates` table already fits; only the vocabulary would widen |
| Grossing up under §195A | Where the tenant bears the tax, the deduction is computed on the grossed-up amount | The money module, and it changes the base, not the rate |
| Interest under §201(1A) for late deduction or deposit | The actual cost of getting any of this wrong | Its own story; nothing computes it today |

---

## 5. What a reviewer should produce

For each row in §2: the section as it stands on the date checked, the amending Act and its
commencement clause, and either a confirmation or a proposed correction. A correction is an
ADR-0008 correction — it retires the wrong row and replaces it, so a deduction already made
under the wrong number can still say which row it used.

Then, and only then, `verification_status = 'verified'` with `verified_by` and
`verified_on`, and a per-row decision on whether it may move to `enforcement = 'block'`.
The schema refuses `block` on an unverified row, which is the whole reason this document
exists rather than a ticket saying "check the rates".
