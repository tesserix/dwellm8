# Payment rails, methods and provider capability

Spike output for [#13](https://github.com/tesserix/dwellm8/issues/13). Feeds
[#73](https://github.com/tesserix/dwellm8/issues/73) (mandate lifecycle),
[#74](https://github.com/tesserix/dwellm8/issues/74) (debit scheduling and retry),
[#127](https://github.com/tesserix/dwellm8/issues/127) (in-app payments) and
[ADR-0011](adr/0011-payment-provider-adapter.md).

**Reviewed**: 2026-07-31 · **Owner**: Payments · **Next review**: 2027-01-31

Every cap, window and threshold below is a regulatory or NPCI parameter that
changes without asking us. They belong in a versioned rule table with this
document as the commentary, not in Go — that is the point of the issue's primary
acceptance criterion.

---

## 1. The headline, because it changes the product and not just the code

**Rent cannot be collected on a credit card to an owner who is not a contracted
merchant.** The RBI's revised Payment Aggregator Directions (15 September 2025)
say a PA may aggregate funds only for a merchant it holds a contract with, and
may not run a marketplace. PhonePe, Paytm, CRED and Amazon Pay shut their
credit-card rent products within days of it, and the compliance deadline for
re-onboarding existing merchants passed on 31 December 2025.

An individual landlord receiving rent through Dwellm8 is not a merchant of
Cashfree's. So the card rail — and therefore every EMI, no-cost-EMI and
pay-later product built on top of it — is not available for **rent** in the
owner-collection model, no matter which aggregator we pick. This is not a
Cashfree limitation to shop around for; it is the rail.

What is still available on cards and EMI is everything where a contracted entity
is genuinely the merchant:

- Dwellm8's own platform fees, subscription tiers and premium AI entitlement (#147).
- A management firm collecting into its own account under its own PA contract —
  it *is* a merchant, and its tenants may pay it by card if the firm accepts the MDR.
- Brokerage, onboarding, agreement/e-stamp, registration and services-marketplace
  charges, billed by whoever performs the service.

**The security deposit is not in that list.** A deposit is a lump sum paid to the
owner at move-in and held against the tenancy; it is not an instalment product and
no collection flow may present it as one. The deposit-alternative products (#109)
are a separate thing that only looks similar: a lender pays the owner the full sum
and the tenant repays the lender. That repayment is loan servicing between tenant
and lender — Dwellm8 does not collect it, does not book it as rent, and does not
put it on a card of ours. See the deposit cap rule table in
[`india-compliance.md`](india-compliance.md), which also carries the statutory
limit on how many months may be taken at all.

So EMI's honest scope on this platform is platform-billed services and fees, and
nothing that is rent or deposit.

---

## 2. Every method, and what it is actually for

Cost figures are public list prices for orientation only; commercial negotiation
is out of scope for this spike. UPI and RuPay debit carry zero MDR by statute.

| Method | Canonical `collect.Method` | Cap | Cost | Cashfree | Razorpay | Verdict for Dwellm8 |
|---|---|---|---|---|---|---|
| UPI intent / QR | `upi_intent` | ₹1L per txn (₹5L some categories) | 0% | ✅ | ✅ | **Primary one-off rail.** First payment, arrears, ad-hoc charges |
| UPI collect request | `upi_collect` | as above | 0% | ✅ | ✅ | Reminder-driven collection; expiry handling is #41 |
| UPI Autopay | `upi_autopay` | ₹15k AFA-free · ₹1L with PIN | 0% | ✅ Subscriptions | ✅ tokens | **Recurring rail for rent ≤ ₹15,000** |
| eNACH (netbanking / debit-card / Aadhaar auth) | *new* `nach_debit` | ₹1 crore | flat per-mandate + per-debit | ✅ | ✅ eMandate | **Recurring rail for rent > ₹15,000** |
| Physical NACH | *new* `nach_debit` (same, different auth) | ₹1 crore | flat + manual handling | ✅ incl. upload API | partial | Fallback for banks with no eNACH; 5–10 working days |
| Netbanking | `netbanking` | bank-set | ~1.9% | ✅ 50+ banks | ✅ | One-off high-value where UPI caps bite; MDR must be a disclosed pass-through (#179) |
| Debit card | `card` | ₹15k AFA-free recurring | ~0.9%–1.9% | ✅ | ✅ | Marginal for rent; useful for platform billing |
| Credit card | `card` | — | ~1.9%+ | ✅ | ✅ | **Not for rent** — §1. Platform billing and firm-as-merchant only |
| Card EMI / no-cost EMI | *new* `emi` | issuer-set | ~1.9%+ subvention | ✅ 35+ issuers | ✅ | Platform-billed fees and services only — **never rent, never the deposit** |
| Cardless EMI / BNPL | *new* `emi` | lender-set | ~2.5% | ✅ FlexMoney, HDFC, ICICI, IDFC, TVS, CASHe | ✅ | Same. #109 is a lender's product, not an EMI we collect |
| Wallets | *new* `wallet` | ₹2L full-KYC | ~1.9% | ✅ | ✅ | Low value for rent-sized amounts. Defer |
| Cash / cheque / bank transfer | `offline_*` | — | 0 | n/a | n/a | Already built. ADR-0011 §6 — a method, not a fallback |

Adding `nach_debit`, `emi` and `wallet` reopens the closed method vocabulary in
`collect`, which is an ADR-0011 amendment plus a schema change in
`tesserix-k8s/.../schemas/dwellm8/` — not a local edit. Only `nach_debit` is
needed for MVP; `emi` and `wallet` should wait until the deposit-alternative and
platform-billing stories are real, so we are not carrying vocabulary nothing
writes.

---

## 3. The rail-selection rule

Rent is **not** in the AFA-exempt category set. That set is insurance premiums,
mutual-fund SIPs, credit-card bills, and the lending/investment MCCs (6211, 6300,
7322, 6529, 5960). Everything else, rent included, is AFA-free only up to
₹15,000 per debit.

| # | Condition | Rail | Consequence |
|---|---|---|---|
| 1 | monthly rent ≤ ₹15,000 **and** payer bank supports UPI Autopay | `upi_autopay` | True autopay: no payer present at debit |
| 2 | monthly rent ≤ ₹15,000 **and** bank has no Autopay support | `upi_collect`, scheduled | Tenant approves each month. Not a mandate; do not describe it as one |
| 3 | ₹15,000 < rent ≤ ₹1,00,00,000 | `nach_debit` (eNACH) | Slower onboarding, bank-dependent, genuinely unattended |
| 4 | rule 3 attempted and the bank has no eNACH support | physical NACH, else `upi_collect` | 5–10 working days to authorise |
| 5 | rent > ₹1,00,00,000 | manual / `offline_transfer` | Out of every retail rail |

The issue's two examples resolve as: **₹12,000 → rule 1**, **₹45,000 → rule 3**.

Rule 3 deserves its reasoning stated, because the tempting answer is wrong. UPI
Autopay *does* reach ₹1,00,000 — but every debit above ₹15,000 requires the
tenant to enter their UPI PIN, within 30 minutes of a collect request. That is a
scheduled collect request wearing an autopay costume: it has the onboarding cost
of a mandate and the monthly failure profile of a manual payment, which is the
worst pairing available. Above ₹15,000, eNACH is the only rail that is actually
unattended.

The threshold rows live in a rule table with `effective_from`, an owner and a
review date, per the backlog guardrail. A regulator changing the AFA ceiling is
then a row, and existing mandates are re-evaluated against the new row — which is
the issue's second acceptance criterion and the reason the rule is data.

---

## 4. Pre-debit notification, and who owes it

RBI requires the payer to be notified at least 24 hours before every recurring
debit, carrying amount, date and mandate reference. In practice the aggregator
and the sponsor bank send it: Razorpay initiates each subscription debit T-24h
precisely so the bank's notification goes out in time, and Cashfree sends
pre-debit alerts two to three days ahead.

Dwellm8's obligation is therefore not the SMS. It is **having the amount final 24
hours before the due date**, which is a real constraint on everything upstream:

- A rent escalation, a late fee or a concession (#192) that lands inside the
  notification window cannot be debited that cycle. It bills next cycle or it is
  collected out-of-band.
- Invoice generation (#37) must complete no later than T-48h to leave room.
- The notification is the tenant's cue to pause or cancel. A cancellation arriving
  after we have initiated is a mandate-state change we learn about by webhook,
  and per ADR-0011 §4 it is confirmed against the provider, never applied from the
  delivery.

## 5. The debit window nobody expects

Since 1 August 2025 NPCI restricts UPI Autopay execution to non-peak hours:
before 10:00, 13:00–17:00, and after 21:30 IST.

Rent is due on the 1st. Every mandate in the book wants to fire in the same three
windows on the same morning, which makes the debit scheduler (#74) a real piece
of engineering rather than a cron line — jitter across the windows, a per-window
concurrency ceiling, and a queue that survives the window closing mid-batch. This
is ADR-0015 durable-workflow territory, not adapter territory, and it is the
single most under-estimated item in the mandate epic.

---

## 6. Cashfree versus Razorpay, on the things that actually differ

| | Cashfree | Razorpay |
|---|---|---|
| Recurring product | Subscriptions: UPI Autopay + eNACH + **physical NACH** in one API, incl. an upload API for signed forms | Tokens/Subscriptions: UPI Autopay + eMandate; paper NACH thinner |
| Mandate identity | `subscription_id` | `token_id` |
| Split settlement | Easy Split — vendor split, custom cycles, instant settlement | Route — split at T+2 into linked accounts |
| Default settlement | T+1 | T+2 (T+1 on request) |
| Payouts | Cashfree Payouts | RazorpayX |
| Webhook signature | HMAC-SHA256 over `x-webhook-timestamp` **+ raw body**, base64 | HMAC-SHA256 over raw body, hex |
| Idempotency | `x-idempotency-key` on some APIs | none general; `receipt` is not enforced unique |
| API versioning | date-pinned `x-api-version` header | URL-versioned |
| Auth | `x-client-id` / `x-client-secret` | basic auth key/secret |

Both are integrable. The case for Cashfree as the head of the chain is that one
product spans all three recurring rails including physical NACH — which rule 4
needs and which is the rail of last resort for the small-bank tenant we cannot
otherwise serve — plus a T+1 default and a split product whose custom cycles fit
the owner-payout model (#79, #80) better than a fixed T+2 split.

Razorpay stays registered as the second link. That is what `Registry.SetChain`
exists for, and because `Registry.By(name)` resolves an existing payment to the
provider that created it, a chain reordering never asks the wrong provider about
a mandate it has never heard of. Note that #127 is titled "Razorpay and UPI
intent" and should be retitled — the story is provider-neutral by construction.

## 7. What the aggregator gives us and what we build

**Theirs**: mandate registration UX and the PSP handshake, sponsor-bank
relationships and eNACH/NACH plumbing, the pre-debit notification, debit
execution and retries at the rail level, settlement files, and the webhook feed.

**Ours**: the rail-selection rule table and its re-evaluation; the mandate object
and its lifecycle (#73); amount-final-by-T-48h; the non-peak scheduler and the
failure-retry policy (#74); everything ADR-0011 already insists on — idempotency
by index, confirmation-before-status, deduplicated advisory webhooks; the ledger
postings a debit produces (ADR-0006); reconciliation against the settlement file
(ADR-0012); and the tenant-facing story of a failed debit, which no aggregator
has an opinion about.

---

## 8. What this changes in ADR-0011

The `Adapter` interface cannot carry either provider's mandate flow, and one part
of it cannot carry Cashfree at all. Both are amendments to a Rejected-nothing ADR
rather than a supersession — the seam is right, it is incomplete.

1. **A mandate is not a payment.** `upi_autopay` is currently a bare `Method`
   constant with nothing behind it (`collect/payment.go:39`), and `grep -ril
   mandate services/` finds no domain code. A standing authority needs its own
   object, its own forward-only lifecycle (`registered → pending → active →
   paused → revoked | expired`), its own provider id, and `payments.mandate_id`
   pointing at it. Debits remain ordinary `collect.Payment` rows, which keeps the
   state machine, the idempotency index and the webhook contract intact.
2. **A second interface, not a wider one.** `MandateAdapter` —
   `Register / ConfirmMandate / Debit / Amend / Revoke` — implemented only by
   adapters that have mandates. Offline does not, and widening `Adapter` would
   force it to stub five methods that must never be called.
3. **`VerifyWebhook(payload []byte, signature string) bool` cannot express
   Cashfree.** Cashfree signs `timestamp + rawBody`; the interface passes no
   timestamp, so a Cashfree adapter physically cannot verify a delivery. It needs
   the delivery headers rather than one string — which also gives the timestamp
   replay window the current design has nowhere to put. `VerifyHMACSHA256` is
   hex-over-body-only, i.e. Razorpay's scheme exactly, and needs a base64
   sibling.
4. **Raw bytes.** Both providers sign the exact body received. Any HTTP layer
   that decodes and re-marshals before verification breaks both, silently and
   only in production.
5. **`max_amount` is fixed at registration.** A UPI mandate registered at today's
   rent cannot absorb an escalation; the mandate must be registered with headroom
   or re-registered on escalation, and re-registration means the tenant acts
   again. See the open question — it decides which.

## 9. Open questions, to close with Cashfree rather than from public docs

1. **Does the ₹15,000 AFA threshold bind on the mandate's `max_amount` or on the
   debit amount?** Cashfree documents ₹15,000 as the maximum *mandate* amount
   without AFA; Razorpay documents it as the threshold at which a *debit* needs a
   PIN. If it binds on the mandate, then registering escalation headroom pushes an
   otherwise-AFA-free ₹12,000 tenancy into PIN-per-debit, and rule 1's ceiling is
   effectively lower than ₹15,000. This is the single question that most changes
   the mandate design.
2. Does Cashfree support amending a live mandate's amount, or only cancel-and-
   re-register?
3. eNACH authorisation success rate and median time-to-active by bank, on their
   book, for residential rent.
4. Whether a property-management firm can be onboarded as a sub-merchant under
   Easy Split while remaining the contractual merchant for card acceptance — this
   determines whether rule-of-§1 card collection is available to firms at all.
5. Settlement-file format and delivery cadence, which ADR-0012's reconciliation
   is written against.

---

## Sources

- [Cashfree — subscription payment modes and mandate limits](https://www.cashfree.com/docs/payments/subscription/payment-modes)
- [Cashfree — webhook signature verification](https://www.cashfree.com/docs/payments/online/webhooks/overview)
- [Cashfree — pay-later and cardless EMI](https://www.cashfree.com/docs/payments/manage/payment-methods/paylaters-and-cardless-emis)
- [Razorpay — UPI Autopay S2S recurring](https://razorpay.com/docs/payments/payment-gateway/s2s-integration/recurring-payments/upi/)
- [RBI Payment Aggregator Directions 2025 — analysis](https://indiacorplaw.in/2025/10/09/decoding-rbis-overhaul-of-the-payment-aggregator-directions/)
- [Credit-card rent payments withdrawn under the revised PA rules](https://www.medianama.com/2025/09/223-rbi-pa-rules-phonepe-paytm-cred-credit-card-rent-payments/)
- [RBI e-mandate AFA exemption to ₹15,000](https://www.rocketpay.co.in/blog/rbi-e-mandate-recurring-payments-15000)
