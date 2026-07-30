# ADR-0015 — Durable workflow standard for money and document operations

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Platform, Payments
- **Issue**: [#26](https://github.com/tesserix/dwellm8/issues/26)
- **Related**: [ADR-0002](0002-event-backbone-and-outbox.md) (the platform organisation this needed, and the delivery guarantee it does not replace), [ADR-0006](0006-chart-of-accounts-and-posting-rules.md) (a compensation is a reversing entry), [ADR-0011](0011-payment-provider-adapter.md) (the idempotency key a retry depends on), [ADR-0012](0012-settlement-reconciliation-and-drift.md) (the backstop a workflow hands to when it gives up)

---

## Context

Mandate creation, autopay debits, payouts, refunds, stamping and eSign all span
systems. The failure that must never occur is tenant debited, owner not credited,
nobody notified.

That failure has a specific shape, and it is not "the request errored". It is a
process that did three of five things, reported nothing, and left no record of
which three. ADR-0002 made sure no *event* is lost that way. It said nothing about
a sequence of external calls, which is a different problem: an outbox guarantees a
fact eventually reaches a consumer, and guarantees nothing about a bank transfer
that happened after a fee was posted and before anything was written down.

Two things shaped this ADR more than the choice of engine.

**The org has already run this experiment.** HomeChef has Temporal in production —
namespace per product, `<product>-<domain>` task queues, `<product>:<domain>:<id>`
workflow ids, a worker sharing the API image with a command override, a PreSync
ArgoCD hook that registers the namespace before the worker can CrashLoop on its
absence. Reusing that is most of §6, and it is the cheapest part of this decision.
Reading its code also produced the two places this standard deliberately diverges,
both in §7 — and both are cases where a choice that is right for a notification is
wrong for money.

**A durable engine does not make an operation safe.** It makes it resumable, which
is a different property. Resumption is only safe if every activity presents the
same idempotency key on its second attempt as on its first, and compensation is
only *possible* if the irreversible step has not happened yet. Both of those are
properties of how the workflow is written, not of Temporal. So this ADR is mostly
about ordering and keys, and only incidentally about an SDK.

---

## Decision

**A rule that generates the list of durable operations rather than a list somebody
maintains; the irreversible step last, with the boundary declared and enforced;
compensations in reverse order, each reusing its step's key; a failed compensation
that escalates instead of failing; and a record of every run in our own tables,
because Temporal's retention is days and a dispute is not.**

### 1. The seam, and what this package is not

`internal/platform/workflow` holds the standard. Nothing in it imports the Temporal
SDK, and an arch test confines the SDK to a single adapter package — named now
(`internal/platform/workflow/temporalx`), created with the first workflow
implementation (#80). Same seam discipline as ADR-0011's `provider`, and the same
argument: the import leaks one convenient call site at a time.

The reason it is worth doing here is narrower than tidiness. The parts of this
standard that can actually go wrong — the order compensations run in, what may
still be undone, where a key is computed — are pure logic, and pure logic is
testable without a cluster. A standard whose only test needs a Temporal test server
is a standard nobody runs on a laptop, and the acceptance scenario for this story is
exercised in 20ms by `go test ./internal/platform/workflow/`.

What this means concretely: the SDK is not yet a dependency of this repository. The
API's `go.mod` still has one direct requirement. That is deliberate — adding gRPC,
protobuf and forty modules to ship zero workflows is a real cost — and it is the
one thing in this ADR that is a sequencing decision rather than a design one.

### 2. The rule, and the list it generates

**An operation must be a durable workflow when it changes state in more than one
system and a partial completion is not self-healing.**

Both halves matter. Multi-system alone is not enough: an invoice writes a row and
an outbox row in one transaction, so a partial completion cannot exist.
Not-self-healing alone is not enough: a failed webhook delivery is redelivered by
the provider. It is the conjunction that produces the failure this ADR exists to
prevent.

`Spec.Because` is a required field, so an operation cannot join the list without
saying which half of the rule puts it there, and a test rejects a clause short
enough to be a label. Eleven operations across six domains, each with a sentence
about what breaks:

| Operation | Compensable prologue | Irreversible step | Escalates after |
|---|---|---|---|
| `mandate.create` | yes | no | 24h |
| `mandate.amend` | yes | no | 24h |
| `mandate.revoke` | no | yes | 4h |
| `collect.autopay_debit` | yes | yes | 6h |
| `payout.execute` | yes | yes | 2h |
| `refund.issue` | yes | yes | 4h |
| `refund.deposit` | yes | yes | 4h |
| `refund.chargeback` | no | yes | 12h |
| `document.stamp` | no | yes | 4h |
| `document.esign` | yes | no | 48h |
| `recon.day` | no | no | 3h |

`recon.day` is here because ADR-0012 deferred its retry and backoff to this ADR.
It is also the operation that forced §6's tenancy decision.

**The negative half of the rule is load-bearing**, and it is the mistake teams make
in the month after adopting Temporal: wrapping a single-system transaction in a
workflow adds a distributed failure mode to an operation that had none. An invoice
is not a workflow. A ledger posting is not a workflow. A webhook ingest is not a
workflow — ADR-0011 already made it a table write with an index behind it, which is
strictly stronger.

### 3. Determinism, enforced rather than documented

A replayed workflow must make every decision the same way. The usual list — no wall
clock, no randomness, no I/O, no map iteration — is in every Temporal tutorial and
in none of them is it checked.

So it is a build failure. `TestNothingInAWorkflowReadsAClockOrRollsADie` parses the
workflow package and refuses `time.Now`, `time.Since`, the timer constructors,
`rand.*`, `uuid.New*`, and imports of `math/rand`, `database/sql`, `net/http`, `os`
and `pgx`. Each finding says what breaks rather than saying "non-determinism",
because that is not a thing anybody can act on at 3am.

Tests are excluded from this guard, which is a deliberate difference from ADR-0007's
float guard, where tests are included on purpose. A test is not replayed by
Temporal, so a clock read in one is not this bug — and a test asserting on a timeout
has to be able to name a duration.

The failure being prevented has no error in it. Temporal reports a non-determinism
error *sometimes*; when it does not, the workflow simply takes a different branch
than it took the first time, which for a payout means the second half of a
compensation running against a world that never had the first.

### 4. The irreversible step goes last

The single most valuable rule in this ADR, and it is a rule about the order steps
are written in rather than about any code.

A completed bank transfer cannot be compensated. Asking for the money back is a new
operation with its own failure modes, not an undo. So every reversible step happens
first and the irreversible external call is last. Ordered that way, the story's own
scenario — fee posted, transfer not yet attempted, something fails — is compensable:
reverse the posting and the world is where it started. Ordered the other way, the
same failure leaves money gone with no way to un-send it and no entry explaining
where it went.

After the irreversible step there is nothing to compensate, so everything remaining
must be retried until it succeeds. A workflow therefore has two phases and one
boundary, and `Saga` makes the boundary a declaration rather than a comment:

- `Step` refuses a step that offers a compensation *after* `PointOfNoReturn()` —
  the claim would be believed at review time and false at 3am.
- `Step` refuses a state-changing step with no compensation *before* it, because a
  later failure could not return to the starting state.
- `Step.ReadOnly` is how a step says it changes nothing. It is a flag rather than
  "`Undo` may be nil" because those are two different facts, and a nil conflates
  the harmless one with the bug.
- `Run` refuses an operation whose spec has an irreversible step and never says
  where it is.

**Compensations run in reverse order.** Not stylistic: a later step's effect may
depend on an earlier one, so undoing the earlier first can leave the later
compensation with nothing coherent to act on.

**A compensation reuses its step's key**, so a compensation retried five times is
one correction rather than five reversing entries. For a ledger step the
compensation *is* a reversing entry — ADR-0006 §3 makes that the only correction
there is — with a new reason, `workflow_compensated`. It is its own reason rather
than `operator_error` because nobody made an error: the entry was correct when it
was posted and a later step of the same operation failed. Recording it as an
operator's mistake puts a name against a decision no person made, on an immutable
row that will be read during a dispute.

**A failed compensation is worse than a failed step.** A failed step leaves the
world unchanged. A failed compensation leaves money moved and the record of it
uncorrected, which is the one state nothing downstream can reason about. So
`RetryCompensation` is the most patient of the three tiers, compensation continues
past a failure rather than stopping at the first (stopping leaves steps standing
that could have been undone), and the outcome is `Escalated` — never a clean
failure, because **a workflow that failed cleanly is a workflow nobody is looking
for**.

### 5. The case the story does not name

The story's scenario is a failure *before* the bank transfer. The adjacent scenario
is the transfer itself failing, and it is the one that matters more.

A timeout is not a decline. Whether the money left is unknowable from inside the
workflow, so the design refuses to guess: a failure at or after the point of no
return escalates and compensates nothing. Compensating would reverse the fee for a
payout that may well have gone out, and the next run would send it again.

`Result.PastNoReturn` records that the boundary was reached, and the schema is where
it stops being a convention:

```sql
CONSTRAINT workflow_runs_compensated_means_reversible CHECK (
    state <> 'compensated' OR NOT past_no_return)
```

A run past the boundary cannot be *recorded* as compensated, whatever wrote the row
— a data fix, a support script, a future workflow written by somebody who has not
read this. And `past_no_return` is monotonic, enforced by a trigger, because
otherwise the constraint could be satisfied by editing the evidence rather than by
the world being reversible: clear the flag, then record the compensation.

The run state machine is forward-only and exists in Go and in
`workflow_transition_allowed()`, with the contract test comparing all 25 ordered
pairs — the same two-copies price ADR-0011 §3 pays, for the same reason. `running →
escalated` is a direct edge on purpose: a step that fails past the boundary never
passes through `compensating`.

### 6. Where the key comes from, which is the whole of resumption

`IdempotencyKey(workflowID, step)` is `<workflow-id>#<step>`, computed in the
workflow and passed into the activity.

This is the single most important line in the package. An activity that generates
its own key — `uuid.New()`, `time.Now()` — produces a different key on every
retry, so the provider sees two requests and the tenant is debited twice. Derived
from the workflow id and the step name, every attempt presents the same key and the
provider's own deduplication does the rest. Deliberately **no attempt number and no
timestamp**: both would make a retry a new request, which is exactly the bug.

It composes with ADR-0011 §2. Our key never expires, so a workflow resumed after a
day of retries still cannot create a second collection — long after the provider's
own 24-hour key has gone. That property was written for a replayed queue and this
is what needed it.

The workflow id is `dwellm8:<operation>:<subject>`, deterministic and derived from
the domain entity. Two things follow and both are the point. Starting the same
operation for the same subject twice collides rather than creating a second workflow
— the same guarantee ADR-0011 §2 gets from a unique index, obtained the same way,
and `workflow_runs_workflow_idx` enforces it on our side too. And a support agent
holding a payout id can construct the workflow id without searching; a generated id
would mean a lookup table, and a lookup table can be missing exactly when somebody
needs it.

**Task queues and the namespace reuse HomeChef's conventions**: `dwellm8-<domain>`,
one namespace per product, registered by a PreSync hook, worker as a plain
Deployment sharing the API image with a command override. One queue per *domain*
rather than per operation, because a queue is a worker-fleet boundary and eleven
fleets for eleven operations is eleven things to watch for isolation nobody asked
for. `Domains()` enumerates them, so a domain with no worker is findable rather than
discovered by a workflow that stays queued forever with nothing reporting an error.

### 7. Two deliberate divergences from what is already in production

Both come from reading HomeChef's `apps/api/temporal`, and both are cases where a
choice that is right for a notification is wrong for money.

**An absent Temporal is a startup failure, not a fallback.** HomeChef's client is
opt-in: `Config.Enabled()` is false when `TEMPORAL_HOSTPORT` is unset and callers
fall back to inline execution. For a notification that is the right trade — an
inline send is worse than a durable one and better than none. For a payout,
"Temporal was not configured, so we did it inline" is a payout with no compensation,
no resumption and no record, and it is indistinguishable from a healthy deploy
because nothing errored. So `Config.Validate()` refuses an empty host, an empty
namespace, and another product's namespace. Same move as ADR-0011's empty webhook
secret rejecting every delivery rather than trusting every delivery.

**An unwired activity must not report success.** HomeChef's activities reach their
dependencies through package-level function variables, with a nil guard:

```go
func GatewayRefundActivity(ctx context.Context, in DeferredRefundInput) (string, error) {
	if GatewayRefundFunc == nil {
		return "", nil
	}
	return GatewayRefundFunc(...)
}
```

A worker that forgot to wire the seam returns `("", nil)`, the workflow reads an
empty refund id, treats it as nothing-to-do, and **completes successfully** having
issued no refund. It is a misconfiguration that reports success, and it is invisible
in every log.

To be fair to that code: no money is lost there, because a cron
(`RetryDeferredCancelRefunds`) sweeps the same sentinel independently and both paths
present the same idempotency key. The refund lands on the next tick. But that is the
backstop rescuing a workflow that claimed to have done the job — which makes the
backstop load-bearing for a reason nobody wrote down, and means the metric that
would reveal the misconfiguration (workflows failing) stays flat.

So Dwellm8's activities take their dependencies through a struct registered as an
activity struct, and an unwired dependency is a startup panic. A worker that cannot
do its job must not start; it must not run and quietly agree.

The rest of HomeChef's pattern is adopted unchanged, and the cron backstop is
adopted as a *principle* rather than as a rescue — see §9.

### 8. Observability, and why Temporal is not the record

The story asks how an in-flight workflow is inspected during a support call. The
answer is not "give the support agent namespace access", for two reasons: a
namespace is not tenant-scoped, and its retention is days.

So every durable operation records its own progress in `workflow_runs` and
`workflow_steps`. **Temporal is the executor; these tables are the record.** The
steps table stores the idempotency key each step actually presented, because the
answer to "was the tenant charged twice" has to be what was sent, not what today's
code would send. A retried activity updates its row rather than adding one, with an
attempts counter — forty retries over a day must read as one step that took forty
attempts, not as forty steps, because the trail is for a person and an unreadable
trail is the same as none.

Both tables are ordinary ADR-0003 tenant-scoped tables, and getting there required
a decision. A platform-wide operation — the nightly reconciliation — has no
organisation, which is how ADR-0012 ended up with four nullable-tenant tables. Here
that was avoidable: a platform-wide run carries **the platform organisation**,
exactly as ADR-0002 §1 requires of a platform-level event. `tenant_id` stays NOT
NULL, the tables get the five-part contract instead of the platform-inbox argument,
and assertion 12's list does not grow.

That decision surfaced a latent gap. ADR-0002 has assumed a platform organisation
since it was written, `workflow_runs.tenant_id` has a foreign key to
`organisations`, and **no such row existed**. The first platform-level event would
have failed that key in production, having passed every test that never wrote one.
It is seeded now — with the same uuid the money domain already uses as the platform
*party*, because two magic uuids that both mean "us" is a thing every reader has to
look up.

Neither table has a delegated branch, and the omission is deliberate: a run carries
no `property_id`, so there is nothing for `is_delegated_unit()` to judge, and a
grant-level branch would hand a management firm every durable operation of the owner
that granted it — including payouts to that owner's bank account, which is not the
firm's business.

### 9. Timeouts, escalation, and giving up to somebody

Four timeout knobs, and the one that encodes intent is `ScheduleToCloseTimeout` —
the total retry window. `StartToCloseTimeout` bounds an attempt; the window is what
says "retry until it lands". No retry tier caps attempts, because the question for a
money call is never how many times but for how long.

`Spec.Escalate` is how long a step may wait on something outside this system — a
provider callback, a signatory, a human — before an operator is told. It is
deliberately **not a failure deadline**: a workflow that fails while money may be in
flight destroys the only record that it is. `Spec.Validate()` refuses an escalation
deadline at or beyond the operation's own budget, because such an operation
escalates to nobody — by the time the deadline passes the workflow is over. A test
additionally refuses any money operation that would wait more than 24 hours;
documents are exempt, because an eSign genuinely waits on people.

And when a workflow does give up, it hands the problem to ADR-0012 rather than
dropping it: an escalated run is a row in an ageing report with an owner. The
generalisation of HomeChef's cron backstop is that **a durable workflow does not
remove the need for a reconciling sweep**. The workflow makes the common case
correct; reconciliation is what notices the case the workflow could not finish.

### 10. Versioning

Additive only, and the rule is ADR-0002 §8's rule about event schemas applied to a
different published interface: **an activity's input struct is a published
interface, because a workflow started last week is still running and will call
today's worker.** Add fields; never remove, rename or retype one. Never delete an
activity a running workflow might call.

For behaviour changes, `workflow.GetVersion` for small ones and **a new workflow
name for anything structural**. A `GetVersion` branch is permanent code nobody
deletes, and after four of them the workflow is unreadable — which is its own
correctness risk in a function whose whole job is to be predictable. Temporal's
Worker Versioning (build ids) is the better long-term answer and is not adopted
yet: it is a deployment mechanism, and there is no worker deployment to change.

### 11. What fails the build

- `internal/platform/workflow` — the payout acceptance scenario, reverse-order
  compensation, the transfer-may-have-landed case escalating rather than
  compensating, a failed compensation escalating and naming every step it could not
  undo, the eight declaration-time rules, key determinism over 100 calls and across
  steps and subjects, threshold and spec coherence, and the retry tiers being
  patient in the right order.
- `internal/platform/arch` — the SDK confined to one adapter, and no clock, timer,
  randomness, uuid or I/O inside the workflow package.
- `internal/money/store` — the run state machine over all 25 pairs, the
  compensated-means-reversible constraint evaluated over its four combinations,
  every operation's id round-tripping through unbounded columns, and the platform
  organisation existing.
- `internal/platform/tenancy/isolationtest` — ADR-0003's five-part contract on both
  tables, a run past the boundary refusing to be recorded as compensated, the
  boundary refusing to be un-passed, forward-only transitions with the self-edge
  permitted, the same operation started twice colliding, and forty attempts reading
  as one step.

CI plants six failures and expects red: the compensation constraint dropped, the
monotonic flag neutered, assertion 13 against a missing CHECK and against a missing
trigger, the SDK imported into `money/domain`, and a wall-clock read in the workflow
package. They join the ADR-0003, -0005, -0006, -0007, -0009, -0011 and -0012 guards.

### 12. Assertion 13, and this schema's oldest trap

Not planned, and the most useful thing to come out of implementing this ADR.

**A `CHECK` written inside `CREATE TABLE IF NOT EXISTS` is skipped entirely on a
database that already has the table.** The file replays, exits 0, reports nothing,
and the constraint is absent — present in CI, where every database is fresh, and
missing in the one place it matters. It bit `journal_entries_kind` during ADR-0012
and the reversal reasons during this one; both were fixed one at a time with a
migration. Measured, on a database where one had been dropped by hand:

```
$ psql -v ON_ERROR_STOP=1 -f dwellm8.sql      # exit 0, no output
$ SELECT count(*) FROM pg_constraint
    WHERE conname = 'workflow_runs_compensated_means_reversible';
  0
```

Two changes replace the one-at-a-time fixes. Every CHECK an ADR argues for now
lives in one idempotent block rather than inside its `CREATE TABLE`, so there is a
single definition in a position that reaches every database — duplicating them
inline *and* in a migration was the obvious fix and is worse, because two
definitions of one rule drift and the one that runs is the one nobody reads. And
**assertion 13** asserts twelve constraints and triggers by name, so a rule that
goes missing fails the bootstrap instead of a review.

Two things about assertion 13 are worth stating honestly. The list cannot be
derived — there is no way to ask PostgreSQL which constraints a file *meant* to
create — so it is a list somebody maintains, and what makes it worth having anyway
is that it fails the build. And it does not catch a replay that aborted half way;
that case is already loud, because the job fails.

Implementing the healing block ran into this file's *other* oldest trap, which is
worth recording because the guard for one trap was defeated by the other.
`ALTER TABLE ADD CONSTRAINT` validates existing rows, so a constraint that went
missing and let bad data in cannot simply be restored. The block therefore counts
violating rows first — and the first version counted without the row-level security
window that the migrations section exists for. The bootstrap connects as the table
owner, FORCE row level security applies to the owner, no `app.tenant_id` is set, so
the count came back 0 from a table with a violating row in it, the block concluded
the table was clean, and the `ALTER` failed anyway because DDL validates every row
regardless of any policy. The symptom was the exact error the block exists to avoid,
produced by the check meant to avoid it.

With the window in place the behaviour is what ADR-0009's backfill established as
this file's convention — warn, do not abort:

```
WARNING:  1 row(s) in workflow_runs violate workflow_runs_compensated_means_reversible,
          so it is not being added. Those rows were written while the rule was absent
          and need a decision, not a migration; assertion 13 will fail until they are
          dealt with.
ERROR:    the rule(s) this schema is built on are missing:
          workflow_runs_compensated_means_reversible
```

Loud, specific and actionable, without holding every unrelated statement in the file
hostage to rows somebody has to look at.

---

## Alternatives considered

### A. HTTP handlers with retry loops — rejected

What the code would look like without this ADR, and what most products ship.

Rejected because a retry loop lives in a process. The pod rolls during every deploy,
and a loop halfway through a payout leaves no record of which half. It is the same
argument ADR-0002 made about publishing an event after commit, one level up: the
window is unprotected and the loss is silent.

### B. Compensations as a queue of "undo" events — rejected

Publish `payout.fee-posted-needs-reversal` and let a consumer undo it. Fits the
existing outbox, needs no new engine.

Rejected because the undo has to be ordered against the steps that are still
running, and an event bus has no notion of "after step 3 and before step 4". It also
inverts the failure: a compensation that never gets consumed is invisible, where the
whole point is that a failed compensation is the loudest thing in the system. The
outbox is right for facts; this needs control flow.

### C. An amount-and-state table polled by a cron, with no engine — rejected

A `pending_payouts` table with a state column, swept every minute. Genuinely simpler,
and it is what HomeChef's cron backstop is.

Rejected as the primary mechanism because every workflow becomes a hand-rolled state
machine with its own retry policy, its own timeout handling and its own idea of what
"stuck" means — eleven of them, each subtly different, none of them inspectable while
running. It is retained as a *backstop* (§9), which is the role it is good at.

### D. Everything money-related as a workflow — rejected

The instinct after adopting Temporal, and the reason the rule in §2 has a negative
half. An invoice is a single transaction; wrapping it in a workflow adds a network
hop, a serialisation boundary and a new failure mode to an operation that had none,
and it makes the durable list meaningless — a list containing everything cannot tell
anybody what is dangerous.

### E. A random or sequential idempotency key generated in the activity — rejected

The default if nobody decides, because `uuid.New()` at the top of a function looks
like ordinary hygiene.

Rejected because it is the double-debit bug precisely. A key generated inside an
activity is a new key on every attempt, so the provider's deduplication — the thing
actually protecting the tenant — sees two distinct requests. This is the one rule in
the ADR with a build-time guard on the specific call.

### F. Compensating past the point of no return by "sending the money back" — rejected

Tempting, because it makes every workflow uniformly compensable and removes the
boundary.

Rejected because a reverse transfer is a new operation with its own failure modes,
its own timing and its own possibility of being lost — so "compensation" would mean
"start a second workflow that might also fail", and the invariant that a compensated
saga returns to its starting state would be false. Worse, it would be attempted on
the ambiguous case: a transfer that timed out may have landed, and sending money back
that never left is a second loss. Escalating to a person is the correct handling of
genuine ambiguity.

### G. `workflow.GetVersion` for every change — rejected

The SDK's own recommendation, and right for small behavioural changes.

Rejected as a general policy because a `GetVersion` branch is permanent: it must
stay as long as any execution started before it might still be running, which for a
30-day chargeback workflow is a month, and in practice forever because nobody
audits. Four of them make a workflow unreadable, and unreadability is a correctness
risk in a function whose job is to be predictable. Structural changes get a new
workflow name instead, which costs one deployment and leaves the old one to drain.

### H. Adding the Temporal SDK now, with the standard — rejected for now

It would let the standard be exercised against Temporal's own test framework, which
is a real loss.

Rejected because the acceptance criteria that can be tested are testable without it,
the one that cannot (a killed worker resuming) is Temporal's guarantee rather than
ours, and the SDK brings forty modules into a repository that currently has one
direct dependency and no worker binary. The arch test names the adapter package now,
so the seam is decided before there is pressure to put the import somewhere
convenient.

### I. A nullable `tenant_id` on `workflow_runs` for platform-wide operations — rejected

What ADR-0012 did for its four tables, and the consistent-looking choice.

Rejected because it was avoidable here and the exception has a real cost: a
nullable-tenant table cannot use the ordinary policy shape, has to be platform-write
only, and needs assertion 12's argument. ADR-0002 §1 had already decided that a
platform-level fact carries the platform organisation, so reusing that keeps
`tenant_id` NOT NULL and keeps these tables ordinary. The exception is for tables
whose rows genuinely belong to nobody — a settlement line for an unknown payment —
not for tables whose rows belong to us.

---

## Consequences

**What is now true.** The durable list is derived from a stated rule and every entry
says which half of it applies. The irreversible step is last, declared, and enforced
at declaration time rather than reviewed. Compensations run in reverse, reuse their
step's key, continue past a failure, and escalate rather than failing quietly. A run
that passed the point of no return cannot be recorded as compensated — not by the
saga, not by a script, not by a migration. An activity's idempotency key comes from
the workflow, and a build fails if anything in that package reads a clock or
generates an id. Every run and every step is recorded in our own tables with the key
that was actually presented, so a support call a month later has something to read.
A process that serves a money operation will not start without Temporal. And twelve
constraints this schema is built on now fail the bootstrap if they go missing.

**What this costs.** A worker fleet per domain is six Deployments to watch, and a
domain whose worker is not deployed produces workflows that sit in a queue — which
`Domains()` makes enumerable but does not make monitored. The standard's tests cover
the logic and not the executor, so the first real workflow will find things about
Temporal that these tests cannot. Every activity carries an idempotency key through
its signature, which is boilerplate, and it is the price of at-least-once by another
name. The run record duplicates some of what Temporal already knows, and the two can
disagree — the tables are authoritative and Temporal is the aid, which has to stay
true as people get comfortable with the Temporal UI. And `workflow_steps` grows with
every attempt of every operation, so it will need the same pruning discussion the
outbox has.

**What is not decided.** The Temporal SDK dependency and the `temporalx` adapter,
which arrive with the payout workflow (#80). The worker Deployment, its NetworkPolicy
— cross-namespace, so the ambient-mesh HBONE and per-product ingress rules apply —
and its Kargo wiring. Whether Dwellm8 gets its own Temporal cluster or shares the
platform one; HomeChef ended up with its own, and the condition that forced it is
worth knowing before choosing. Activity heartbeating for the long document
operations. Where escalation notifications go, which is the `notify` module's. And
the pruning policy for `workflow_steps`.
