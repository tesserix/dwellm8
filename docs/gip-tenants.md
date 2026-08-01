# GIP tenants — the six user pools, and how the API consumes them

Provisioned 2026-08-01 in project `tesseracthub-480811` (ADR-0027 §1, issue
#229). One Identity Platform tenant per app surface; the same phone number in
two pools is two user records that cannot see each other, enforced by Google.

| Surface | Tenant ID | Who signs in |
|---|---|---|
| Own | `dwellm8-own-5945j` | Landlords |
| Ops | `dwellm8-ops-r8klu` | Managers and agencies |
| Live | `dwellm8-live-abowj` | Tenants and residents |
| Find | `dwellm8-find-xui6r` | Prospects on the marketplace |
| Pro | `dwellm8-pro-rk0lx` | Vendors and technicians |
| Admin | `dwellm8-admin-6izec` | Dwellm8 staff console |

All six were created with `allowPasswordSignup: false` — no email/password
anywhere, per ADR-0027 §5. Dwellm8's own staff authenticate at the **project**
level, outside every tenant; a staff token carries no tenant claim at all.

## How the API consumes these

The API never configures tenant ids. `auth.SurfaceFor` derives the expected
prefix from the surface (`dwellm8-own`) and accepts the minted id
(`dwellm8-own-5945j`) by prefix-plus-separator — exact-prefix collisions like
`dwellm8-ownx` are refused, and only project admins can create tenants, which
is what makes the suffix trustworthy. A token from a tenant the API does not
recognise is refused outright rather than treated as staff: an unknown tenant
promoted to "no tenant" would be promoted to Dwellm8 staff.

The clients are the ones that need the full ids above: each app's sign-in
screen names its tenant when it initialises the Firebase SDK. That is this
table's audience.

## Still to configure (console, per tenant — USER_REQUIREMENT.md item 1)

1. **Phone** sign-in on all six — the OTP flow every user starts with.
2. **Google** on `own`, `ops`, `pro` only (business surfaces).
3. **Apple** later, with the iOS builds — required by the App Store once any
   other social provider ships, and not before.

Until providers are configured no token can be minted, so the dev impersonation
shim (`DEV_IMPERSONATE_ORG` / `DEV_IMPERSONATE_RESIDENT`, dev-only by
`config.validate`) remains the way to exercise the API locally. Removing it is
#229's closing act, and it gates the `AUTHZ_ENFORCE` flip.

## Related

- ADR-0027 — the decision this implements
- #229 — provisioning and shim removal
- #152 — phone OTP for every user, including admin
- #31 — first sign-in becomes an organisation and a membership (the onboarding
  path; ADR-0027 is its prerequisite, not its implementation)
