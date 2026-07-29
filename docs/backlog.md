# Backlog conventions

The Dwellm8 backlog follows the same model as the other tesserix planning repositories
(`tesserix/hms`, `tesserix/TripBaba`): every work item is a GitHub issue in this
repository written to a single engineering-story template, classified by labels, and
tracked on one org-level project board.

**Board:** _dwellm8 — Property Management Platform_ (private, org-scoped, two views — a
backlog table and a status board). Link added once the board is created.

Until the board exists, **MVP and Priority are carried as labels** (`mvp:MVP 1`,
`priority:P0`) so nothing is lost. When the board is created these map directly onto the
MVP and Priority fields.

## Issue template

All issues use [`.github/ISSUE_TEMPLATE/engineering-story.md`](../.github/ISSUE_TEMPLATE/engineering-story.md):

Situation → Task (story + in/out of scope) → Acceptance Criteria (primary, failure, edge
cases) → Engineering Guardrails → Result → Pull Request Evidence → Definition of Done →
Exceptions.

Three rules make this backlog reviewable:

- **The failure scenario is mandatory.** Gateway timeout, mandate debit declined, webhook
  replayed, settlement mismatch, tenant paid twice, e-stamp vendor down, cross-organisation
  read attempt. Money stories with no failure scenario are not ready.
- **Acceptance criteria must be testable** — concrete amounts, dates, states and rates,
  never "calculates correctly".
- **Money stories state the postings.** If a story moves money, its acceptance criteria
  name the ledger accounts debited and credited.

## Board fields

| Field | Values |
|---|---|
| **Status** | Backlog · Ready · In Progress · In Review · Blocked · Test · Security Review · Done |
| **Priority** | P0 Platform critical · P1 MVP core · P2 Expansion · P3 Ecosystem / Later |
| **MVP** | MVP 0–6 |
| **Epic** | one of the 39 epics below, mirrored as an `epic:` label |
| **Product** | one of the 12 product surfaces (or `Platform`), mirrored as a `product:` label |

### Priority

- **P0 — Platform critical.** Nothing ships correctly without it: the organisation/
  property/unit/lease model, effective dating, the double-entry ledger, multi-tenancy and
  RLS, auth, the payment adapter, idempotency and reconciliation.
- **P1 — MVP core.** In the 18-item MVP 1 list, or required to launch it.
- **P2 — Expansion.** MVP 3–5 capability.
- **P3 — Ecosystem / Later.** MVP 6, enterprise, commercial property, nice-to-have.

### MVP phasing

| Field value | Window | Theme |
|---|---|---|
| MVP 0 | W0–W6 | Planning & foundations |
| MVP 1 | M1–M3 | Landlord core — leases, rent, UPI collection, ledger |
| MVP 2 | M3–M6 | Tenant app, agreements, KYC, maintenance |
| MVP 3 | M6–M9 | Management firms, payouts, accounting, GST/TDS |
| MVP 4 | M9–M12 | Societies — dues, notices, amenities, gate |
| MVP 5 | M12–M15 | Lease funnel, screening, vendors, registration path |
| MVP 6 | M15–M18+ | Whitelabel B2B, enterprise, analytics, commercial |

Priority and MVP must agree: a P0 sits in MVP 0–1, a P1 in MVP 1–2.

## Labels

| Prefix | Meaning |
|---|---|
| `type:` | `feature`, `planning` (RFC/ADR/standards, always MVP 0), `spike` (time-boxed research with a decision as output) |
| `product:` | `Platform`, `Owner`, `Manage`, `Society`, `Tenant`, `Gate`, `Pay`, `Docs`, `Care`, `Books`, `Lease`, `Partners`, `Admin` |
| `team:` | `platform`, `web`, `mobile`, `design`, `payments`, `compliance`, `identity-security`, `data`, `sre`, `growth`, `customer-success` |
| `epic:` | the 39 epics, matching the Epic board field |
| `area:` | `backend`, `frontend`, `mobile`, `data`, `infra`, `devex` |

Every issue carries exactly one `type:`, one `product:`, one `team:` and one `epic:`
label, plus an optional `area:`.

## Epics

**Foundations** — Platform & Architecture · Multi-Tenancy & Data Isolation ·
Identity & Access · Developer Experience

**Core domain** — Property & Unit Model · Lease Lifecycle · Effective-Dated Records ·
Owner & Delegation Model

**Money** — Ledger & Accounting Core · Charge & Invoice Engine · Collections & UPI ·
Mandates & Autopay · Reconciliation & Settlement · Deposits & Move-Out ·
Owner Payouts · Refunds & Disputes

**Compliance** — Rental Agreements & Templates · Stamping & Registration ·
eSign & Document Evidence · KYC & Tenant Verification · GST · TDS on Rent ·
Privacy & DPDP · Security Engineering

**Operations** — Maintenance & Ticketing · Vendor Network & Job Costing ·
Notifications & WhatsApp · Support & Product Ops

**Community** — Society Dues & Funds · Notices & Committee · Amenities & Bookings ·
Visitor & Gate Management

**Growth & surfaces** — Lease Origination & Screening · Mobile Apps ·
UX & Design System · Admin & Internal Control Plane

**Runtime** — Data Platform & Reporting · Reliability & Durability · Observability

## Non-negotiables every relevant issue must respect

- Currency is `amountMinor` (int64) + `currency`. Floats never touch money, including in
  API payloads and analytics extracts.
- Balances are derived from ledger postings. No service stores and mutates an
  "amount due" column.
- Ledger postings are immutable; corrections are reversing entries with a reason code.
- Ownership, rent, tenancy and membership are effective-dated. No in-place history loss.
- `tenant_id` on every table with PostgreSQL row-level security; cross-organisation access
  always traverses an explicit, revocable grant.
- Payment providers sit behind an adapter; the architecture never depends on one gateway.
- Every collection request is idempotent; every webhook is deduplicated and replay-safe;
  webhooks are advisory and never alone release a payout.
- Anything spanning two systems (mandate, debit, payout, refund, stamping, eSign) is a
  durable workflow with compensations, not an HTTP handler with retries.
- Aadhaar numbers are never stored, logged or sent to analytics. PAN, bank and payout
  details are encrypted or held in GCP Secret Manager, never plaintext in the database.
- Statutory rates, slabs and thresholds live in versioned, state-scoped rule tables with
  an owner and a review date — never as constants in a service.
- Offline payments (cash, IMPS, NEFT) are first-class, with evidence and a real receipt.
- All SQL schema lives in `tesserix-k8s/charts/apps/db-schema-bootstrap/schemas/dwellm8/` —
  this repository holds ORM models only.
- Helm values carry memory requests/limits only; no CPU requests or limits.
- ArgoCD owns cluster state; images promote through Kargo. No manual `kubectl apply`.
