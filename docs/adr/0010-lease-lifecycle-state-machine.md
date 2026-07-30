# ADR-0010 — Lease lifecycle state machine

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Platform, Owner
- **Issue**: [#11](https://github.com/tesserix/dwellm8/issues/11)
- **Related**: [ADR-0008](0008-effective-dating-and-temporal-queries.md) (the interval that stops the billing), [ADR-0006](0006-chart-of-accounts-and-posting-rules.md) (why charges cannot be deleted), [ADR-0009](0009-property-block-unit-model.md) (the unit a tenancy is of), [ADR-0002](0002-event-backbone-and-outbox.md) (the events, three of which ADR-0001 already declared), [ADR-0005](0005-owner-delegation-grants.md) (whether a firm may act)

---

## Context

The lease is the object every other module hangs off. Invoicing bills against it,
documents are generated for it, deposits are held under it, notifications are sent
about it. So its states have to be fixed before any of those exist, or four modules
each invent their own idea of what "ended" means.

The story supplies a list of eight states and a set of scenarios. Two of the eight
turned out to be wrong, in opposite directions, and finding that was most of the work.

---

## Decision

**Eight stored states with a forward-only machine in Go and in PostgreSQL; the agreed
term and the actual end kept as separate facts; billing driven by an ADR-0008 interval
rather than by a flag; renewal as a new lease that starts exactly where its predecessor
ends; and one flat that cannot be let twice over the same days.**

### 1. The agreed term and the actual end are different facts

The most important decision here, and the one the first version of this model got
wrong.

A lease agreement says "1 July 2025 to 30 June 2026". That is what the parties signed
and it never changes, whatever happens next. Whether the tenancy actually ran that long
is a separate fact. So `valid_from`/`valid_to` are the agreement, immutable, and
`ended_on` is the date occupancy actually ceased.

```sql
validity daterange GENERATED ALWAYS AS
         (daterange(valid_from, LEAST(valid_to, ended_on), '[)')) STORED
```

`LEAST` ignores NULLs, which is exactly the arithmetic wanted: unbounded when both are
absent, whichever exists when one is, and the earlier when both do. `Lease.Occupancy()`
computes the same thing in Go and the store contract test evaluates both over five
cases including "ran to term", which is the one a naive `prefer ended_on` extends past
what was signed.

The first version had one interval and terminated by *closing* it. That is wrong twice
over, and the test caught it immediately: a fixed-term lease's interval is already
closed, so there was nothing to close — and shortening it would have edited the
agreement to say the parties had signed up for less than they did.

### 2. Billing stops because an interval ended

"A terminated lease stops billing exactly once" is the story's hardest requirement and
it needs no mechanism of its own.

Charge generation is the rent schedule intersected with `leases.validity`. Terminating
sets `ended_on`, which shortens `validity`, so the generator stops producing periods —
there is no flag for anything to check and no code path that has to remember. And
"exactly once" is ADR-0006 §6's invoice idempotency key on `(lease, period)`: the same
period cannot be billed twice because the second insert loses a race against a unique
index.

Two states bill: `active` and `in_notice`. **Notice announces an ending and does not
perform it.** Getting that backwards is the more expensive mistake, because it is a
month of rent the owner never sees and the tenant never disputes.

### 3. The machine, and the one edge that goes backwards

```
draft ──> pending_signature ──> active ──> in_notice ──> terminated ──> settled
  │              │                │  ↑         │
  └──> lapsed <──┘                │  └─────────┘
                                  └──> renewed
```

Ten transitions. `from == to` is a permitted no-op, for ADR-0011 §3's reason: a
redelivered event asks for the state the lease is already in.

**`in_notice → active` is the only backward edge**, and it is here because withdrawing
notice is a real thing people do — notice is served, terms are renegotiated, the
tenancy continues. Refusing it would make the workaround a new lease, which loses the
ledger history the tenancy already has. It is therefore the one transition that
requires a recorded reason, enforced by the trigger, because it is the one that makes
the history non-monotonic and the one somebody will later ask about.

Who may cause each transition is data rather than a switch in a handler, because the
interesting question is "may a tenant do this" and it has to be answerable without
reading the handler. A tenant may serve notice and may sign; that is all. A tenant may
not withdraw their own notice — that is a negotiation, and the owner records it. The
clock may lapse an unsigned lease and may not terminate a live tenancy.

ADR-0005 still decides whether a management firm's grant permits the act; `Actor`
records which side acted, which is the first question in any dispute.

### 4. Two changes to the story's list of states

**`lapsed` is added.** The story's edge case is a lease whose tenant never signs, and
its list has nowhere for one to go. Without `lapsed`, an unsigned draft ends up
`terminated` — which conflates a tenancy that ran and ended with one that never
started. Those differ in every report and, more importantly, in whether there is a
deposit to settle at all: `State.DepositSettleable()` is true only for `terminated`.

**`expiring` is removed, and derived instead.** It is a fact about a clock, not about
anything that happened. Storing it would need a job to move leases into it, and a lease
that should be `expiring` and is not would be a silent bug — the same argument ADR-0012
§7 makes about ageing buckets, and the same conclusion. `Lease.Expiring(asOf, within)`
and the `lease_expiring` view are one definition read by the renewal reminder and the
owner's dashboard.

The view also exposes `inside_notice_window`, because whether notice can still be given
in time is the thing an owner actually needs and is one subtraction away from being got
wrong in a dashboard.

**`renewed` is kept and is deliberately not `terminated`.** The tenant did not leave, so
the deposit carries into the successor; returning it at renewal would mean collecting it
again a day later.

### 5. One flat, one tenancy — and drafting is still allowed

```sql
EXCLUDE USING gist (tenant_id WITH =, unit_id WITH =, validity WITH &&)
  WHERE (state IN ('active', 'in_notice', 'renewed', 'terminated', 'settled'))
```

A double-let is two families with keys, two rent rolls, and a dispute nothing in the
data can settle. This is ADR-0008's machinery applied to the thing that matters most in
a rental product.

The scope is the half that would be lost by making it simpler. `draft` and
`pending_signature` are excluded because two competing offers on one flat is how
letting works, and refusing them would mean an owner cannot prepare a renewal while the
current tenancy runs. `lapsed` is excluded because it never was a tenancy.

That scope exists twice — as `State.Tenancy()` in Go and as the constraint's `WHERE` —
so the store contract test compares them state by state. A disagreement means either
two tenancies of one flat are possible, or an owner cannot draft a renewal, and neither
would announce itself.

Because `validity` is the *occupancy* interval, a tenancy that ended in June does not
block a new one starting in July even though its agreement ran to December. That falls
out of §1 rather than needing a rule.

### 6. Renewal is a new lease, contiguous by construction

`renews_lease_id` points backward from the successor. The predecessor keeps its id,
which is what "preserves its ledger history" means: two years of postings hang off that
id, and mutating the row would silently re-attribute them to a different set of terms.

Three constraints:

- **Contiguous.** The successor's `valid_from` must equal the predecessor's `valid_to`.
  ADR-0008's half-open interval doing the work: a gap leaves a day unbilled and an
  overlap is a double-let. Enforced by a trigger, because it is a cross-row rule.
- **At most once.** A partial unique index on `renews_lease_id`. Two successors would
  each claim the tenancy continued, and the deposit would have two places to go.
- **Same unit.** A renewal of a tenancy of a different flat is a new letting.

### 7. The failure scenario, and the half that was already impossible

A termination effective before the last invoiced period must require an explicit
decision rather than silently deleting charges.

Half of that is already true and was true before this ADR: ADR-0006 §3 revokes DELETE
and UPDATE on the ledger and refuses both again in a policy, so charges cannot be
deleted by anybody. What remains is that somebody must *decide*, and a closed
vocabulary is how a decision is distinguishable from a shrug: `adjust`, `refund`,
`forfeit`, and `none` — where `none` is only legal when nothing was over-billed.

`Lease.Terminate` refuses `none` when the effective date precedes what has been billed,
and refuses a real decision when nothing was over-billed — because that would credit a
period nobody was charged for. `ErrDecisionRequired` is distinguishable, since the
caller's response is to ask a person rather than to retry.

The schema enforces the same thing by **asking the ledger**:

```
ERROR:  lease … is ending 2026-05-21 and charges are raised through 2026-06-01:
        an over-billed period exists, so adjust it, refund it, or forfeit it —
        but say which
```

The contract it depends on is that a lease charge is a journal entry with
`source_kind = 'lease_charge'` and `source_id` the lease id. **That convention belongs
to the invoicing story and is not yet written**, so until it is, the trigger finds
nothing and permits everything. That is stated here rather than discovered, and the
isolation test writes such an entry itself — which is what makes the convention a
contract rather than a hope.

The trigger is SECURITY INVOKER, so the lookup runs under the writer's own row-level
security. A session that cannot see the ledger gets no rows and the check fails open
rather than reading another organisation's charges. Failing open is the wrong direction
and is accepted here for one reason: the alternative fails *closed* on a legitimate
termination by a delegated firm, which would make the product unusable for the case it
exists to serve.

### 8. Lock-in and early exit

`notice_days` and `lock_in_until`, with the two shapes that are contradictions refused:
a lock-in that expired before the tenancy started is a data-entry error, and one that
outlasts the tenancy locks the tenant in past the end of their own lease — not a term
anybody agreed to.

An early exit inside lock-in is not a separate mechanism: it is a termination, and
`forfeit` is the decision that says what a lock-in clause is for. Making it a decision
with a name means an owner cannot make it by default.

### 9. Events

ADR-0001 already declared `lease.tenancy.started`, `lease.tenancy.ended` and
`lease.notice.served` before this ADR existed, so those are used unchanged.
`lease.tenancy.renewed`, `lease.tenancy.lapsed`, `lease.tenancy.settled` and
`lease.notice.withdrawn` are additive under ADR-0002 §2's
`<module>.<aggregate>.<past-tense-verb>` rule, and a test asserts all seven follow it —
with `withdrawn` on a named exception list, because "-ed" is a spelling heuristic and
English has irregular participles.

`draft → pending_signature` publishes nothing, on purpose: nobody outside the lease
module cares that a document was sent, and an event with no consumer is one somebody
will later wire something to by accident.

### 10. What fails the build

- `internal/lease/domain` — the termination scenario including that the agreement is
  left as signed, the retrospective-termination refusal and its inverse, the full
  transition table over all 64 pairs, terminal absorption, what each state means for
  money, who may move a lease, renewal contiguity and single-use, occupancy over five
  shapes, lock-in contradictions, the event naming rule, and validation.
- `internal/lease/store` — the state machine against `lease_transition_allowed()` over
  64 pairs, three vocabularies against their CHECKs, the double-let scope against
  `State.Tenancy()` state by state, the occupancy interval against PostgreSQL's
  `LEAST`, and the expiring view being derived and `security_invoker`.
- `internal/platform/tenancy/isolationtest` — ADR-0003's five-part contract on
  `leases`, the double-let refusal with adjacency and drafts still permitted,
  termination shortening occupancy while leaving the agreement, re-letting from the day
  occupancy ceased, the retrospective-termination trigger against a real ledger entry,
  renewal contiguity in both failure directions, the agreed term refusing four edits,
  and a rent revision answering correctly on both boundary days.

CI plants four failures and expects red: the transition function narrowed, the
double-let scope widened to include drafts, the retrospective-end trigger neutered, and
the double-let constraint dropped entirely.

**Assertion 14 policed `rent_schedule` before this section existed.** It is
column-driven, so it found the table by its `valid_from` rather than by anybody adding
it to a list — the same property that made ADR-0009's assertion 6 catch ADR-0011's
payments table.

**A limit of assertion 13, found while planting these defects.** It checks that a named
constraint *exists*, not that its definition is right, and the schema's `ADD CONSTRAINT`
blocks are guarded by `IF NOT EXISTS` — so a constraint that exists with the wrong body
is neither corrected by a replay nor reported by the assertion. The double-let scope is
covered anyway, by the contract test that compares it against Go. Where such a
comparison is not possible, this gap is real.

---

## Alternatives considered

### A. One interval, terminated by closing it — rejected, after being built

The first version of §1. Rejected because a fixed-term lease's interval is already
closed, so there was nothing to close, and shortening it would rewrite the agreement to
say the parties signed up for less than they did. The tests found it in the first run;
it is recorded because the model reads perfectly well until you try to terminate
anything.

### B. `expiring` as a stored state — rejected

What the story asked for. Rejected because it is a fact about a clock: something has to
move leases into it, and a lease that should be expiring and is not is a silent bug —
a renewal reminder that never fires, discovered when a tenancy has already lapsed. Same
argument and conclusion as ADR-0012 §7's ageing buckets.

### C. No `lapsed` state, using `terminated` for unsigned leases — rejected

Keeps the story's list intact. Rejected because it makes an unsigned draft and a
completed tenancy the same state, so every report has to distinguish them by looking at
whether charges exist, and `DepositSettleable()` becomes true for a lease that never
held a deposit.

### D. A `billing_stopped_at` flag rather than an interval — rejected

The obvious implementation of "charges stop", and it is what makes "exactly once" hard.
Rejected because a flag has to be checked by every generator, and the one that forgets
bills a terminated tenancy — which is a charge against somebody who has moved out and
will not be paying it. The interval cannot be forgotten, because the generator has
nothing to iterate over.

### E. Renewal as a mutation of the existing lease — rejected

Update `valid_to`, update the rent, done. It is what a CRUD screen would naturally do.

Rejected because the ledger history hangs off the lease id: two years of postings would
silently become postings under the new terms, and the question "what was the rent when
this deposit was taken" would have one answer where it needs two. It also loses the
audit trail of what was agreed and when, which is the thing a renewal dispute is
about.

### F. Notice as a boolean on the lease rather than a state — rejected

`notice_served_on date`, and `in_notice` becomes `notice_served_on IS NOT NULL`.
Slightly fewer states.

Rejected because it makes the transition table incomplete: `in_notice → terminated` and
`in_notice → active` are different transitions with different permitted actors and
different events, and a boolean cannot express that only the owner may clear it. It also
makes the one backward edge invisible — clearing a boolean records nothing, and that
edge is precisely the one that needs a reason.

### G. Refusing `in_notice → active` — rejected

It would make the machine strictly forward-only, which is tidier and matches ADR-0011's
payment machine.

Rejected because withdrawing notice is common: notice is served, terms are
renegotiated, the tenancy continues. The workaround would be to terminate and create a
new lease, which loses the ledger history and produces a spurious
`lease.tenancy.ended`. Recording the real thing with a mandatory reason is better than
recording a fiction cleanly.

### H. Enforcing the retrospective-termination rule only in Go — rejected

The schema's version needs a cross-table lookup, which is unusual for a trigger here.

Rejected for the reason every other double-enforcement in this schema exists: a data
fix, a support script, or a future workflow written by somebody who has not read this
would otherwise bypass it, and the failure is money charged to a tenant who has moved
out. The cost — a coupling to the invoicing convention, and a check that fails open
until that convention exists — is stated in §7 rather than hidden.

---

## Consequences

**What is now true.** A lease's agreed term cannot be edited, and an early exit is
recorded as what it is. Billing stops because an interval ended, so no generator has a
flag to forget. One flat cannot be let twice over the same days, while two competing
drafts remain legal. A renewal starts exactly where its predecessor ends, at most once,
on the same unit, and the predecessor keeps its id and its postings. A tenancy cannot
reach `terminated` without saying who ended it, why, and what happened to the money —
and a retrospective end with no decision is refused by asking the ledger. Notice can be
withdrawn, and only with a reason. `expiring` is derived, so it is right tomorrow.

**What this costs.** The lease has four date-ish columns — `valid_from`, `valid_to`,
`ended_on` and `lock_in_until` — plus a generated range, and the difference between the
second and the third will be explained to every new engineer at least once. The
retrospective-termination trigger couples the lease table to a convention the invoicing
story has not yet established, and fails open until it does. The child tables'
delegated branch is an `EXISTS` against `leases`, which is one definition of who may
see a tenancy but is also a subquery in a hot policy. And `in_notice → active` means the
machine is not monotonic, so nothing downstream may assume a lease that was in notice
is ending.

**What is not decided.** Agreement document generation and the eSign ceremony, which are
MVP 2 and ADR-0015's `document.*` operations. The move-out settlement workflow, MVP 2.
The invoicing story that establishes the `lease_charge` convention §7 depends on, and
which is what turns that trigger from correct-but-inert into enforcing. Deposit
apportionment across co-tenants who joined at different times, which `lease_parties` now
makes answerable and nothing yet answers. Society membership as an effective-dated
record, which is the community module's. And what happens to a lease when its unit is
subdivided — ADR-0009 has the tree, and nothing says whether a tenancy follows the
parent or the children.
