# ADR-0031 — The platform fee: retained at capture, priced by a row, accrued when it cannot be split

- **Status**: Accepted
- **Date**: 2026-08-01
- **Deciders**: Platform, Product, Compliance
- **Issues**: [#234](https://github.com/tesserix/dwellm8/issues/234)
- **Related**: [ADR-0006](0006-chart-of-accounts-and-posting-rules.md), [ADR-0007](0007-money-representation-and-rounding.md), [ADR-0011](0011-payment-provider-adapter.md), [ADR-0012](0012-settlement-reconciliation-and-drift.md), [ADR-0023](0023-statutory-rule-tables.md), [`india-compliance.md`](../india-compliance.md) §9.3 and §9.4

---

## Context

Dwellm8 charges 2.99% of each collection. The accounting for it already existed —
`platform_fee` debits `owner_payable` and credits `platform_fee_income` and
`gst_output` — and **nothing said how the money reaches Dwellm8's bank account**.

Two of the three obvious answers are closed:

- **Settle everything to Dwellm8 and pay the owner out.** §9.4 states a
  prohibition, not a preference: *"No custody of client funds. Money settles
  owner-side through the regulated aggregator. Dwellm8 does not hold rent
  overnight in a platform account."* Taking ₹25,000 in and forwarding ₹24,252 is
  aggregating without a licence.
- **Settle everything to the owner and invoice the fee back.** Legal, and it
  turns revenue into a receivable chased from thousands of small landlords.

The rate was also a constant in two places that disagreed with a third.

---

## Decision

**The aggregator retains the fee at capture and settles two legs. The rate is a
row in an effective-dated table. A collection that cannot be split accrues the
fee rather than skipping it.**

### 1. The split, and why it is also the safer posture

The bearer is registered with Cashfree as a vendor; the order carries a split
instruction; Cashfree settles the vendor's leg to their bank and ours to ours.
No client money rests in a Dwellm8 account at any point.

This is not only operationally simpler. §9.3 puts the e-commerce-operator risk —
TCS under GST, TDS §194-O — as *"least likely on residential rent collected
owner-side"*, and a split keeps the funds owner-side by construction. Choosing
the mechanism that avoids custody also chooses the one that argues best on ECO.

### 2. The rate is a row

`platform_fee_rules` is reference data on ADR-0023's pattern, with its governance
columns: an owner, a review date and a source. The product owner changes the
price by writing a row with a `valid_from`; the rate that priced a payment in
March is still answerable in September, and an exclusion constraint over
`validity` means there is never more than one answer for a day.

It carries no `tenant_id` and no row-level security, which is the same exemption
`statutory_rules` has and for the same sentence: this is the rule rather than the
data. An organisation that could write it would price itself at zero effective
last April. The privilege is the boundary — revoked from `dwellm8_app`, handed
back to `dwellm8_platform` alone so a future admin surface can change the price
without a deployment, and asserted by assertion 18.

Per-organisation negotiated pricing is deliberately **not** this table. It needs
tenancy, row-level security, and a different set of questions about who may agree
a discount.

### 3. Tax is charged on top, and that is a stored decision

If 2.99% were deemed tax-inclusive, income would be 2.99 ÷ 1.18 — about fifteen
per cent less. So `tax_inclusive` is a column with a date on it rather than an
assumption in code, and `FeeSchedule.Charge` implements both.

The arithmetic has one property worth stating, because it is the one that can be
wrong without looking wrong: **the two legs add back to the gross, exactly**. The
vendor leg is `gross − retained`, a subtraction, never a second percentage — two
independently rounded percentages miss the whole by a paisa, on every payment,
and the settlement file is reconciled on legs that must add up. A capped fee is
decomposed *from the cap*, not from the rate, for the same reason.

### 4. Quote before the provider, post after the money

Two steps, deliberately separate:

- **Quote** runs before `CreateOrder`, because the split has to be on the order.
- **Post** runs at capture, because a fee on money that never arrived is income
  we did not earn.

Both resolve the rate as of the payment's own creation date, so the rule that
priced the split is the rule that posts. A rate change between order and capture
cannot make the two disagree.

The posting is keyed `platform_fee:<payment id>`, so a redelivered confirmation
posts one fee however many times it arrives — the same guarantee ADR-0006 gives
every other entry, reused rather than reinvented.

### 5. Accrual is the interesting case

A split is impossible more often than it is possible today:

| | |
|---|---|
| `offline` | cash, NEFT, IMPS paid straight to the bearer. `apps/live/README.md` calls this *"the majority of Indian rent today"* |
| `no_vendor` | the bearer is not registered with the aggregator, or their payout account is inside its cool-off |
| `nothing_to_charge` | a waived rate, or an amount too small to round to a paisa |

In all of them the fee **posts anyway**, against `owner_payable`, and simply
sits there until a collection or a payout clears it. There is one posting path,
not two: whether the money was retained at capture or is carried as a balance
changes who is holding the cash, not what is owed.

The alternative — charge nothing when we cannot split — leaks revenue precisely
where the volume is, and leaves no record that it happened. A fee that silently
becomes zero is revenue lost without a trace.

The cool-off reuse matters: `Vendor()` resolves through
`payout_account_payable()`, so an account held after a bank-detail change has no
vendor and cannot be split to. The control that stops a payout must also stop a
split, or the hold is bypassed by every collection.

### 6. Never fail a collection over our own fee

`Quote` returns an empty quote rather than an error when the rule cannot be read
or the bearer is unknown. Refusing a tenant's rent because we could not arrange
our own invoice is the wrong trade every time. The failure is logged, and the
absence of a fee posting is visible in the ledger.

---

## Rejected alternatives

**A constant in Go.** Where the rate already was, in two files that disagreed
with a third. A price with no effective date cannot answer "what did we charge in
March", which is the first question an accountant asks.

**Deriving the fee at settlement from what the aggregator actually retained.**
Attractive — the settlement file is the truth (ADR-0012) — and it means the
owner's statement has no fee on it until the file arrives days later. The bearer
should see what they were charged when they were charged. Reconciliation against
the file stays the check, not the source.

**Storing the quote on the payment.** Considered, and unnecessary: resolving the
rule as of the payment's creation date is deterministic and needs no column. The
one thing that *is* stored is `fee_bearer_party_id`, because the bearer is a fact
about the arrangement at order time and cannot be re-derived at capture.

**Per-organisation rates in the same table.** Tried first, and it collided with
assertion 12 — a nullable `tenant_id` demands platform-only writes, which the
bootstrap running as the table owner cannot satisfy. The collision was the
schema telling the truth: a negotiated price is tenant data and belongs in a
tenant-scoped table, not in the rule.

---

## Consequences

- `payments` gains `fee_bearer_party_id`. Nullable, because a collection with no
  bearer charges no fee and says so rather than guessing.
- Bearer *resolution* is not here — it is [#179](https://github.com/tesserix/dwellm8/issues/179).
  Until it lands, the caller supplies the bearer and an empty one charges
  nothing, visibly. `bearerOf()` is one line so replacing it is one line.
- The Cashfree adapter now sends `order_splits`. Whether a management firm can be
  a sub-merchant under Easy Split is `payment-rails.md` open question 4 and is
  still open — the code is written, the commercial arrangement is not.
- Reconciliation must match two legs per split payment. `recon.MatchPartial`
  already anticipates split settlements, so ADR-0012 needs extending rather than
  revisiting.
- Refunds must reverse the fee and its GST proportionately. Not built here; the
  reversing-entry machinery exists and the rule does not yet.
- Whether Dwellm8 becomes an e-commerce operator for these supplies remains
  §9.3's open question 6. Splitting improves the argument; it does not settle it.
