# Dwellm8 Admin

The mobile half of the Admin surface. The app carries the urgency — alerts,
approvals, triage and on-call intervention. The web console carries the depth —
rule tables, fee configuration, bulk operations and cross-record investigation.
One permission model, one audit trail, two form factors.

| Journey | Screens |
|---|---|
| On call | `(tabs)/index` → `alert` — acknowledge, run a bounded intervention |
| Approve | `(tabs)/approvals` → `approval` — decisions require a recorded reason |
| Triage | `(tabs)/triage` → `dispute`, `reconcile` |
| Support call | `(tabs)/lookup` → `customer` |

Screens deliberately absent from the app are listed on the Approvals tab and in
the profile — they are console work by design, not omissions.

Everything renders from `src/data/mock.ts` — demonstration data per
requirements §9.6.

```bash
npm install          # from the repository root
npm run ios -w @dwellm8/admin
```
