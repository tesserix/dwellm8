# dwellm8 Own

The property owner's app — the surface an owner opens to answer one question:
*did I get paid, and what came out before I did?*

Built to the design language in `docs/requirements` §7 (mobile-first) and the
data model in §9.2. Currency is Indian throughout: `₹`, lakh grouping, and
amounts stored as integer paise — never floats.

## Run it

```bash
npm install
npm start          # then press i / a, or scan with Expo Go
npm run web        # browser
npm run typecheck
```

## What is here

| Screen | Route | Notes |
|---|---|---|
| Home | `app/(tabs)/index.tsx` | Property card, quick actions, filter chips, Up Next, Recent Activities |
| Financials | `app/(tabs)/financials.tsx` | Dashboard / Statements / Transactions; balance, income-vs-expense chart, expense breakdown |
| Chat | `app/(tabs)/chat.tsx` | Agency list |
| Thread | `app/thread.tsx` | Conversation with out-of-hours banner |
| Property | `app/property.tsx` | Details / Contact / Documents sheet |
| Job | `app/job.tsx` | Work order with quote, attachment and a *who pays* explanation |
| Switcher | `app/switcher.tsx` | Property filter |
| Profile | `app/profile.tsx` | Account, terms, delete |

## Mobile-native on the web

`src/components/Shell.tsx` keeps the web build honest. Below 520px it is a
pass-through and the app runs full-bleed exactly as it does on a device. Above
that it constrains the app to a 440px column and centres it, so a desktop
browser shows the phone app rather than a phone layout stretched across a
monitor.

## Data

`src/data/mock.ts` is **demonstration data** per requirements §9.6 — a
fictional Indian owner with two managed units. It must never gain the ability
to produce a posting, payment, fee, message or dispatch. When the API lands,
this file is replaced by the generated client, not supplemented by it.
