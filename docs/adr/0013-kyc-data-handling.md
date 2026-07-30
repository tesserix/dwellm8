# ADR-0013 — KYC data handling standard

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Security, Platform
- **Issue**: [#17](https://github.com/tesserix/dwellm8/issues/17)
- **Related**: [ADR-0003](0003-tenancy-and-row-level-security.md) (the isolation this tightens), [ADR-0005](0005-owner-delegation-grants.md) (the permission this deliberately does not add), [ADR-0006](0006-chart-of-accounts-and-posting-rules.md) (the party a verification is about)

---

## Context

KYC identifiers are the highest-liability data in the platform, and the standard has to
exist before any verification feature does — because the default implementation of every
vendor integration stores far too much. The SDK returns the full identifier and the
obvious thing to do is put it in a column named after it.

The specific liability is Aadhaar. The Act and the UIDAI regulations bar storage by
anyone who is not an authorised agency, and a table of twelve-digit numbers is a
honeypot whose value to an attacker does not depend on anything else in the product being
interesting.

**The org has already built the encryption half of this once.** HomeChef has envelope
encryption with a KMS key, and it is dormant — because the migration dual-wrote
ciphertext into a new column while reads still came from the plaintext one. That is the
lesson this ADR is built on, and it is not "we forgot to finish": encryption that leaves
a plaintext column in place *is* a plaintext column, and the plaintext will be read — by
a report, by a support query, by a backup.

---

## Decision

**Three tiers, with prohibited enforced by making a full identifier unstorable rather
than discouraged; no plaintext column for anything encrypted, so there is nothing to
read from; a positive column allowlist on the KYC table so a field cannot be added
without arguing for it; and every read logged with a purpose.**

### 1. Three tiers, each with a reason

| Tier | At rest | Kinds |
|---|---|---|
| `prohibited` | never, in any form | Aadhaar |
| `encrypted` | ciphertext only, **no plaintext column** | PAN, bank account, passport, driving licence, voter ID |
| `open` | as it is | IFSC, GSTIN, UPI VPA |

`pii.Why(kind)` carries the reason and a test refuses one short enough to be a label,
because a classification list with no reasons is a list somebody reclassifies. PAN and
bank account are `encrypted` rather than `prohibited` for a concrete reason: a TDS return
and a payout need the real value, so no mask will do. IFSC and GSTIN are `open` because
they identify an institution or are a public register — an IFSC is published by the RBI in
full.

### 2. A full identifier is unstorable, not discouraged

The story's failure scenario is "a developer adds a field that would persist a full
Aadhaar number → CI fails". Three mechanisms, and they catch different developers.

**The column will not take one.** `kyc_verifications.masked_reference` is the only column
that holds anything derived from an identifier, and it has a per-kind CHECK:

```sql
CASE kind
    WHEN 'aadhaar' THEN masked_reference ~ '^X+[0-9A-Z]{4}$'
    …
```

Per kind, because the mask of a PAN is a different shape from the mask of an Aadhaar and
a single loose pattern would accept both a mask and the thing it was made from. Measured
— a full number, a partial that keeps too much, and the number with dashes are all
refused:

```
ERROR:  new row for relation "kyc_verifications" violates check constraint
        "kyc_verifications_reference_is_a_mask"
```

That holds for application code, a migration, and a psql prompt equally.

**Any other column fails the bootstrap.** Assertion 15's second clause is a *positive
allowlist*: `kyc_verifications` may hold exactly twelve named columns, and any thirteenth
fails. This is the strong half, because it does not depend on guessing what somebody will
call the field:

```
ERROR:  kyc_verifications has column(s) ADR-0013 does not list: full_number
```

**And a name scan, in the schema and in Go.** Assertion 15's first clause refuses a
column whose name contains `aadhaar`, `aadhar`, `adhaar`, `adhar` or `uidai`;
`TestNothingIsNamedAfterAProhibitedIdentifier` refuses a struct field, a JSON tag or a
variable. Weak on its own — somebody determined calls it `NationalID` — and it covers the
careless case, which is the common one: the SDK returns `aadhaarNumber` and the obvious
name follows it.

The name list lives in `internal/platform/pii` and both checks read it, so there is one
copy.

**It matches on tokens, not substrings**, and that correction is worth recording. The
first version matched substrings and flagged every `Provider` in the money module,
because "provider" contains `vid` — Aadhaar's Virtual ID. A three-letter token cannot be
looked for inside arbitrary identifiers, and a guard with false positives is a guard
somebody deletes. So identifiers are split on camelCase and non-letters, tokens are
matched exactly, and a prohibited name of six characters or more is additionally matched
inside a token so `aadhaarnumber` written without a separator is still caught.

The guard covers test files, deliberately. A fixture that is a real identifier is exactly
the risk, and a test file is committed and grepped like any other — it caught two names
in this ADR's own tests.

### 3. Encrypted means there is no plaintext column

The lesson from HomeChef, stated as a rule: **an encrypted field has no plaintext column
beside it.** Not "the plaintext column is not read" — not read today, read next quarter
by a report somebody writes without knowing.

So there is no `pan` column in this schema. When payout credentials land they hold
ciphertext only, and what `kyc_verifications` keeps is the *masked* reference, so a screen
can show which document was checked without anything being decryptable from this table at
all.

The envelope-encryption mechanism itself is out of scope here — it is the org's
`piicrypto`, and the provider integration story wires it. What this ADR fixes is the
shape: ciphertext with no plaintext beside it, and a `pii.Secret` in Go so the value in
flight cannot be printed.

### 4. `pii.Secret`, and the accidents it stops

Every PII leak has the same shape: somebody formatted a struct. So `Secret` implements
`String`, `GoString`, `Format` and `MarshalJSON`, all returning `[redacted]`, and the test
asserts every path — `%v`, `%s`, `%q`, `%d`, `%x`, `%#v`, `%+v`, `fmt.Sprint`, a wrapped
`Errorf`, `json.Marshal` directly and nested in a struct, and a `slog` JSON handler.

Four details:

- **The field is unexported**, so a `Secret` cannot be built by a struct literal and
  reflection-based marshallers that ignore `MarshalJSON` cannot reach it.
- **`Format` is implemented**, not just `String`, because `%q` and `%x` do not go through
  `Stringer`.
- **`Reveal()` is the only way out**, and it is named so every use is grep-able. The point
  is not that it is hard, it is that it is *visible*.
- **`Redaction` is a fixed string, not empty.** An empty value in a log is
  indistinguishable from a field nobody set, and the difference matters when you are
  trying to establish whether a leak happened.

And `pii.Validate` takes a `Secret` and **never quotes the value in its error**, which is
the mistake every validation function makes: `fmt.Errorf("invalid PAN %q", pan)` puts the
identifier straight into the log the error is written to.

### 5. Reading is logged, and support access is time-bound

Auditing a read cannot be done by the reader — a `SELECT` leaves no trace — so
`kyc_access_log` is written by the service performing the read, and what makes it more
than a convention is that a row without a purpose is refused: `reason` has a minimum
length, because an auditor reading `x` learns nothing.

`reason` is free text, and it is the one place in this schema that is deliberate: a closed
vocabulary of reasons becomes `other` within a month.

`actor_kind` distinguishes the subject reading their own record from a support engineer
reading somebody else's, which is the distinction an audit is for. And
`kyc_access_log_support_needs_a_grant` refuses a support read with no grant behind it —
which is what makes access time-bound, because the window is on the grant and is checked
at read time.

The log cannot be updated or deleted. A log somebody can edit is not a log.

### 6. No delegated branch, and that is the decision

`kyc_verifications`'s policy is strict tenancy with **no `is_delegated` branch at all**,
and ADR-0005's permission vocabulary has no `kyc.read`.

A management firm holds a grant to collect rent and manage a property. Nothing about that
requires reading a tenant's passport number. The isolation test asserts it from both
sides — organisation B sees nothing while declaring every permission it could hold — and
also asserts the policy *text* contains no `is_delegated`, because the second is the
structural version: there is nothing to widen by accident.

If a firm ever needs this, it is a new permission argued for in an ADR, not a widened
policy.

### 7. What fails the build

- `internal/platform/pii` — every formatting path redacting, the zero `Secret` being
  empty rather than half-initialised, every kind classified with a reason, a verification
  record holding no full identifier, a full identifier refused as a reference in three
  shapes, a validation error never quoting the value, every mask satisfying its own
  pattern, masking colliding on purpose, and a verification being incomplete without the
  things that make it checkable later.
- `internal/platform/arch` — no struct field, JSON tag or variable named after a
  prohibited identifier, anywhere in `internal/`, tests included.
- `internal/identity/store` — the mask patterns against the schema's CHECK per kind
  including that a kind absent from the `CASE` would make the constraint evaluate to NULL
  and therefore pass, PostgreSQL's regex engine against Go's over seven values, the column
  allowlist, and the result and kind vocabularies.
- `internal/platform/tenancy/isolationtest` — ADR-0003's five-part contract, a full
  identifier refused by five paths, a delegated firm reading nothing under any permission
  and the policy having no delegated branch, and a support read without a grant refused.

CI plants five failures and expects red.

### 8. The trap, once more, on this ADR's own constraint

`kyc_verifications_reference_is_a_mask` was written inline in the `CREATE TABLE`, and this
ADR's own verification dropped it to check the assertion — after which the replay could
not put it back, because `CREATE TABLE IF NOT EXISTS` never revisits an existing table.
Assertion 13 correctly reported a schema missing a rule it is built on.

So it moved into the load-bearing rules block with the other five. The trap bit a
constraint added in the same session as the guard written for it, which is the best
argument available that the guard was worth writing.

It also produced the first live demonstration of the warn-don't-abort path: the rows
written while the CHECK was absent — including a full twelve-digit number — cannot be
constrained over, so the block warns and assertion 13 fails, rather than the bootstrap
dying on an opaque DDL error:

```
WARNING:  5 row(s) in kyc_verifications violate kyc_verifications_reference_is_a_mask,
          so it is not being added. Those rows were written while the rule was absent
          and need a decision, not a migration; assertion 13 will fail until they are
          dealt with.
```

---

## Alternatives considered

### A. Store the Aadhaar number encrypted rather than not at all — rejected

It is the obvious middle path, and it is what most products do.

Rejected on two grounds. Legally, the bar is on *storage* by an unauthorised entity, and
ciphertext in our database is storage — the key being elsewhere does not change who holds
the data. Practically, an encrypted honeypot is still a honeypot: the value to an attacker
is the set of numbers, and every mechanism that decrypts for a legitimate reason is a
mechanism that decrypts.

### B. Hash the identifier so it can be matched without being readable — rejected

Tempting, because deduplication and "have we seen this person" are real needs.

Rejected because a hash of an Aadhaar number is not anonymous. The space is
10^11 — sequentially enumerable on a laptop — so a hash column is a lookup table for
anybody who takes a copy, and it is a *worse* lookup table than plaintext because it looks
safe. Salting per row breaks the matching the hash was for; salting globally means one
leaked salt undoes every row.

What replaces it: the provider's own transaction id for the audit trail, and the masked
reference for a human to recognise their own document.

### C. A denylist of column names as the only mechanism — rejected

The cheapest thing that satisfies the letter of the acceptance criterion.

Rejected because it only stops the careless case. `national_id`, `uid_number`, `govt_ref`
all pass a denylist, and the developer who writes one of those is exactly the developer
who has the full value in hand. The denylist is kept — it is clause (a) of assertion 15 —
and it is the weakest of the three, which is why the other two exist.

### D. A closed vocabulary for the access-log reason — rejected

Consistent with every other vocabulary in this schema, and it would let reasons be
reported on.

Rejected because the list becomes `other` within a month, and `other` is what every
interesting access will be logged as. An auditor reading free text learns what happened;
an auditor reading `other` learns that somebody had a dropdown. The minimum length is the
compromise: present and non-trivial, without pretending to be enumerable.

### E. `pii.Secret` as a type alias for `string` — rejected

Simpler, and it would still document intent.

Rejected because an alias formats as itself. The whole value is that `%v` prints
`[redacted]`, and a type with methods is the only way to get that. The unexported field is
the second half: a distinct named string type could still be built by a literal and
printed by `%s` if somebody removed the method.

### F. Redacting in the log handler rather than in the type — rejected

A `slog` handler that drops attributes named like PII, which is what several products do.

Rejected because it protects one output path. The same value reaches an error message, an
HTTP response body, a test failure, a panic trace and a metric label, and a handler sees
none of those. Redacting at the type covers all of them, and the `slog` test is there to
show the handler path is covered *as well*, not instead.

---

## Consequences

**What is now true.** There is no column anywhere for a full Aadhaar number, and the one
column that holds anything derived from an identifier will not accept one — from any code
path, migration or prompt. A thirteenth column on the KYC table fails the bootstrap. A
struct field, JSON tag or variable named after a prohibited identifier fails the build,
tests included. A value in flight cannot be printed, marshalled, logged or interpolated
without `Reveal()` appearing in the diff. A validation error never quotes what it refused.
Every read of a KYC record carries a purpose, a support read carries a grant, and the log
cannot be edited. And a management firm cannot reach an identity record under any grant it
can hold.

**What this costs.** The name scan has to be maintained: a new spelling, or a new
prohibited kind, means editing `ProhibitedColumnNames` — and its weakness against a
determined name is real, mitigated only by the allowlist. The allowlist means adding a
legitimate field to `kyc_verifications` requires editing the assertion, which is friction
by design and will be felt. `Secret` makes the value awkward to work with, which is the
point and is still awkward. And the masked reference collides deliberately — two numbers
sharing their last four mask identically — so it cannot be used to deduplicate a person,
which somebody will eventually want it to do.

**What is not decided.** Provider integration, which is #66 and is where `piicrypto` gets
wired and where the Aadhaar checksum belongs. The document-upload rule the story names as
an edge case: a tenant photographing an Aadhaar card is a file whose *contents* this ADR
does not reach, and it needs a retention and redaction decision of its own — the document
module's, and currently unwritten. Payout credential storage, which is where the
ciphertext-only rule gets exercised. The retention matrix and the DPDP consent artefact
schema, which are #20. And the police-verification workflow, explicitly out of scope.
