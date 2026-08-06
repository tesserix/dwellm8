# Dwellm8 PM

The manager, field agent and warden app — the mobile half of a surface whose
other half is the Ops web console. Anything the app can do, the console can do;
the app carries what happens away from a desk.

| Journey | Screens |
|---|---|
| Start the day | `(tabs)/index` — collections against target, SLA risk, worklist |
| Recover money | `(tabs)/collect` → `arrear` → `receipt` |
| Run the job queue | `(tabs)/jobs` → `ticket` → `dispatch` |
| Inspect a property | `(tabs)/inspect` → `inspection` (offline capture) |
| Know the portfolio | `(tabs)/portfolio` → `property` |
| Hostel and society | `beds`, `gate` |
| Let a vacant unit | `leads` |
| Pay the owners | `payouts` — the single point the platform fee is charged |

Screens read the live API through `src/data/*`. Four remain on `src/data/mock.ts`
because they have no endpoint behind them yet — `beds` (#299), `society` (#300),
`compliance` (#301) and `inspection` (#298) — and each says so on screen.

```bash
npm install          # from the repository root
npm run ios -w @dwellm8/pm
```
