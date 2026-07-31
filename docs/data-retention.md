# Data retention matrix

What Dwellm8 keeps, for how long, and under what. The authority for an erasure request:
when somebody asks to be erased, this is the table that decides which records go, which
are kept, and what they are told about the ones that stay.

`internal/platform/dpdp` holds the same matrix as code, and a contract test fails the
build if the two disagree. Read [ADR-0026](adr/0026-dpdp-posture.md) for why it is
structured this way and [`india-compliance.md`](india-compliance.md) §6 for the DPDP
obligations it serves.

---

## 1. The classes

Retention is decided per **class**, not per table. "The rent ledger" is one answer to a
data principal; eleven table names are not.

| Class | Years | Anchor | Statute or reason |
|---|---|---|---|
| `financial` | **8** | End of the financial year the entry falls in | Section 128(5), Companies Act 2013, read with Rule 6F, Income-tax Rules 1962 |
| `tax` | **8** | End of the financial year the deduction falls in | Section 149 and Rule 31A, Income-tax Act 1961 — the period in which an assessment may be reopened |
| `agreement` | **12** | End of the tenancy | Articles 65 and 66, Limitation Act 1963 — the period in which a suit concerning immovable property may be brought |
| `kyc` | **5** | End of the relationship | Section 12, PMLA 2002 — but see §4 below on whether Dwellm8 is a reporting entity at all |
| `audit` | **8** | The event | Not a statute. The security record; erasing it on request is how an audit trail gets erased |
| `contact` | **0** | — | Nothing requires it. Held on consent and erased on request |
| `support` | **0** | — | Nothing requires it |

**The years are ceilings on erasure, not targets.** A record whose period has run is
erased on request; it is not automatically deleted the day it expires, because a
scheduled deletion job that runs unattended over financial records is a worse risk than
holding them a while longer.

---

## 2. Which tables are in which class

| Table | Class | What is personal about it |
|---|---|---|
| `journal_entries`, `ledger_postings` | `financial` | `party_id` on a posting; `created_by` |
| `payments`, `payment_events` | `financial` | Payer references from the provider |
| `settlement_batches`, `settlement_lines`, `settlement_drift`, `reconciliation_runs` | `financial` | Owner party in a payout |
| `mandates` | `financial` | The payer's standing authority |
| `tds_obligations`, `tds_obligation_steps` | `tax` | The deduction against a person's rent |
| `tds_certificates` | `tax` | `party_id` — a landlord's own certificate |
| `lease_tax_facts` | `tax` | `acknowledged_by` — who accepted the §195 obligation |
| `leases`, `lease_parties`, `rent_schedule` | `agreement` | Who was on the tenancy and what they paid |
| `property_ownership` | `agreement` | `owner_party_id` |
| `kyc_verifications` | `kyc` | Masked reference, result, provider transaction — never the document |
| `kyc_access_log` | `kyc` | Who looked at a verification |
| `consent_artefacts` | `kyc` | Retained as long as what it authorised, which is the point of it |
| `prospects`, `enquiries`, `prospect_shortlist`, `contact_bridges` | `contact` | Name, masked contact, what they enquired about |
| `audit_events` | `audit` | Who did what, as an actor id |
| `organisations`, `properties`, `blocks`, `units` | — | Not personal data. A property is not a person |
| `statutory_rules`, `statutory_rule_slabs`, `ledger_accounts`, `posting_template*` | — | Reference data, no tenant, no person |
| `demo_sessions`, `workflow_runs`, `workflow_steps` | — | Operational. Sandbox rows are purgeable (ADR-0021) |

**No table holds an Aadhaar number**, and none may: `internal/platform/pii` fails the build
on a struct field or JSON tag named after one, and the schema's own assertion refuses the
column. ADR-0013 §2.

---

## 3. What defers an erasure

Three things, and each is named in the answer rather than being summarised as "cannot
comply":

1. **An open dispute.** Erasing the records while an argument about them is running
   destroys the evidence for both sides.
2. **Unsettled money** — a deposit not yet returned, a payout in flight.
3. **An outstanding statutory obligation** — most often a Form 16A or 16C the landlord is
   still owed. The tenant may be finished with the tenancy; the certificate is still the
   landlord's to receive.

A deferral is reassessed when the blocking item closes, and the requester is told either
way. Silence is the failure mode this exists to prevent.

**A live relationship also defers** everything with a period, because the clock has not
started. It does **not** defer `contact` — an active tenancy is not consent to be marketed
to.

---

## 4. What is not settled

- **Whether the PMLA period applies at all.** Dwellm8 is probably not a reporting entity
  under §2(1)(sa) — see [`india-property-compliance.md`](india-property-compliance.md) §9.
  Five years is held as the conservative answer, and if the platform is outside the Act
  then KYC records should follow the shorter contractual period instead. This is a real
  question for a professional, not a formatting one.
- **The financial period.** Eight years is the Companies Act figure; Rule 6F says six from
  the end of the relevant assessment year. Eight is used because it is the longer, and
  because a single number across the financial class is defensible where two overlapping
  ones are confusing. A reviewer may reasonably split them.
- **Whether erasure should be deletion or crypto-shredding.** For a record inside its
  retention period the personal fields could be encrypted with a per-principal key that is
  then destroyed, leaving the financial shape intact. That is a better answer than either
  keeping or deleting and it is not built.
- **Backups.** Nothing here reaches a backup. An erasure that leaves the data in a
  restorable snapshot is not an erasure, and the backup rotation that resolves it belongs
  to [#25](https://github.com/tesserix/dwellm8/issues/25).
