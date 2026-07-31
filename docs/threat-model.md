# Threat model

Dwellm8 moves money between strangers and holds identity documents for people who are not
its paying customers. This is the model of how that goes wrong, written against the code
and schema that exist rather than against an architecture diagram.

Every threat below has an **owner**, a **control**, and a status: implemented, an issue
that implements it, or an accepted risk with the reason. A threat with none of those is
not modelled, it is noticed.

Read with [`security-baseline.md`](security-baseline.md), which is what every service must
meet, and [`breach-runbook.md`](breach-runbook.md), which is what happens when one of these
lands anyway.

---

## 1. What is worth attacking

Three paths, in the order an attacker would rank them.

| Path | The prize | Where it lives |
|---|---|---|
| **Money** | Rent collected, deposits held, owner payouts | `journal_entries`, `ledger_postings`, `payments`, `settlement_*`, `mandates` |
| **Identity** | PAN, bank details, verification results, occupancy history | `kyc_verifications`, `consent_artefacts`, `lease_parties` |
| **Documents** | Executed agreements, the evidence in a dispute | `leases`, `tds_certificates`, `tds_obligation_steps` |

The asymmetry that shapes everything: **the tenant did not choose this platform.** They
cannot leave, they did not accept the terms, and their exposure is the landlord's decision.
Controls that would be adequate for a customer are not automatically adequate for them.

---

## 2. STRIDE over the money path

| | Threat | Control | Status |
|---|---|---|---|
| **S** | A forged webhook marks a payment confirmed that never arrived | HMAC verification per adapter; an unverified delivery is parked and **its contents are never used to look up a payment**, so it cannot even probe which ids exist | Implemented — `IngestWebhook`, ADR-0011 |
| **T** | An invoice or posting is altered after the fact | `DELETE` and `UPDATE` revoked on the ledger; a correction is a reversing entry with a reason code | Implemented — ADR-0006 §3 |
| **R** | A party denies authorising a debit | Mandates are a standing authority with their own lifecycle and provider reference | Implemented — ADR-0022 |
| **I** | One organisation reads another's ledger | `tenant_id` + FORCE row-level security; the five-part isolation contract over every table | Implemented — ADR-0003 |
| **D** | Webhook flood, or a rent-day thundering herd | A token bucket per tenant, and an unkeyed one on the webhook route where an attacker has no identity to be limited by | Implemented — `internal/platform/httpx`, [#228](https://github.com/tesserix/dwellm8/issues/228) |
| **E** | The request role gains the platform exemption | Separate PostgreSQL roles; `is_platform_session()` is a policy branch, not a role attribute; assertion 2 refuses `BYPASSRLS` on any application role | Implemented — ADR-0003 |

### 2.1 The one that actually worries me

A compromised aggregator webhook secret does **not** move money out, and that is the
story's edge case. But it does something worse than nothing: it can **fabricate a receipt**.
A forged, correctly signed delivery marks a payment confirmed, which posts a receipt to the
ledger, which makes a tenant look paid and — once payouts are wired — makes an owner
payable for money that never arrived.

The control is that a payout must settle against **funds the provider says it settled**,
not against a receipt (ADR-0012's reconciliation and drift). Today the property holds
partly *by absence*: the payout workflow ([#80](https://github.com/tesserix/dwellm8/issues/80))
is not built, so no code path releases money at all.

> **This is the single most important thing to preserve when #80 lands.** A payout that
> reads the ledger instead of the settlement file re-opens this, and it will look correct
> in every test written against a clean database.

### 2.2 The tenant surface, and the boundary that is not the organisation

Every control in the table above draws the line between organisations. Issue #51 opened a
surface where that line is the wrong one: a renter scoped only to their landlord's
organisation reads **every other tenant of that landlord** — their rent, their arrears,
their payment history — and the failure is silent, because every policy still reads
correctly and every screen still renders. A firm with three hundred flats has three hundred
tenants who can each read the other two hundred and ninety-nine.

| | Threat | Control | Status |
|---|---|---|---|
| **I** | A tenant reads another tenant of the same landlord | `app.resident_party_id` narrows the session a second time; nine tables opened by restrictive policy, every other row-level-secured table denied by a generated policy | Implemented and asserted — ADR-0029, assertion 19 |
| **I** | A tenant changes the lease id in the URL | The session's residency list is the authorisation, PostgreSQL refuses independently, and the answer is `404` rather than `403` — a `403` confirms the tenancy exists | Implemented — `internal/surface/resident` |
| **I** | Two landlords of one renter learn of each other | No query in the product returns rows from two organisations; a renter's tenancy list is one scoped read per landlord | Implemented — ADR-0029 §4 |
| **S** | Somebody claims a renter's number and inherits their tenancies | The claim is conditioned on the reservation marker, so it can only take over an unclaimed row; the number itself is verified by Identity Platform's OTP | Implemented — ADR-0029 §5 |
| **E** | A tenant writes a payment against somebody else's tenancy | `payments_resident_scope` constrains payer *and* lease in `WITH CHECK`; a payment naming no lease is refused | Implemented and asserted — `TestResidentScope` |
| **D** | A stolen tenant token is used to scrape | A third rate-limit bucket, keyed per sign-in — the tenant surface sends no organisation header, so the per-tenant limiter would key on `""` and limit nothing | Implemented — `httpx.ByBearer` |
| **R** | "I never received this receipt" | The receipt is derived from the payment and names the ledger entry it posted; nothing stores a second copy that can disagree | Implemented — ADR-0029 |

Two CI steps plant defects here and require a red build. One removes the renter narrowing
from `leases` and `lease_parties`; one drops four deny policies. Note that weakening
`resident_holds_lease` alone is *not* a usable planted defect — the policy on
`lease_parties` catches it. That is two independent locks working, and it is why the
planted defect has to remove both.

**The gap that remains**: a denied attempt is written to the application log and not to
`audit_events`, because `audit_events.tenant_id` is `NOT NULL` and a denied attempt belongs
to no organisation. A probe across a hundred lease ids is therefore visible in logs and not
in any customer's audit trail, which is the right place for it but is not alertable today.

---

## 3. STRIDE over the identity path

| | Threat | Control | Status |
|---|---|---|---|
| **S** | A fake landlord lists a flat they do not own | KYC tiering and ownership records | **Partial** — `property_ownership` exists, verification of ownership against it does not. [#67](https://github.com/tesserix/dwellm8/issues/67) |
| **T** | A verification result is edited to say verified | Column allowlist assertion on `kyc_verifications`; no `UPDATE` path in the store | Implemented — ADR-0013 |
| **R** | "I never consented to that check" | `consent_artefacts` with purpose, notice version and language; the foreign key from the verification | Implemented — ADR-0026 |
| **I** | A managing firm reads a tenant's identity documents | The KYC policy has **no delegated branch at all** — a grant cannot reach it, and there is no `kyc.read` permission to grant | Implemented and asserted — ADR-0013 |
| **D** | Verification provider outage blocks onboarding | Tiering: the tier that gates money is separable from the tier that gates browsing | Implemented — ADR-0013 |
| **E** | Support staff read identity data at will | Reads are logged to `kyc_access_log` and a read without a grant behind it is refused | Implemented, **audit gap below** |

### 3.1 The support-impersonation gap

The story's other edge case: *support-staff impersonation must be technically constrained
and fully audited.* It is constrained — a separate role, a policy branch, no delegated path
to KYC — and it is **not fully audited.**

`tenancy.Platform()` requires a `reason` argument and then **discards it**. Nothing writes
an `audit_events` row. So today: every platform action states a reason to the compiler and
to nobody else.

- **Owner**: Platform
- **Control**: `tenancy.Support()` — it cannot be called without naming who, to what and
  why, and it writes the `audit_events` row **in the same transaction as the work**, so the
  trail cannot claim an action the database rolled back or miss one it committed.
- **Status**: **Implemented** — [#226](https://github.com/tesserix/dwellm8/issues/226).
  `Platform()` remains for machine paths with no human actor (the webhook inbox,
  reconciliation, fixtures), and an arch test fails the build if a module's handlers take
  it.

---

## 4. Rental-specific abuse cases

These are not STRIDE categories. They are what actually happens in Indian rental, and they
are the reason a generic model is not sufficient.

| Abuse | How it works | Control | Status |
|---|---|---|---|
| **Fraudulent listing** | A flat that is not the lister's, advertised to collect deposits | Ownership verified against `property_ownership`; enquiries require a verified prospect | **Partial** — the prospect gate is implemented (ADR-0019); ownership verification is not |
| **Impersonated owner** | Payout bank details changed to an attacker's | Bank changes as an effective-dated, audited event with a cool-off before the next payout | **Not implemented** — [#227](https://github.com/tesserix/dwellm8/issues/227). The highest-value unaddressed threat in this table |
| **Altered invoice** | A tenant is shown a UPI handle that is not the platform's | Collection only through the adapter; no free-text payment instructions rendered to a tenant | Implemented by construction — ADR-0011 |
| **Deposit diversion** | The deposit is collected and never held against the tenancy | Deposit posts to `deposit_liability`, not to income, and the holder is recorded on the lease | Implemented — ADR-0006, [#34](https://github.com/tesserix/dwellm8/issues/34) |
| **Collusive vendor invoice** | A manager approves inflated work to a related vendor | Approval thresholds and vendor–manager relationship disclosure | **Not implemented** — maintenance module is unbuilt |
| **TDS diversion** | A tenant deducts tax and never deposits it, leaving the landlord without credit | The obligation is tracked to receipt and the landlord can see the certificate is outstanding | Implemented — [#86](https://github.com/tesserix/dwellm8/issues/86), [#87](https://github.com/tesserix/dwellm8/issues/87) |
| **Erasure as evidence destruction** | A party asks to be erased to remove the record of a dispute | Erasure defers on an open dispute, unsettled money, or an outstanding obligation | Implemented — ADR-0026 §3 |

The last one is worth dwelling on: **a privacy right is an attack surface.** A product that
implements erasure enthusiastically and without deferral hands every bad actor a
one-request evidence shredder.

---

## 5. What is not controlled yet

Named here so that "we have a threat model" cannot be mistaken for "we are covered".

1. **Rate limiting is per replica, not per fleet** — the effective limit is the configured
   number times the replica count. Deliberate ([#228](https://github.com/tesserix/dwellm8/issues/228)):
   a shared limiter in Redis is exact and is a dependency in the request path that fails at
   exactly the moment load is high. Stated rather than discovered.
2. ~~Platform actions are unlogged~~ — closed by [#226](https://github.com/tesserix/dwellm8/issues/226);
   `tenancy.Support()` writes the row in the same transaction as the work.
3. **No bank-detail change control** — [#227](https://github.com/tesserix/dwellm8/issues/227), the impersonated-owner path.
4. **No ownership verification** for listings — [#67](https://github.com/tesserix/dwellm8/issues/67) neighbours it.
5. **No secret rotation runbook.** Secrets are in GCP Secret Manager and reachable through
   ExternalSecrets; what to do when one leaks is in [`breach-runbook.md`](breach-runbook.md)
   §1, but there is no tested rotation for the Cashfree credentials specifically.
6. **Backups are unaddressed** for both retention and restore-integrity
   ([#25](https://github.com/tesserix/dwellm8/issues/25)).

---

## 6. When a story needs a security review

A story must pass a security review before Done when it touches any of:

- money movement, mandates or settlement;
- identity documents, KYC, or consent;
- row-level security policies, roles or grants;
- a provider adapter, a webhook, or any endpoint reachable without a tenant;
- personal-data retention or erasure.

That is a board convention rather than a technical gate, and it is honest to say so: the
project board has a Security Review status, and nothing in CI enforces that a story passed
through it. What CI *does* enforce is narrower and harder to argue with — the isolation
contract, the planted defects, `govulncheck`, and the Trivy scan before an image is
published.

**A review is not a reading.** It asks for a planted defect: show the check failing with
the control removed. Every guard in `.github/workflows/api.yml` earns its place that way,
and a control nobody has seen fail is a control nobody has seen.
