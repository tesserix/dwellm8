# Dwellm8 Live

The tenant, resident and guardian app — the highest-volume surface in the
product. Everything here is built around three jobs: **pay in seconds**,
**report a problem and know who pays**, and **prove what happened**.

## Run it

```bash
npm install          # from the repo root (npm workspaces)
npm start            # then i / a, or scan with Expo Go
npm run web
npm run typecheck
```

## Screens

| Screen | Route | Notes |
|---|---|---|
| Home | `app/(tabs)/index.tsx` | Amount due front and centre, one-tap Pay, tenancy tiles, open requests, notices |
| Pay | `app/(tabs)/pay.tsx` | Invoice breakdown, method picker with honest fee disclosure, receipts, HRA statement |
| Requests | `app/(tabs)/requests.tsx` | Open and resolved tickets, each showing who pays |
| Chat | `app/(tabs)/chat.tsx` | Thread with the property manager |
| Raise | `app/raise.tsx` | Category, description, photos, urgency — with the liability rule shown before submitting |
| Ticket | `app/ticket.tsx` | Who pays, the reason, cost, and a progress timeline |
| Documents | `app/documents.tsx` | Tenancy terms, agreement, condition report, deposit acknowledgement |

## Two product rules this app makes visible

**The tenant is never surcharged by dwellm8.** The 2.99% platform fee (ADR-0031, configurable) is borne
by the property manager at payout (requirements §5.5). The only amount ever
added to a tenant's payable is a genuine payment-instrument cost — card
processing — and it is disclosed before payment with UPI offered as the free
alternative. `app/(tabs)/pay.tsx` shows this explicitly rather than burying it.

**Who pays is settled before the technician is called.** `app/raise.tsx` shows
the liability rule at the point of raising, and `app/ticket.tsx` restates it
with the reason and the amount. This is the cost-sharing engine surfacing
itself, and it is why a tenant, an owner and a manager can look at the same job
and agree.

## Data

`src/data/mock.ts` is demonstration data per requirements §9.6 — a fictional
Bengaluru tenancy. Amounts are integer paise, rendered only through `inr` from
`@dwellm8/mobile-shared`.
