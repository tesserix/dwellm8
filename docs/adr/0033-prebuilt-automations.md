# ADR-0033 — Prebuilt automations: a catalogue in code, settings that are overrides, and a ceiling that asks rather than acts

- **Status**: Accepted
- **Date**: 2026-08-01
- **Deciders**: Platform, Product, Operations
- **Issues**: [#200](https://github.com/tesserix/dwellm8/issues/200)
- **Related**: [ADR-0001](0001-modular-monolith-api.md), [ADR-0002](0002-event-backbone-and-outbox.md), [ADR-0015](0015-durable-workflow-standard.md), [ADR-0028](0028-periodic-jobs.md), [ADR-0032](0032-checklist-automation.md)

---

## Context

Most of a manager's day is the same five sequences: chase arrears, remind about
an expiry, schedule an inspection, renew a compliance item, run a move-in or a
move-out. The product currently records all five and performs none of them.

The story's primary scenario is the constraint that shapes everything here:

> **Given** a new organisation onboarding
> **When** they add their first tenancy
> **Then** the arrears ladder, expiry reminders and inspection scheduling are
> already running with sensible defaults and **no configuration**

"No configuration" rules out the obvious design. If an automation is a row, then
an organisation with no rows has no automations, and every onboarding needs a
seeding step that will one day be missed — quietly, because an organisation with
nothing chasing its arrears looks exactly like an organisation whose tenants all
pay on time.

The failure scenario is the other constraint:

> **Given** an automation would take an action beyond the mandate ceiling
> **When** it fires
> **Then** it stops and requests the required approval instead of acting

Which means an automation cannot be a function that does something. It has to be
a thing that *proposes* something, so there is a point at which the proposal can
be refused and recorded rather than performed.

---

## Decision

**The automations are a catalogue in code. A settings row is an override, never a
prerequisite. Every automation acts through a proposal that a ceiling can refuse,
and every action it does take leaves a row saying which automation caused it.**

### 1. The catalogue is code; the rows are overrides

Each automation is a `Definition` in Go: a key, what it is for, what triggers it,
its parameters and their defaults, and the action it proposes. An organisation
that has never opened the settings screen has no rows at all and gets every
automation with its default parameters — the story's "no configuration", obtained
by resolving absence rather than by seeding.

`automation_settings` holds only the differences: this organisation turned the
arrears ladder off, that one moved the first reminder from day 3 to day 5. Same
resolution shape as ADR-0032's template order, and for the same reason: a default
that lives in a row is a default somebody can delete.

**"Switchable off per organisation without a release" is therefore one UPDATE**,
and switching the default for everybody is a deploy — which is correct, because
that is a product decision rather than a customer one.

### 2. It is a platform engine and a catalogue above the modules, not a ninth module

The engine — settings resolution, the run log, the ceiling, the runner — owns
tables and no domain knowledge, so it lives in `platform/automation` beside the
outbox and the workflow tables, which are platform-owned for the same reason.

The catalogue is the opposite: it knows what an arrears ladder is and it needs
the lease, money and maintenance modules to do anything. That makes it a **seam**
in `internal/routine`, the shape `internal/e2e` already established for the
billing run — coordination above the modules, expressed as narrow interfaces the
modules satisfy, so neither module acquires a dependency on the other.

A ninth module was considered and rejected in §"Alternatives".

### 3. An automation proposes; the ceiling decides

An automation never calls a module service directly. It calls `Propose`, which
carries what it wants to do, what it is about, and the money it would move.

`Propose` does one of three things:

- **acts**, when the proposal is inside the ceiling, and records the run;
- **asks**, when it is not: it writes an `automation_approvals` row naming the
  automation, the subject, the amount and the ceiling it exceeded, and returns
  `ErrApprovalRequired`. Nothing is performed;
- **skips**, when the same proposal has already been acted on — the idempotency
  key, because a CronJob that overruns is a CronJob that runs twice.

The ceiling is itself a parameter, so an organisation that wants every outward
action approved sets it to zero and an organisation that trusts the ladder raises
it. `approval_ceiling_minor` is money and is therefore an integer of minor units
like every other amount in the schema (ADR-0007).

**"Automations that move money without approval" is out of scope in the story,
and this is the mechanism that makes it true rather than a promise.** No prebuilt
automation in §5 moves money at all — every one of them proposes zero; the ceiling exists so that the first one
that wants to has to pass through here.

### 4. Provenance is a row, not an actor kind

"A record must show which automation caused an action and when" needs the record
to be able to answer the question later.

The tempting move is to widen `actor_kind` to include `automation` in the outbox
and the audit trail. Rejected: that vocabulary is asserted by name in the schema
(assertion 13), every consumer switches on it, and it would still not say *which*
automation — only that one of them did it.

Instead `automation_runs` carries the subject the run acted on, so "what has been
automated on this tenancy" is one indexed read, and the run says which automation,
with which parameters, on which date, and what it did. Automated actions continue
to be `system` actors in the outbox, which is what they are.

### 5. The eight prebuilt automations

| Key | Trigger | What it proposes |
|---|---|---|
| `arrears_ladder` | daily | A reminder at each step of the ladder, once per tenancy per step |
| `lease_expiry_reminder` | daily | A reminder as a tenancy approaches its end, inside the notice window |
| `renewal_kickoff` | daily | Fires ADR-0032's `tenancy_renewal` checklist before the term ends |
| `inspection_scheduling` | daily | A routine inspection due on a tenancy, dated far enough ahead for the notice the tenant is owed |
| `compliance_renewal` | daily | A section 197 certificate about to lapse, raised while it can still be renewed |
| `move_in_checklist`, `move_out_checklist`, `owner_onboarding_checklist` | event | Fires the matching ADR-0032 checklist when the tenancy or the mandate reaches the state that calls for it |

The reminders **emit their event and record the run; they do not deliver
anything.** Delivery is [#126](https://github.com/tesserix/dwellm8/issues/126)
and the `notify` module is unbuilt — an automation that pretended to send a
WhatsApp message would be the worst of the three available options, because the
run log would say a tenant was chased and no tenant was.

### 6. A CronJob per organisation, per ADR-0028

The scheduled automations run from `jobs automations`, once per organisation, in
that organisation's own session. ADR-0028 §3's argument applies unchanged and
more strongly: this run reads every tenancy and every balance, so running it as
the platform role would put the whole platform's arrears in one result set.

Idempotency is the same trade ADR-0028 makes: each proposal carries a key, so an
interrupted run leaves the actions it took and the next run takes the rest.

---

## Alternatives considered

**Automations as rows, seeded at onboarding.** The obvious design, and it fails
the story's primary scenario on the first organisation whose seeding step errors.
It also makes "we changed the default" a data migration across every tenant.

**A ninth module.** Automations own tables and have rules, which is ADR-0001's own
test for a module, so this was close. It was rejected because the rules split
cleanly in two: the *engine's* rules are about settings, ceilings and idempotency
and know nothing about property, and the *catalogue's* rules are entirely about
other modules' vocabularies and own no table at all. A module would have to hold
both, and would end up importing four other modules' services into something that
also owns a table — the position ADR-0001 §3 is written to prevent.

**A durable workflow per automation.** ADR-0015's machinery is for an irreversible
step with a compensation ordering. A reminder has neither, and ADR-0028 already
made this argument for billing. Reserve it for the payout path.

**A ticker in the API.** Two replicas, two runs, two reminders. ADR-0028 §2.

**A no-code rule builder.** Explicitly out of scope in the story, and it is the
right call: a builder is only worth having once there is evidence about which
parameters customers actually change, and the settings rows this ADR introduces
are how that evidence is collected.

**Refusing the action silently when it exceeds the ceiling.** Considered and
rejected as the worst possible reading of the failure scenario: an automation
that stops and says nothing is indistinguishable from one that is switched off.
The approval row is the request, and it names the ceiling it hit so somebody can
raise it deliberately.

---

## Consequences

- Three new tables in `270_automations.sql`, all tenant-scoped and covered by the
  existing column-driven assertions.
- `automation_runs` is append-only in practice and never deleted: it is the answer
  to "why did this tenant get a message on a Sunday".
- The lease module gains an `Expiring` reader over the `lease_expiring` view,
  which ADR-0010 §6 wrote and nothing had read.
- Adding an automation is a Go change plus a test. Changing what an existing one
  does by default is also a Go change — deliberately, so it is reviewed.
- The catalogue's actions are limited by what the modules can do today. Four of the
  eight propose a reminder that is recorded rather than sent (§5), and that limit is
  visible in the run log rather than hidden behind an interface that pretends.
- The arrears ladder's rungs are measured from the most recent charge rather than
  from the oldest unpaid one, because payment-to-charge allocation does not exist
  yet (ADR-0012 §7). It is the conservative direction — it never chases earlier
  than true ageing would — and [#180](https://github.com/tesserix/dwellm8/issues/180)
  is where the fix belongs.
- The property-level compliance register (fire NOC, lift certificate, society dues)
  has no table, so `compliance_renewal` covers the statutory certificates that do.
  A second register is a second reader and a second block, not a different design.
- An organisation that switches everything off gets a product that records rather
  than acts, which is the product they had before this ADR. That is a supported
  state and the settings screen says what each one does before it is turned off.
