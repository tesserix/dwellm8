# ADR-0008 — Effective dating and the temporal query standard

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Platform
- **Issue**: [#9](https://github.com/tesserix/dwellm8/issues/9)
- **Related**: [ADR-0005](0005-owner-delegation-grants.md) (the one table this standard deliberately excludes), [ADR-0009](0009-property-block-unit-model.md) (the tree ownership hangs off), [ADR-0006](0006-chart-of-accounts-and-posting-rules.md) (append-only, for the same reason), ADR-0010 (leases and rent schedules, next)

---

## Context

"What was the rent in March?" and "who owned this flat when the deposit was taken?"
must be answerable from the primary tables.

Not from an audit log, and the reason is sharper than "logs are inconvenient". A log
records that a row *changed*; the question is what the row *said*, and reconstructing
that from a sequence of diffs requires every diff to have been captured, in order,
with no gaps, forever — and it gives you a plausible answer rather than a
constraint-checked one. Nothing stops two reconstructions disagreeing.

The product has six things that are effective-dated: ownership, the rent schedule,
lease parties, society membership, delegation grants and fee agreements. Every one
of them is a question somebody will eventually ask a lawyer about.

Two things were already true going in. `btree_gist` has been created by this schema
since ADR-0003, with the comment "exclusion constraints for effective dating" and no
exclusion constraint anywhere — somebody anticipated this ADR and left the extension
ready. And `delegation_grants` already carries `valid_from`/`valid_to`, so the naming
convention is ADR-0005's rather than this ADR's invention. It also carries them as
`timestamptz`, which turned out to be the most interesting decision here.

---

## Decision

**Half-open intervals of dates, `[valid_from, valid_to)`, with the interval exposed
as a generated `daterange` column; no-overlap enforced by an exclusion constraint
rather than by a trigger or by application code; history append-only, with the two
writes effective dating needs and no others; and a correction that retires the row
it replaces rather than editing it.**

### 1. Half-open, and why closed intervals cost more than they look

The successor's `valid_from` equals the predecessor's `valid_to` exactly. No gap to
leave a date uncovered, no overlap to make two rows true at once, and no arithmetic.

Closed intervals — `[1 Jan, 31 Mar]` then `[1 Apr, …]` — require the writer to
compute 31 March from 1 April. That computation is in every revision, and it is
wrong across a month boundary, across a leap day, and every time somebody writes
`valid_to = new_from - 1` against a timestamp instead of a date. Half-open removes
the subtraction rather than making it easier to get right.

The cost is one thing that reads oddly and has to be said out loud: **a lease running
through 31 March has `valid_to = 2026-04-01`.** A single day is `[1 Apr, 2 Apr)`.
Both are tested, because both are what somebody will query wrong first.

### 2. Dates, not timestamps — and the exception that is real

A rent revision is effective from a date. Ownership changes on a date. A lease starts
on a date. A timestamp forces a question with no good answer: is 1 April effective at
00:00 IST or 00:00 UTC? The two differ by five and a half hours, during which the
rent is legally one number and technically another. Indian rental agreements are
dated by day, so the column is `date` and the interval is `daterange`.

`effective.DateOf` will not truncate an instant to a day without being told the
location, for the same reason — and the test spells out what defaulting to UTC would
do: file a 00:30 IST payment under the previous day.

**`delegation_grants` stays `timestamptz` and is correctly not part of this**, which
is a distinction rather than a fudge. An authorisation window is not an effective
date: a firm's access begins at a moment, you grant it at 9am and it is live at 9am,
and no legal document is dated by it. ADR-0005 §1 already evaluates it with
`now() >= valid_from`, which is the right predicate for a permission and the wrong
one for a rent.

Assertion 14 encodes the split by column type rather than by a list, so a new
effective-dated table is policed automatically — and names the timestamptz tables, so
a new one of those has to argue for itself.

### 3. `validity` is generated, so there is exactly one expression

```sql
validity daterange GENERATED ALWAYS AS (daterange(valid_from, valid_to, '[)')) STORED
```

This is the whole of the story's second edge case — "an open-ended interval is
handled consistently in every helper" — turned from a rule people follow into a
property of the schema.

The hand-written as-of predicate is
`valid_from <= $1 AND (valid_to IS NULL OR valid_to > $1)`. It has two places to get
the NULL wrong, one to get the boundary wrong, and it appears in every query. The
generated column reduces it to `validity @> $1::date`, which has none, because
`daterange(from, NULL)` is unbounded above without anybody saying so.

**No SQL helper functions.** The range operators *are* the helpers: `@>` for as-of,
`&&` for overlap, `-|-` for adjacency. A function wrapping them would be a second
name for an operator every reader already knows, and it would be the thing somebody
forgets to use.

### 4. The no-overlap guarantee is an exclusion constraint

```sql
EXCLUDE USING gist (tenant_id WITH =, unit_id WITH =, owner_party_id WITH =,
                    validity WITH &&)
  WHERE (retired_at IS NULL AND unit_id IS NOT NULL)
```

An exclusion constraint rather than a trigger, and the argument is the one ADR-0011 §2
made about idempotency: a trigger has to read the table it is protecting, so it is
racy under concurrency — which is precisely when two people revise the same flat. The
GiST index makes the range itself the lock, so the second writer waits and then
fails. Measured:

```
ERROR:  conflicting key value violates exclusion constraint
        "property_ownership_no_overlap_unit"
```

Three details are load-bearing:

- **`WHERE retired_at IS NULL`** is what makes corrections possible at all. A retired
  row still occupies its interval, and without the predicate it would block its own
  replacement.
- **Two constraints, not one.** `unit_id WITH =` compares NULL to NULL as unknown, so
  two property-level rows would not conflict — and they should, because both are about
  the whole property. So there is one constraint for unit rows and one for
  property-level rows, each with the other excluded by its `WHERE`.
- **The party is part of the key.** Two owners can hold the same flat at the same time
  on undivided shares, which is normal in India. What must not overlap is the *same*
  party's own history. `share_bps` is an integer of basis points for the same reason
  ADR-0007 gives: a share expressed as a float is a share that does not add to 100.

### 5. Correction is not change, and the difference cannot be recovered later

The story's first edge case, and the deepest thing in this ADR.

- A **change**: the world changed. Rent went from 25,000 to 27,000 on 1 April. The old
  row is closed on that date and stays exactly as it was, so an as-of query for March
  still says 25,000 — because in March it *was* 25,000.
- A **correction**: we were wrong. The agreement always said 26,000 and somebody typed
  25,000. Closing and opening would assert that rent was 25,000 in March, which was
  never true of the world — only of our record of it. So the wrong row is retired and
  the right one replaces it **over the same interval**, and March now says 26,000.

Both operations exist as functions — `effective.Change` and `effective.Correct` — so
a caller has to say which it means. The rows say so too: `KindCorrection` with a
`corrects` link versus `KindChange` with none, and `Validate` refuses a row that
claims one and looks like the other. That is the criterion: distinguishable from the
rows alone, without an audit log.

**The limit, stated rather than discovered.** This is uni-temporal with corrections
retired rather than deleted. It answers "what was true in March". It does **not**
answer "what did we believe about March, last Tuesday" — that is bitemporality, the
story puts it out of scope, and the retired rows make it answerable by hand and
nothing more. Anybody who needs the second question needs a new ADR, not a clever
query.

### 6. History is append-only, with exactly two permitted writes

`property_ownership_append_only()` refuses an in-place change to `valid_from`, the
owner, the share, the property or the unit. A row whose amount can be edited is a row
that never had a history, and this is the same argument ADR-0006 §3 makes about the
ledger — arrived at independently, for a table that is not the ledger.

The two writes effective dating actually needs are permitted, and each only once:
closing an open interval, and retiring a row. Re-closing an already-closed interval
moves a boundary that something downstream has already reported on; un-retiring a
retired row makes it live again alongside its replacement. Both are refused by name.

Nothing may be deleted. The record of who owned a flat is what a dispute turns on.

### 7. The helpers, and what they refuse to do

`internal/platform/effective` holds `Date`, `Interval`, `Record[T]` and `Timeline[T]`.

`Interval` has **unexported fields and two constructors** — `Since(from)` for
open-ended and `Between(from, to)` for closed-above. That is deliberate and it is the
open-ended criterion again: a struct with an exported `To` field has a zero value
that silently means "true forever", so a forgotten field would be a rent that never
expires. `Between` refuses a zero upper bound with a message pointing at `Since`, so
open-ended is something a caller *says* rather than something a zero value means.

`Timeline.Current(today)` takes today as a parameter and does not read the clock —
the same rule the reconciliation and workflow packages follow, for the same two
reasons: a function that reads the clock cannot be tested at a boundary, and every
interesting bug in effective dating is at a boundary.

`AsOf` returns `(Record, bool)` rather than a zero value, because **a gap is legal and
is not zero**. A flat is unoccupied between leases; "no rent on record" and "rent of
nothing" are different facts. Gaps are therefore not a validation error — a timeline
that refused them would force a fictional row to cover a vacancy.

And `Change` refuses one case that looks fine: a change effective on the day the
current row starts would close that row to an empty interval. That is not a change,
it is a correction of a row that was never true for a single day, and the caller has
to say which.

### 8. What fails the build

- `internal/platform/effective` — the story's rent-revision scenario across both
  sides of 1 April and on both boundary days, overlap refused, open-ended handled in
  every helper including two open intervals overlapping each other, the zero interval
  refusing to contain anything, correction versus change giving different answers for
  March, a gap returning not-found, and every day in a three-year range having at most
  one live answer.
- `internal/platform/arch` — platform does not import a module. New, and it exists
  because this package wanted `money.Minor` for a rent amount: harmless-looking, and
  it inverts ADR-0001's dependency direction so the money module could no longer be
  changed without checking what platform assumed about it.
- `internal/platform/tenancy/isolationtest` — ADR-0003's five-part contract on
  `property_ownership`, the as-of answers against PostgreSQL on both boundary days,
  the exclusion constraint refusing an overlap by name, a correction over the same
  interval succeeding because the retired row does not block it, three in-place edits
  refused, and each of the two permitted writes refused the second time.

CI plants four failures and expects red: the exclusion constraints dropped, an
effective-dated table with no exclusion constraint at all, an effective date modelled
as a timestamp, and a module imported into platform.

**Assertion 14** is column-driven, like assertion 6, and that is what makes it worth
having: it polices a table nobody updated it for. Measured against a table invented
after it was written —

```
ERROR:  effective-dated table(s) without a generated validity range and an
        exclusion constraint over it: rent_schedule_draft
```

— which is ADR-0010's rent schedule, arriving next, already covered.

---

## Alternatives considered

### A. Audit log plus in-place updates — rejected

Update the row, write the old values to a log. The default, and it is what the story
explicitly rules out.

Rejected because the question is what the row said, and a log gives you a
reconstruction rather than a fact. Every diff has to have been captured, in order,
with no gaps, forever; nothing constrains two reconstructions to agree; and the
reconstruction cannot be joined against. "What was the rent in March" becomes a
program rather than a `WHERE` clause.

### B. Closed intervals `[from, to]` — rejected

More natural to read: a lease runs 1 January to 31 March.

Rejected for the arithmetic in §1. The predecessor's end has to be computed from the
successor's start, that computation appears in every revision, and it is wrong across
month boundaries and leap days. It also makes the no-overlap constraint express
adjacency as a one-day gap, which `&&` on a `daterange` handles for free.

### C. Timestamps rather than dates — rejected

Consistent with `delegation_grants` and with every `created_at` in the schema.

Rejected because an effective date has no time and no zone, and pretending otherwise
creates a five-and-a-half-hour window in which the rent is legally one number and
technically another. The consistency argument is answered by §2: `delegation_grants`
is a different kind of thing, and assertion 14 makes the distinction checkable rather
than a matter of taste.

### D. A trigger enforcing no-overlap — rejected

The obvious implementation, and it looks equivalent.

Rejected because it reads the table it protects, so two concurrent revisions of the
same flat both see no conflict and both commit. It is correct in review and wrong
under concurrency — the same shape as ADR-0011's rejected check-then-create, and the
same fix: make the second writer collide with an index rather than consult one.

### E. A `superseded_by` pointer instead of a `retired_at` flag — rejected

Slightly more informative: it names the replacement rather than just marking the row
dead.

Rejected because the exclusion constraint has to be able to exclude retired rows in
its `WHERE`, and a partial index on `superseded_by IS NULL` is the same predicate with
an extra join to interpret it. The link exists anyway, on the replacement, as
`corrects` — pointing forward from the new row rather than backward from the old one,
which is the direction that keeps the old row immutable.

### F. Bitemporal modelling — rejected, and the story says so

Two time axes: when a fact was true, and when we recorded it. It answers "what did we
believe about March, last Tuesday", which is genuinely useful in a dispute.

Rejected because it doubles the width of every effective-dated table, makes every
query state two dates, and makes the no-overlap constraint a four-dimensional
problem. The story puts it out of scope; §5 says explicitly what that costs rather
than leaving somebody to find out.

### G. A SQL function `as_of(validity, date)` — rejected

It would give the standard a name to point at.

Rejected because it is a second name for `@>`, and the extra name is the one somebody
forgets. The generated column already gives the standard something to point at, and it
is a column rather than a call, so an index can use it.

---

## Consequences

**What is now true.** Ownership history cannot be edited, deleted, or made to overlap.
An as-of query is `validity @> $1::date` and has one answer or none, for every date, by
constraint rather than by convention. A change and a correction are distinguishable
from the rows, without an audit log. Open-ended intervals are expressible only by
saying so. An effective-dated table added by a future ADR is policed by assertion 14
before anybody writes a test for it — demonstrated against ADR-0010's rent schedule
before that table existed. And platform can no longer quietly depend on a module.

**What this costs.** `valid_to` is exclusive, which reads oddly and will be queried
wrong at least once by everybody; the tests exist to make that a fast failure rather
than a slow one. Every effective-dated table needs a GiST index, which is larger and
slower to write than a btree — irrelevant at rental volumes, worth knowing at
marketplace ones. A correction leaves two rows where one would do, and the retired one
is invisible to every ordinary query, which means a `count(*)` on the table is not a
count of anything meaningful. And the pattern is copied per table rather than factored
out: PostgreSQL cannot inherit a constraint, so `rent_schedule` will repeat the
generated column, the two exclusion constraints and the append-only trigger. Assertion
14 is what stops a copy being made wrong.

**What is not decided.** Bitemporality (alternative F). The other four effective-dated
entities: rent schedule and lease parties are ADR-0010, society membership is the
community module, and the fee agreement has no issue yet. Whether `property_ownership`
should carry a document reference to the sale deed that evidences it. And the retention
question a correction raises — retired rows accumulate and nothing prunes them, which
is right for now and will not be right forever.
