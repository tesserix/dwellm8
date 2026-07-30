# ADR-0012 — Settlement reconciliation, drift classification and alerting

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Payments
- **Issue**: [#14](https://github.com/tesserix/dwellm8/issues/14)
- **Related**: [ADR-0011](0011-payment-provider-adapter.md) (what the `settled` state and the parked index were built for), [ADR-0006](0006-chart-of-accounts-and-posting-rules.md) (the clearing account this measures against), [ADR-0007](0007-money-representation-and-rounding.md) (the amounts), [ADR-0003](0003-tenancy-and-row-level-security.md) (the exception this generalises), ADR-0015 (durable workflows: the retry and backoff policy, later)

---

## Context

A payment is not money until the settlement report agrees. ADR-0011 built
everything that leads up to that sentence and deliberately stopped short of it:
a captured payment posts into `gateway_clearing` rather than the bank, the
`settled` state exists with nothing that can reach it, and parked webhook
deliveries accumulate behind an index whose comment says they need a sweep with
somebody's attention on it. This is that sweep.

The obvious framing is "read the settlement file and tick off the payments in
it", and the obvious framing is what loses money. Three things about the problem
are not obvious, and each of them changes the design:

**There are three accounts of the same money, not two.** What the provider
settled, what `payments` says was collected, and what the ledger's clearing
balance says is owed to us. That is three pairwise comparisons, and they fail
differently: a provider line with no payment is the provider's fact and our gap,
a captured payment with no line is money somebody else is holding, and a clearing
balance that does not equal the captured-and-unsettled total is our own bug that
no settlement file will ever reveal.

**Reconciliation has two directions and only one of them has a row.** A line that
will not match is a row that can be flagged. A payment the provider never settled
is an *absence*, and an absence cannot be found by looking at the file that was
delivered. Every implementation that reads the settlement file and iterates over
it is complete in the direction that does not matter, and the acceptance criterion
for this story — three payments missing from a file of five hundred — is
specifically the direction it cannot see.

**The reconciler cannot be the thing that alerts on the reconciler being down.**
A nightly job that never runs raises no alerts. A job that dies mid-run raises
none either. The check that matters most therefore cannot live inside the job, and
the acceptance criterion about a settlement file being unavailable is really a
question about who is watching when nothing happens at all.

---

## Decision

**A settlement batch whose arithmetic is checked before any line of it is
believed; matching per line with a classification, of which exactly two may post
unattended; drift classified by which pair of accounts disagreed; ageing derived
from a clock rather than stored; and a provider-day that is marked reconciled by
the database only once the drift table agrees it can be.**

### 1. The seam, and where the code lives

`internal/money/recon` sits beside `internal/money/domain/collect` for the same
reason `collect` sits beside `domain`: a settlement batch is a different thing
from a payment, and a payment is a different thing from an entry.

`Reconcile(Input) (Result, error)` is a pure function. It reads no clock — `AsOf`
is passed in, so a run over yesterday's file produces yesterday's answer and a
test is a test — opens no connection, and writes nothing. What it produces is
matches and drift; what may then be posted is `Class.Posts()`.

Nothing in the package names an aggregator. Fetching a settlement file is an
adapter's job under ADR-0011's `provider` seam, and the file format is that
adapter's problem.

### 2. A batch must add up before any line of it is believed

`Batch.Validate()` asserts `gross − refunds − fee − tax = net` from the
provider's own figures, and `settlement_batches_adds_up` asserts the same thing
in the schema.

The reason it is first is that a file we parsed wrong must never be able to look
like a file we disagree with. Those are different incidents with different
responses — one is a bug, one is a phone call to the provider — and a misread
column surfaces as several hundred inexplicable drift rows if the arithmetic is
not checked at the door. `ErrBatchArithmetic` is distinguishable for exactly that
reason. Measured: a batch one paise out is refused by name.

It is stated twice on purpose, and the argument is different from the usual one.
Elsewhere in this codebase the schema is the backstop for a path that skipped Go.
Here the failure is that *our own numbers are the wrong ones*, and a guard that
lives only in the code that got it wrong is not a guard.

`utr` is on the batch and is not decorative: it is the only handle that connects a
batch to a line on a bank statement. A batch without one can be reconciled against
our records and never against the bank's.

### 3. Three accounts, three comparisons

`DriftKind` names which pair disagreed, because the fix differs and one
"unreconciled" bucket loses that:

| Kind | Pair | What it means |
|---|---|---|
| `missing_settlement` | provider vs payments | We captured it; no file has ever mentioned it |
| `unknown_line` | provider vs payments | The file settled a payment we never issued |
| `amount_mismatch` | provider vs payments | Matched by id, and the money does not reconcile |
| `duplicate_settlement` | provider vs payments | Settled twice |
| `late_settlement` | provider vs payments | It arrived, past the SLA |
| `clearing_balance` | payments vs ledger | **Our own bug** |

The last one is the one this design would not have without the three-account
framing. `ClearingCheck(balance, capturedUnsettled)` compares the ledger's
clearing balance against what `payments` says is captured and unsettled. A payment
marked settled whose settlement entry was never posted, or an entry posted twice,
shows up here and nowhere else — no provider disagrees with us, because the
provider did nothing wrong. It carries no tenant, because the clearing account is
platform-wide, and no age, because how long two balances have disagreed is not
knowable from two balances.

### 4. The fee is an expense, and clearing is credited the gross

The story asks for a "fee-adjusted" match, which sounds like a tolerance and is
not. When a provider settles ₹10,000 and pays ₹9,764 into the bank, the payer paid
₹10,000, the receivable was settled in full, and ₹236 is a cost of ours.

So `settlement_with_fee` is a new event kind — separate from `settlement`, for the
same reason `payment_with_tds` is separate from `payment` — with two new accounts:
`gateway_fee` (expense) and `gst_input` (asset, because GST on a fee is creditable
rather than lost).

The load-bearing detail: **clearing is credited the gross, not the net.** Clearing
was debited the gross when the payment was captured, so it must be credited the
gross or it keeps a residue forever. Netting the fee against the clearing credit
balances just as well, passes the double-entry trigger, and leaves the one account
this whole subsystem measures against permanently wrong. There is a test whose
only job is that number.

The provider's own fee figure is authoritative for the posting, for the plain
reason that they took it. Comparing it against a contracted rate card is a
different story and is not what stops a reconciliation closing.

### 5. Matching: six classes, two of which may post

`exact`, `fee_adjusted`, `partial`, `unknown_payment`, `duplicate`,
`amount_drift`. `Class.Posts()` is true for the first two and false for the rest,
and that is a method on the class rather than a decision made per call site — the
same move ADR-0011 §4 made with `ApplyConfirmed`. The schema holds the other half:
`settlement_lines_only_matched_lines_post` refuses an `entry_id` on a line whose
class is not one of the two, whatever the code believed.

The two that may post are exactly the two that account for the whole gross
clearing is carrying. Everything else would post an entry leaving clearing wrong
in a way no later line can fix.

Two details are where this design differs from the story as written:

**There is no `timing` class, and the story asked for one.** A timing difference
is not a different kind of match — it is the same match arriving later than
expected — and a line can be both late and fee-adjusted. Making lateness a class
forces a choice between them and silently discards one. So it is `Match.Late`, a
`late_settlement` drift row for the ageing report, and a match that still posts:
the money arrived, and it belongs in the bank.

**A split settlement's completing line posts the whole payment's gross, not its
own amount.** The running totals are per provider payment id rather than per line,
because clearing was debited once. Getting this wrong is not visible as an error —
it is visible as a clearing account that never quite empties.

**There is no amount tolerance anywhere.** See alternative C.

### 6. Four platform-owned tables, and an exception that became a pattern

None of `settlement_batches`, `settlement_lines`, `settlement_drift` or
`reconciliation_runs` is tenant-scoped in the ordinary ADR-0003 sense.

A settlement batch is not tenant data. It is one payout from one aggregator
account, and its totals are every organisation's collections added together — so
no organisation sees it, not even one whose money is in it. What an owner is shown
is their own payments and their own drift. The batch *resolves into* tenant data
one matched line at a time, and until a line is matched it belongs to nobody:
`settlement_lines.tenant_id` and `settlement_drift.tenant_id` are nullable, and
`settlement_lines_attribution_shape` makes attribution and matching arrive
together or not at all.

ADR-0011 §5 introduced that nullable-tenant exception once, for the webhook inbox,
with a paragraph explaining why. Three more tables of the same shape is not an
exception any more, so it gets a guard:

**Assertion 12** — every table whose rows may belong to no organisation must have
`is_platform_session()` alone as its `WITH CHECK`. The hazard is specific: where
`tenant_id` may be NULL, `tenant_id = current_tenant_id()` constrains nothing, so
the ordinary policy shape would let any organisation insert a row belonging to
none. The consequence, worth stating because it is architecture rather than
detail: **settlement ingestion and drift resolution are platform-role paths**, the
same conclusion ADR-0011 reached for webhook ingestion and for the same reason.

The assertion covers `payment_events`, which was written before it existed.
Measured:

```
ERROR:  table(s) whose rows may belong to no organisation and whose writes are
        not platform-only: payment_events
```

That is the mirror image of ADR-0011 §5's story, where ADR-0009's assertion 6
caught the payments table without being touched. Both directions are planted in
CI, because a guard is only general if something proves it.

**Assertion 10 was generalised at the same time**, and for the same reason. It
named `ledger_balances` by hand; it now requires `security_invoker` of every view
in the schema. Naming the object was the mistake assertion 6 was written not to
make, and here the decay would have been an ageing report silently under-reporting
what is missing.

`settlement_batches` and `reconciliation_runs` have no `tenant_id` at all, which
made the isolation harness's `SchemaAudit` fail the moment they existed —
correctly. They are on its exempt list with the argument above, which is the point
of that guard: the pair had to be argued for rather than added.

Nothing here may be deleted, and a batch may not be updated either: its totals are
the provider's statement, and a statement that can be edited is not one.

### 7. Ageing is derived, and resolution is audited

`settlement_drift.since` is when the money became wrong. The age is `now() − since`
and the bucket is derived from the age — never stored, because a stored bucket is
wrong the day after it is written and the whole value of an ageing report is that
yesterday's three-day-old item is four days old today.

The boundaries live in `settlement_age_bucket(interval)` rather than inline in the
view, and that is a correction made during implementation. Inline, the only thing
a contract test can compare is the view's *text*, which catches a boundary that
moved and cannot catch a boundary rewritten to mean something else. As a function
it is a seam a test can evaluate, exactly like `payment_transition_allowed()`.
CI plants both failures: a moved boundary, and a view given its own inline copy
while the function stays correct.

The two copies had in fact already diverged, in the least visible way available.
Go computed the bucket from truncated whole days, PostgreSQL compared intervals,
and the contract test found it:

```
an age of 73h0m0s buckets as "1_3_days" in Go and "4_7_days" in the database
```

Nothing was broken, no test failed, and an operator comparing an alert against the
ageing report would have found the numbers disagreed for a range of ages nobody
could describe. The view is what an operator queries, so the view's semantics won.

Resolution is the manual workflow the story asks for, and it is constrained rather
than documented. `settlement_drift_resolution_shape` requires a state, a note, an
actor and a timestamp to move together: a resolution with no note is a row
somebody closed, which is not a row somebody explained. A write-off must carry an
`entry_id` — money abandoned that the ledger does not know about is exactly the
defect `payments_captured_has_entry` exists to prevent — and `clearing_write_off`
is the event kind it posts, an expense with a reason rather than a correction. And
`late_settlement` cannot be resolved at all, because there is nothing to do: the
money arrived.

One drift row per thing that is wrong, per provider, enforced by partial unique
indexes on the open rows. A payment missing for nine consecutive nights is one row
that ages, not nine.

### 8. `reconciled` is earned, not claimed

The acceptance criterion here is a negative: a day whose settlement file was
unavailable must not be marked reconciled. The difficulty is that a comparison
over no lines looks perfectly clean, and the job that would report the day clean
is the same job that failed to fetch the file. Its own report is not evidence.

So `reconciliation_runs` has five states and the interesting decisions are about
what they mean. `reconciled` does **not** mean everything matched — `drift` is a
finished state, not a failed one, because the job did its job. `incomplete` is the
file never arriving. `running` is a run in progress, and a run that died leaves
one, which must be indistinguishable from a day that never ran.

Two mechanisms make the state honest:

- `reconciliation_runs_reconciled_saw_the_file` is a `CHECK`: neither `reconciled`
  nor `drift` is reachable without `file_present`.
- `reconciliation_run_counters()` is a trigger that **recomputes**
  `unresolved_count` and `unresolved_minor` from `settlement_drift` rather than
  accepting them, and refuses `reconciled` while anything is open. A job that
  reports zero while three drift rows are open is refused by PostgreSQL:

  ```
  ERROR:  reconciliation of razorpay for 2026-07-30 cannot be called reconciled:
          3 open drift item(s) worth 8250000 remain
  ```

`recon.StateFor(res, filePresent)` computes the state in Go from what the run
found, so the job never chooses it either.

### 9. Alerting, and a ceiling on the thresholds

`Count` and `AmountMinor` are ORed, not ANDed, and both are needed: one payment of
₹5,00,000 and three hundred of ₹50 are both incidents, and either threshold alone
misses one of them.

Alerts are grouped by drift kind, not emitted per row — three missing payments is
one incident with three items in it, and three alerts is how a pager gets muted.
Each alert names the count, the money and the oldest bucket in its first line,
because an alert that says "reconciliation drift detected" is an alert people
learn to close. `amount_mismatch`, `duplicate_settlement` and `clearing_balance`
fire on a single occurrence regardless of thresholds: the money not adding up is
not a matter of degree.

The thresholds are configuration, and **configuration does not get to switch
alerting off by choosing a number no incident will reach**. `Thresholds.Validate()`
enforces ceilings — 25 items, ₹1,00,000, 48 hours — at startup, the way the
provider chain's names are checked at startup. A threshold set beyond an incident's
reach is alerting switched off in the shape of a number, and it survives review
because it looks like a number rather than like a decision. Zero is the strictest
setting, not the loosest.

`StaleRuns` is the third alert and the one that matters most, computed from
`reconciliation_runs` alone: no run has to happen for it to fire, and a run stuck
in `running` is treated exactly as a day with no run at all. The retry and backoff
policy around fetching a file belongs with ADR-0015's durable workflow standard;
what belongs here is that the *state* cannot be faked and the absence is what
alerts.

### 10. Offline money is not expected in a settlement file

`Captured.Method` is threaded into the comparison so an offline payment is skipped
rather than flagged. Cash, a cheque and a bank transfer generate no settlement
line, ever, and the alternative is one alert per cash payment every night until
somebody turns the alerting off — which is the outcome that matters, not the
noise. `Result.SkippedOffline` reports the count rather than hiding it: a number
that suddenly jumps is a method being recorded as offline that is not.

A captured *online* payment with no provider id is flagged as missing rather than
skipped. Nothing can ever match it, and skipping it is how it stops existing.

### 11. What fails the build

- `internal/money/recon` — the 500-with-3-missing scenario, silence inside the SLA
  not being drift, the class table, split settlements completing, offline
  exclusion, the batch arithmetic, the line shape rules, the clearing check, the
  threshold ceilings, the watchdog firing on a dead reconciler, determinism over
  twenty identical runs, and the assertion that only two classes may post.
- `internal/money/domain` — that a settlement fee does not come out of the
  clearing credit, and that `gst_input` is an asset.
- `internal/money/store` — the four vocabularies against their `CHECK`s, the
  ageing boundaries evaluated through `settlement_age_bucket()`, that the view
  calls it, the four deduplication indexes, and the batch arithmetic evaluated by
  PostgreSQL against Go's.
- `internal/platform/tenancy/isolationtest` — batches invisible to every tenant
  and immutable once ingested, unmatched lines invisible and matched ones visible
  only to their owner, drift readable by its owner and resolvable only by the
  platform, and a day refusing to be called reconciled while money is missing.

CI plants five failures and expects red: the counters trigger neutered, assertion
12 against `settlement_lines` and again against `payment_events`, assertion 10
against the ageing view, and the ageing boundary moved — twice, once in the
function and once by giving the view its own copy. They join the ADR-0003, -0005,
-0006, -0007, -0009 and -0011 guards.

---

## Alternatives considered

### A. Reconcile by iterating the settlement file — rejected

The obvious design, and the one most integrations have. Every line is looked up,
matched or flagged, and the day closes when the file is exhausted.

Rejected because it is complete in the direction that does not matter. The file is
the provider's account of what it *did* send; nothing in it mentions the payment it
forgot. The story's own acceptance criterion is three payments absent from a file
of five hundred, and a run over that file finds nothing wrong — which is why
`Reconcile` takes the captured payments as well, and why the missing direction is
found by a clock rather than by a row.

### B. Deriving the missing-payment set from the clearing balance — rejected

A cheaper version of the same idea: the clearing balance is what has not settled,
so alert when it is too old or too large.

Rejected because a balance is a sum and an alert needs a list. "₹87,500 has been in
clearing for four days" cannot be chased, assigned or explained to an owner;
"these three payments" can. The clearing balance is still used — it is the third
comparison in §3 — but as a cross-check on our own bookkeeping, not as the source
of the ageing report.

### C. A small per-line amount tolerance — rejected

One paise, to absorb rounding differences between our fee arithmetic and the
provider's. It is the standard reconciliation feature and it is the standard way
reconciliation stops working.

Rejected on two grounds. It is unnecessary: matching is against the gross, which is
what the payer paid and what clearing carries, and the provider's fee is taken as
authoritative for the posting rather than recomputed, so there is nothing for a
tolerance to absorb. And it aggregates in the one direction nobody watches — a
systematic one-paise difference across ten thousand payments is ₹100 of real money
reported as "all matched", and the tolerance is precisely the mechanism that makes
it invisible. If our fee arithmetic and theirs disagree, that is a fact worth
seeing, not a rounding artefact worth swallowing.

### D. Making a settlement file advisory, as ADR-0011 made webhooks advisory — rejected

Superficially the consistent choice, and it would make it impossible for anything
to ever reach the `settled` state.

Rejected because the two are not the same kind of evidence, and the difference is
*how they arrive*. A webhook is pushed to a public endpoint by anybody who can
reach it, which is why ADR-0011 §4 will not let one move money. A settlement report
is pulled from the provider over an authenticated channel and is their own
accounting record of a payout that hit a bank account. Treating it as a hint would
leave the design with no authority for settlement at all.

So a settlement line that matches by provider payment id and reconciles to the
paisa is sufficient to move `captured → settled`. A line that does not reconcile
moves nothing, which is the part of ADR-0011's instinct that does carry over.

### E. A `reconciled` flag on the day, set by the job — rejected

One boolean, written by the nightly job when it finishes.

Rejected because the job that failed to fetch the file is the job that would report
on it, and a comparison over zero lines is indistinguishable from a clean day. The
trigger recomputing the counters from `settlement_drift` is what replaces the
flag's honesty with the database's, and `reconciliation_runs_reconciled_saw_the_file`
is what stops an empty comparison being called a clean one.

### F. `late` as a match class — rejected

What the story asked for: exact, fee-adjusted, partial, timing. Rejected because a
line can be both late and fee-adjusted, so the class has to pick one and the other
is lost — and it would be the fee, since lateness is the more interesting-sounding
of the two. Lateness is orthogonal to whether the money reconciled, so it is a
flag, a drift row for the ageing report, and no obstacle to posting.

### G. One drift row per run per problem — rejected

Insert what the run found, every night. Simpler, and it gives a free history.

Rejected because a payment missing for nine nights becomes nine rows, the ageing
report counts it nine times, and the alert says "nine missing payments" when one is
missing. The partial unique indexes on the open rows make the row the thing that
ages, which is what an ageing report needs it to be. The history is in the drift
row's own `since` and `detected_at`, and in the runs.

### H. Storing the ageing bucket on the drift row — rejected

Computed once at detection, indexed, cheap to report.

Rejected because it is wrong the following day and the report exists specifically
to show that things are getting older. Same argument as ADR-0006's derived
balances, and the same conclusion.

### I. Giving `settlement_lines` a `property_id` so delegated firms see their own — rejected for now

It would let ADR-0009's unit-granular scope reach settlement lines directly, so a
management firm could see settlement status for the flats it manages.

Rejected because the place belongs to the payment, not to the line, and duplicating
it would put two answers in the database about where a collection was — with
assertions 5 and 6 then requiring the line's own delegated branch, which would be a
second copy of the payments policy to keep in step. A firm reaches settlement
status through the payment it already has scoped access to. The cost is real and is
in the consequences.

---

## Consequences

**What is now true.** A settlement file that does not add up is refused at
ingestion rather than believed and puzzled over. Reconciliation looks in both
directions, so a payment nobody settled is found by the run that could not see it
in any file. Drift says which pair of accounts disagreed, so the response is not a
triage exercise. Two match classes may post and the database enforces which. A
provider-day cannot be called reconciled while money is missing — not by a job, not
by an operator, not by a migration — because the counters are computed from the
drift table by a trigger. The ageing report is derived, so it is right tomorrow. An
alert names what and how much. Thresholds cannot be set to silence. And the alert
that fires when the reconciler itself is dead reads only the run table, so it does
not depend on the reconciler.

**What this costs.** Four more platform-owned tables, which means the platform role
is now used by settlement ingestion, drift resolution and the nightly run as well
as by webhook ingestion and onboarding — a wider connection than a request handler
uses, and the set of paths that need it is growing rather than shrinking. Drift is
not visible to a delegated management firm as drift (alternative I); a firm sees
its units' payments and reaches settlement status through them. Every batch
ingestion holds the whole file's lines in one transaction, which is fine at Indian
rental volumes and will not be at marketplace volumes. The ageing boundaries and
the four vocabularies live in two places each; the contract tests are real but they
are checks, not impossibilities. And `late_settlement` rows accumulate with no
workflow to close them, by design — they are a metric, and if that metric turns out
to need retention it will need a decision.

**What is not decided.** The retry, backoff and dead-letter policy for fetching a
file, which is ADR-0015's durable-workflow territory and is the mechanism behind
this story's "retries with backoff" wording — what is settled here is the state it
may not reach. Razorpay's settlement API client. Where alerts go: this produces
`Alert` values with a kind, a count, an amount and a message, and the notification
path is the `notify` module's. Payout reconciliation, which is the same problem
pointing the other way and is MVP 3. The society fund audit pack, MVP 4. Comparing
the provider's fee against a contracted rate card. And GST input credit
*reporting* — `gst_input` is an account and a template line here; the return itself
is issues #18 and #19, which have no ADR number yet.
