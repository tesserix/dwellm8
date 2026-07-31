# E-stamping channel coverage by state

Spike output for [#15](https://github.com/tesserix/dwellm8/issues/15). Feeds
[#60](https://github.com/tesserix/dwellm8/issues/60) (duty computation from the
state rule table), [#61](https://github.com/tesserix/dwellm8/issues/61) (e-stamp
integration and certificate storage), [#65](https://github.com/tesserix/dwellm8/issues/65)
(12-month guard) and [#107](https://github.com/tesserix/dwellm8/issues/107)
(registration path).

**Reviewed**: 2026-07-31 · **Owner**: Compliance · **Next review**: 2026-10-31
(quarterly, not half-yearly — see §7)

Every rate, base and slab below is a state fiscal parameter that changes with a
state budget and does not ask us first. They belong in a versioned, state-scoped
rule table with this document as the commentary, never in Go. That is the issue's
own edge case: *the output must be a rule table, not prose.*

**Source quality warning.** No line in this document has been read against a bare
Act, a gazette notification or a signed vendor contract. It is assembled from
state portals, law-firm commentary and vendor marketing. Every row carries a
verification status, and no row marked **Unverified** or **Conflicting** may seed
the production rule table — §6 says who closes each one.

---

## 1. The headline, because it changes the launch plan and not just the code

**Stamping and registration are two different products with two different maps,
and the state that is best at one is worst at the other.**

- **Karnataka** now has the cleanest *stamping* channel in the country — the
  Karnataka Stamp (Digital e-Stamp) Rules 2025, operationalised by notification
  dated 16 January 2026, make KAVERI-2 the authorised issuer, so a digital
  e-stamp is generated end to end with no vendor, no bank counter and no visit.
- **Maharashtra** has the country's most mature *registration* channel — e-filing
  of Leave & Licence has existed for years — and it is **the one launch candidate
  we cannot serve without a physical touch**, because e-registration captures a
  **biometric thumb impression** of every party against Aadhaar. A thumb scanner
  in the tenant's flat is a field visit. Maharashtra's ~122 Authorised Service
  Providers exist precisely because somebody must carry that device to the door.

That inverts the naive launch order. The instinct is to launch Mumbai and Pune
first because the market is largest and the L&L flow is the most digitised in
India; the instinct is wrong for MVP 2, because s.55 of the Maharashtra Rent
Control Act 1999 makes registration mandatory *regardless of tenure* — the
11-month convention buys nothing there. A Maharashtra tenancy is either
registered (biometric, feet on the ground, an ASP or our own licence) or it is
defective. There is no stamp-only middle product to sell.

So the second consequence: **the 11-month convention is a Karnataka/Delhi/
Telangana convention, not a national one.** [`india-compliance.md`](india-compliance.md)
states the 11-month default as a platform-wide rule. It is state-scoped, and the
lease builder must treat "does a term under 12 months avoid registration here?"
as a per-state flag beside the deposit cap, not as a constant.

Third: **no state gives us an API.** Not one launch candidate exposes a
government e-stamping API to a private platform. Every digital path is one of
three things — an aggregator holding an SHCIL Authorised Collection Centre
relationship, an ASP licence we would have to hold ourselves, or a human on a
state portal. That makes the vendor choice in §5 a *dependency on a
single point of failure with no fallback of our own*, which is a materially
different risk posture from payments, where the adapter chain has a real second
link ([ADR-0011](adr/0011-payment-provider-adapter.md)).

---

## 2. The three channels, and which of them we can actually reach

| Channel | What it is | Reachable by us | Live examples |
|---|---|---|---|
| **SHCIL CRA / ACC** | Stock Holding Corporation is the central record-keeping agency for e-stamping; it issues through Authorised Collection Centres — banks, CSCs, licensed vendors | **Only through an aggregator** that holds ACC standing. SHCIL publishes no public partner API | Delhi, UP, Gujarat, Rajasthan, TN, Telangana and ~20 states/UTs |
| **State treasury / registration portal** | GRAS, e-GRAS, GARVI, IGRS and friends — duty paid into the state treasury, challan produced | **Human web flow only.** No API, session-based, CAPTCHA, no idempotency, and it breaks when the state redeploys | Maharashtra (GRAS), Gujarat (GARVI), Telangana (IGRS), Rajasthan (e-GRAS) |
| **State document platform with its own e-stamp application** | The state issues the digital stamp itself from its own repository | **Not yet reachable programmatically.** Third-party access is an open question, not a documented product | Karnataka (KAVERI-2), Maharashtra L&L e-registration (via ASP) |

The channel that matters for MVP 2 is the first one, because it is the only one
with a commercial path to an API. The uncomfortable part is that Karnataka —
our largest launch market — has just moved *off* it toward the third.

---

## 3. State rule table: the shape #60 must implement

Keyed `(state, instrument_type, effective_from)` returning a computation
expression, per [`india-compliance.md`](india-compliance.md) §2. Duty is
`amountMinor` (int64 paise) throughout; percentage bases are evaluated in paise
and rounded once, at the end, by the rule engine — never in a caller.

`deposit_in_base` is the column the issue singles out, and it is the one that is
wrong most often in commercial rent-agreement tooling.

| State | Duty base (short residential lease) | `deposit_in_base` | Registration under 12 months | Status |
|---|---|---|---|---|
| **Karnataka** | Statutory: ~1% of average annual rent (Art. 30). Market practice for unregistered 11-month agreements: a flat ₹200 instrument | **No** | Optional | **Conflicting** — flat vs ad valorem must be resolved against the Karnataka Stamp Act Schedule before launch |
| **Delhi (NCT)** | Statutory: 2% of average annual rent for terms up to 5 years (Indian Stamp Act Sch. I-A Art. 35). Market practice: ₹100 | **No** (a separate nominal amount is cited for the deposit) | Optional | **Conflicting** — same flat-vs-ad-valorem split |
| **Telangana** | ~0.4% of total consideration = rent **+ advance** | **Yes** | Optional under 12 months | Unverified |
| **Maharashtra** | 0.25% of consideration, where consideration = (monthly rent × months) + non-refundable deposit + (refundable deposit × 10% × years) | **Yes, both kinds, differently** | **Mandatory at any tenure** (Rent Control Act s.55) | Unverified formula; the mandatory-registration rule is the load-bearing fact and is well attested |
| **Gujarat** | ~1% of (rent × months + deposit) | **Yes** | Optional | Unverified |
| **Tamil Nadu** | ~1% of average annual rent × years; registration fee cited separately | **No** | Optional | Unverified |
| **Haryana** | ~1.5% of annualised rent | Unknown | Optional | Unverified |
| **Uttar Pradesh** | Flat ₹100–₹200 cited for short terms; the statutory article is ad valorem on annual rent | Unknown | Optional | **Conflicting** |
| **Rajasthan** | Flat ~₹500 cited for a standard 11-month agreement | Unknown | Optional | Unverified |
| **West Bengal** | Flat ~₹100 cited | Unknown | Optional | Unverified |

Four things this table forces into the schema, all of which #60 must carry:

1. **The base is an expression, not a rate.** Maharashtra's notional-interest term
   (`refundable_deposit × 10% × years`) cannot be represented as `rate × base`.
   The rule table stores an expression over named inputs
   (`monthly_rent`, `months`, `refundable_deposit`, `non_refundable_deposit`,
   `years`), not a percentage column.
2. **A floor and a cap are separate fields.** Several states pair an ad valorem
   rate with a minimum instrument value; a rate alone under-computes.
3. **`deposit_in_base` is per state and per deposit kind.** A single boolean is
   already known to be insufficient — Maharashtra needs two.
4. **The flat-vs-ad-valorem conflict is not a documentation defect, it is a
   product decision.** The cheap flat figures circulating for Karnataka, Delhi,
   UP and West Bengal describe what the market pays for an *unregistered* short
   agreement; the statutory articles are ad valorem. Dwellm8 computes the
   statutory amount, and if the market convention is materially lower we say so
   in the UI rather than silently issuing an under-stamped instrument. An
   under-stamped agreement is inadmissible in evidence — which is the entire
   reason the tenant is buying it.

---

## 4. Deposit treatment, because it is where the money model and the tax model meet

The deposit already has three separate rules attached to it, and they do not
agree with each other:

- The **statutory cap** — two months residential under the Model Tenancy Act
  where adopted, and the per-state Rent Act caps in
  [`india-compliance.md`](india-compliance.md) §3.
- The **ledger treatment** — a deposit is a liability of the owner, posted to
  `deposit_liability`, never revenue ([ADR-0006](adr/0006-chart-of-accounts-and-posting-rules.md), #207).
- The **duty base** — in Maharashtra, Telangana and Gujarat the deposit inflates
  the stamp duty; in Karnataka and Tamil Nadu it does not.

The trap is that the same number feeds all three and only one of them is under
our control. A tenancy that is lawful on the cap and correctly posted in the
ledger can still be under-stamped, because the duty base picked up a deposit the
lease builder treated as out-of-scope. **Duty computation must read the deposit
from the lease, not from the collection plan**, and must be re-evaluated whenever
the deposit changes before signature.

---

## 5. Vendors: what they claim, and how they fail

| Vendor | Claimed state coverage | Product shape | What it actually is |
|---|---|---|---|
| **Leegality** | SHCIL e-stamp, "18+" to "25+" states | eSign + eStamp + notarise + archive in one API | The most complete single-vendor story for our exact flow; stamp *and* signature in one workflow object |
| **Digio** | "22+" states | Stamp duty collection, eSign, KYC | Comparable; the KYC overlap with [ADR-0013](adr/0013-kyc-data-handling.md) is worth pricing as a bundle |
| **SignDesk** | Multi-state | eStamp + rental agreement templates | Templates are a liability, not an asset — our agreement builder owns the document |
| **Protean/NSDL, eMudhra** | ESP-side | eSign only | Signature layer; not a stamping channel |

**Coverage claims are marketing until contracted.** "25+ states" is a count of
states where the vendor can issue *something*, not a guarantee that a residential
lease instrument in a named state is issuable today at the denomination we need.
The first vendor conversation asks for coverage *by instrument type*, per state,
in writing.

Failure modes to design against, all of which are ours to absorb because the
tenant sees them:

1. **Pre-funded stamp wallet.** Most aggregators debit a float we top up, not a
   per-transaction charge. That float is a Dwellm8 asset, it can be exhausted
   mid-signature at 9pm on a Sunday, and it must be a real ledger account with a
   low-balance alert — not a number in a vendor dashboard.
2. **E-stamp certificates are near-irreversible.** Cancellation and refund of a
   wrongly-issued certificate is a state-by-state manual process measured in
   weeks, with a haircut. This makes duty computation a **pre-commitment
   validation**, not a post-hoc correction: re-validate immediately before
   purchase, and never purchase from a stale quote.
3. **Party names are baked in.** First party, second party and "duty paid by" are
   printed on the certificate and cannot be edited afterward. A typo in the
   owner's name is a re-purchase. These fields must be confirmed on screen by the
   party who owns them, before money moves.
4. **State outage propagates.** When a state portal is down the aggregator is
   down for that state and no other vendor helps, because they all sit on the
   same SHCIL/state plumbing. The honest mitigation is a queued, resumable
   stamping workflow with a truthful "the state's system is down, your agreement
   is saved and will be stamped automatically" state — not a second vendor.
5. **No idempotency guarantee worth trusting.** A retried purchase that issues a
   second certificate costs real money and produces two valid instruments for one
   lease. The adapter carries our own idempotency key and a unique index, exactly
   as [ADR-0011](adr/0011-payment-provider-adapter.md) requires of payments, and
   confirms state with the provider before retrying.
6. **The certificate arrives asynchronously.** UIN issuance is not always
   same-call. The lease state machine ([ADR-0010](adr/0010-lease-lifecycle-state-machine.md))
   needs a `stamp_pending` state, and a signed agreement with no stamp evidence
   stays invalid until it clears.

Points 1, 5 and 6 mean **e-stamping is a durable workflow with compensations**
([ADR-0015](adr/0015-durable-workflow-standard.md)) and not a request/response
call, on the same footing as a payment. It moves money, it is hard to reverse,
and it can fail after the tenant has already signed.

---

## 6. Recommendation

**Launch (MVP 2): Karnataka and Delhi. Defer everything else.**

| State | Verdict | Reason |
|---|---|---|
| **Karnataka** | **Go** | Fully digital by law since January 2026; 11-month agreements need no registration; Bengaluru is the densest market for the management-firm segment. Conditional on §7 Q1 |
| **Delhi (NCT)** | **Go** | Mature SHCIL channel, aggregator-reachable, no registration under 12 months, low duty however the flat-vs-ad-valorem question resolves |
| **Telangana** | **Go on evidence** | Digitally deliverable and rules are simple; promote to Go once the 0.4%-of-rent-plus-advance base is confirmed against the Act |
| **Maharashtra** | **Defer — explicitly, to MVP 5** | Registration mandatory at any tenure and e-registration requires biometric capture. Cannot be delivered without a field visit. Entering it means either an ASP licence or an ASP partnership, and that is a business decision with a headcount attached, not an integration |
| **Tamil Nadu, Gujarat, UP, Haryana, Rajasthan, West Bengal** | **Defer** | Deliverable in principle through the same aggregator, but each adds a rule-table row that must be verified against a bare Act, and none is worth a launch state until the two Go states prove the flow |

The issue's failure scenario is the right instinct and it applies to Maharashtra
above all: **a state with no fully digital channel is deferred explicitly, not
launched with a manual workaround nobody staffs.** Shipping Mumbai with "and then
somebody visits with a thumb scanner" is exactly the workaround that goes
unstaffed, and it fails in the worst possible place — after the tenant has paid.

What "defer" must mean in code, so it is not a slogan: the lease builder
**refuses to generate an agreement for a state with no rule-table row**, the same
way #65 refuses to generate a 12-month unregistered lease. A missing row is a
refusal, never a default, and never a zero.

---

## 7. Open questions, to close with the state and the vendor rather than from public docs

1. **Can a private platform integrate with KAVERI-2's Digital e-Stamp
   Application, and does the SHCIL channel still work in Karnataka?** This is the
   single question that decides the Karnataka launch. The 2025 rules were
   reported as removing SHCIL service charges; if they also close the SHCIL
   channel, our aggregator cannot stamp in Karnataka at all and the Go verdict
   above collapses to a manual Kaveri flow. Owner: Compliance, before any #61
   work starts. **Tracked as [#208](https://github.com/tesserix/dwellm8/issues/208).**
2. **Coverage by instrument type per state, in writing, from Leegality and
   Digio** — specifically a residential lease instrument in Karnataka, Delhi and
   Telangana, at the denominations our duty table produces.
   **[#209](https://github.com/tesserix/dwellm8/issues/209).**
3. **The wallet model**: float mechanics, top-up latency, low-balance webhook,
   and whether an unused balance is recoverable. This determines the ledger
   accounts (ADR-0006) the stamping module needs.
   **[#209](https://github.com/tesserix/dwellm8/issues/209).**
4. **Cancellation and refund**, per state, with actual timelines and haircut.
   **[#209](https://github.com/tesserix/dwellm8/issues/209).**
5. **Whether the flat figures are lawful or merely tolerated** for Karnataka,
   Delhi, UP and West Bengal — a bare-act reading, not a blog. Until this closes,
   #60 ships the ad valorem computation with the flat amount as a floor.
6. **Maharashtra ASP economics** — cost per registration and geographic coverage
   — so the MVP 5 decision is made on numbers rather than on the observation that
   it is hard.
7. **Does any state treat GST or the platform fee as part of the duty base?**
   Assumed no; unverified.

---

## Sources

Secondary throughout; see the warning at the head of this document.

- [Karnataka Stamp (Digital e-Stamp) Rules 2025 — KAVERI-2 rollout analysis](https://www.mondaq.com/india/government-contracts-procurement-ppp/1740014/karnatakas-digital-e-stamp-regime-goes-live-what-the-kaveri-2-rollout-means-for-property-banking-and-compliance)
- [KAVERI-2 e-stamp rollout — compliance impact](https://ksandk.com/real-estate/karnatakas-kaveri-2-e-stamp-rollout-impact/)
- [Karnataka Stamp (Digital e-Stamp) Rules, 2025 — notification record](https://www.teamleaseregtech.com/updates/article/45449/karnataka-stamp-digital-e-stamp-rules-2025/)
- [IGR Maharashtra — e-filing of Leave & Licence](https://efilingigr.maharashtra.gov.in/)
- [IGR Maharashtra — RFAP for ASPs (e-registration of Leave & Licence)](https://igrmaharashtra.gov.in/pdf/onlineservices/ASP%20RFAP_v1.0.pdf)
- [Maharashtra e-registration — biometric and eKYC requirements](https://www.e-stampdutyreadyreckoner.com/faq-e-registration.php)
- [Registration of Leave and Licence in Maharashtra — s.55 mandatory registration](https://legaldesk.com/property/registration-of-leave-and-license-agreement-in-maharashtra)
- [SHCIL — e-stamping, CRA and ACC model](https://blog.stockholding.com/e-stamping-services-by-stockholding.html)
- [NeSL — states and UTs live for digital e-stamping](https://nesl.co.in/states-for-digital-e-stamping/)
- [Leegality — eSign and eStamp product](https://www.leegality.com/esign)
- [Stamp duty on rent agreements, state-wise](https://esahayak.io/blog/stamp-duty-on-rent-agreement-india)
- [Maharashtra leave-and-licence duty formula incl. notional interest on deposit](https://signyu.com/rent-agreement/maharashtra)
- [Telangana rent agreement — duty on rent plus advance](https://signyu.com/rent-agreement/telangana)
