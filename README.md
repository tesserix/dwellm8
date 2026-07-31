# Dwellm8

**Rent, maintenance and compliance for Indian property — in one ledger.**

Dwellm8 is a property management platform built for how Indian rental property actually
works: an 11-month agreement, a cash-or-UPI rent cycle, a security deposit nobody has
reconciled, a WhatsApp thread that is the only record of a repair, and a plumber the
owner found through a neighbour. Landlords, management firms, housing societies and
tenants each get the surface they need, on top of one shared property, lease and money
model.

> A landlord in Pune with four flats, a management firm in Bengaluru running 300 units
> for 90 owners, and a 240-flat RWA in Noida are the same platform with different
> permissions — not three different products.

## Who it is for

| Segment | What they get |
|---|---|
| **Landlords & small owners** (1–20 units) | Properties, leases, automated rent invoicing, UPI collection, receipts, deposit ledger, maintenance requests, renewal reminders |
| **Property management firms** | Multi-owner portfolios, staff roles and field agents, owner payouts and statements, vendor management, GST-compliant fee invoicing, portfolio reporting |
| **Housing societies & RWAs** | Maintenance dues and interest on arrears, notices and circulars, amenity booking, visitor and gate management, complaint tracking, society accounts |
| **Tenants** | Pay rent by UPI or autopay, receipts and rent-paid statements, raise and track tickets, view the agreement, deposit status, move-out settlement |

## What makes it defensible

Rent software is easy to demo and hard to trust. The difference is not the UI — it is
whether the money and the paperwork survive an audit twelve months later.

Three rules the architecture enforces everywhere:

1. **Money is a double-entry ledger, not a status column.** Rent, late fees, deposits,
   adjustments, owner payouts, refunds and gateway settlements are postings. A balance
   is always derived, never stored and edited. Currency is `amountMinor` + `currency`;
   floats never touch money.
2. **Every rupee movement is reconciled against the provider, not against our own UI.**
   A payment is settled when the gateway says so and the settlement report matches —
   webhooks are idempotent, replayable and never the only source of truth.
3. **Compliance artefacts are first-class objects, not attachments.** An agreement knows
   its stamp duty, its e-stamp certificate, its eSign audit trail, its registration
   status and its expiry. A TDS deduction knows its section, its challan and its
   certificate. A PDF in a folder is not a record.

## Product surfaces

| Surface | Purpose |
|---|---|
| Dwellm8 Owner | Landlord portal — properties, leases, rent, deposits, documents |
| Dwellm8 Manage | Management-firm control plane — portfolios, staff, owners, payouts |
| Dwellm8 Society | RWA module — dues, notices, amenities, complaints, society accounts |
| Dwellm8 Tenant | Tenant app — pay, receipts, tickets, agreement, move-out |
| Dwellm8 Gate | Guard app — visitor entry, delivery, staff attendance, gate passes |
| Dwellm8 Pay | Collections, UPI autopay & mandates, payouts, refunds, ledger |
| Dwellm8 Docs | Agreements, e-stamp, eSign, KYC vault, police verification |
| Dwellm8 Care | Maintenance, ticketing, SLA, vendor dispatch and job costing |
| Dwellm8 Books | Accounting, GST, TDS, owner statements, society audit pack |
| Dwellm8 Lease | Listings, enquiries, screening, lead-to-lease |
| Dwellm8 Partners | Vendor, broker and society-partner portal |
| Dwellm8 Admin | Internal control plane — tenants, plans, support, reconciliation |

## Built for India, not localised for it

The India-specific mechanics are the product, not a settings page. Details in
[`docs/india-compliance.md`](docs/india-compliance.md).

- **UPI first.** UPI intent and collect for one-off rent; **UPI Autopay mandates** for
  recurring rent, with NACH as the higher-value fallback. Cash and bank transfer are
  recorded, not ignored — most rent in India still moves outside a gateway.
- **The 11-month agreement.** Templates per state, stamp duty computed from state slabs,
  e-stamping, Aadhaar eSign with an audit trail, and a hard rule that any term beyond
  11 months triggers the registration path rather than pretending it does not exist.
- **Tenant verification.** DigiLocker/PAN-based KYC and police tenant verification,
  where the verified *result* is stored and the raw identifier is not.
- **GST and TDS.** 18% GST on management fees, the ₹7,500-per-member society exemption,
  commercial-rent reverse charge, and §194-I / §194-IB TDS on rent with Form 26QC and
  16C handled as workflow rather than advice.
- **WhatsApp is a primary channel.** Rent due, receipt, ticket update and notice all
  reach a tenant on WhatsApp; email is secondary and the in-app inbox is the record.

## Repository status

Building. The Go API, six Expo apps and the shared design system live here; the schema and
the charts live in [`tesserix-k8s`](https://github.com/tesserix/tesserix-k8s). The plan is
still GitHub issues and the org project board.

| Resource | Where |
|---|---|
| **Running it locally** | [`SETUP_LOCAL.md`](SETUP_LOCAL.md) |
| **Changing the database schema** | [`docs/schema-changes.md`](docs/schema-changes.md) — it is not in this repository |
| Architecture decisions | [`docs/adr/`](docs/adr/README.md) |
| **Threat model & security baseline** | [`docs/threat-model.md`](docs/threat-model.md), [`docs/security-baseline.md`](docs/security-baseline.md) |
| Privacy: retention, breach | [`docs/data-retention.md`](docs/data-retention.md), [`docs/breach-runbook.md`](docs/breach-runbook.md) |
| Product & architecture brief | [`docs/product-brief.md`](docs/product-brief.md) |
| India regulatory, payments & tax model | [`docs/india-compliance.md`](docs/india-compliance.md) |
| Backlog conventions & taxonomy | [`docs/backlog.md`](docs/backlog.md) |
| Backlog | [Issues](../../issues) — 117 stories across 39 epics and MVP 0–6 |
| Issue template | [`.github/ISSUE_TEMPLATE/engineering-story.md`](.github/ISSUE_TEMPLATE/engineering-story.md) |

## Intended stack

Web Next.js 16 · Mobile React Native or Flutter (decision pending) · Backend Go 1.26 +
Gin · PostgreSQL (CNPG) with row-level security · Redis · NATS JetStream · Temporal for
money and document workflows · Keycloak for identity · GKE + Istio + ArgoCD ·
OpenTelemetry.

Everything is served from **dwellm8.com** — the production domain, bound to the Istio
VirtualService in `tesserix-k8s`. The marketing site and the web consoles live on it;
the mobile apps talk to the same origin. Sub-brands keep the `dwellm8.com` root rather
than taking their own domains.

REST externally, gRPC internally where it earns its keep. Start as ~6–8 services with
clean domain boundaries — property, lease, money, maintenance, community, documents,
notifications, identity — not thirty microservices.

## Delivery phases

| Phase | Window | Ships |
|---|---|---|
| MVP 0 | W0–W6 | Foundations: tenancy model, ledger design, auth, schema, CI/CD |
| MVP 1 | M1–M3 | Landlord core — properties, leases, rent invoicing, UPI collection, receipts |
| MVP 2 | M3–M6 | Tenant app, agreements with e-stamp + eSign, KYC, maintenance tickets |
| MVP 3 | M6–M9 | Management firms — portfolios, staff, owner payouts, accounting, GST/TDS |
| MVP 4 | M9–M12 | Societies — dues, notices, amenities, visitor & gate, complaints |
| MVP 5 | M12–M15 | Lease funnel, screening, vendor marketplace, deposit alternatives |
| MVP 6 | M15–M18+ | Whitelabel B2B, enterprise SSO, analytics, commercial property |

Monetisation is staged deliberately: free ledger → per-unit SaaS for owners and firms →
society subscription → transaction take on collections, vendor jobs and lease origination.
Collection reliability has to be proven before any revenue depends on it.
