# India — payments, documents, tax and regulatory model

What Dwellm8 must implement because of where it operates. Every rule below is
**state- and date-scoped in code**: rates, thresholds and slabs live in versioned rule
tables with an owner and a review date, never as constants in a service.

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
| **Card / netbanking** | Fallback | Rare for rent; keep enabled, do not optimise |
| **Offline (cash / IMPS / NEFT)** | The majority of Indian rent today | Recorded with evidence, produces a real receipt |

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

### eSign

- Aadhaar-based eSign through a licensed ESP (via an authorised gateway), producing a
  digitally signed PDF with a completion certificate and an audit trail (signer, time,
  IP, consent artefact). The audit trail is part of the record, not a by-product.
- Aadhaar-based eSign uses the number transiently in the ESP flow; **Dwellm8 never stores
  the Aadhaar number** — only the ESP's transaction reference and the signed artefact.
- A non-Aadhaar eSign path (OTP/email-verified electronic signature) must exist for
  signers who decline Aadhaar, with its weaker evidentiary weight recorded on the lease.

### Registration

Where registration is required (12+ month terms, and commercial leases in several
states), the platform produces the document set, computes duty and registration fee,
tracks the sub-registrar appointment, and stores the registered instrument. This is
MVP 5 work; before then the platform declines to generate the long lease rather than
generating a defective one.

### Model Tenancy Act 2021

Adopted state by state. Where adopted, it constrains security deposits (commonly two
months' rent for residential), mandates written agreements filed with a Rent Authority,
and defines notice and eviction process. Modelled as a per-state feature flag with the
deposit cap enforced in the lease builder, not as a warning banner.

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

| Section | Deductor | Trigger | Rate | Deposit / certificate |
|---|---|---|---|---|
| **§194-I** | Business/company tenants and other non-individual deductors | Annual rent above the section threshold | Standard rate for land/building rent | Monthly challan, quarterly TDS return, Form 16A |
| **§194-IB** | Individuals/HUF not liable to tax audit | Monthly rent above the section threshold | Section rate on the year's/lease's rent | Form 26QC challan-cum-statement, Form 16C to the landlord |
| **§195** | Any tenant paying a non-resident landlord (NRI) | Any rent | Rates per Act/DTAA, no small-value exemption | Form 15CA/15CB, Form 16A |

Product requirements:

- The lease captures **deductor class** (individual vs business) and **landlord residency**
  at creation, because those two facts alone determine the entire TDS path — and getting
  them at settlement time is too late.
- NRI landlords are a distinct, high-risk flow: TDS applies from the first rupee, the
  tenant is liable for failure, and this is where an unaware tenant is most exposed. The
  platform must surface the obligation prominently at lease creation.
- Deducted TDS is a ledger posting against `tds_receivable` on the owner side, reflected
  in the owner statement so the payout reconciles.
- The platform generates reminders and the required forms/references, tracks challan and
  certificate receipt, and never files on the user's behalf without explicit authority.
- The obligation stays with the deductor. Dwellm8's role is documented as facilitation,
  in the terms of service and in the UI.

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
  states this explicitly rather than failing silently. The retention matrix is a document
  the backlog owns.
- Children's data and verifiable-consent provisions are avoided by not onboarding minors
  as account holders; occupants who are minors are recorded with the minimum data.
- Breach notification runbook with defined timelines, owners and communication templates.

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
