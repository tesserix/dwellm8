# Security baseline

What every part of Dwellm8 must meet. Each control says whether it is **enforced** — by a
test, a constraint or a CI step that fails the build — or merely **expected**, which means
somebody has to remember it.

The distinction is the whole point of the page. An expected control is a control that has
already been forgotten once somewhere.

Threats are in [`threat-model.md`](threat-model.md); this is the answer to them.

---

## 1. Tenancy and authorisation

| Control | Enforced by |
|---|---|
| Every tenant-scoped table carries `tenant_id` and **FORCES** row-level security | Schema assertion 1 — the bootstrap refuses to complete otherwise |
| No application role holds `BYPASSRLS` | Schema assertion 2 |
| Every policy states `WITH CHECK` explicitly | Schema assertion 3 |
| A property-scoped table's policy uses `is_delegated_unit`, not a bare tenant check | Schema assertion 5 |
| No table's rows may belong to no organisation while being runtime-writable | Schema assertion 12 |
| Cross-organisation access traverses an explicit, revocable, effective-dated grant | ADR-0005; delegation tables are no-delete |
| Every new data path has the five-part isolation contract over it | `internal/platform/tenancy/isolationtest` |

**Three PostgreSQL roles, not one.** `dwellm8_api` is the request path and is subject to
RLS; `dwellm8_platform` is the audited support path; `dwellm8_purge` may delete and only
inside a sandbox organisation. Connecting as the owner would make every policy decorative —
which is exactly what `ENABLE` without `FORCE` did, and why assertion 1 exists.

---

## 2. Money

| Control | Enforced by |
|---|---|
| `DELETE` and `UPDATE` are revoked on the ledger; corrections are reversing entries | ADR-0006 §3, and a planted defect in CI |
| Amounts are `int64` minor units — no float reaches money code | `internal/platform/arch/money_float_test.go`, with a float planted in CI to prove it fires |
| One rounding primitive, used once per posting | ADR-0007 |
| Provider calls go through an adapter, are idempotent, and have defined retry and dead-letter behaviour | ADR-0011 |
| A webhook is deduplicated on provider event id and is replay-safe | Unique index on `(provider, provider_event_id)` |
| An unverified webhook's contents are never used to look up a payment | `IngestWebhook` |
| **A webhook alone never releases money** | Today by absence — see [`threat-model.md`](threat-model.md) §2.1 before building payouts |

---

## 3. Personal data

| Control | Enforced by |
|---|---|
| No Aadhaar number is stored, logged or sent anywhere | `internal/platform/pii` fails the build on a field or JSON tag named after one, **including in test files**, plus a schema assertion |
| KYC holds a result, a masked reference and a provider transaction — nothing else | Column allowlist assertion on `kyc_verifications` |
| No delegated grant can reach KYC | The policy has no delegated branch, and it is asserted |
| Every KYC read is logged | `kyc_access_log`, no-delete |
| A verification cites a consent artefact that exists | Foreign key, ADR-0026 (`NOT VALID` for legacy rows) |
| Retention periods match the reviewed document | Contract test parsing `data-retention.md`, with a bent document planted in CI |
| PAN, bank and payout details are encrypted or in GCP Secret Manager | Expected — the `piicrypto` path is not wired in this product |

---

## 4. Supply chain and build

| Control | Enforced by |
|---|---|
| Reachable vulnerabilities fail the build | `govulncheck` in the `api` workflow — it found a pgx SQL injection and an `x/text` loop on its first run |
| Container image scanned **before** it is published | Trivy on the loaded image, HIGH and CRITICAL fatal, then push |
| Base images come from the org repo and are pinned by digest | `services/api/Dockerfile` |
| A dependency cannot be adopted within 7 days of publication | Dependabot `cooldown` — the compromised-release window is measured in hours |
| Expo SDK majors are not auto-merged | Dependabot `ignore` |
| No SQL and no migration tooling in this repository | `repo` workflow, no path filter |

---

## 5. Secrets

- Secrets live in **GCP Secret Manager**, reach the cluster through ExternalSecrets, and
  appear in a pod as environment variables. None is in git, and none is in a values file.
- The API **reads the credential to decide whether it is a sandbox one** rather than
  trusting a boolean in the values file — a flag can only acknowledge the situation, never
  disguise it (`config.Cashfree.IsSandbox`).
- Rotation is `gcloud secrets versions add` followed by a restart of the consumers.
  **Rotation without a restart leaves the old value live in memory**, which is the step
  people miss under pressure.
- **Not enforced**: nothing scans for a committed secret. `mfw` and Semgrep secret scanning
  exist at the org level and are not wired into this repository's CI.

---

## 6. Audit

| Control | Status |
|---|---|
| `audit_events` is append-only and no-delete | Implemented |
| Every KYC read is logged with actor, reason and grant | Implemented |
| Delegation grants and their revocations survive | Implemented — no-delete policy |
| **Platform (support) actions are logged** | **Not implemented** — `tenancy.Platform()` takes a reason and discards it. [#226](https://github.com/tesserix/dwellm8/issues/226) |
| Audit retention | 8 years — [`data-retention.md`](data-retention.md); an erasure request does not remove it |

---

## 7. What is expected and not enforced

The honest list. Each of these is a sentence somebody has to remember rather than a build
that fails:

1. **Rate limiting** — nothing exists at any layer. [#228](https://github.com/tesserix/dwellm8/issues/228)
2. **Input validation at the edge** — handlers validate in the domain, which is the right
   place, but there is no request-size or payload-shape limit before the handler.
3. **Security headers and CSP** on the web surfaces — the Expo apps do not set them, and
   there is no web console yet to set them on.
4. **Secret scanning in CI.**
5. **Dependency licence policy** — nothing checks what is being pulled in.
6. **A named security owner.** The threat model has owners per threat; the programme has
   none, and that is a person to appoint rather than a control to build.
