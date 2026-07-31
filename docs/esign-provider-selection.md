# eSign — provider selection, evidence and boundaries

Spike output for [#16](https://github.com/tesserix/dwellm8/issues/16). Feeds
[#62](https://github.com/tesserix/dwellm8/issues/62) (Aadhaar eSign flow with
audit trail) and [#63](https://github.com/tesserix/dwellm8/issues/63)
(non-Aadhaar fallback).

**Reviewed**: 2026-07-31 · **Owner**: Compliance · **Next review**: 2027-01-31

Unlike [`payment-rails.md`](payment-rails.md) and
[`e-stamping-by-state.md`](e-stamping-by-state.md), most of this document is read
from the **primary source** — the CCA's eSign API Specification v3.3 — rather than
from commentary. Where a statement comes from vendor marketing it says so.

---

## 1. The headline: three assumptions in the issue that the spec contradicts

**1. The document does not stay inside Dwellm8 the way "we only send a hash"
suggests.** True: the ASP computes a SHA-256 of the document and sends only that
hex hash; the ESP signs the hash and never receives the file. But `docUrl` is a
**mandatory** attribute on every `InputHash`, must be an HTTP/HTTPS URL "accessible
by the signer during the transaction permitted duration", and the ESP displays it
as a hyperlink so the signer can read what they are signing. So the lease PDF must
be **fetchable from the public internet for the duration of the signing window**.
That is the real boundary, and it is a security design problem — an unguessable,
signed, short-lived URL scoped to one transaction, expiring with `maxWaitPeriod`,
never a stable object path. A lease agreement is a document full of personal data;
a leaked `docUrl` is a data breach, not a broken link.

**2. "Select an ESP" is the wrong question, because the spec makes multi-ESP
mandatory.** §3.1 of the specification: an ASP providing eSign facility to the
public *should integrate with all other ESPs within one month after on-boarding
with the first ESP*. The API is deliberately uniform — only the service URL and
the ASP ID differ per ESP. So the decision is not *which of the seven* but **which
access route**, and an aggregator's real value is that it already absorbs that
obligation. §3.

**3. The non-Aadhaar fallback need not be evidentially weak.** The issue and
[`india-compliance.md`](india-compliance.md) §2 both assume the fallback is an
"OTP/email-verified electronic signature" with lower weight. eSign 3.x supports
eKYC accounts verified from **Aadhaar Offline XML, Bank eKYC, PAN KYC,
Organisational KYC and a foreign-national route** — every one of which ends in a
CA-issued Digital Signature Certificate under the Second Schedule of the IT Act,
carrying the *same* legal standing as the Aadhaar path. So there are **three**
tiers, not two, and the middle one is nearly free to add. §6.

---

## 2. Who is who

| Role | Who | What they do |
|---|---|---|
| **CCA** | Controller of Certifying Authorities | Licenses CAs, empanels ESPs, publishes the API spec |
| **CA** | Licensed certifying authority | Issues the Digital Signature Certificate |
| **ESP** | eSign Service Provider — a "Trusted Third Party" under the IT Act Second Schedule | Authenticates the signer via eKYC, generates the key pair in an HSM, obtains the DSC, signs the hash |
| **ASP** | Application Service Provider | **This is Dwellm8** (or the aggregator acting for us). Builds the document, computes the hash, calls the ESP, attaches the signature |
| **eKYC provider** | UIDAI, a bank, or the CA itself | Verifies the signer's identity |

The seven ESPs currently empanelled by CCA: **eMudhra, C-DAC, Capricorn Identity
Services, Protean eGov Technologies, Verasys, CSC e-Governance Services, and CDSL
Ventures**. Only licensed CAs may operate as ESPs.

**The certificate is single-use.** The spec is explicit that a certificate issued
through eSign service has a limited validity period and is *only for one-time
signing of the requested data*. There is no key material to store, rotate or
protect on our side — which removes an entire class of risk, and is the strongest
argument for eSign over issuing signers a real DSC token.

---

## 3. Access route: direct ESP versus an ASP aggregator

| | Direct to ESPs | Through an aggregator (Leegality, Digio, SignDesk) |
|---|---|---|
| The multi-ESP obligation | Ours — seven integrations, seven contracts | Theirs |
| ESP outage | We build the failover | They route |
| Stamp + sign in one workflow | We compose it | One workflow object covering both |
| Per-signature cost | Lower unit cost, higher fixed cost | ₹3–₹25 per signature, vendor-quoted |
| Contractual counterparty | A CA, per ESP | One vendor |
| What we lose | — | A layer of visibility into the ESP transaction, and a dependency with our evidence in it |

**Recommendation: an aggregator for MVP, and the same one that does our
e-stamping.** The deciding argument is not price, it is that stamping and signing
are **one ordered operation on one artefact**, not two integrations (§5.3). The
e-stamping spike reached the same vendor shortlist from the other direction, and
a vendor that can produce a stamped-then-signed PDF as a single workflow removes
the ordering bug before it can be written.

Two conditions on that recommendation, because this vendor ends up holding the
chain of custody for every lease we produce:

1. **Evidence portability is a contract term, not a feature request.** We must be
   able to export the raw `EsignResp` XML, the ESP signature over it, the DSC and
   the full audit trail in a form that outlives the vendor relationship.
2. **The adapter is ours.** Same posture as
   [ADR-0011](adr/0011-payment-provider-adapter.md): a `SignatureAdapter`
   interface with the aggregator as the first implementation, so a move to direct
   ESP integration is a new adapter rather than a rewrite of the lease flow.

---

## 4. Exactly what crosses the boundary

This is the issue's central acceptance criterion, answered from the spec.

| Data | Dwellm8 → ESP | ESP → Dwellm8 | Dwellm8 stores |
|---|---|---|---|
| The lease PDF | **No** — never sent | — | Yes, in GCS |
| SHA-256 of the PDF (hex) | **Yes** | — | **Yes — mandatory, see §5.2** |
| `docInfo` — ≤50 chars describing the document | Yes | — | Yes |
| `docUrl` — public HTTPS URL the signer opens | **Yes** | — | The URL is ephemeral; the policy that issued it is stored |
| `txn`, `aspId`, `responseUrl`, `redirectUrl`, `maxWaitPeriod` | Yes | `txn` echoed | Yes |
| **Aadhaar number** | **Never.** The ASP does not send it; the signer enters it (or authenticates) on the ESP's page | **Never** | **Never** |
| OTP / PIN | Never — captured on the ESP's page | Never | Never |
| eKYC identity data — name, address, DOB, photo | Never | **No.** The spec states plainly that the API does not return any identity-related data of the eSign user | — |
| `UserX509Certificate` | — | Yes (public cert only, no private key) | **Yes** — it is the identity evidence |
| `DocSignature` — PKCS#7 | — | Yes | **Yes** |
| `resCode` — ESP's unique transaction code | — | Yes | **Yes — the spec expects the ASP to store it in the audit log** |
| ESP's XML signature over the response | — | Yes | **Yes, verified and retained raw** |

**The Aadhaar guarantee holds, and it holds structurally rather than by
discipline.** There is no field in the eSign request that carries an Aadhaar
number, and the response returns no identity data at all. Authentication happens
on the ESP's own page, between the signer and the ESP. Dwellm8 cannot store an
Aadhaar number through this flow even by accident — the only way to break the
guarantee is to build a *separate* screen that asks for one, which is exactly what
the guardrail in the issue template forbids.

**We also do not need an Aadhaar authentication licence.** The ESP is the KUA and
performs the eKYC; Dwellm8 never calls UIDAI. The Aadhaar Authentication for Good
Governance (Amendment) Rules 2025, which opened direct Aadhaar authentication to
private entities on ministry approval, are therefore not on our path for eSign —
they would only matter if we wanted to authenticate Aadhaar ourselves, which §4
says we must not.

The one nuance worth stating: **the DSC itself is identity evidence.** Its subject
carries the signer's verified name, and under the CA's certificate policy it may
carry a hashed or partial identity reference. That is not an Aadhaar number and it
is the whole point of the artefact — but it means the certificate is personal data
under DPDP, retained for its statutory evidentiary period rather than deleted on
request ([`india-property-compliance.md`](india-property-compliance.md) §9.5).

---

## 5. The evidence, and what must survive a decade

### 5.1 Ask for `PKCS7pdf`, not `raw` or `PKCS7`

`responseSigType` offers three values and the choice is permanent, because it is
baked into the artefact:

- `raw` — a bare PKCS#1 signature. Verifiable only if you still hold everything else.
- `PKCS7` — signer certificate only, **no revocation information**.
- `PKCS7pdf` — all issuer certificates up to and including the root CA, plus
  CRLs or OCSP responses for each, embedded as a signed attribute
  (`pdfRevocationInfoArchival`).

Only the third produces a **long-term-validation** signature: one that can still be
verified after the signing certificate has expired — which it will have, within
minutes, because it is single-use and short-lived. A lease disputed in year six is
verified against evidence captured in year one or not at all. **Use `PKCS7pdf`.**

### 5.2 Store the hash, because the law now asks for it by name

Section 63 of the **Bharatiya Sakshya Adhiniyam 2023** replaced s.65B of the
Evidence Act with effect from **1 July 2024**. The accompanying certificate must
now disclose the **hash value** of the electronic record, and must be signed both
by the person in charge of the device and by an expert.

So the SHA-256 we computed and sent is not an implementation detail to be
recomputed on demand — it is a **named element of the admissibility certificate**,
and it must be stored on the lease at signing time alongside the algorithm and the
exact bytes it was computed over. Recomputing it years later from a re-rendered
PDF proves nothing, because the re-render is a different file.

### 5.3 The order is stamp, then sign — and it is not negotiable

Embedding the e-stamp certificate changes the bytes of the PDF, which changes its
hash, which invalidates any signature already applied. Therefore:

```
build document → embed e-stamp certificate → compute hash → sign → attach signature
```

A flow that signs first and stamps afterwards produces a document with a broken
signature and no way to fix it without re-signing. This is the strongest practical
reason to hold stamping and signing in one durable workflow with one vendor, and
it is why [`e-stamping-by-state.md`](e-stamping-by-state.md) §5 puts stamping in a
`stamp_pending` state before signature rather than after.

### 5.4 Our audit trail, beside theirs

The ESP's evidence proves *a certified signature was applied to this hash*. It does
not prove what we showed the signer or what they agreed to. Our own record, per
lease and per signer, carries: signer identity as recorded on the lease, `txn` and
`resCode`, request and response timestamps, source IP and user agent, the consent
artefact and the version of the notice displayed, the `docInfo` and `docUrl`
policy, the document hash, and the outcome including rejections and timeouts.
Both halves are the evidence; either alone is thin.

---

## 6. The three signature tiers

| Tier | Mechanism | Legal basis | Use |
|---|---|---|---|
| **1. Aadhaar eSign** | ESP authenticates via UIDAI eKYC, OTP to the registered mobile | IT Act Second Schedule — a DSC | Default for every signer who has an Aadhaar-linked mobile |
| **2. Non-Aadhaar eSign** | ESP eKYC account verified from Bank eKYC, PAN KYC, Offline Aadhaar XML, organisational KYC, or the foreign-national route; signed with username + PIN + second factor (SMS-OTP / T-OTP) | **The same** — IT Act Second Schedule, a DSC | Signers who decline Aadhaar, NRI owners, company signatories |
| **3. Electronic signature** | Click-to-sign with OTP-verified mobile and email, plus our own audit trail | IT Act s.10A — a valid contract, but not a Second Schedule signature and no presumption attaches | Last resort, recorded on the lease as such |

**Tier 2 is the finding.** The issue's fallback scenario — "an alternative
electronic signature completes the lease and the lower evidentiary weight is
recorded" — should be satisfied by Tier 2 in the overwhelming majority of cases,
at no loss of evidentiary weight. Tier 3 exists, but it is the third choice rather
than the second, and #63 should be re-scoped accordingly.

Tier 2 costs something the others do not: the signer creates an **eSign user
account** (username, PIN, second factor) with the ESP before first use. That is
real friction at first signing and none at all afterwards — which suits an owner
with twelve units and suits a one-off tenant badly. Route by signer type.

**NRI owners belong in Tier 2 by default**, not Tier 1. An overseas mobile number
does not receive UIDAI's OTP reliably, and this is the same population that
[`india-compliance.md`](india-compliance.md) §5 already flags as the highest-risk
TDS path. Discovering the signature problem at signing time, after the lease is
agreed, is avoidable.

---

## 7. Multi-signer flows

The spec signs **hashes, not people**: up to **5** `InputHash` elements per
transaction, ids sequential from 1, and one authenticated signer per transaction.
So a lease with an owner, two tenants and a guarantor is **four eSign
transactions**, not one.

Two consequences that decide the design:

1. **Signatures are applied incrementally, so each signer signs a different
   hash.** Once signer A's signature is attached, the PDF's bytes have changed;
   signer B signs the updated document. The hash is therefore computed **per
   signer, immediately before that signer's transaction**, never once for the
   whole lease. A flow that computes one hash up front and reuses it produces
   signatures that verify against nothing.
2. **Ordering is therefore explicit and sequential by construction.** True
   parallel signing would require merging concurrent incremental updates to one
   PDF, which is a bug waiting to happen. The lease carries a signer sequence;
   each signer is invited when the previous one completes, and the UI tells
   everyone where in the queue they are rather than implying they are blocked.

`docInfo` is capped at 50 characters and the spec requires it to adhere strictly
to the document's content — it is displayed to the signer as the thing they are
about to sign. `"Lease · Flat 402 Brigade · 2026-08-01"` is a compliant and honest
50 characters; `"Document"` is neither.

The spec also requires the ESP to let a signer **uncheck** any document hash,
returning `User Rejected` for it. With one document per transaction that is a
per-signer rejection, and the lease must model it as a first-class outcome — a
tenant declining to sign is a business event, not an error.

---

## 8. The edge cases in the issue

**Abandoned session.** `maxWaitPeriod` is mandatory, in minutes, defaulting to
1440 — after which the ESP marks the transaction `User timeout`. The ESP calls our
`responseUrl` on success, failure *or* cancellation, and separately must expose
`checkStatus` for the same `txn` for **at least 30 days**. So the resumable state
the issue asks for is directly supported: the signing workflow is durable
([ADR-0015](adr/0015-durable-workflow-standard.md)), the lease sits in
`awaiting_signature` with the transaction recorded, the callback is advisory, and
the workflow reconciles against `checkStatus` rather than trusting a webhook it
may never receive. Same posture as payments — the callback is a hint, the status
query is the truth.

Set `maxWaitPeriod` deliberately. The 1440-minute default means a tenant who walks
away at 9pm blocks the lease until 9pm tomorrow; a short window means a signer who
steps into a meeting has to start again. A few hours, with an explicit re-invite,
beats both.

**Signer without a smartphone.** Aadhaar eSign needs a mobile that receives SMS,
not a smartphone — the ESP flow runs in any browser, and the OTP arrives by SMS.
The genuinely excluded signer is the one with no mobile at all, or no mobile linked
to Aadhaar, and for them Tier 2 with Bank eKYC or Tier 3 is the answer. The
feature-phone case also argues for the redirect web flow over any ESP mobile app.

**Idempotency, and one sharp edge.** `txn` must be unique for a given ASP-ESP
combination **for that calendar day**. A retry must reuse the same `txn` to be
idempotent — but the uniqueness rule is scoped to a day, so a retry that crosses
midnight sits in an undefined corner of the spec. Keep the signing window well
inside a single day, or confirm the behaviour with the ESP before relying on it.
Deduplicate our side on `(txn, resCode)`, and verify the ESP's XML signature on
every response before acting on it — the response is signed precisely so that a
forged callback cannot mark a lease signed.

---

## 9. Recommendation

1. **Go through an aggregator for MVP 2, the same vendor as e-stamping**, behind
   our own `SignatureAdapter`. Evidence portability and raw-artefact export are
   contract terms.
2. **Request `PKCS7pdf`** on every signature, always.
3. **Stamp before sign**, in one durable workflow, with `stamp_pending` and
   `awaiting_signature` as real lease states.
4. **Store the SHA-256, the DSC, the raw signed `EsignResp`, `resCode` and our own
   audit trail** on the lease — the BSA s.63 certificate will ask for the hash by
   name.
5. **Ship Tier 1 and Tier 2 together.** Tier 2 is the honest fallback; Tier 3 is a
   documented last resort with its weaker standing recorded on the lease.
6. **Hash per signer, sign sequentially**, and model rejection and timeout as
   outcomes rather than errors.

---

## 10. Open questions, to close with the vendor rather than from the spec

Questions 1–4, 6 and 7 are vendor-facing and are tracked as
[#209](https://github.com/tesserix/dwellm8/issues/209). The `docUrl` exposure this
spike uncovered is tracked as [#212](https://github.com/tesserix/dwellm8/issues/212).

1. **Which ESPs does the aggregator actually route to, and what happens on ESP
   outage** — silent failover, or a failed transaction we must retry?
2. **Does the aggregator expose the raw `EsignResp` and the ESP's XML signature**,
   or only its own normalised summary? If the latter, our evidence has a vendor in
   the middle of it and condition 1 of §3 is unmet.
3. **Who hosts `docUrl`** — us or them? If them, the lease PDF is sitting on a
   vendor's public URL and the access policy is theirs, not ours.
4. **Tier 2 enrolment UX**: can the eSign-account creation be embedded in our flow,
   and how long does bank or PAN eKYC actually take at first signing?
5. **The midnight `txn` uniqueness question** in §8.
6. **Per-signature pricing at our volume**, and whether Tier 2 is priced
   differently from Tier 1.
7. **Long-term verification**: does the vendor offer verification of an old
   artefact years later, and does that survive us leaving them?

---

## Sources

- [CCA — eSign API Specification v3.3, 9 December 2020](https://cca.gov.in/sites/files/pdf/esign/eSign-APIv3.3.pdf) — primary source for §1, §2, §4, §5.1, §7, §8
- [CCA — eSign Online Electronic Signature Service](https://cca.gov.in/eSign.html)
- [CCA — external empanelled eSign Service Providers](https://cca.gov.in/service-providers.html)
- [CCA — eSign API Specification v2.1, February 2023](https://cca.gov.in/sites/files/pdf/ACT/eSign-APIv2.1.pdf)
- [Section 63, Bharatiya Sakshya Adhiniyam 2023 — admissibility of electronic records](https://indiankanoon.org/doc/125020475/)
- [BSA s.63 certificate requirements and the hash-value disclosure](https://ksandk.com/litigation/section-63-bharatiya-sakshya-adhiniyam-2023/)
- [Aadhaar Authentication for Good Governance (Amendment) Rules 2025 — private-entity access](https://www.khaitanco.com/thought-leadership/Aadhaar-authentication-for-private-entities)
- [Leegality — eSign product and pricing](https://www.leegality.com/esign)
