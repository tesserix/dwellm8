# ADR-0027 — Identity: a GIP tenant per app, a bearer token per request, and a membership lookup between them

- **Status**: Accepted
- **Date**: 2026-07-31
- **Deciders**: Platform, Product
- **Issues**: [#31](https://github.com/tesserix/dwellm8/issues/31), [#85](https://github.com/tesserix/dwellm8/issues/85)
- **Related**: [ADR-0003](0003-tenancy-and-row-level-security.md), [ADR-0005](0005-owner-delegation-grants.md), [ADR-0013](0013-kyc-data-handling.md), [ADR-0026](0026-dpdp-posture.md), [`docs/threat-model.md`](../threat-model.md)
- **Supersedes**: the phone-first identity sketch in ADR-0004's reserved slot, which was closed as not planned

---

## Context

Dwellm8 is six apps on one domain, **dwellm8.com**, over one database. The people
are not one population: a tenant in Live, a landlord in Own, a manager in Ops and
a plumber in Pro are different users with different exposure, and several of them
are the same human being in different roles.

Two things have to be true at once, and they pull against each other:

1. **The user bases are isolated.** A tenant must not be able to sign into the
   manager app with the credentials they use for their own. Not "must not be
   authorised to" — must not be able to *sign in*.
2. **Dwellm8's own staff cross all of it**, because support is one team.

Everything else follows from getting that pair right, and getting it wrong is not
a bug that shows up in a test. It shows up as one customer seeing another's data,
which is the failure this platform's entire schema is arranged to prevent.

---

## Decision

**One Google Identity Platform tenant per app surface, inside the org's existing
project. The apps send the GIP ID token as a bearer credential and the Go API
verifies it itself. Verification says who signed in; a membership row says what
they may act as, and nothing conflates the two.**

### 1. Six pools, and the isolation is Google's

Identity Platform's own multi-tenancy, one tenant per surface:

```
GCP project tesseracthub-480811
└── Identity Platform
    ├── (project level)      ← Dwellm8 staff only
    ├── dwellm8-own          landlords
    ├── dwellm8-ops          managers and agencies
    ├── dwellm8-live         tenants
    ├── dwellm8-find         prospects on the public marketplace
    ├── dwellm8-pro          vendors and technicians
    └── dwellm8-admin        the internal console
```

The same phone number in `dwellm8-own` and `dwellm8-live` is **two user records
that cannot see each other**. That separation is enforced by Google, not by a
claim we set — and the difference matters more than it sounds: a claim we set is
a claim we can get wrong, and the shape of getting it wrong is a tenant signing
into the manager app.

The tenant id is **derived from the surface** in code, not configured per
deployment. A values file that could point two surfaces at one pool is a values
file that can silently merge two user bases.

### 2. Staff are the absence of a tenant, not a flag

Dwellm8 employees authenticate at the **project** level, which produces a token
with no `firebase.tenant` claim at all. GIP will not mint such a token for
somebody who signed in against a tenant, so "is this person staff" is answered by
Google's own issuance rather than by a boolean in our database that an attacker
would try to set.

That is the product-owner exception the product needs, and it is the one place
where a *missing* value is the strong signal. Which is exactly why an
**unrecognised** tenant is an error rather than a fallback: treating "a tenant I
do not know" as "no tenant" would promote a stranger to staff.

### 3. Bearer tokens, verified in the API

The apps hold the ID token and send `Authorization: Bearer <token>`. No session
cookie, no auth-BFF.

mark8ly's auth-BFF pattern is proven and it is browser-shaped. Dwellm8 is six
Expo apps, where the Firebase SDK already owns the refresh cycle and a cookie
would have to be re-implemented per platform. A second deployable in the
authentication path of every request is also a second thing to be down.

**The token's tenant claim is what selects the surface**, so the same middleware
serves every app and the app does not get to say which one it is.

### 4. Verified with stdlib crypto, not the Firebase Admin SDK

This service has **one direct dependency** and it is a database driver. The
Firebase Admin SDK would bring `google.golang.org/api`, gRPC and OpenCensus for
what is, in the end, RS256 over Google's published certificates plus an issuer,
an audience and an expiry.

So verification is about a hundred lines of `crypto/rsa` and `encoding/json`, and
we take on the risk of writing it. That risk is answered the only way it can be:
**every check is a named test that forges the token which would exploit its
absence** — `alg: none`, HS256 against the public key, an unknown `kid`, a valid
Google token for another project, the right audience with a wrong issuer, a
tampered payload, an unknown tenant, a Dwellm8-shaped tenant that does not exist.
Thirteen forgeries, each refused.

Two rules inside that code are worth naming because they are where JWT
verification is usually got wrong:

- **The algorithm is fixed and compared, never selected from the header.** The
  header is the attacker's to write.
- **Every refusal returns the same error.** A message distinguishing "expired"
  from "wrong audience" is an oracle telling a forger which half worked.

### 5. Sign-in methods

**Phone OTP on every surface.** Indian rental runs on a mobile number: it is the
identifier the tenancy, the notices and the payments all key on, and it is the
one a tenant actually has.

**Google on Own, Ops and Pro** — business users live in Gmail and it yields a
verified email for free. **Apple on the iOS builds**, which the App Store requires
once any other social provider is offered. **Not email and password anywhere**,
including Admin: a password is a thing to breach, rotate and support, and every
surface already has a stronger factor.

### 6. Verification is not authorisation

The middleware puts a `Principal` in the context and stops. It does **not**
resolve an organisation.

A GIP uid identifies a person; Dwellm8's isolation is by organisation; the step
between them is `organisation_members`, a database row. Reading the organisation
from a token claim would put the tenancy boundary — the thing ADR-0003's entire
row-level-security scheme exists to hold — inside a value the client's own app
composed.

`identity_principals` is keyed on **(surface, gip_uid)**, because a uid is unique
within a pool and not across pools. It has no `tenant_id`: somebody signing into
Find is nobody's tenant yet. That means RLS has nothing to scope it by, so the
**privilege** is the boundary instead — the table is revoked from `dwellm8_app`
after the blanket grant, and only the identity module holds it. Otherwise every
module could read a table of every verified phone number on the platform.

---

## Alternatives considered

### A. One user pool with an `apps` custom claim — rejected

Operationally simplest and it moves the isolation from Google to us. One mistake
in claim handling and a tenant signs into Ops. The whole point of §1 is that this
boundary should not depend on our correctness.

### B. A GIP tenant per customer organisation — rejected

The strongest isolation between customers, and unworkable: GIP's default ceiling
is 1,000 tenants per project, provisioning one becomes a hard dependency inside
signup, and a tenant renting from two different firms would need two logins for
one flat each. Customer isolation is the `organisations` table under RLS, which
is what ADR-0003 built and what scales.

### C. auth-BFF with an encrypted session cookie — rejected, with a caveat

mark8ly's pattern, and correct for a browser-first product. Awkward for six
native apps, and a second deployable in front of everything. If the web consoles
ever become the primary surface this is worth revisiting — the verifier here is
the same either way, so the cost of changing is one service and not one model.

### D. Keycloak, as HomeChef uses — rejected

A cluster dependency to run, patch and back up, for a product whose auth needs
are phone OTP and two social providers. GIP is managed, already in the project,
and its multi-tenancy is precisely the shape of the requirement.

### E. A dedicated GCP project for Dwellm8 — rejected, for now

Total blast-radius separation, and a second set of everything to provision and
operate. Reusing `tesseracthub-480811` keeps one Identity Platform and one set of
operational knowledge. **The condition attached**: Dwellm8 gets its **own web API
key with its own referrer allowlist**. A shared key's allowlist has silently
broken custom-domain login in this org before, and dwellm8.com must not be
sitting behind another product's key.

---

## Consequences

**What is now true.** Six user bases that cannot reach each other, by Google's
enforcement rather than ours. Staff who cross all of them, identified by an
absence GIP will not counterfeit. A verifier with one dependency and thirteen
forgeries refused in its test file. A middleware that refuses unauthenticated
requests centrally, rather than leaving each handler to remember. And a hard line
between "who signed in" and "what they may act as", with the second in a table
under row-level security.

**What this costs.** Six GIP tenants to provision and keep configured, and a
sign-in screen per app that must name the right one — the derivation in code
protects the API, not the client. We own JWT verification, which is a thing to
keep tested as GIP changes. A person who is a landlord *and* a tenant has two
accounts and will not always understand why. And the API now needs outbound HTTPS
to Google for the certificate set, cached, with a documented fallback to a stale
key when Google is unreachable, because the alternative is authentication going
down with somebody else's outage.

**What is not decided.** Onboarding — turning a first sign-in into an
organisation and a membership — is [#31](https://github.com/tesserix/dwellm8/issues/31),
and this ADR is its prerequisite rather than its implementation. The app-side
sign-in screens are unbuilt, which is why
[#85](https://github.com/tesserix/dwellm8/issues/85) is still blocked. Fine-grained
permission within an organisation stays ADR-0005's delegation grants and OpenFGA;
`organisation_members.role` is deliberately coarse and must not grow into a
permission system. Token revocation is bounded by the token's own lifetime — a
disabled principal keeps a working token until it expires, and closing that gap
means a check per request against a store, which is a cost to take deliberately
rather than by default.
