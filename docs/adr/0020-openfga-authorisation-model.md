# ADR-0020 — OpenFGA authorisation: verbs from a relationship graph, rows from RLS, and neither trusting the other

- **Status**: Accepted
- **Date**: 2026-08-01
- **Deciders**: Platform, Identity & Security
- **Issue**: [#148](https://github.com/tesserix/dwellm8/issues/148)
- **Related**: [ADR-0003](0003-tenancy-and-row-level-security.md) (rows), [ADR-0005](0005-owner-delegation-grants.md) (the grant this projects), [ADR-0027](0027-identity-gip-tenants.md) (who is asking), [ADR-0002](0002-event-backbone-and-outbox.md) (how tuples will be written)
- **Artefacts**: [`services/api/internal/platform/authz/model.fga`](../../services/api/internal/platform/authz/model.fga), validated by [`model.fga.yaml`](../../services/api/internal/platform/authz/model.fga.yaml) — 14 scenarios, 82 checks, run by CI

---

## Context

Authentication is settled: ADR-0027 verifies a GIP token and produces a
principal, deliberately stopping short of what that principal may do. Row
reachability is settled: ADR-0003's row-level security decides which rows a
session can touch, and ADR-0005's grant row carries access across
organisations. What is not settled is the verb. RLS can say whether a session
may see an invoice row; it is the wrong tool for "may this person *approve
this spend*, while that person may only *look*", because both verbs read the
same row.

Three earlier decisions constrain the answer before it starts:

- `organisation_members.role` "is deliberately coarse and must not grow into a
  permission system" (ADR-0027). The fine-grained answer must live elsewhere.
- ADR-0005 promised that this ADR replaces its permission *vocabulary* and
  policy *template*, "not §1's grant object or §5's guarantees — the row model
  is chosen to survive that". The grant row stays; only who consults it changes.
- The requirements (§6 M1) say authorisation is "a fail-closed OpenFGA check at
  the service boundary … a token never carries authority", and the personas
  (§3) demand that roles are scoped to (organisation, property, unit) and
  never merged across organisations.

The remaining question was never *whether* OpenFGA but *what model* — the type
and relation definitions are the artefact every service, screen and audit
answer will depend on, and reshaping them after tuples exist in production is
the migration nobody wants. So the model was written first, as a file in this
repository, with a test suite that executes against a real evaluator — not as
prose that an implementation will later interpret.

---

## Decision

**OpenFGA answers the verb: may this principal perform this action on this
object. PostgreSQL answers the row: which rows exist for this session. Every
mutating or reading handler asks OpenFGA before it runs SQL, fails closed, and
then runs that SQL under the same RLS that would have protected the data had
the check been wrong. Verbs are `can_*` relations computed from the graph;
everything a person *is* — owner, manager, tenant, warden — is a tuple
somebody wrote and can revoke.**

The model is [`model.fga`](../../services/api/internal/platform/authz/model.fga):
eighteen object types, three conditions, and a rule that keeps it reviewable —
a relation is either something you *are* (written as a tuple) or something you
*can do* (computed), never both.

### 1. Roles are relation sets, so a role change is a tuple change

An organisation carries the persona relations directly: `owner`, `co_owner`,
`manager`, `staff`, `accountant`, `committee_member`. The verbs are unions over
them — `can_view_financials` is owner or co-owner or accountant or committee
member or an active support session. Promoting a staff member to manager is one
tuple delete and one tuple write; no code changes, no claim re-issue, and the
next check reflects it.

The same shape repeats down the tree. What a property adds is the personas
that only make sense at a property: `field_agent`, `warden`, `guard` — people
assigned to a building, not to an organisation.

### 2. Two enforcement layers, deliberately redundant

OpenFGA is the decision; RLS is the blast wall. The service checks
`can_adjust` on an invoice before touching it, and then the `UPDATE` runs as a
session whose RLS policies would return zero rows if the check had lied — or
if the tuple pipeline had written a tuple RLS knows nothing about.

The two layers can only disagree in the safe direction. OpenFGA-deny stops the
request at the boundary. OpenFGA-allow with RLS-deny updates nothing and reads
nothing. There is no state in which a wrong tuple widens what SQL can reach,
because SQL never consults the tuples. This redundancy is the reason the model
can be iterated on with ordinary caution rather than schema-migration dread.

### 3. Delegation is a projection of the grant row, not a second truth

ADR-0005's `delegation_grants` row remains the system of record — effective
dating, revocation columns, audit stamping, the no-delete policy. What OpenFGA
holds is a projection:

```
property:sunrise # managed_by @ organisation:keyline
    [condition mandate_active(valid_from, valid_until)]
```

one tuple per property the mandate's scope rows name, with the row's dates
copied into the condition's parameters and an open-ended `valid_to` written as
a far-future timestamp. The firm's people then reach the property through
tuple-to-userset: `manager from managed_by`, `staff from managed_by`,
`accountant from managed_by` — the firm's own role tuples, crossed with the
mandate. Nobody is ever written *into* the owner's organisation, which is the
mistake ADR-0005 alternative A already rejected once.

Revocation deletes the tuple. That is allowed to be a plain delete precisely
because the tuple is not the record — the grant row keeps `revoked_at`,
`revoked_by` and the reason, so ADR-0005's rule that "revocation must remove
access, not evidence" holds with the labour divided: OpenFGA removes the
access, PostgreSQL keeps the evidence. Expiry needs no delete at all; the
condition evaluates `now` from the check's context and an expired mandate
confers nothing, which the suite asserts.

### 4. Inheritance flows down the tree, and where it deliberately stops

`property → block → unit → room → bed` each take their verbs from their
parent, so a warden who can operate the hostel can operate every bed in it
without a tuple per bed. The stops are the design:

- **An agreement is not inherited.** `can_view from unit` would hand a field
  agent the rent terms of every flat they inspect. An agreement is visible to
  its tenant, its guardian, whoever can *manage* the unit, and whoever holds
  the financial-view verb — the operational chain ends at the door.
- **A field agent works the property and never its money.** They hold
  `can_view`, `can_collect`, `can_work` on tickets — and on an invoice they
  hold exactly `can_record_collection`: enough to mark cash collected, no
  path to amounts history, adjustment or the owner's ledger.
- **The manager does not see the payout.** A payout's `can_view` derives from
  the owner organisation's financial verb, which a mandate never joins. The
  firm initiates collections; what the owner is paid is the owner's.

### 5. Platform staff hold the platform, not the customer

`platform` is a type with admin verbs — rule tables, onboarding approval, the
dispute desk — and **no relation into any organisation's objects**. There is no
model path from `platform_admin` to a property, an invoice or a document; the
acceptance test asserts the absence.

Customer data is reached one way: a `support` tuple on the specific
organisation, carrying a `support_window` condition with an expiry. It is a
written artefact, so writing it is an auditable act (the tuple pipeline logs
it — #151), it names one organisation, and it lapses on its own. When the
window closes the same person, same tuple, gets `deny` — asserted in the
suite at two timestamps an hour apart.

### 6. Public access is a wildcard on one type, read-only by construction

A published listing carries `public_viewer: user:*`. That is the entire
anonymous surface: `can_view` on `listing` includes the wildcard, `can_edit`
does not, and no other type has one. Anonymous is a modelled audience, not a
missing check — the same posture as ADR-0019's read-only listing table, now
stated in the authorisation layer too.

### 7. Short-lived grants are conditions, not cron jobs

A technician exists, authorially, for one job for one day:

```
job:j-9 # assigned_technician @ user:tarun
    [condition job_window(starts_at, ends_at)]
```

Inside the window they can see and complete the job — not the ticket behind
it, not the unit, not the property. Outside it the same tuple answers `deny`
with nothing having run to make it so. No sweeper, no revocation queue for
the routine case; deletes are for the exceptional one.

### 8. What was actually validated, and how it stays valid

`fga model test` runs 82 checks across 14 scenarios against the shipped model
file: every persona in the requirements §3 exercised against a representative
object; the mandate reaching the property it names and not the neighbour; an
expired mandate; the field-agent financial stop; the tenant/staff persona held
by one person across two organisations conferring nothing across them; the
hostel chain down to an occupancy; the technician window open and shut; the
public wildcard reading and failing to write; the platform admin denied; the
support window closing.

CI runs the suite on every push (`authz-model` job), so the model in the
repository is permanently the model that passes its own contract. What is
*not* yet validated is everything operational: no store exists, no service
calls a check, no tuple is written from an event. Those are #149, #150 and
#151, and this ADR is their input, not their record.

---

## Alternatives considered

### A. RLS only — no second system — rejected

- **For**: one enforcement layer, already built, already tested, cannot be
  bypassed.
- **Against**: RLS answers per-row visibility per session; it has no natural
  shape for two verbs on one row ("view" vs "approve"), for "what may I do"
  queries a UI needs before rendering buttons, or for relationship traversal
  like *technician of the vendor assigned to the job on this ticket*. Encoding
  those as policies turns every table's policy into a program.
- **Why rejected**: the requirements' permission matrix is verb-shaped, and
  §2 keeps RLS in the loop anyway — this is not a replacement, it is a divide.

### B. Grow ADR-0005's permission vocabulary into the intra-org system — rejected

- **For**: one mechanism for delegation and roles; pure PostgreSQL.
- **Against**: the vocabulary is flat strings checked per row. Object-scoped
  roles (warden of one property, technician of one job) force a scope table
  per persona, reinventing the tuple store without its evaluator — and
  ADR-0027 explicitly forbade the membership row from becoming this.
- **Why rejected**: it rebuilds OpenFGA badly, in SQL, one table at a time.

### C. Policy-as-code — OPA or Casbin embedded in the API — rejected

- **For**: no new deployable; policies versioned with the code.
- **Against**: rules engines evaluate predicates over inputs the caller
  assembles, so every check is only as right as the data the handler loaded;
  the relationship state still needs to live somewhere, and "list every object
  this user can see" has no engine support. The failure mode is a handler
  forgetting to load the fact the rule needed — silent allow or silent deny.
- **Why rejected**: authorisation here is relationship lookup, not rule
  evaluation; the hard part is the data, which these engines do not hold.

### D. SpiceDB — rejected, narrowly

- **For**: the same Zanzibar lineage, a mature schema language, consistency
  tokens (ZedTokens) that OpenFGA lacks.
- **Against**: heavier to operate for this estate, and the consistency token
  solves a new-enemy problem that §2's RLS backstop already bounds — a stale
  allow still reads zero rows. OpenFGA is CNCF, runs as one binary on the
  PostgreSQL this platform already operates, and its conditions cover the
  effective-dating this product runs on.
- **Why rejected**: both would work; the one that loses is the one with the
  larger operational surface for a benefit §2 makes redundant.

### E. Authority in the token — custom claims — rejected

- **For**: zero-latency checks; no authorisation service at all.
- **Against**: a claim minted at sign-in answers with sign-in-time facts; a
  revoked mandate or a closed support window lives until token expiry. ADR-0027
  put the organisation lookup in a table for exactly this reason, and the
  requirements state the principle outright: a token never carries authority.
- **Why rejected**: revocation is the requirement the whole grant model is
  built around, and this design cannot express it.

### F. Effective dating by sweeping tuples on a schedule — rejected

- **For**: no conditions; a simpler model with plain tuples.
- **Against**: a sweep is a window in which an ended mandate still answers
  allow, and ADR-0005 measured revocation to the next statement. Conditions
  evaluate at check time; the deleted-versus-lapsed distinction in §3 and §7
  costs nothing at all.
- **Why rejected**: correctness at check time was available and a scheduled
  approximation of it was not better at anything.

---

## Consequences

**Good**

- Every persona action in the requirements maps to a named check that runs in
  CI, before any of it is deployed — the model is reviewable as a file and
  falsifiable as a test suite.
- A role change, a mandate, a support session and a technician's day are all
  the same operation: a tuple write. The audit story (#151) is therefore one
  story, not five.
- The two-layer split means model iteration is not schema-migration-grade
  risk: a wrong tuple cannot widen SQL's reach.
- Platform administrative access is structurally incapable of being implicit.

**Bad, and accepted**

- Two authorities describe overlapping truth, and the tuple projection can lag
  the grant row it mirrors. The lag is bounded by the outbox pipeline (#151)
  and fails safe per §2, but "why can't the firm see the flat yet" is now a
  distributed-systems question.
- Every guarded request gains a network hop to OpenFGA. The latency budget,
  p99 targets and the deny-when-unreachable behaviour are #149's contract to
  set; fail-closed means an OpenFGA outage is a platform outage for writes,
  which is the price of never failing open.
- Conditions put the clock inside authorisation. Checks must pass `now` from
  the server, never from the client, and clock skew between API replicas is
  now a security-relevant quantity — bounded, in practice, by GKE's NTP.
- The model file and the schema's RLS policies express related intent in two
  languages, and nothing mechanical proves them equivalent. Divergence fails
  safe but confuses; reviews of either must read both.

**Follow-up work this ADR creates**

- #149 deploys OpenFGA, owns the store, model versioning and rollout, the
  latency budget and the unreachable-means-deny contract.
- #150 builds the middleware: resolve principal → membership (ADR-0027) →
  check → handler, with the check identifier logged for the audit trail.
- #151 writes tuples from domain events off the outbox (ADR-0002): membership
  changes, mandate lifecycle from the grant row, support sessions, job
  assignment — each write itself an audited event.
- The demo surface (ADR-0021) needs a decision on whether demo objects share
  the store with a `demo` type or a separate store per sandbox.
- When list endpoints need "which objects" rather than "may I", evaluate
  `ListObjects` against the RLS-scoped SQL both for correctness and cost —
  today the answer stays: SQL lists, OpenFGA gates.
