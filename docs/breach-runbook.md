# Personal data breach — runbook

For the person who has just realised something has gone wrong, at an hour when nobody
wants to be reading a policy document. Follow it in order.

**Under DPDP §8(6), every personal data breach is notifiable to the Data Protection Board
and to each affected data principal.** There is no severity threshold and no materiality
test to hide behind: "it was only a few records" is not an exemption. Assume you are
notifying, and let the assessment tell you otherwise.

The CERT-In direction of 28 April 2022 is the tighter clock — **6 hours** from noticing,
for the incident types it lists.

---

## 0. In the first ten minutes

- **Do not delete anything.** Not logs, not the offending record, not the deploy. The
  first instinct is to make it go away and it destroys the evidence that establishes scope.
- **Do not fix it silently.** A quiet patch with no incident record is how the same breach
  happens twice.
- Start a timestamped note. Every step below wants a time against it, and reconstructing
  them afterwards is guesswork presented as fact.

---

## 1. Contain

| | |
|---|---|
| Credential or token exposed | Rotate it in GCP Secret Manager, then restart the consumers. Rotation without a restart leaves the old value live in memory |
| A policy or grant is too wide | Fix it in `tesserix-k8s`, push, let ArgoCD sync. **Never `kubectl apply`** — a manual fix is reverted by the next sync and the window reopens without anyone noticing |
| An endpoint is leaking | Take the route out at the VirtualService rather than deploying a code fix under pressure |
| A person has access they should not | Revoke the delegation grant. It is effective-dated, so the revocation is a row and the history survives |

Containment is not resolution. Note the time and move on.

---

## 2. Establish scope

Answer these four, in writing:

1. **Whose data?** Named data principals if you can, a count and a class if you cannot.
2. **Which classes?** Use [`data-retention.md`](data-retention.md) §1 — `financial`, `tax`,
   `agreement`, `kyc`, `contact`. The class decides how bad it is; KYC and financial are
   the two that make a notification urgent.
3. **What was actually reachable, as opposed to theoretically exposed?** Row-level security
   means a widened policy often exposes less than it appears to. Query it as the affected
   role rather than reasoning about it.
4. **How long was the window open?** From the deploy or the change that opened it, not from
   when somebody noticed.

`audit_events` and `kyc_access_log` are where this is reconstructed. Neither is deletable,
which is the reason they exist.

**No Aadhaar number can be in scope** — nothing stores one, enforced by
`internal/platform/pii` and by a schema assertion. If one appears in an export or a log,
that is a separate and more serious finding: the guard has been bypassed.

---

## 3. Notify

| Who | When | What it must contain |
|---|---|---|
| **CERT-In** | **6 hours** of noticing, for a listed incident type | The incident, systems affected, timeline so far |
| **Data Protection Board** | Without delay, per §8(6) | Nature, extent, timing, likely consequences, measures taken |
| **Each affected data principal** | Without delay | What happened, what data, what they should do, who to contact |
| **The organisation whose tenants are affected** | Same | They have their own obligations to their tenants and will hear about it anyway |

Notify the affected people **before** the exposure becomes public knowledge, and in plain
language. A tenant does not need the CVE; they need to know whether their bank details were
in it and what to do this evening.

Do not wait for a complete picture. A first notification saying what is known and what is
not is better than a complete one sent late, and both are required.

---

## 4. Record

One incident record: what happened, when it started, when it was noticed, when it was
contained, who was affected, what was notified and to whom, what changed as a result.

The gap between **started** and **noticed** is the number that matters. It is the detection
capability, measured honestly, and it is the only figure in the record that predicts the
next incident.

---

## 5. Close it out

- The fix goes through the normal pipeline with a test that fails without it. An incident
  fixed with no test is an incident scheduled for a repeat.
- If a guard would have caught it and did not exist, **write the guard** — the planted
  defects in `.github/workflows/api.yml` are the pattern: prove the check fails when the
  defect is present.
- If a guard existed and did not fire, that is the more serious finding, and the incident
  is not closed until it is understood.

---

## What is not decided

**Owners and contact points are not filled in.** This runbook has the timelines and the
steps; it does not have a named incident lead, a Data Protection Officer, a grievance
contact or an out-of-hours rota, because those are people rather than decisions and nobody
has been appointed. Until they are, the runbook is procedurally complete and
operationally incomplete — and the 6-hour CERT-In clock does not care which.

The grievance SLA that DPDP requires is in the same position: [#20](https://github.com/tesserix/dwellm8/issues/20)
specified the mechanism, and the number and its owner remain outstanding.
