# India — compliance and governance across asset classes and transactions

The regulatory surface Dwellm8 stands on, and the machinery that keeps it
current. Two axes: **what kind of property** (residential, commercial, hostel/PG,
short-stay, land) and **what kind of transaction** (let, manage, buy, sell,
invest). Each cell has a different regulator, a different tax treatment and a
different set of things that make the platform liable rather than the user.

**Scope split.** [`india-compliance.md`](india-compliance.md) is the operating
detail for a residential tenancy — payments, agreements, KYC, GST, TDS on rent,
DPDP. This document is the map around it and the governance that holds it
together. Nothing is restated across the two; where a rule already lives there,
this document points at it.

**Reviewed**: 2026-07-31 · **Owner**: Compliance · **Next review**: 2026-10-31

> **Standing rule, inherited from [`india-compliance.md`](india-compliance.md).**
> No figure here is authoritative for production. Every rate, slab and threshold
> is confirmed with counsel or a practising CA before the feature depending on it
> ships, and lives in a versioned rule table — never as a constant in a service.
> Sources for this document are secondary; §11 lists what must be read against a
> bare Act.

---

## 1. The governance model

Compliance content decays. A platform that encodes it as code, constants or prose
is wrong within a year and does not know it. So every statutory parameter Dwellm8
depends on is a **row in a rule registry**, not a line in a service.

The registry is built. [ADR-0023](adr/0023-statutory-rule-tables.md) is the
decision, `statutory_rules` and `statutory_rule_slabs` are the tables, and
`internal/platform/statutory` resolves against them. What follows describes the
columns; the ADR describes what the database refuses.

The first consumer is the TDS decision matrix
([ADR-0024](adr/0024-tds-decision-matrix.md)), which is also the first place the
registry's deliberate silence matters: there is no §195 rate row, so the platform
names the section, names the exposure, and refuses to compute a deduction.

### 1.1 The registry entry

Every rule table in the platform — stamp duty, deposit caps, GST rates, TDS
thresholds, registration triggers — carries the same governance columns beside
its domain columns:

| Column | Purpose |
|---|---|
| `jurisdiction` | State, UT, or `IN` for central |
| `effective_from` / `effective_to` | Effective-dated per [ADR-0008](adr/0008-effective-dating-and-temporal-queries.md); rows are never updated in place |
| `statute_ref` | Act, section, notification or circular number — a URL is not a citation |
| `verification_status` | `verified` \| `needs_bare_act_check` \| `unverified` \| `conflicting` |
| `verified_by` / `verified_on` | A named human, with a date |
| `owner` | The team accountable for the row |
| `review_due` | The date it must be looked at again |
| `enforcement` | `block` \| `warn` \| `record_only` — what the product does with it |

`verification_status` and `enforcement` are the two that matter and they interact:
**an unverified row may never carry `enforcement = block`.** A cap enforced from a
blog post is worse than no cap, because it is wrong with authority. This is
already the stated position on deposit caps in
[`india-compliance.md`](india-compliance.md) §2; it generalises to every table.

### 1.2 Change control

- A rate change is a **new row with a new `effective_from`**, never an edit. Any
  computation already performed keeps resolving against the row that was in force
  when it happened — a tax computation from March must still reproduce in
  November.
- The historical computation is **stored on the artefact** alongside the rule
  version that produced it, not recomputed on read.
- Rule changes go through the same review as code, because they are code. A
  change with no `statute_ref` and no `verified_by` does not merge.

### 1.3 Review cadence

| Trigger | Action |
|---|---|
| Union Budget (February) | Full review of income-tax rows — TDS thresholds, capital gains, exemption caps |
| GST Council meeting | Review of GST rows within 30 days of the notification, not the press release |
| State budget | Review of that state's stamp duty and registration rows |
| Quarterly | Every row whose `review_due` has passed; overdue rows raise an alert |

**A notification, not a news report, moves a row.** GST Council decisions are
routinely reported before they are notified and occasionally differ when they
arrive. The registry tracks the notification.

### 1.4 The five questions

Every issue touching money, documents or identity answers the five questions in
[`india-compliance.md`](india-compliance.md) §8. This document adds a sixth for
anything that crosses asset classes:

> **6. Which asset class is this, who decided that, and what happens when it
> changes?** A residential flat let to a company for staff housing, a flat run as
> a PG, and a flat on a 20-day booking are three different tax and licensing
> regimes on one property record. The class is an explicit, effective-dated
> attribute of the tenancy — never inferred from the rent amount or the term.

---

## 2. The matrix

What binds, per cell. `—` means the cell is out of Dwellm8's scope entirely.

| | Residential | Commercial | Hostel / PG / co-living | Short-stay |
|---|---|---|---|---|
| **Let / rent** | §3 | §4 | §5 | §6 |
| **Manage for an owner** | §3 | §4 | §5 | §6 |
| **Buy / sell** | §7 | §7 | §7 | §7 |
| **Invest / hold** | §8 | §8 | §8 | §8 |
| **Primary regulator** | State rent law, Registration Act | Same + Shops & Establishments | State PG/hostel rules, municipal | State tourism, municipal |
| **GST on the occupancy charge** | Exempt to an unregistered individual | Standard rate, taxable | Exempt under the ₹20,000 / 90-day entry, else taxable | Taxable, rate by tariff band |
| **TDS on the payment** | 194-I / 194-IB / 195 | 194-I / 195 | 194-I / 194-IB | Not rent — a service |
| **Registration of the instrument** | 12 months+ (state-scoped) | Usually, often shorter trigger | Rarely — a licence, not a lease | No — a booking |
| **The occupier's legal status** | Tenant, with statutory protection | Tenant / licensee | **Licensee, not tenant** | Guest |

The bottom row is the one that breaks products. A PG resident and a short-stay
guest are **licensees or guests, not tenants**: they do not acquire tenancy
protection, the lease lifecycle does not apply to them, and a platform that
models them as tenants has both over-promised rights to the occupier and
mis-stated the owner's position. [ADR-0010](adr/0010-lease-lifecycle-state-machine.md)
covers leases; hostel and short-stay occupancy need their own state machine or an
explicit decision that they reuse it with a different instrument type.

---

## 3. Residential tenancy

Fully covered in [`india-compliance.md`](india-compliance.md) — the 11-month
convention and its state-scoped exceptions, stamp duty and e-stamping
([`e-stamping-by-state.md`](e-stamping-by-state.md)), eSign, Model Tenancy Act
deposit caps, tenant KYC and police verification, GST, TDS on rent, DPDP.

The only addition here is the class boundary: **a residential unit let to a
company for staff occupation is still residential for rent-law purposes but is
not automatically GST-exempt.** The exemption turns on the recipient's
registration status, not the building's use. The lease must capture the
recipient's GSTIN and registration status, because that single fact decides
whether reverse charge applies.

---

## 4. Commercial leasing

**Deferred to MVP 6** and the reasons are worth recording, because commercial is
not "residential with bigger numbers".

| Dimension | How it differs |
|---|---|
| **GST** | The standard rate applies on rent where the landlord is registered; reverse charge applies in defined cases where the landlord is not and the tenant is. Rent is a taxable supply by default, not an exempt one |
| **TDS** | §194-I, deducted by the tenant, at the land-and-building rate; the tenant is nearly always a business, so §194-IB rarely applies |
| **Registration** | Commercial leases attract compulsory registration in more states and at shorter terms than the residential 11-month convention |
| **Deposit** | Six months under the Model Tenancy Act baseline for non-residential — three times the residential cap |
| **Instrument** | Lock-in, escalation clauses, CAM and outgoings, fit-out periods, rent-free windows. Each is a distinct posting stream, not a note in the agreement |
| **Local obligations** | Shops & Establishments registration, trade licence, fire NOC, change-of-use approval. These belong to the occupier, and the platform tracks them as compliance tasks |

**The engineering consequence** is that CAM, escalation and rent-free periods
cannot be bolted onto a residential rent schedule. Each is an effective-dated
term producing its own ledger postings ([ADR-0006](adr/0006-chart-of-accounts-and-posting-rules.md)),
and a rent-free month is a real month with a zero charge, not a missing month.

---

## 5. Hostel, PG and co-living

The most misunderstood cell in the matrix, and the one where a platform most
easily creates liability for its users.

### 5.1 GST — the ₹20,000 / 90-day exemption

Entry 12AA of Notification 12/2017-CT(R), inserted by Notification 04/2024 with
effect from **15 July 2024**, exempts accommodation services where **both**
conditions hold:

```
value ≤ ₹20,000 per person per month
     AND
minimum continuous stay of 90 days
```

Circular 228/22/2024-GST regularised the period 1 July 2017 to 14 July 2024 on an
as-is-where-is basis for supplies meeting the same test — which matters, because
before this the AAR rulings taxing PG accommodation at 12% or 18% were a live
exposure for every operator on the platform.

Three implementation traps, all of which produce a wrong invoice:

1. **Per person, not per room.** A ₹36,000 triple-sharing room at ₹12,000 a head
   is exempt. A `rent_per_unit` model cannot evaluate the test at all — the
   occupancy charge must be per occupant.
2. **Ninety days is a minimum *continuous* stay**, so the test depends on the
   agreed term and is falsifiable by what actually happens. A resident who leaves
   on day 60 of a 6-month licence has taken a supply that may not qualify, and
   the platform must at minimum flag it rather than silently keep the exemption.
3. **Both limbs, or neither.** ₹22,000 a month for a year is taxable; ₹15,000 a
   month for 60 days is taxable. Two booleans, evaluated together.

### 5.2 Everything that is not GST

- **The resident is a licensee.** No tenancy protection, no rent-control
  application, no registered lease — a leave-and-licence or a house-rules
  agreement. Modelling a PG bed as a tenancy is a legal misstatement.
- **State PG regulation** — registration with the local body, per-room occupancy
  limits, minimum facilities, and in several cities a police clearance for the
  operator. This is city-level, not just state-level.
- **Fire safety NOC** and building-use approval. A residential building operated
  as a hostel above a threshold occupancy needs a change-of-use consent that most
  operators do not have.
- **FSSAI registration** where meals are served — which is most PGs.
- **Shops & Establishments registration** for the operating entity.
- **Police verification of residents**, an owner/operator obligation in several
  states, tracked as a compliance task with evidence per
  [`india-compliance.md`](india-compliance.md) §3.

### 5.3 What the platform must model

Bed-level inventory (not unit-level), per-occupant charges, sharing type,
lock-in and notice in days rather than months, a joining-and-leaving flow that is
not a lease lifecycle, and a compliance-task set per property that is an operator
checklist with expiry dates on every licence. Deposits still post to
`deposit_liability`; nothing about the ledger changes.

---

## 6. Short-stay, serviced apartments and homestays

Not rent at all. This is a **supply of accommodation service**, and treating it as
tenancy is wrong in every dimension — tax, licensing, and the occupier's rights.

### 6.1 GST

| Value of supply | Rate | ITC |
|---|---|---|
| ≤ ₹7,500 per unit per day | 5% | **No ITC** — inputs must be reversed, treated like an exempt supply for ITC purposes |
| > ₹7,500 per unit per day | 18% | ITC available |

Two changes fold into this and both matter for how the rule table is keyed. From
**1 April 2025**, the "declared tariff" concept was removed and replaced by
"specified premises", assessed on the **value of accommodation supplied in the
previous financial year** — so the applicable regime for restaurant services in a
property depends on last year's tariffs, not today's booking. From **22 September
2025**, the 56th Council's rationalisation cut the ≤ ₹7,500 band from 12% to 5%,
mandatory and without ITC.

The engineering consequence: the rate is a function of `(value_per_unit_per_day,
financial_year)`, and the previous-year assessment is a **stored annual
determination on the property**, not a computation over the bookings table at
invoice time.

### 6.2 Everything else

- **State tourism / homestay registration** in most states, with its own
  categories and renewal cycle.
- **Municipal permission and change of use.** Running a flat as short-stay
  accommodation is a commercial use of residential premises; several cities
  restrict or prohibit it outright.
- **Society bye-laws.** Housing societies commonly bar short-let, and this is the
  most frequent practical blocker — enforceable, and entirely invisible to a
  platform that only reads the state rules.
- **Guest registration and identity records**, including the separate regime for
  foreign nationals.
- **No TDS on rent**, because it is not rent. Consider instead whether Dwellm8
  itself becomes an **e-commerce operator** for the supply — §9.3.

**Product position:** short-stay is not an MVP asset class. If it ships, the
listing flow must capture society permission and municipal consent as blocking
evidence, not as a checkbox, because the platform is the party that made the
letting easy.

---

## 7. Buying and selling

A conveyance is a different product from a tenancy, with a much heavier
compliance load and a much larger downside per transaction.

### 7.1 The transaction spine

| Stage | Obligation | Notes |
|---|---|---|
| Title diligence | Encumbrance certificate, mother deed chain, approvals, litigation search | Not a platform assertion — a professional opinion, attributed |
| Agreement to sell | Stamp duty (state, often creditable against the conveyance) | State rule table, same shape as the lease table |
| **TDS §194-IA** | **1% of consideration** where consideration **or** stamp duty value is ≥ ₹50 lakh, resident seller | Form 26QB within 30 days; buyer is the deductor |
| **TDS §195** | **Non-resident seller — no threshold**, from the first rupee, at the applicable rate | The single highest-risk item on this page. §7.3 |
| Conveyance | Stamp duty and registration fee on the higher of consideration and circle rate | Registration Act |
| Post-completion | Mutation, society transfer, utility transfer | Long-tail tasks the buyer forgets |

**The §194-IA aggregation amendment, effective 1 October 2024**, is the one that
catches platforms: where there are multiple buyers **or** multiple sellers, the
₹50 lakh threshold is tested on the **aggregate consideration across all parties**,
not per party. Two co-buyers at ₹30 lakh each are in scope. Any computation that
evaluates the threshold per party is wrong for a co-owned property, which in
India is the normal case rather than the exception.

### 7.2 Capital gains, seller side

| Item | Position |
|---|---|
| Holding period for long-term | 24 months for immovable property |
| LTCG rate | 12.5% without indexation |
| Acquired **before** 23 July 2024 | Resident seller may elect 20% with indexation or 12.5% without, whichever is lower — a grandfathered choice, not a default |
| STCG | Slab rate |
| §54 / §54F | Reinvestment relief, capped at ₹10 crore |
| §54EC | Specified bonds, within the statutory window |
| §50C | Where consideration is below stamp duty value beyond the tolerance band, the stamp duty value is deemed to be the consideration — and §56(2)(x) taxes the shortfall in the buyer's hands |

§50C and §56(2)(x) together mean **an under-declared sale is taxed twice, once on
each side.** A platform that lets parties record a consideration below circle rate
without surfacing this has walked its users into it.

**Dwellm8 computes and explains; it does not advise.** Capital gains depends on
facts the platform does not hold — cost of improvement, other property owned,
prior exemptions claimed. The output is an indicative computation with its inputs
shown and a clear statement that it is not tax advice, or it is not shipped.

### 7.3 Non-resident sellers

Separated because it is where the money is lost. §195 applies from the first
rupee with no ₹50 lakh threshold, the rate is materially higher than 1%, it is
computed on the **capital gain only if a lower-deduction certificate under §197
has been obtained** and otherwise on the gross consideration, and **the resident
buyer is personally liable for the failure**, not the seller. The buyer also needs
a TAN, which §194-IA buyers do not.

A retail buyer transacting with an NRI seller without professional help is the
single most exposed user on this platform. The residency of the seller is
captured at listing, and the flow surfaces the §195 path prominently — the same
posture [`india-compliance.md`](india-compliance.md) §5 takes for NRI landlords.

### 7.4 GST on purchase

Under-construction property is a supply of construction service and carries GST;
a completed property with a completion certificate does not. The distinction is
the completion certificate date against the booking date, and it is the buyer's
most common surprise.

---

## 8. Investment and ownership vehicles

### 8.1 Fractional ownership and SM REITs

SEBI notified the **Small and Medium REIT** framework in **March 2024** as an
amendment to the REIT Regulations, expressly to bring fractional ownership
platforms into the regulatory fold: minimum asset value ₹50 crore, at least 200
investors, minimum subscription ₹10 lakh, investment manager net worth ₹20 crore,
and at least 95% of the scheme's assets in completed and revenue-generating
property — no under-construction exposure.

**The line for Dwellm8:** pooling money from multiple investors into a property
and issuing them a return is a collective investment scheme. Doing it without SM
REIT registration is not a grey area to be managed with disclaimers. The platform
may **list, manage and account for** a property held by many co-owners; it may not
**pool, issue units, or promise a return**. Co-ownership accounting is a ledger
feature; unit issuance is a licensed activity, and the distinction must be
enforced in the product rather than in the terms of service.

### 8.2 Non-residents (FEMA)

- An NRI or OCI may acquire **residential and commercial** property in India
  under general permission.
- **Agricultural land, plantation property and farmhouses may not be acquired** —
  by purchase, in any structure. They may only be inherited, and an inherited
  agricultural holding may be sold only to a resident Indian citizen.
- Repatriation of sale proceeds is subject to limits and to the annual USD 1
  million facility, with tax clearance.
- Funding must come through the correct account type; the source-of-funds route
  is part of the record.

**Product rule:** buyer residency and property class together form a hard gate. A
listing classified as agricultural land does not accept an NRI or OCI buyer, and
this is `enforcement = block` — it is a prohibition, not a threshold.

### 8.3 Benami and structure

Property held in one name for another's benefit is a benami transaction with
criminal consequence for both parties. The platform records the **beneficial
owner** alongside the registered owner, and a mismatch is a disclosure the
platform surfaces rather than a field it quietly stores. Corporate and LLP
holdings change the TDS path (a company tenant is on §194-I — ADR-0024 §1) and the
capital gains treatment; the owning entity's type is a first-class attribute, not
a free-text field.

---

## 9. What binds Dwellm8 itself

The sections above are obligations of owners, tenants and buyers. These are ours,
and they are the ones that decide what the product may do at all.

### 9.1 PMLA — this one is live

By notification **G.S.R. 798(E) dated 28 December 2020**, a **real estate agent as
defined in RERA s.2(zm), with annual turnover of ₹20 lakh or above**, is a person
carrying on a designated business or profession under PMLA s.2(1)(sa)(iii) — and
therefore a **reporting entity** under s.2(1)(wa). That brings:

- Registration with **FIU-IND**.
- Client due diligence and record maintenance for the prescribed period.
- Reporting of prescribed and **suspicious transactions**.
- A designated principal officer.

**If Dwellm8 brokers sale or purchase transactions, this binds Dwellm8**, and it
is not a light obligation — it is a named officer, a filing pipeline and a
retention regime. The threshold is turnover, which we will cross.

### 9.2 RERA agent registration

RERA agent registration is **per state**, required to facilitate the sale of
units in registered projects, and carries advertising and conduct rules. Listing
claims are a compliance surface: a promised yield, an "approved" label, or a
completion date repeated from a builder's brochure is our statement once we
publish it.

### 9.3 The e-commerce operator question

Where the platform facilitates a supply between two other parties and collects
the consideration, the e-commerce operator provisions come into view — TCS under
GST, and TDS under §194-O. This is most likely to bite on **vendor services and
short-stay bookings**, least likely on residential rent collected owner-side.
It is unresolved and it is open question 6.

### 9.4 The three lines the platform does not cross

Stated as prohibitions because each is a licensing boundary rather than a risk to
be managed:

1. **No custody of client funds.** Money settles owner-side through the regulated
   aggregator. Dwellm8 does not hold rent overnight in a platform account —
   [`india-compliance.md`](india-compliance.md) §1.
2. **No pooling, no units, no promised return** — §8.1.
3. **No advice.** Tax, legal and investment outputs are computations with their
   inputs shown and their limits stated. The moment the product says "you should",
   it needs a licence it does not have.

### 9.5 Records and retention

Statutory retention beats erasure and the DPDP flow must say so rather than fail
silently ([`india-compliance.md`](india-compliance.md) §6). The retention matrix
is owned by this document: executed agreements, e-stamp certificates, tax
challans and certificates, ledger postings, KYC outcomes and consent artefacts
each have a statutory period, and PMLA records have their own. A single "delete
my account" path that does not know these periods is a compliance failure wearing
a privacy feature's clothes.

---

## 10. What ships when

| Asset class × transaction | Phase | Gate |
|---|---|---|
| Residential let and manage | MVP 1–2 | Launch states per [`e-stamping-by-state.md`](e-stamping-by-state.md) |
| Residential registration (12 months+) | MVP 5 | Registration flow |
| Hostel / PG / co-living | MVP 4 | Per-bed inventory and per-occupant GST evaluation |
| Commercial | MVP 6 | CAM, escalation, lock-in as ledger terms |
| Buy / sell facilitation | **Gated on §9.1 and §9.2** | FIU-IND registration and per-state RERA agent registration, or the feature is listing-only with no facilitation fee |
| Short-stay | Not planned | Would need §6.2 consents as blocking evidence |
| Fractional / pooled investment | **Refused** | SM REIT registration, i.e. a different company |

The buy/sell row is the decision this document exists to force. Brokerage is the
obvious adjacent revenue line from a rental platform, and it is the one that
converts Dwellm8 from a software vendor into a **regulated reporting entity with
a named officer**. That is a founder-level decision with a compliance hire
attached, and it must be made deliberately rather than arrived at by shipping a
"connect me to a buyer" button.

---

## 11. Open questions

1. **Do we broker sales?** Everything in §9.1 and §9.2 follows from the answer.
   Owner: founders, before any MVP 5 scoping.
   **Tracked as [#211](https://github.com/tesserix/dwellm8/issues/211).**
2. **The 90-day PG test when the stay ends early** — is the exemption lost
   retrospectively, and who bears the tax? Owner: Compliance, with a CA.
3. **Class changes mid-tenancy** — a residential unit converted to PG use, or a
   let unit listed for short-stay. Effective-dated reclassification is the
   mechanism; the tax consequence of the switch is unresolved.
4. **Company-let residential** — confirm the GST reverse-charge trigger turns on
   the recipient's registration status alone.
5. **Circle-rate data** — §50C and stamp duty both need it, it is per state, per
   locality, and there is no national source. Build, buy or ask the user?
   **Tracked as [#214](https://github.com/tesserix/dwellm8/issues/214).**
6. **E-commerce operator status** for vendor services and any facilitated
   booking — §9.3.
7. **Retention matrix**, per artefact type, reconciled against DPDP erasure.
   Owner: Compliance; blocks the erasure endpoint.
   **Tracked as [#213](https://github.com/tesserix/dwellm8/issues/213).**
8. Every rate in §5–§8 needs a bare-Act or bare-notification reading before it
   enters a rule table with `enforcement = block`.

---

## Sources

Secondary throughout; see the standing rule at the head of this document.

- [CBIC Notification 04/2024 — accommodation exemption, ₹20,000 and 90 days](https://www.taxscan.in/no-gst-for-accommodation-services-less-than-rs-20000-month-for-90-day-periods-cbic-notifies/418399)
- [GST on hostels and student residences — changes post July 2024](https://www.caclubindia.com/articles/gst-on-hostels-student-residences-changes-post-july-2024-52271.asp)
- [GST rate changes for hotels and restaurants w.e.f. 01.04.2025 — specified premises](https://taxguru.in/goods-and-service-tax/gst-rate-changes-hotels-restaurant-services-wef-01-04-2025.html)
- [GST 2.0 rationalisation and the hotel industry — 5% band from 22 September 2025](https://taxguru.in/goods-and-service-tax/gst-implication-hotel-industry-due-rate-rationalization-gst-2-0-regime.html)
- [Section 194-IA — TDS on transfer of immovable property](https://taxguru.in/income-tax/tdssection-194ia-payment-transfer-immovable-property.html)
- [New TDS rules on immovable property from 1 October 2024 — aggregation across parties](https://taxguru.in/income-tax/new-tds-rules-immovable-property-sales-effective-1st-october-2024.html)
- [Capital gains on property — 12.5% LTCG and the pre-23-July-2024 grandfathered option](https://tax2win.in/guide/capital-gains-on-property-tax)
- [SEBI SM REIT framework — thresholds and asset composition](https://www.cbre.co.in/insights/reports/navigating-the-sm-reit-landscape-a-look-at-regulations-and-implications)
- [SEBI notifies SM REITs to regulate fractional ownership](https://www.vestian.com/news/sebi-notifies-sm-reits-move-to-regulate-fractional-ownership-industry-and-safeguard-investors-interests)
- [Real estate agents notified as reporting entities under PMLA — G.S.R. 798(E)](https://www.mondaq.com/india/money-laundering/1051028/real-estate-agents-brokers-notified-as-reporting-entity-under-the-prevention-of-money-laundering-act-2002-and-prevention-of-money-laundering-maintenance-of-records-rules-2005)
- [PMLA compliance obligations for real estate agents](https://www.ahlawatassociates.com/blog/mandatory-compliance-by-real-estate-agents-under-pmla)
- [FEMA — NRI/OCI acquisition of property and the agricultural-land prohibition](https://nriinformation.com/property/agricultural-land)
