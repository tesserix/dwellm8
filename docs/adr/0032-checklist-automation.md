# ADR-0032 — Checklist automation: a template resolved by process and kind, a task graph, and a blocking step that refuses a close

- **Status**: Accepted
- **Date**: 2026-08-01
- **Deciders**: Platform, Product, Operations
- **Issues**: [#202](https://github.com/tesserix/dwellm8/issues/202)
- **Related**: [ADR-0001](0001-modular-monolith-api.md), [ADR-0005](0005-owner-delegation-grants.md), [ADR-0008](0008-effective-dating-and-temporal-queries.md), [ADR-0010](0010-lease-lifecycle-state-machine.md), [ADR-0015](0015-durable-workflow-standard.md), [ADR-0029](0029-the-resident-scope.md)

---

## Context

A move-in is fifteen steps and a move-out is twenty. Today they are a person's
memory, and the person is on their fourth property of the day.

Three things about that make this more than a to-do list.

**The steps are not independent.** The deposit cannot be settled before the final
meter reading is taken, and the meter reading cannot be taken before the keys are
collected. A list that lets a manager tick the last box first is a list that
records a process which did not happen.

**Some of the steps are the reason the tenancy may close at all.** ADR-0010 says
a terminated lease's deposit becomes settleable and its billing stops. If the
final inspection was never done, that transition has re-let a flat nobody looked
at and released money against damage nobody assessed. The checklist is therefore
not decoration on the lifecycle — for some steps it is a precondition of it.

**A hostel move-out is not a commercial one.** A commercial exit has a dilapidation
schedule and a GST reversal; a PG exit has a bed to reallocate and a warden to
hand keys to. One list cannot serve both, and per-organisation configuration
alone does not solve it either: a firm that manages a tower and a co-living block
needs two lists and has one organisation.

And a fourth thing, which is what makes an abandoned checklist worse than no
checklist: a half-completed process that nobody is looking at reads, on every
screen, as a process that is under way.

---

## Decision

**Checklists live in the `maintenance` module. A template is resolved by process
and property kind with an organisation override; an instance is a task graph with
owners, due dates and dependencies; and a step marked blocking is enforced twice —
in Go so the refusal is legible, and in PostgreSQL so a path that never went
through Go cannot close a tenancy round it.**

### 1. `maintenance` owns it, and the lease module asks

ADR-0001 fixes eight modules and a checklist is not a ninth. It is operational
work with an owner and a due date, which is what `maintenance` is for: the role
exists, `maintenance.read`/`maintenance.write` are already in ADR-0005's closed
vocabulary of delegable permissions, and a management firm that may work tickets
on a unit is the same firm that may work its move-out.

The lease module reaches it through `maintenance/service`, never its store — the
seam ADR-0001 §3 requires and the boundary test enforces.

### 2. A template is resolved, not chosen

Five processes: `move_in`, `move_out`, `owner_onboarding`, `manager_handover`,
`tenancy_renewal`. A template names a process and, optionally, the property kind
it is for — `properties.kind`, the vocabulary that already exists, rather than a
new "vertical" column that would immediately disagree with it.

Resolution is most-specific-wins, in one order:

1. this organisation's template for this process **and** this property kind
2. this organisation's template for this process, any kind
3. the platform default for this process **and** this kind
4. the platform default for this process, any kind

A firm that has never configured anything gets a sensible move-out. A firm that
has configured one for co-living gets theirs for the co-living block and the
default for the tower — which is the case that made "configurable per organisation"
insufficient on its own.

Templates are versioned and an instance snapshots the version it was fired from.
Editing a template must not silently rewrite what a manager is halfway through.

### 3. An instance is a graph, and the graph is materialised at trigger time

Firing a checklist writes every task in one transaction: title, owner, due date,
blocking flag, and the tasks it depends on. Nothing is computed lazily on read.

Due dates come from an **anchor** — the move-out date, the handover date, the
tenancy start — plus a signed offset in days held on the template step. A step is
"three days before the tenant leaves" or "seven days after handover", and both are
one integer. The anchor is supplied at trigger time because only the caller knows
it; a checklist with no anchor is refused rather than dated from `now()`, which
would be right on the day it was fired and wrong forever after.

A task with an outstanding dependency is `blocked` rather than `pending`, and
completing its last dependency is what releases it. That is a state the UI can
show, so "why can I not do this yet" is answered on the screen instead of by a
refusal after the fact.

### 4. Blocking is enforced in two places, and the refusal names the step

A step marked `blocking` prevents the checklist's own completion **and** prevents
the lease transition the process gates. `move_out` gates `terminated` and
`settled`.

In Go: `maintenance/service.Outstanding(leaseID)` returns the outstanding blocking
steps, and the lease module's terminate path refuses with their titles. That is
the refusal a person reads.

In PostgreSQL: a trigger on `leases` refuses the same transition by asking
`checklist_tasks` directly, in the same shape as ADR-0010's existing
`leases_retrospective_end_needs_a_decision`, which asks `journal_entries`. It is
`SECURITY INVOKER`, so the lookup runs under the writer's own row-level security
and a session that cannot see the checklist gets no rows.

Two copies of one rule, paid for the way ADR-0010 §3 and ADR-0011 §3 pay for it:
the Go one exists to produce a sentence naming the missing step, the SQL one
exists because a script, a backfill or a future module will eventually update
`leases.state` without going through the service.

**Skipping is not a hole in this.** A non-blocking task may be skipped with a
reason. A blocking task may not be skipped at all — if a step is skippable it was
not blocking, and a `skipped` blocking step that satisfied the gate would make the
gate advisory.

### 5. Abandonment is derived, never a stored flag

A checklist may be abandoned explicitly, with a reason, and that is a state.

What cannot be a state is the checklist nobody abandoned and nobody finished. It
is surfaced by `checklist_stalled`, a `security_invoker` view of open checklists
whose earliest outstanding task is overdue — the same argument ADR-0010 §6 makes
about `expiring` and ADR-0012 §7 makes about ageing buckets. A stored `stalled`
flag needs a job to maintain it, and a checklist that should be flagged and is not
is invisible in precisely the way this section exists to prevent.

Portfolio progress is `checklist_progress`: tasks done, tasks outstanding, blocking
outstanding, and the earliest due date, per checklist. Derived for the same reason
— a stored counter is one failed update away from lying.

### 6. Not a durable workflow

ADR-0015 requires a durable workflow for operations that span systems and need
compensations. A checklist spans no system: it writes its own tables in one
transaction and it has nothing to compensate, because a task nobody did is a task
in state `pending`, not a side effect to undo.

The events it publishes — `maintenance.checklist.started`, `.completed`,
`.abandoned` and `maintenance.checklist_task.completed` — are the seam for
anything that later does need a workflow (a move-out settlement, [#75](https://github.com/tesserix/dwellm8/issues/75)).

---

## Alternatives considered

**A `tasks` table with no template.** Simplest, and it fails the story's primary
scenario: "one action fires the whole checklist" is exactly the part a bare task
table leaves to the person it was meant to help.

**A ninth module.** The story is labelled `product:Manage`, and there was a real
pull to make `manage` a module. Rejected: ADR-0001's eight are a boundary
commitment, and adding one costs a database role, a delegable permission, grants
across every chapter of the schema and a new seam — for work that is operational
task management, which `maintenance` already is.

**A `vertical` column on the template.** Rejected because the product already has
`properties.kind`. A second vocabulary for the same idea drifts on the first day
somebody adds `coliving` to one and not the other.

**Enforcing blocking only in Go.** Rejected for the reason ADR-0010's own trigger
exists: `leases.state` is updated by more than one path already, and the one that
matters most here is a support action or a data fix, which is exactly the path
that skips the service.

**Enforcing blocking only in SQL.** Tried, and the refusal is unusable: a
`check_violation` from a trigger can name the step, but the caller then has no
structured list to render, and the manager sees one step when four are outstanding.

**Computing due dates on read from the template.** Attractive until a template is
edited. An instance must be a record of what was asked for at the time, which is
the same argument ADR-0008 makes about effective dating and the same one §3 makes
about snapshotting the version.

**Blocking the state machine itself — a new lease state.** Rejected: whether a
checklist is done is not a property of the tenancy, and ADR-0010's transition
table is a fixed set that the schema, the Go domain and a 64-pair contract test
agree on. A gate is a precondition, not a state.

---

## Consequences

- Four new tables in `260_checklists.sql`, all tenant-scoped, all row-level
  secured, all covered by the existing column-driven assertions. The two that
  identify a unit carry the delegation branch through `is_delegated_unit()`.
- `dwellm8_lease` gains `SELECT` on `checklist_tasks` and `checklists`, because
  the trigger on `leases` runs as the lease module's role.
- Platform default templates are seeded as reference data. They are tenant-less
  rows, so they are platform-writable only — assertion 12's requirement, and the
  same treatment ADR-0023's rule tables get.
- A new FGA type, `checklist`, hanging off the unit it concerns. Tuples are
  projected from `maintenance.checklist.started` by the existing projector.
- The lease module acquires the terminate path it did not have. It was written
  here because this story's acceptance criterion is a refusal on it, and a
  criterion that can only be met by a trigger is a criterion no client can show.
- A blocking step is a commitment: an organisation that marks eight steps blocking
  has eight ways to be unable to close a tenancy at five o'clock on a Friday. The
  refusal names the step and the step names its owner, which is the mitigation;
  the alternative — a bypass — would make every one of them advisory.
- Move-out settlement ([#75](https://github.com/tesserix/dwellm8/issues/75)) and
  prebuilt automations ([#200](https://github.com/tesserix/dwellm8/issues/200))
  both consume these events rather than re-deriving the process.
