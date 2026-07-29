# Dwellm8 Ops

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

Everything renders from `src/data/mock.ts`, which is demonstration data per
requirements §9.6. No network call is made and no side effect originates here.

```bash
npm install          # from the repository root
npm run ios -w @dwellm8/ops
```
