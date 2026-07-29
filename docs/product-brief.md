# Dwellm8 — product & architecture brief

The reference document for what Dwellm8 is, what it refuses to be, and the architectural
decisions that later issues must respect. Regulatory, payment and tax mechanics live in
[`india-compliance.md`](india-compliance.md); backlog taxonomy lives in
[`backlog.md`](backlog.md).

---

## Positioning

Indian residential rental is enormous, informal and almost entirely unrecorded. The
owner keeps a WhatsApp thread and a notebook. The management firm keeps twelve Excel
files. The society keeps a register at the gate and a Tally file the treasurer's CA
touches once a year. Everyone involved knows exactly what is owed and can prove none of it.

Dwellm8's position is **the system of record for a rented property in India** — one
property, one lease, one ledger, one document set — with four front doors onto it. It is
not a listings portal that added rent collection, and not a payments app that added a
property list.

Competitors are strong in one lane: portals own discovery, a few apps own society dues,
one or two own owner-side rent collection. Nobody owns the *lease lifecycle end to end*
across owner, firm, society and tenant, and nobody makes the compliance artefacts
trustworthy. That gap is the wedge.

## Non-goals

- Not a brokerage. Dwellm8 may originate leases, but the platform does not take
  possession of the transaction as a broker with an inventory obligation.
- Not a lender. Rent-now-pay-later and deposit financing are partner-provided, never
  balance-sheet products, and never in MVP 1–4.
- Not an escrow. Money moves owner ↔ tenant through a regulated PA/PG; Dwellm8 is not a
  custodian of client funds and the architecture must never quietly become one.
- Not a valuation or investment product.

## The product loop

```
List / onboard a unit → screen & verify a tenant → paper the lease (stamp + eSign)
   → collect rent on a cycle → handle maintenance → renew or exit → settle the deposit
```

Everything else — accounting, notices, amenity booking, vendor dispatch, payouts — hangs
off that loop. A feature that does not make one of those seven steps cheaper, faster or
more provable is not MVP work.

## Domain model

The whole platform is one hierarchy. Getting this wrong is the mistake that cannot be
retrofitted.

```
Organisation (tenant)          -- landlord account, PM firm, or society
└── Property                   -- building, standalone house, plot, commercial premises
    └── Block / Wing (opt)
        └── Unit                -- flat, floor, shop, desk, parking slot
            ├── Ownership       -- who owns it, effective-dated, possibly fractional
            ├── Lease           -- effective-dated occupancy contract
            │   ├── Party       -- tenant(s), guarantor, occupants
            │   ├── Schedule    -- rent, escalation, due day, cycle
            │   ├── Deposit     -- amount, held-by, interest terms
            │   └── Documents   -- agreement, stamp, eSign, KYC, verification
            └── Meter / Charge  -- electricity, water, maintenance, parking, amenity
```

Rules that fall out of it and must hold in every service:

- **Everything effective-dated.** Ownership changes, rent revisions, tenant swaps and
  society membership are `valid_from`/`valid_to` rows, never in-place updates. "What was
  the rent in March?" must be answerable without an audit table.
- **A unit can be simultaneously** owner-occupied, leased, and a society member. Society
  dues follow the *unit and its owner*; rent follows the *lease*. Conflating them is the
  bug that makes society modules unusable for rented flats.
- **A person is one identity with many roles.** The same phone number is an owner in one
  society, a tenant in another city and a committee member in a third. Roles are scoped
  to `(organisation, property, unit)`, never global.
- **Organisation is the tenancy boundary**, and `tenant_id` sits on every table with
  PostgreSQL row-level security. A PM firm managing an owner's unit gets a delegated
  grant, not a copy of the data.

## Money architecture

The single most important subsystem. Rent software fails here or nowhere.

### Ledger

Double-entry, append-only, per organisation:

- Accounts: `tenant_receivable`, `rent_income`, `late_fee_income`, `deposit_liability`,
  `owner_payable`, `platform_fee_income`, `gst_output`, `tds_receivable`,
  `gateway_clearing`, `bank`, `society_dues_receivable`, `sinking_fund`, `write_off`.
- Every posting is immutable. Corrections are reversing entries with a reason code.
- Balances are always derived. No service may store and mutate an "amount due" column.
- `amountMinor` (int64) + ISO currency. Floats are prohibited platform-wide, including
  in API payloads and analytics extracts.

### Charge cycle

1. **Schedule** — a lease produces charges on a cycle (monthly, quarterly, custom due day,
   proration on join/exit, escalation clause with an effective date).
2. **Invoice** — generated N days ahead as an immutable document with a GST breakdown
   where applicable.
3. **Reminder ladder** — WhatsApp/SMS/push at T-5, T-1, T+1, T+3, T+7, each configurable
   per organisation, each suppressible once paid.
4. **Collection** — UPI intent/collect, UPI Autopay mandate debit, NACH, card, netbanking,
   or an offline record (cash/bank transfer) marked by the owner with an audit trail.
5. **Late fee** — rule-driven (flat, %, per-day, grace period, cap), computed by the
   ledger, never typed in.
6. **Reconciliation** — nightly against the gateway settlement report; unmatched postings
   raise an operational alert rather than silently sitting in `gateway_clearing`.
7. **Payout** — owner settlement net of management fee, GST, TDS and adjustments, on a
   payout schedule, with a statement that reconciles to the ledger to the paisa.

### Deposits

Deposits are a **liability**, never income, and are the most disputed object in Indian
rental. The deposit record holds: amount, who physically holds it, whether interest
accrues (some states/societies require it), deductions with itemised evidence, the
settlement window, and the final refund posting. Move-out settlement is a workflow with a
tenant acknowledgement step, not an owner-only screen.

### Money workflows are durable

Anything spanning more than one system — mandate creation, debit, payout, refund,
settlement, TDS challan — runs as a **Temporal workflow** with compensations, not as an
HTTP handler with a retry loop. The failure mode that must never occur is *tenant debited,
owner not credited, nobody notified*.

## Payments

Providers sit behind an adapter with a canonical model; the platform never depends on one
gateway. Razorpay is the primary integration (already proven in this org), with the
adapter shaped so a second PA/PG can be added without touching domain code.

- **Idempotency everywhere.** Every collection request carries a client idempotency key;
  every webhook is deduplicated on provider event id and is replay-safe.
- **Webhooks are advisory.** State transitions require verification against the provider
  API or the settlement file. A webhook alone never releases a payout.
- **Mandates are first-class.** A UPI Autopay mandate has its own lifecycle — created,
  pending approval, active, paused, revoked, expired — with pre-debit notification
  obligations tracked as scheduled work, not as a cron that hopes.
- **Offline money is modelled, not ignored.** Cash and direct bank transfer are a
  recorded payment method with an evidence field and a tenant-visible receipt. Pretending
  every rupee flows through the gateway is why owners abandon rent apps.

Full tax and regulatory treatment: [`india-compliance.md`](india-compliance.md).

## Documents & compliance

An agreement is a state machine, not a file: `draft → stamped → signed → active →
expiring → renewed | terminated`. Each transition carries its evidence — the e-stamp
certificate number, the eSign audit trail, the registration receipt where applicable.

- Templates are versioned per state and per property type; a lease records which template
  version produced it.
- Stamp duty is computed from state slabs at draft time and re-checked before stamping.
- Any term beyond 11 months routes to the registration path explicitly. The system never
  produces an unregistered long lease and calls it done.
- Police tenant verification is a tracked obligation with a per-state channel (online
  portal where one exists, generated form where it does not).

## Maintenance & vendors

Tickets carry category, priority, SLA, photos, and — critically — **who pays**. The
owner/tenant liability split is the source of most disputes, so it is a rule on the lease
(`liability_matrix`), evaluated at ticket creation and shown to both parties before work
starts.

Vendor dispatch, quotation, approval threshold, job completion with tenant sign-off, and
the resulting posting to the ledger form one flow. A completed job that does not produce
a ledger posting or a documented owner-borne cost is a bug.

## Society / RWA module

Reuses the same property/unit/ledger core with different vocabulary and one added concept:
**membership**. Dues are levied per unit (area-based, flat, or slab), interest on arrears
follows the society's bye-laws, and collections post to society funds (`maintenance`,
`sinking`, `corpus`) that must be reportable separately for audit.

Also in scope: notices and circulars with read receipts, committee roles and elections,
amenity booking with conflict rules and charges, complaint escalation, and visitor/gate
management with pre-approved entries, delivery handling, staff attendance and gate passes.

The society module's hard requirement is **transparency**: any member can see the fund
balances and their own ledger without asking the treasurer.

## Identity & access

- Keycloak, phone-number-first (OTP) because that is the Indian reality; email optional.
- Roles scoped to `(organisation, property, unit)`: `owner`, `co_owner`, `manager`,
  `field_agent`, `accountant`, `tenant`, `occupant`, `committee_member`, `guard`,
  `vendor`, `support`, `platform_admin`.
- **Delegation is explicit and revocable.** An owner granting a firm the right to manage a
  unit creates a scoped, effective-dated grant. Revoking it stops access immediately and
  leaves the historical record intact.
- Fail-closed authorisation at the service boundary; the UI never carries the decision.

## Multi-tenancy

Designed in from day one — retrofitting is brutal and this org has learned it twice.

- `tenant_id` (organisation) on every table, RLS enforced in PostgreSQL, and a shared SDK
  that binds it from the request context so no handler can forget.
- Whitelabel from MVP 5: a PM firm or a large society federation runs Dwellm8 on its own
  subdomain with its own branding, notification templates and support routing.
- Cross-organisation reads (a firm managing an owner's unit) always traverse a grant
  object; there is no "trusted internal query" that bypasses RLS.

## Notifications

WhatsApp Business API is the primary channel in India and is treated as such: templated,
approved message categories, opt-in and opt-out honoured, delivery receipts recorded.
SMS is the fallback for OTP and statutory notices, push for app users, email for
statements and documents, and the in-app inbox is the durable record.

Every notification is idempotent, suppressible, and rate-limited per recipient. A tenant
who paid must never receive the next reminder in the ladder.

## Data platform

- PostgreSQL (CNPG) as the transactional store; **all SQL schema lives in
  `tesserix-k8s/charts/apps/db-schema-bootstrap/schemas/dwellm8/`** — this repository
  holds ORM models only.
- Redis for cache, locks and rate limits. NATS JetStream for the domain event backbone,
  with an outbox on every write path so no event is lost on a pod roll.
- Analytics reads from a replica or a warehouse export; reporting queries never run
  against the primary.
- Documents in GCS with signed, short-lived URLs and per-organisation prefixes.

## Security & privacy baseline

- DPDP Act 2023 posture: purpose-limited collection, consent records, data-principal
  rights (access, correction, erasure) as implemented endpoints rather than an email
  address, and breach notification runbooks.
- **KYC identifiers are not profile data.** Aadhaar numbers are never stored; verification
  is performed via DigiLocker/authorised flows and only the *result*, the masked reference
  and the audit trail persist. PAN, bank account and payout details live in GCP Secret
  Manager or KMS-envelope-encrypted columns, never plaintext in the app database.
- Telemetry, logs and analytics exclude KYC identifiers, bank details, document contents
  and full phone numbers.
- Tenant-isolation tests are mandatory for any story that touches a data path.
- Secrets never in source; all sensitive material in GCP Secret Manager.

## Reliability

- Collections are the critical path: the rent-due → invoice → reminder → collect →
  reconcile chain must be resumable at every step, and every step is a durable workflow
  or an outbox-backed event.
- Idempotency, retry with backoff, and dead-letter handling on every provider call.
- Reconciliation is a first-class scheduled job with alerting on drift, not a report
  someone runs when a landlord complains.
- Defined RPO/RTO, tested restore, and a documented degraded mode: if the gateway is down,
  the platform still records offline payments and still shows correct balances.

## Observability

OpenTelemetry traces across the money path end to end, metrics on collection success rate,
mandate health, reconciliation drift, ticket SLA breach and notification delivery, and
structured logs with no PII. A dashboard per organisation for support, and a platform
dashboard that answers "is rent collection working right now" in one glance.

## MVP 1 scope — launch with only these

18 items. Anything not on this list is MVP 2 or later.

1. Phone-OTP sign-up and organisation creation for a landlord
2. Add property, block and unit (bulk import from spreadsheet)
3. Add tenant and create a lease with schedule, deposit and due day
4. Automated monthly invoice generation with proration
5. UPI intent/collect payment link per invoice
6. Offline payment recording (cash/bank transfer) with evidence
7. Receipt generation and delivery
8. Reminder ladder over WhatsApp and SMS
9. Late-fee rules
10. Double-entry ledger with derived balances
11. Gateway reconciliation job with drift alerting
12. Deposit tracking (no settlement workflow yet)
13. Tenant view — dues, pay, receipts, history (web, no app yet)
14. Rent-paid statement export for the tenant, income statement for the owner
15. Basic maintenance ticket with photos and status
16. Lease expiry and renewal reminders
17. Multi-tenancy with RLS and the tenant-isolation test suite
18. Observability and the collections dashboard

Explicitly **not** in MVP 1: agreements/e-stamp/eSign, KYC, autopay mandates, owner
payouts, GST/TDS, society module, vendor marketplace, mobile apps, whitelabel.

## Known risks

| Risk | Mitigation the backlog must carry |
|---|---|
| Rent stays in cash and never enters the platform | Offline recording is first-class; value is the ledger and documents, not only the gateway |
| Owners churn after one collection failure | Reconciliation, alerting and a degraded mode that still produces correct balances |
| Deposit disputes become support load | Itemised deductions with evidence and a tenant acknowledgement step |
| Compliance drift across states | State-scoped rule tables with an owner and a review date, never hardcoded constants |
| Society treasurers refuse to migrate | Import from Excel/Tally exports and a read-only parallel-run mode |
| KYC data becomes a liability | Never store Aadhaar; verification result only; encryption and access audit |
| WhatsApp template rejection blocks reminders | SMS fallback path proven before launch, template inventory reviewed |

## Earliest decisive decisions

1. Ledger schema and posting rules — everything else depends on it.
2. Organisation/property/unit/lease model with effective dating and RLS.
3. Payment provider adapter shape and the mandate lifecycle.
4. Mobile framework (React Native vs Flutter) — gate the decision before MVP 2.
5. Whether society dues and rent share one charge engine (strong default: yes).

## Tesserix platform conventions this repository inherits

- Go 1.26 + Gin services, Next.js 16 web, `@tesserix/web` design system.
- All SQL schema in `tesserix-k8s/charts/apps/db-schema-bootstrap/schemas/`; app repos
  hold ORM models only.
- ArgoCD owns cluster state; no manual `kubectl apply`. Kargo promotes images.
- Memory-only resource specs (no CPU requests/limits) in Helm values.
- Secrets in GCP Secret Manager and External Secrets, never in the database or git.
- CI runs under the public → build → private cycle for private repos.
