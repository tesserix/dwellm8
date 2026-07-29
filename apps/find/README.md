# Dwellm8 Find

The marketplace. Owners and managing agencies list on the same terms, seekers
search listings that have actually been checked, and a listing that is not
current is not here.

| Rule | Where it lives |
|---|---|
| Free to list, for owners and agencies alike | `(tabs)/list` |
| Nothing goes live until ownership, identity and address are verified | `publish` step 4, `listing` verification panel |
| A listing runs 90 days, then comes down | `LISTING_DAYS` in `src/data/mock.ts`, shown on every card |
| Promotion buys position, never a badge | `manage`, and stated on the listing itself |
| Inspection visitors check in with a QR | `inspect` — the lister sees registered against attended |
| Dwellm8 earns 2.99% at payout, only on managed tenancies | `(tabs)/list` |

| Journey | Screens |
|---|---|
| Find a home | `(tabs)/index` → `listing` → `inspect` → `apply` |
| Watch the market | `(tabs)/saved` — saved homes and searches |
| Track what you applied for | `(tabs)/enquiries` |
| List your own | `(tabs)/list` → `publish` → `manage` |

Everything renders from `src/data/mock.ts` — demonstration data per
requirements §9.6.

```bash
npm install          # from the repository root
npm run ios -w @dwellm8/find
```
