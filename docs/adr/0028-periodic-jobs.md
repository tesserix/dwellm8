# ADR-0028 — Periodic jobs: a CronJob per organisation, and when not to use a workflow

- **Status**: Accepted
- **Date**: 2026-07-31
- **Deciders**: Platform, Books
- **Issues**: [#37](https://github.com/tesserix/dwellm8/issues/37), [#41](https://github.com/tesserix/dwellm8/issues/41)
- **Related**: [ADR-0003](0003-tenancy-and-row-level-security.md), [ADR-0006](0006-chart-of-accounts-and-posting-rules.md), [ADR-0011](0011-payment-provider-adapter.md), [ADR-0015](0015-durable-workflow-standard.md)

---

## Context

Two pieces of machinery now exist and neither fires: invoices can be generated
and payments can be polled, and nothing schedules either. That is the difference
between "invoicing works" and "tenants get invoices".

ADR-0015 already chose a durable-workflow standard, so the obvious move is to
reach for it. This ADR is mostly the argument for **not** doing that, because
"we have a workflow engine" is exactly how every periodic task ends up inside
one.

---

## Decision

**Idempotent periodic work runs as a Kubernetes CronJob against a second
entrypoint in the API's image, once per organisation.**

### 1. A workflow is for an irreversible step, and these have none

ADR-0015's standard exists for operations where the order of compensations is the
problem: the irreversible step goes last, each compensation reuses its step's
key, and a failed compensation is an alert rather than a retry. That machinery
earns its cost when there is a bank transfer that cannot be taken back.

Billing writes a ledger entry keyed on `(lease, period)`. Polling asks a provider
a question. Neither has an irreversible step, neither compensates anything, and
both are safe to interrupt and safe to run twice.

**Their durability is the idempotency key.** A pod killed half way through a
billing run leaves the invoices it raised; the next run raises the rest and
nothing else. That is the same guarantee a workflow would give, obtained from a
unique index that already exists.

Reserve ADR-0015 for the payout path ([#80](https://github.com/tesserix/dwellm8/issues/80)),
where money leaves the platform and the compensation ordering is the entire
difficulty.

### 2. A CronJob, not a ticker in the API

The API runs two replicas. A `time.Ticker` inside it fires once per replica, and
because both jobs are idempotent that is *safe* — which is not the same as
correct. Two replicas racing produce one invoice and a pair of log lines that
read like a duplicate every month, and the first person to investigate that
spends an afternoon on it.

A CronJob is one runner, visible in `kubectl get jobs`, with a history somebody
can read after an incident. `concurrencyPolicy: Forbid`, because a run that
overruns its interval must not be joined by the next one — a double run is
survivable rather than harmless, and it doubles the provider load at the moment
the first run is already struggling.

Missed windows are skipped rather than caught up. Billing is keyed on the period,
so the next run raises whatever the missed one would have; replaying a backlog
would ask the provider the same questions many times over.

### 3. Once per organisation, not once as the platform

This is the part worth the ADR. A billing run spans the platform and **every read
inside it does not.**

The run lists organisations with the platform role — the one query that has no
tenant to be asked inside — and then does all of its work in one organisation's
session at a time. Running the whole thing as the platform role would be simpler
and faster, and it would put every organisation's leases in one query result,
where a single wrong join is an invoice in somebody else's ledger.

ADR-0003 built row-level security so that a bug is contained rather than
catastrophic. A background job that bypasses it is a background job where that
protection is off, in the process that writes the most rows.

One organisation's failure does not reach another's: a lease with no recorded
owner fails its own tenancy, and the run continues. A month's rent must not go
unbilled because of one bad row, discovered by the owners it did not reach.

### 4. The same image, a second entrypoint

`/api` and `/jobs` in one image. A separate image would be a second thing to
build, scan and promote for a binary that shares every package — and it would let
the two drift to different commits of the same schema, which is the failure that
looks like a database bug.

### 5. A run reports rather than rolls back

A run with failures exits non-zero so the CronJob is marked failed and somebody
sees it. **The invoices it did raise stand.** There is nothing to roll back and
nowhere to roll back to: those tenancies owe that rent, and un-raising a correct
invoice because a different lease was broken would be a worse answer than a red
job.

---

## Alternatives considered

### A. ADR-0015's durable workflows — rejected, for these two jobs

§1. It is the right tool for the payout path and the wrong one here, and adopting
it for idempotent periodic work would put a workflow engine in the path of every
rent cycle to buy compensations nothing needs.

### B. A ticker inside the API process — rejected

§2. No extra deployable, and no answer to "which replica ran it" or "did it run
last night".

### C. A leader-elected singleton in the API — rejected

It solves the double-fire and adds a lease-election mechanism to debug, for a
scheduler Kubernetes already has.

### D. One run as the platform role, all organisations at once — rejected

§3. Fewer sessions and one query, and it turns the isolation guarantee off for
the process that writes the most rows.

### E. `pg_cron` inside PostgreSQL — rejected

The work is Go — proration, provider calls, adapter selection — and expressing it
in SQL would be a second implementation of the money rules in a language the
tests cannot reach.

---

## Consequences

**What is now true.** Invoices are raised nightly, five days ahead of the due
date so the reminder ladder has something to point at. Payments nobody was told
about are asked about every ten minutes. Both run per organisation with row-level
security in force, both are safe to run twice, and both are visible in
`kubectl get jobs` with a readable history.

**What this costs.** Two more things to deploy and to alert on, and their
schedules are now operational facts — a paused CronJob is silent, and nothing
currently notices that billing has not run since Tuesday. The 02:30 IST slot
means a lease created at 02:35 waits a day for its first invoice, which is
correct and will look like a bug to somebody.

**What is not decided.** Nothing alerts on a job that has not run — the failed-job
history is a thing somebody has to look at, and the monitoring that watches for
absence is [#27](https://github.com/tesserix/dwellm8/issues/27). The generation
horizon is a flag rather than per-organisation configuration, which
[#37](https://github.com/tesserix/dwellm8/issues/37) named as in scope and this
leaves at a fixed five days. And the reminder ladder that the horizon exists to
feed is [#49](https://github.com/tesserix/dwellm8/issues/49), unbuilt — so today
the invoices are raised early and nobody is told about them.
