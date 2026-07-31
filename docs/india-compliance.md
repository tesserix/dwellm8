# India — payments, documents, tax and regulatory model

What Dwellm8 must implement because of where it operates. Every rule below is
**state- and date-scoped in code**: rates, thresholds and slabs live in versioned rule
tables with an owner and a review date, never as constants in a service — that is
`statutory_rules`, decided in [ADR-0023](adr/0023-statutory-rule-tables.md).

**Scope.** This document is the operating detail for a **residential tenancy**. Other
asset classes (commercial, hostel/PG, short-stay), other transactions (buying, selling,
investing), the obligations that bind Dwellm8 itself, and the rule-registry governance
model are in [`india-property-compliance.md`](india-property-compliance.md).

> **Standing rule for the backlog.** No figure in this document is authoritative for
> production. Each rule table entry must cite its source and be confirmed with counsel or
> a practising CA before the feature that depends on it ships. Tax rates, GST slabs and
> statutory thresholds change with every Budget and every GST Council meeting.

---

## 1. Payments

### UPI — the default rail

| Flow | Use | Notes |
|---|---|---|
| **UPI intent / QR** | One-off rent, dues, ad-hoc charges | Fastest path to first payment; no mandate setup |
| **UPI collect** | Reminder-driven pull | Request lands in the payer's UPI app; expiry handling required |
| **UPI Autopay (e-mandate)** | Recurring rent and society dues | Per-mandate transaction cap applies; pre-debit notification is mandatory |
| **NACH e-mandate** | High-value rent above the UPI Autopay cap, commercial leases | Slower onboarding, bank-dependent |
| **Netbanking** | Fallback for one-off high value | MDR is a disclosed pass-through, never absorbed silently |
| **Card / EMI** | Platform billing, deposits, brokerage — **not rent** | RBI's 2025 PA Directions bar a PA from aggregating card funds for a non-merchant landlord |
| **Offline (cash / IMPS / NEFT)** | The majority of Indian rent today | Recorded with evidence, produces a real receipt |

Rail selection by rent amount, the AFA and non-peak-window rules, the full method
matrix and the Cashfree/Razorpay comparison are in
[`payment-rails.md`](payment-rails.md).

**Mandate lifecycle** is modelled explicitly: `created → pending_approval → active →
paused → revoked | expired`, with debit attempts scheduled against it. The
**pre-debit notification** obligation (notify the payer ahead of each debit) is tracked
work with its own delivery record — a missed notification is a compliance failure, not a
missed message.

**Autopay is never mandatory.** A tenant must always be able to pay a single invoice
without a standing mandate.

### Provider posture

- Razorpay is the primary PA/PG. The adapter is written so a second provider can be added
  without domain changes, mirroring the email-provider registry pattern used elsewhere in
  this org.
- Dwellm8 is **not** an escrow or a custodian of client funds. Money settles owner-side
  through the regulated aggregator. Any design that has Dwellm8 holding rent overnight in
  a platform-owned account is out of scope and must be escalated, not implemented.
- Payouts to owners use the provider's payout rails with the platform's fee, GST and TDS
  deducted as ledger postings, and a statement that reconciles to the paisa.

### Failure handling

- Every webhook idempotent on provider event id, replay-safe, and advisory only — payouts
  release on verified state plus settlement match.
- Mandate debit failures follow a retry policy that respects the rail's rules and never
  double-debits.
- Settlement reconciliation runs nightly; drift raises an operational alert.

---

## 2. Rental agreements

### The 11-month convention

Indian residential leases are conventionally written for 11 months because a lease of
12 months or more attracts compulsory registration under the Registration Act with
materially higher stamp duty and a registrar appointment.

**Product rule:** the agreement builder defaults to 11 months. Any term of 12 months or
more switches the flow to the registration path — it never silently produces a long
unregistered lease. Renewal, not extension, is the default motion.

**The convention is state-scoped, not national.** Maharashtra requires registration of a
tenancy at any tenure (Rent Control Act 1999, s.55), so 11 months buys nothing there.
"Does a term under 12 months avoid registration in this state?" is a per-state flag beside
the deposit cap — see [`e-stamping-by-state.md`](e-stamping-by-state.md).

### Stamp duty and e-stamping

- Stamp duty on a lease is a **state subject**: the base, the slab and the treatment of
  the security deposit differ by state, and several states compute duty on average annual
  rent plus a deposit component.
- Implementation: a `stamp_duty_rule` table keyed by `(state, instrument_type,
  effective_from)` returning a computation expression, with the computed amount stored on
  the agreement and re-validated immediately before stamping.
- E-stamping is delivered through the authorised channel for the state (SHCIL/ e-stamp
  vendors where available, state portals elsewhere). The certificate identifier is stored
  on the agreement; a signed agreement with no stamp evidence is an invalid state.
- No state exposes an e-stamping API to us; every digital path runs through an aggregator
  holding SHCIL ACC standing. Channel, duty base and launch verdict per state:
  [`e-stamping-by-state.md`](e-stamping-by-state.md).

### eSign

- Aadhaar-based eSign through a licensed ESP (via an authorised gateway), producing a
  digitally signed PDF with a completion certificate and an audit trail (signer, time,
  IP, consent artefact). The audit trail is part of the record, not a by-product.
- Aadhaar-based eSign uses the number transiently in the ESP flow; **Dwellm8 never stores
  the Aadhaar number** — only the ESP's transaction reference and the signed artefact.
- The non-Aadhaar path is **not** automatically weaker. eSign 3.x issues the same
  Second Schedule certificate from a bank, PAN or offline-Aadhaar eKYC account; a plain
  OTP/email-verified electronic signature is the third choice, not the second, and its
  weaker evidentiary weight is recorded on the lease. Tiers, artefacts, boundaries and
  provider route: [`esign-provider-selection.md`](esign-provider-selection.md).

### Registration

Where registration is required (12+ month terms, and commercial leases in several
states), the platform produces the document set, computes duty and registration fee,
tracks the sub-registrar appointment, and stores the registered instrument. This is
MVP 5 work; before then the platform declines to generate the long lease rather than
generating a defective one.

### Model Tenancy Act 2021

Adopted state by state. Where adopted, it mandates written agreements filed with a Rent
Authority, defines notice and eviction process, and caps the security deposit at **two
months' rent for residential and six months' for non-residential** premises. Modelled as
a per-state feature flag with the cap enforced in the lease builder, not as a warning
banner.

### Security deposit and advance rent — a state-scoped rule table

The cap is state law, not national, and the states that matter most to us disagree with
each other and with market practice. Bengaluru's ten-month deposit is the extreme case:
lawful under the Karnataka Rent Act 2001, and reportedly cut to two months by a 2025
amendment. A platform that hard-codes any single number is wrong in half the country
within a year of shipping.

So it is a versioned, state-scoped rule table — `state`, `use` (residential /
non-residential), `max_deposit_months`, `max_advance_rent_months`, `statute`,
`effective_from`, `verified_against_bare_act`, owner, review date — and the lease
builder reads it. Same construction as the rail rule table in
[`payment-rails.md`](payment-rails.md), for the same reason.

| Jurisdiction | Residential | Non-residential | Statute | Status |
|---|---|---|---|---|
| Model Tenancy Act baseline | 2 months | 6 months | MTA 2021, s.11 | Verified |
| Maharashtra | 2 months | 6 months | Maharashtra Rent Control Act 1999, s.56 | Needs bare-act check |
| Karnataka (pre-amendment) | up to 10 months | up to 10 months | Karnataka Rent Act 2001 | Needs bare-act check |
| Karnataka (from Jan 2026) | 2 months | — | Karnataka Rent (Amendment) Act 2025 | **Unverified** — secondary sources only |
| Tamil Nadu | 3 months (some sources say 1) | 6 months | TN Landlords and Tenants Act 2017 | **Conflicting** — resolve before use |
| Elsewhere | MTA baseline where adopted, else no statutory cap | | | |

Every row marked unverified or conflicting must be checked against the bare act and
signed off by legal before it gates anything a user sees. Until then the builder warns
and does not block, and the row records that it is doing so — a cap enforced from a blog
post is worse than no cap, because it is wrong with authority.

**Advance rent is not the deposit** and is capped separately (or prohibited) in several
states; the table carries both columns so the lease builder cannot conflate them. Where
a statute caps the deposit and the parties want more, the excess is not recorded as a
deposit under another name.

### The platform bound: two months minimum, three months maximum

On top of the statutory ceiling, Dwellm8 sets its own bound on the Advance, everywhere:

```
min 2 months' rent   ·   max 3 months' rent
```

The two rules compose, and the statute always wins:

```
effective_max = min(3, statutory_max)        where the state row is verified
effective_min = min(2, effective_max)        a floor may never exceed a lawful ceiling
```

So a Model Tenancy Act state permits exactly two months — the platform maximum of three
is unreachable there, and the builder must not offer it. Three is reachable only where
the statute allows three or more, or where no statutory cap applies. The floor collapses
with the ceiling rather than fighting it: a state verified at one month yields one, not
an unlawful two.

Two consequences worth stating rather than discovering:

- **The bound is a product decision, not a legal one**, so it lives beside the statutory
  table with its own owner and review date — not inside it. Nothing should read the
  platform floor and conclude a statute requires it.
- **Bengaluru is where it bites.** Ten months is lawful under the 2001 Act and normal in
  practice; capping at three is a deliberate refusal of the local norm. That is a
  defensible tenant-side position and it will cost listings, so it belongs to product
  with eyes open rather than being smuggled in as a validation rule.

### Where the money goes, and what it is called

**The Advance settles to the owner's own account.** Dwellm8 does not hold it, pool it, or
earn on the float; the owner is the beneficiary of record and the platform's role ends at
routing and recording. That keeps us clear of holding customer funds we have no licence
to hold, and it means the owner — not the platform — carries the refund obligation at
move-out.

Two things follow directly. The owner must be onboarded as a settlement beneficiary
before an Advance can be collected at all, which makes beneficiary onboarding a
precondition of the move-in flow rather than a payout-time concern
([#79](https://github.com/tesserix/dwellm8/issues/79)). And the aggregator's merchant
rules apply to that beneficiary, which is the same constraint that closed the card rail
in [`payment-rails.md`](payment-rails.md) §1.

**"Advance" is the label; the ledger decides the substance.** ADR-0006 already has two
accounts and they are not interchangeable:

| The money is | Account | Tax treatment |
|---|---|---|
| Refundable at move-out, net of damages | `deposit_liability` | Not income on receipt; no TDS on receipt |
| Adjustable against future rent months | `tenant_advance` | Rent in the year of receipt; TDS under 194-I / 194-IB applies |

The Advance is a **refundable amount and posts to `deposit_liability`**, following the
South Indian usage the word comes from. It is never revenue and never offsets rent by
default.

The conversion is the part that goes wrong. Where the parties agree to adjust the Advance
against the final months' rent — a common arrangement, and one the lease builder must
capture explicitly rather than infer — the adjusted portion moves from
`deposit_liability` to `tenant_advance` **at the point of adjustment**, and the rent and
TDS consequences attach from that date. A platform that lets a tenancy quietly consume
its deposit as rent without that posting has understated the owner's income and missed a
TDS deduction, which is a real liability and not a reporting nit.

**The Advance is a lump sum.** It is a single payment at move-in, not financeable into
instalments by this platform, and no collection flow may present it as one. A third-party
deposit-alternative product ([#109](https://github.com/tesserix/dwellm8/issues/109)) is a
different thing entirely: a lender pays the owner the full sum and the tenant repays the
lender. That repayment is loan servicing between tenant and lender, and Dwellm8 neither
collects it nor books it as rent.

---

## 3. Tenant verification & KYC

| Check | Mechanism | Stored |
|---|---|---|
| Identity | DigiLocker-issued documents or an authorised KYC provider | Verification result, masked reference, timestamp, provider txn id |
| PAN | Provider verification against name | Result + masked PAN (encrypted), never plaintext |
| Employment / income | Uploaded documents, optional bank-statement analysis with consent | Assessment output, documents in GCS with restricted access |
| Police tenant verification | State portal where one exists; generated statutory form where it does not | Submission reference, acknowledgement, status |
| Previous-landlord reference | In-platform reference request | Response record with consent |

**Hard rules**

- Aadhaar numbers are never stored, logged, indexed or sent to analytics. Verification
  runs through authorised flows and only the outcome persists.
- Every check requires an explicit, recorded consent artefact from the data principal
  naming the purpose.
- Screening outputs are advisory to the owner. Dwellm8 does not publish a tenant score,
  does not maintain a cross-owner blacklist, and does not let one owner's dispute follow a
  tenant across the platform. This is both a DPDP exposure and a product-integrity line.
- Police verification is an owner obligation in several states; the platform tracks it as
  a compliance task with reminders and evidence, and states plainly that it is the owner's
  legal duty.

---

## 4. GST

| Situation | Treatment (verify before shipping) |
|---|---|
| Residential rent to an unregistered individual | Exempt |
| Residential dwelling rented to a **registered** person | Reverse charge on the recipient in defined cases |
| Commercial rent by a registered landlord | Taxable at the standard rate on the rent |
| Commercial rent by an unregistered landlord to a registered tenant | Reverse charge on the tenant in defined cases |
| Property management service fee | Taxable at the standard service rate |
| RWA maintenance charges | Exempt up to a per-member monthly threshold; the RWA's aggregate-turnover registration threshold also applies |
| Platform subscription / SaaS | Taxable at the standard service rate |

Implementation requirements:

- GSTIN capture and validation for organisations, owners and vendors; place-of-supply
  derivation for CGST/SGST vs IGST.
- Tax-invoice generation with the mandated fields, sequential per-GSTIN numbering, and
  credit notes for adjustments — the invoice series may never have a gap.
- The RWA per-member threshold is evaluated **per member per month**, correctly handling
  members owning multiple units, which is the classic implementation error.
- Reverse-charge cases produce the correct document and a clear statement of who pays,
  because the platform is often the only party that understands the rule.
- E-invoicing and e-way concerns do not apply to rent but do apply to some vendor jobs
  above threshold; the vendor module must not assume otherwise.

---

## 5. TDS on rent

Specified as a decision matrix in [ADR-0024](adr/0024-tds-decision-matrix.md) and
implemented in `internal/platform/statutory/tds`. Rates and thresholds come from the
registry ([ADR-0023](adr/0023-statutory-rule-tables.md)), never from this page.

| Section | Deductor | Threshold | Timing | TAN | Artefacts |
|---|---|---|---|---|---|
| **§194-I** | Anyone not on the row below — companies, firms, LLPs, government, and individuals/HUF liable to audit under §44AB | The year's rent to that landlord, exceeded | Every payment or credit | Yes | Challan (or Form 24G for government), quarterly 26Q, Form 16A |
| **§194-IB** | An individual or HUF **not** liable to tax audit | The month's rent, exceeded | **Once**, at year end or lease end, on the whole period's rent | No | Form 26QC challan-cum-statement, Form 16C |
| **§195** | Anyone paying a **non-resident** landlord | **None** — from the first rupee | Every payment or credit | Yes | Challan, quarterly **27Q**, Form 16A, Forms 15CA/15CB |

The line between the first two is audit liability under §44AB, not "individual versus
company": a salaried tenant and a doctor with a large practice are both individuals and
are not on the same path. Residency is tested first and overrides both — a non-resident
landlord puts every deductor on §195.

Product requirements:

- The lease captures **deductor class** and **landlord residency** before the tenancy
  starts, because those two facts determine the entire TDS path and discovering them at
  the first payout means months of deduction, interest under §201(1A) and penalty.
- Both are **effective-dated**. A landlord who moves abroad in October was a resident in
  April, and April's deduction is not restated by what happened in October.
- NRI landlords are a distinct, high-risk flow: TDS applies from the first rupee and the
  tenant carries the liability. The lease cannot be completed until the deductor has
  acknowledged it — enforced in the domain and by a deferred constraint trigger.
- **There is no §195 rate in the registry, deliberately.** It is the Act's or a treaty's,
  read with the landlord's tax residency certificate. The platform names the section and
  the exposure and refuses to compute a deduction against a number nobody chose.
- Joint owners are assessed **per payee**, and only on an exact split — shares totalling
  10,000 basis points and amounts totalling the rent to the paisa. The §194-I threshold
  is per landlord across their whole portfolio, not per property.
- Deducted TDS is a ledger posting against `tds_receivable` on the owner side, reflected
  in the owner statement so the payout reconciles.
- The platform generates reminders and the required forms/references, tracks challan and
  certificate receipt, and never files on the user's behalf without explicit authority.
- The obligation stays with the deductor. Dwellm8's role is documented as facilitation,
  in the terms of service and in the UI, in one set of words.

The rate deducted is **not** the section's rate. Four provisions move it, all turning on a
fact about the landlord rather than the payment — [ADR-0025](adr/0025-payee-level-rate-overrides.md):

| Provision | Turns on | Effect |
|---|---|---|
| **§197** | A certificate the Assessing Officer issued | Replaces the rate, downwards, possibly to nil. This is what makes a §195 deduction computable |
| **§206AA** | Whether a PAN has been furnished | Floors it at 20%. Rule 37BC exempts a non-resident who furnishes TRC, TIN and contact details |
| **§206AB** | Being a specified non-filer | Floored at twice the rate or 5% — and **the section was omitted from 1 October 2024**, so it applies only to deductions before then |
| **Surcharge and cess** | Being a non-resident, and how much they receive | Raises it: 10% at ₹80,000 a month becomes 11.44% for an NRI over the surcharge threshold |

A payee whose PAN status is unknown is deducted at the §206AA floor, not at the section
rate: an over-deduction is a refund claim for the landlord, an under-deduction is interest
and penalty for the tenant.

Open questions the platform has taken a position on, and the rows that are not yet verified
against a bare act, are in [`tds-rate-verification.md`](tds-rate-verification.md).

---

## 6. Data protection (DPDP Act 2023)

- Purpose-limited collection with a plain-language notice, in English and at minimum
  Hindi, at every collection point.
- Consent records are objects — purpose, timestamp, version of the notice, and a working
  withdrawal path.
- Data-principal rights implemented as endpoints: access, correction, erasure, grievance
  routing to a named contact with a tracked SLA.
- Erasure must be reconcilable with statutory retention: financial records, tax documents
  and executed agreements are retained for their statutory period, and the erasure flow
  states this explicitly rather than failing silently.

The posture is decided in [ADR-0026](adr/0026-dpdp-posture.md) and implemented in
`internal/platform/dpdp`; the periods are in [`data-retention.md`](data-retention.md), and
a contract test fails the build if that document and the code disagree. Three things to
know before reading either:

- **Not everything runs on consent.** Rent is processed to perform the agreement and a TDS
  deduction because the Act requires it, so withdrawing consent does not stop them — and
  the product says so rather than accepting the withdrawal in silence.
- **Erasure has three answers**: erase, retain with the statute and the expiry named, or
  defer because something is unresolved. An open dispute, unsettled money, or a Form 16A
  or 16C the landlord is still owed each defer, and the deferral names what is blocking it.
- **A request is scoped to one organisation.** The same person is a tenant of one and an
  owner in another, and reaching across would be a tenancy breach dressed as a privacy
  feature.
- Children's data and verifiable-consent provisions are avoided by not onboarding minors
  as account holders; occupants who are minors are recorded with the minimum data.
- Breach notification runbook with defined timelines: [`breach-runbook.md`](breach-runbook.md).
  Every breach is notifiable under §8(6) — there is no severity threshold — and CERT-In's
  clock is 6 hours. The owners and the grievance SLA are named as outstanding there, because
  they are people to appoint rather than decisions to make.

---

## 7. Other regulators to keep in view

| Regime | Relevance |
|---|---|
| **RERA** | Applies to listings and promoter/agent conduct. If Dwellm8 lists or brokers, agent registration and advertising rules bind it. Claims in listings are a compliance surface. |
| **State Rent Control Acts** | Legacy tenancy protections still bind older tenancies in several states; the lease builder must not assume Model Tenancy Act rules everywhere. |
| **RBI PA/PG directions** | Bind the payment aggregator, and by extension how Dwellm8 may hold or route funds. The "no escrow, no custody" rule above exists because of this. |
| **Shops & Establishments, municipal rules** | Commercial leases carry local obligations; commercial property is deliberately deferred to MVP 6. |
| **Society bye-laws & state Co-op Acts** | Govern dues, interest on arrears, elections and audit for RWAs; these are per-society configuration, not platform constants. |

---

## 8. What this means for engineering

Every issue touching money, documents or identity must answer:

1. Which state's rule applies, and where is that rule stored and versioned?
2. What happens when the rule changes — is the historical computation preserved?
3. Who is legally obliged here, and does the UI say so plainly?
4. What identifier is being stored, and is it allowed to be stored at all?
5. Where is the evidence — challan, certificate, audit trail, acknowledgement — and is it
   retrievable years later?

A feature that cannot answer all five is not ready, regardless of test coverage.

Questions 1 and 2 have a standing answer:
[ADR-0023](adr/0023-statutory-rule-tables.md). The rule lives in
`statutory_rules`, scoped to a state and a date, and the computation records the
row it used.
