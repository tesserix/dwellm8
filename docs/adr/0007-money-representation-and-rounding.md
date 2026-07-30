# ADR-0007 — Money representation, rounding and the currency standard

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Payments
- **Issue**: [#8](https://github.com/tesserix/dwellm8/issues/8)
- **Related**: [ADR-0006](0006-chart-of-accounts-and-posting-rules.md) (the ledger this standard feeds, and the `Minor` type it borrowed in advance), [ADR-0009](0009-property-block-unit-model.md) (what a prorated charge is charged against), ADR-0011 (payment provider, later), ADR-0012 (GST and TDS, later — this ADR fixes the arithmetic, not the rates)

---

## Context

ADR-0006 built the ledger on a type it did not define. `Minor` was declared as
`int64` paise with a comment saying the full standard was this ADR's to write,
because the chart of accounts needed a unit and nothing more. Everything that
actually produces a fraction was still ahead: proration when a tenancy starts
mid-month, a percentage late fee, GST on a management fee, TDS deducted by a
company tenant, a deposit split between two co-owners.

Every one of those is a division, and a division is where money software goes
wrong. The failure is never dramatic. A statement is off by one paisa, nobody
notices for four months, and then an owner reconciles a year of payouts against
their bank and the two numbers differ by ₹11 with no single transaction to blame.
By that point the arithmetic is spread across a dozen call sites, each of which
rounded reasonably, and there is no way to say which one was wrong because none
of them were.

So the decision has to be made before the calculations exist rather than after,
and it has to be enforceable rather than documented. Three things were already
fixed going in. The database stores `amount_minor bigint` with a positive check
and a `currency char(3)` constrained to `INR` (ADR-0006 §3). Entries balance to
the paisa, checked by a deferred constraint trigger that fires at `COMMIT`. And
correction is a reversing entry, never an update — which turns out to constrain
the rounding rule more than anything else here.

---

## Decision

**Money is `int64` in the currency's minor unit, carried with an explicit ISO
currency everywhere. Division rounds half away from zero, exactly once, at the
posting boundary. An amount split across parties or periods is allocated by
largest remainder so the slices sum to the whole by construction. On the wire it
is a whole number of minor units, bounded by what a JSON number represents
exactly. A float anywhere in the money module fails the build.**

### 1. `int64` minor units, and the currency travels with the number

Paise, as an integer, in the database, in Go, in the API and in every analytics
extract. Not rupees with a decimal part, not a decimal type, not a string.

The argument for an integer is not performance, it is that an integer cannot
hold the thing that causes the bug. ₹27,500 prorated across 17 of 31 days is
1,508,064.516129… paise. A `float64` holds an approximation of that and will
print it back as something plausible. A `decimal(14,4)` holds four decimal places
of it, which is worse: it is precise enough to look right and imprecise enough to
disagree with the next system that stores two. An `int64` cannot hold it at all,
so the code is forced to decide what the amount *is* at the moment it is
computed, which is exactly the decision that must not be deferred.

The currency is fixed at `INR` and is stored anyway, next to every amount, in
`journal_entries`, in `ledger_postings` and in every API response. A
single-currency system that stops recording which currency is a system whose
second currency costs a migration of every number it has ever stored. Recording
it costs three bytes.

Multi-currency itself is rejected, not deferred-with-a-plan (§A below): it needs
an FX rate with a date and a source, a decision about which currency a balance is
stated in, and a rounding rule per currency — Kuwaiti dinars have three decimal
places, yen has none — and this product has no caller for any of it. The `CHECK`
in the schema and the constant in Go both say `INR`, so a second currency fails
in both places rather than in neither.

### 2. One rounding rule: half away from zero, applied once

There is one division primitive in the money module, `mulDivRound`, and every
fraction in the product resolves through it. It carries the multiplication in 128
bits via `math/bits`, so 2,750,000 × 17 is exact before anything is divided, and
rounds the remainder half away from zero.

Away from zero, not up. The two differ only for negative amounts, and negative
amounts are exactly where it matters, because **correction in this ledger is a
reversing entry** (ADR-0006 §3). Half-up rounds −0.5 to 0 and +0.5 to 1. Under
that rule an entry computed at a rounding boundary and then reversed does not
cancel: the pair leaves a stranded paisa in a balance that no subsequent
transaction will ever clear, and the account is permanently off by an amount too
small for anyone to investigate and too persistent to go away. Away from zero
makes the rule symmetric — `round(−x)` is always `−round(x)` — so a reversal
reverses to the paisa. That property is asserted for every case in the rounding
test, not just the ones with an obvious sign.

**Rounding is permitted once, at the posting boundary.** The boundary is the
point where an amount becomes a `Posting.Amount`. Before it, values are exact
integers or they are inputs to a single division; after it, nothing divides
again. The rule that follows from this and matters in practice: a fee on a total
that will later be split is computed on the total and then *allocated*, never
computed per slice and summed. The two differ, and only the first makes the
owner's statement agree with the portfolio total.

### 3. Rates are basis points

`Rate` is an `int64` count of basis points — one hundredth of one percent — so
18% GST is `1800`, TDS under 194-I is `1000`, and the platform fee of 2.99% is
`299`. `Rate.Of(amount)` applies it through the same primitive and rounds once.

Every rate this product applies is exact in hundredths of a percent, and an
integer rate cannot drift the way a parsed decimal can. Measured against the real
cases: 2.99% of ₹25,000 is 74,750 paise exactly, 18% of ₹1,499 is 26,982 paise
exactly, and 0.5% of ₹33,333.33 is 16,666.665 paise, which the rule takes to
16,667.

What the rates *are* is not this ADR's business. Which GST slab applies to which
supply, and when 194-I applies rather than 194-IB, belongs to the tax decision
and to the tax module. This ADR fixes only how a rate is represented and how it
is applied.

### 4. Splitting is largest remainder, and the whole is preserved by construction

`Allocate(total, weights)` divides an amount across slices so that the slices sum
to the amount **exactly**. Every slice takes the truncated share; the paise left
over — always fewer than there are slices — go one each to the slices with the
largest discarded fraction, ties broken toward the earlier slice so a re-run of
the same split produces the same rows.

This is the piece that makes the ledger's balance property survive division.
Rounding each slice independently is the classic defect: ₹27,500 across three
co-owners rounds to three slices totalling ₹27,499.99, the entry does not
balance, and the deferred constraint trigger rejects the whole transaction at
`COMMIT` with nothing to point at — which is the good case. The bad case is a
split that is *almost* balanced and gets a plug line to make up the difference.

Nor is the remainder pushed onto the last slice, which is the usual fix and is
worse than it looks: it is deterministic in the wrong way, so the same party
absorbs everybody's rounding on every invoice, every month, forever. Largest
remainder distributes it to the slices that were closest to earning it.

Weights need no particular scale — days, square feet, ownership shares, or all
ones through `AllocateEqually`. A zero weight gets zero rather than a paisa.

Measured, and asserted in the test:

| Split | Result | Sums to |
|---|---|---|
| ₹27,500 across 17 and 14 days | 1,508,065 and 1,241,935 paise | 2,750,000 ✓ |
| 100 paise across 3 equal shares | 34, 33, 33 | 100 ✓ |
| ₹27,500 across 31 equal days | 88,710 ×21, 88,709 ×10 | 2,750,000 ✓ |

`Prorate(amount, days, inPeriod)` remains for the single-slice case, where the
rest of the amount is not being posted at all — a joining tenant's first month,
where the days before they moved in are nobody's charge. It and `Allocate` agree
on the 17-of-31 case above; in general they can differ by a paisa, and when both
sides of a split are posted, `Allocate` is the authority.

### 5. The wire form, and the ceiling JSON imposes

**JSON**: a bare integer of minor units, paired with an explicit currency —
`{"amountMinor": 1508065, "currency": "INR"}`. Never a decimal, never a float,
never a quoted string.

`UnmarshalJSON` is the strictest thing in the package on purpose. A client that
sends `27500.50` means rupees, and a permissive parser truncates that to 27,500
paise — ₹275 — which is a hundredfold error that produces a perfectly plausible
invoice. So a fraction, an exponent or a quoted string is an error naming the
value it rejected, not a value. `null` is a no-op, matching the convention every
`Unmarshaler` in the standard library follows.

**The ceiling is not the `int64` range, and that is the point.** JSON numbers are
`float64` to every JavaScript client in existence, and beyond 2⁵³ they stop being
exact. An amount the ledger stored correctly would arrive in a browser as a
different number with no error raised anywhere in the chain. So `MaxSafeMinor` is
2⁵³−1 paise — roughly ₹90 lakh crore — and it is enforced on the way *in*:
`Minor.Valid()` is checked by `Entry.Validate()` for every posting, by
`Allocate`, by `Prorate` and by `Rate.Of`. Nothing unrepresentable can be written
and then fail on the way out, when the transaction that wrote it is long gone. A
₹100 crore transaction is 10¹¹ paise against a ceiling of about 10¹⁶, so nothing
real is turned away.

The same ceiling is a `CHECK` on `ledger_postings.amount_minor`, for everything
that does not come through Go — psql, a fixture, an import job written in two
years. Measured on PostgreSQL 16, with the schema replayed twice to confirm the
constraint is added idempotently:

```
amount_minor = 9007199254740992 → ERROR: new row for relation "ledger_postings"
                                  violates check constraint "ledger_postings_amount_representable"
amount_minor = 9007199254740991 → accepted
```

Two copies of a constant is one more than one, so the contract test in
`internal/money/store` reads the constraint definition out of `pg_constraint` and
fails if it and `MaxSafeMinor` disagree — the same treatment the chart of
accounts and the posting templates already get. Without it the looser of the two
is the real limit and the stricter one is decoration.

**CSV and PDF**: `Rupees()` — always two decimal places, no symbol, no thousands
grouping, no locale, leading minus for a negative. `2750000` renders as
`27500.00`. Grouping is left out because a comma inside a CSV field is a bug
waiting for the one export nobody quotes, and Indian grouping (₹27,50,000) is not
what any spreadsheet or parser expects on import. Presentation with a symbol and
lakh-crore grouping belongs to whatever is presenting; it is not the export
format.

### 6. What fails the build

`TestNoFloatInAMoneyPath` in `internal/platform/arch` parses every `.go` file
under `internal/money` — tests included — and fails on:

- the identifiers `float32` and `float64`, anywhere they are named: a field, a
  parameter, a return, a conversion, a type argument;
- any float or imaginary literal, which is how this defect usually arrives —
  `amount * 0.0299`, with no type written down anywhere;
- an import of `math`, whose `Round`, `Floor`, `Ceil` and `Abs` are all float
  functions. `math/bits` and `math/big` are integer packages and are allowed;
  `mulDivRound` is built on `math/bits`;
- any selector containing `Float` — `strconv.ParseFloat`, `FormatFloat`,
  `AppendFloat`.

It parses rather than greps, so `float64` inside a comment is not a false
positive and an untyped `1.5` is not a false negative, and it reports
`file:line:column`. Tests are in scope because a test that computes its expected
value in floating point is not a lesser version of this bug — it is the bug,
wearing the costume of the thing that would have caught it.

The guard is only worth having if it fails, so CI plants a real float in
`internal/money/domain` and expects a red build, alongside the four ADR-0003,
-0005, -0006 and -0009 guards that do the same. Planted locally, it reports:

```
money/domain/planted.go:3:53: names float64 — money is int64 paise, and ADR-0007 §2 permits no float in this module
money/domain/planted.go:3:71: is the float literal 0.0299 — money is int64 paise, and ADR-0007 §2 permits no float in this module
```

The rest of the contract is `internal/money/domain/money_test.go`: the issue's
primary scenario, allocation totalling correctly across every month length for
five different amounts, the rounding rule's symmetry about zero asserted case by
case, the rejection of `27500.50` on the wire, and the range check refusing an
oversized amount at all five entry points including `Entry.Validate`. The
drift check between Go and the schema — the ceiling and the currency — is in
`internal/money/store/catalogue_test.go`, alongside ADR-0006's.

---

## Alternatives considered

### A. Multi-currency now — rejected

Not deferred with a design, rejected. It needs an FX rate with a date and a
source, a rule for which currency a balance and a statement are stated in, and a
per-currency minor-unit exponent, because the assumption that 100 minor units
make a major one is false for the dinar and for the yen. Every one of those is a
decision with no caller behind it, and a wrong guess is more expensive to unwind
than the eventual migration would be. The currency column exists so that
migration is a schema change rather than a rewrite of every stored number.

### B. A decimal type in Go — rejected

`shopspring/decimal` or similar, matching a `numeric` column. ADR-0006 §C already
rejected `numeric(14,2)` in the schema; the Go side follows for the same reason
and one more. Arbitrary precision does not remove the rounding decision, it
*hides* it: `d1.Div(d2)` yields something with a default precision, the code
compiles, and the moment where the amount was decided is invisible in review.
With `int64` the division is a function call that returns an error, and the
rounding is a thing you can point at. The dependency also has to be trusted in
the one package that has no other dependencies at all.

### C. Banker's rounding (half to even) — rejected

The usual argument is that it removes upward bias across many roundings. That
argument does not apply here, because bias across a split is handled by
allocation, not by rounding — `Allocate` preserves the total exactly, so there is
no accumulating drift for banker's rounding to correct. What is left is its cost:
2.5 rounds to 2 and 3.5 rounds to 4, which is indefensible to an owner checking
an invoice by hand, and it is not what any Indian accounting practice, invoice
template or tax computation does. It also breaks the symmetry §2 depends on
unless implemented with care, and the reversal property is worth more than a bias
that has already been eliminated elsewhere.

### D. Round wherever convenient, reconcile later — rejected

This is the default that happens when no decision is made: each call site rounds
sensibly, a nightly job compares totals and posts an adjustment for the
difference. It is rejected because the adjustment has no explanation. ADR-0006
made every correction a reversing entry with a reason code precisely so that no
line in the ledger is unattributable, and a "rounding adjustment" line is the
first unattributable line — after which the second one is easy.

### E. Money as a decimal string on the wire — rejected

`"27500.50"` as a JSON string is the common answer to the 2⁵³ problem and it is
defensible. Rejected because it moves parsing into every client, and every client
parses it into a float on arrival anyway — which reintroduces the problem at the
one point nobody is looking. An integer of paise with an enforced ceiling keeps
the value exact all the way to the client's arithmetic, and the ceiling is five
orders of magnitude above anything this product will carry. It also matches the
column, so the API shape and the storage shape are the same shape.

### F. Pushing the allocation remainder onto the last slice — rejected

Simpler than largest remainder and it does preserve the total. Rejected because
the party in the last position absorbs every split's rounding on every invoice,
every month — a small systematic transfer, always in the same direction, from a
party who never agreed to it. Largest remainder gives the paisa to whoever was
closest to earning it, which is both fairer and, unlike "last slice", explicable
to the person who receives it.

### G. A `Money` struct carrying its own currency in Go — rejected for now

`struct{ Minor int64; Currency string }`, with arithmetic that refuses to add two
different currencies. It is the right shape for a multi-currency system, and with
one currency it is ceremony: every posting would carry a field whose value is
always `INR`, and the mismatched-currency error would be unreachable code. The
currency is stored on the row and stated on the wire, which is where the second
currency would actually need it; when §A is revisited, this type comes with it.

---

## Consequences

**What is now true.** Money has one representation across the schema, the domain,
the API and any extract. Every fraction in the product resolves in one function.
A split adds up to the whole by construction rather than by inspection, and a
reversing entry reverses exactly, including at a rounding boundary. An amount
that could not survive a round trip to a browser is refused when it is written
rather than when it is read. A float in a money path is a red build with a line
number.

**What this costs.** Rounding and allocation return errors, so callers handle a
failure that is unreachable for realistic inputs — the price of making the
overflow and range checks real rather than assumed. `Rate` in basis points cannot
express a rate finer than a hundredth of a percent; nothing in Indian rent, GST
or TDS needs one, and if something does, it is a new ADR rather than a quiet
widening. And the float guard's scope is `internal/money` only: a float in a
reporting or analytics package that touches an amount is not caught. Widening the
scope means deciding what "a money path" is outside the module, which is a real
question and is deliberately not answered here.

**What is not decided.** The GST and TDS rates and when each applies (ADR-0012).
Whether statements round display values differently from stored ones — they do
not today, and `Rupees()` is exact, so the question only arises if a summary view
ever shows amounts in thousands. And the shape of a money value in the event
payloads of ADR-0002, which follow the JSON rule in §5 but have not been written.
