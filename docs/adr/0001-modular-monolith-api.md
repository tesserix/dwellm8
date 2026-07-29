# ADR-0001 — Modular monolith API: module boundaries and repository layout

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Platform
- **Issue**: [#1](https://github.com/tesserix/dwellm8/issues/1)
- **Supersedes**: nothing
- **Related**: requirements v2.0 §9.2–§9.4, ADR-0016 (mobile stack), ADR-0017 (identity)

---

## Context

Dwellm8 spans property, lease, money, maintenance, community, discovery,
documents, notifications and identity, and serves six mobile apps, three web
consoles and a public listing site from one product.

The domain is unusually interconnected. Closing a single maintenance job touches
the ticket, the liability matrix from the agreement, the vendor's job record,
the ledger, the owner's statement and the notification ladder — in one logical
operation that must either happen completely or not at all. The same is true of
a move-out: inspection findings, deposit deductions, a reversing ledger entry, a
final statement and a refund, all of which must agree.

The organisation has twice learned the cost of the two obvious answers. Thirty
microservices turned every feature into a coordination problem and every bug
into a distributed-tracing exercise. A single unstructured monolith became
untestable because nothing stopped one part reaching into another's tables.

This decision has to be made once and defended, so that every later story lands
in a module that already exists and already owns its data.

---

## Decision

**Dwellm8 is one Go API — a modular monolith — with eight modules, enforced
boundaries and one transaction boundary. It lives in this repository.**

### 1. One deployable, eight modules

One Gin API serves every client. One OpenAPI specification, one authentication
path, one authorisation path, one database, one deployment.

| Module | Owns (aggregates) | Requirement modules |
|---|---|---|
| `identity` | Person, organisation, membership, role assignment, KYC record, consent | M1, M4 |
| `property` | Property, unit, bed, asset, meter, compliance item | M2, M11 (inventory), M12 (inventory), M14 (register) |
| `lease` | Tenancy, agreement, renewal, notice, move-in and move-out record | M3, M4 (onboarding), M6 (terms) |
| `money` | **Ledger posting**, invoice, charge, receipt, mandate, payout, fee, deposit balance, tax record | M5, M6 (balances), M9, M10 |
| `maintenance` | Ticket, liability decision, quote, job, vendor, inspection, evidence | M7, M8 |
| `community` | Society, due, notice, amenity booking, visitor pass, staff attendance, bed allocation | M11 (operations), gate, RWA |
| `discovery` | Listing, verification, enquiry, viewing, attendance, application, offer | M15 |
| `notify` | Template, message, delivery receipt, inbox thread, deep link | M13 |

`money` is the centre of gravity, and the only module permitted to write a
ledger posting. Everything financial that any other module wants to express —
a recharged repair, a society due, a deposit deduction — is a request to
`money`, never an insert.

### 2. What is deliberately not a module

| Concern | Why it is not a module | Where it lives |
|---|---|---|
| Platform administration (M16) | It is an authorisation tier over the same aggregates, not a separate domain | Admin-scoped endpoints inside each module, gated by OpenFGA |
| Analytics and reporting (M17) | Read-only; a module boundary would buy nothing and cost joins | Read replica plus a reporting schema, no writes |
| AI capabilities (M18) | An adapter over existing module APIs, with no aggregates of its own | `internal/ai`, calls module services like any other caller |
| Demo and sandbox (M19) | A data-scoping concern, not a domain | A sandbox organisation flag honoured by every module |
| Workflow automation (M20) | A rules engine that calls module APIs; it must own no domain data | `internal/automation`, driven by module events |

Any of these growing its own aggregates is the signal to revisit this ADR.

### 3. Data ownership — one writer per table

- Every table belongs to exactly one module, declared in that module's schema
  file. **Only the owning module writes it.**
- A module reads another module's data through that module's Go interface, or
  by subscribing to its events. Never by querying its tables — including in
  reports, migrations and one-off scripts.
- Cross-module reads that would be a join are solved by the caller asking for
  what it needs, or by a projection the owning module maintains and publishes.
- PostgreSQL row-level security on `tenant_id` remains in force under all of it,
  as the second line of defence rather than the first.

Enforced by a CI check that fails when a module's package imports another
module's `store` package, and by per-module PostgreSQL roles in the schema
bootstrap so that a stray write fails at the database, not at review.

### 4. Synchronous and asynchronous contracts

| Contract | Style | Rule |
|---|---|---|
| Client to API | REST over HTTPS, one OpenAPI document | Every app and console uses the same endpoints; no client-specific API |
| Module to module, in a request | A Go interface call inside the process | Same transaction, same request context — this is the reason for the monolith |
| Module to module, after the fact | NATS JetStream event | Anything that may fail independently, retry, or fan out |
| Anything crossing money or a provider | Temporal workflow | Durable, with compensations — payouts, refunds, e-stamp, eSign, mandate lifecycle |
| Provider integrations | Adapter package behind an interface | Idempotent, with defined retry, timeout and dead-letter behaviour |

**gRPC is not used internally.** In one process it is a serialisation tax for no
benefit. It becomes the contract only if a module is extracted, at which point
its existing Go interface is the gRPC service definition.

Published events, initially:

```
identity.member.added            property.unit.vacated
lease.tenancy.started            lease.notice.served
lease.tenancy.ended              money.payment.received
money.invoice.raised             money.payout.released
money.mandate.failed             maintenance.ticket.raised
maintenance.job.completed        maintenance.inspection.filed
community.due.raised             discovery.application.received
```

Consumers are durable and replay-safe, deduplicated on the provider event id
where one exists. **A webhook alone never releases money** — it advances a
Temporal workflow that does.

### 5. Repository layout — one repository

`tesserix/dwellm8` holds the API, the six mobile apps, the web surfaces and the
docs. One pull request can change an endpoint and the app that calls it.

```
dwellm8/
  services/api/                 the one Go API
    cmd/api/                    main, wiring, graceful shutdown
    internal/
      identity/  property/  lease/  money/
      maintenance/  community/  discovery/  notify/
        http/       handlers, request and response types
        service/    the module's public Go interface — the extraction seam
        domain/     aggregates and rules, no framework imports
        store/      SQL for this module's tables only
        events/     what it publishes and subscribes to
      platform/     config, auth, telemetry, middleware, errors
      automation/   the rules engine (M20)
      ai/           adapters (M18)
    migrations/     ORM models only — SQL lives in tesserix-k8s
  apps/             live, own, ops, pro, admin, find, web
  packages/         mobile-shared, and shared web packages
  docs/             product brief, requirements, ADRs
```

Schemas are **not** in this repository. Per the workspace rule, every `.sql`
lives in `tesserix-k8s/charts/apps/db-schema-bootstrap/schemas/dwellm8/`, one
file per module, applied idempotently by the bootstrap CronJob. This repository
holds ORM models only.

Monorepo over multi-repo because the client and the API change together in
almost every story, the org already runs this pattern in `Home-Chef-App` and
`mark8ly`, and six apps sharing one package is the point of `mobile-shared`.

### 6. When a module may be extracted

Only when at least two hold:

1. It needs to scale on a different curve — sustained, measured, not projected.
2. Its failure must not take the API down.
3. It has a genuinely separate release cadence or compliance boundary.
4. Its event contract has been stable for two quarters.

`notify` and `discovery` are the plausible first candidates. Neither is
extracted now.

---

## Alternatives considered

### A. Six to eight microservices from day one — rejected

- **For**: independent deploys and scaling; forced boundaries; familiar shape.
- **Against**: the closing of one maintenance job spans four services, so every
  such operation becomes a saga with compensations. Correctness turns into
  coordination for a team that has no independent scaling need. Local
  development needs the whole estate. Six months of platform work before the
  first paying customer.
- **Why rejected**: the domain is too interconnected and the team too small.
  This is the failure the organisation has already paid for twice.

### B. Single unstructured monolith — rejected

- **For**: fastest to start; no boundary ceremony.
- **Against**: nothing stops a handler reaching into another domain's tables,
  and within a year every change touches everything. Untestable in exactly the
  way already experienced.
- **Why rejected**: it is the same monolith we are choosing, minus the one
  property that makes it survivable.

### C. Modular monolith — **accepted**

- **For**: one transaction boundary for a densely transactional domain; one
  deployment and one database to operate; boundaries enforced in code and in
  database roles; the extraction seam exists on day one.
- **Against**: boundaries need active policing, or it decays into B; one bad
  module can exhaust the shared process; the whole API deploys together.
- **Mitigation**: the CI import check, per-module database roles, per-module
  ownership of tables, and the extraction criteria above.

### D. Serverless functions per endpoint — rejected

- **For**: scale to zero; no servers.
- **Against**: cold starts against a three-taps-to-money requirement, connection
  pooling against PostgreSQL with RLS, and no transaction boundary at all.
- **Why rejected**: fails §7.2 performance and the ledger's transactional
  requirements simultaneously.

---

## Consequences

**Good**

- A maintenance job closes in one transaction: ticket, ledger, statement.
- One API, one OpenAPI document, one auth path for six apps and four web surfaces.
- One database, one deployment, one thing to observe and roll back.
- Every later story has a module to land in, with data it already owns.

**Bad, and accepted**

- Boundary discipline is a standing cost. Without the CI check it decays.
- The whole API deploys as one unit; a hot fix to `notify` redeploys `money`.
- One process means one memory and connection budget to share.

**Follow-up work this ADR creates**

- The module-import CI check (must exist before the second module is written).
- Per-module PostgreSQL roles in the schema bootstrap.
- The event catalogue as a versioned document, not a list in an ADR.
- ADR-0002 for the ledger's posting model, which this ADR assumes and does not define.
