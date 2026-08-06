# PM app — end-to-end scenarios

The manager's app (`apps/pm`, `@dwellm8/pm`) driven against the deployed API, on
a simulator, with no demonstration data. Every row is a thing a real manager
does; a row passes only when the API and the database agree with the screen.

Run order matters: A before B before the rest, because each leaves the state the
next one reads.

**Accounts.** Solo manager `samyak.rout@gmail.com`; firm owner
`samyak.rout1988@gmail.com`. Both are kept between runs — the onboarding gates
only fire once per sign-in, so a deleted account is a scenario that cannot be
re-run without re-verifying an inbox.

**Recording a failure.** File the GitHub issue before fixing it, with the screen,
the request, and the log line that explains it. The issue number goes in the
Status column so this file is the index of what is known to be broken.

**Merging fixes found here.** Branch protection may be bypassed with admin merge
for a fix raised by this catalogue, standing permission given 2026-08-06 — CI
must still be green first. It does not extend to skipping the public → build →
private cycle, to deploying, or to any change not traceable to a row below.

---

## A — Solo manager, own property

The owner-operator: their own flats, their own Cashfree account, no delegation.

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| A1 | Create an account with an unused email | Signed in; the app parks on Verify | pass |
| A2 | A six-digit code arrives at that address | Mail accepted by Resend; code readable in the inbox | pass |
| A3 | Wrong code five times | Each refusal reads as itself; the fifth closes the code | |
| A4 | Ask for a sixth code inside the hour | 429 carrying the wait; the screen counts down in minutes | |
| A5 | Code older than ten minutes | "that code has expired", not "not right" | |
| A6 | Right code | Gate passes to Name your firm | pass |
| A7 | Relaunch the app after verifying | Opens past the code screen, offline as well | |
| A8 | Name the firm | Organisation minted; gate passes to registration | pass |
| A9 | File statutory details as a sole proprietor | Own PAN asked for, not an entity PAN; state and PIN validated | pass — #286 |
| A10 | Save with a malformed PAN / GSTIN / PIN | Refused per field, at the field | fixed #287 — verify after deploy |
| A10a | Search for the registered office | Picking a match fills line, locality, city, state code and PIN | blocked — endpoint not deployed |
| A10b | Search while the geocoder is down | "unavailable", and the fields stay typeable by hand | pass — #285 |
| A11 | Add a property, "It's mine" | Property owned by the firm; `grant_id` empty on every later call | blocked by #288 — fixed, verify after deploy |
| A12 | Add units to that property | Units listed under the property, addressable in a tenancy | |
| A13 | Onboard a tenant into a unit | Tenancy `pending_signature`; rent, deposit and dates as entered | |
| A14 | Today screen after the first tenancy | Rent roll and arrears real, not the demonstration figures | |
| A15 | Connect the firm's own Cashfree account | Merchant recorded against this organisation only | |
| A16 | Record an offline rent payment | Ledger entry and receipt; arrears fall by the amount | |

## B — Firm managing for other owners

The nationwide firm: staff, roles, and property that belongs to somebody else.

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| B1 | Create the firm account and verify it | Same gates as A1–A6, independent of A's state | |
| B2 | File firm registration (LLP) | Entity PAN and TAN asked for; RERA agent registration accepted | |
| B3 | Add a property for "Somebody new" | Owner created; mandate granted; header shows whose book is open | |
| B4 | Add a second property for the same owner | One owner row, `property_count` 2 — not a duplicate owner | |
| B5 | Switch between own books and the mandate | `X-Dwellm8-Grant` set only under the grant; own rows unreachable from it | |
| B6 | Onboard a tenant under the mandate | Tenancy belongs to the owner's book, not the firm's | |
| B7 | Hire a manager and assign properties | Staff row; that manager sees those properties and no others | |
| B8 | Spend authority below a job's cost | Approval requested rather than the job proceeding | |
| B9 | Owner payout run | Rent less fee and TDS; the split is the adapter's, not Cashfree's | |
| B10 | Revoke a mandate | Rows become unreadable at once; history stays | |

## C — Tenant application and screening

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| C1 | Applicant supplies five years of addresses | Gaps flagged rather than accepted silently | |
| C2 | Six months of bank statements, three of payslips | Stored to the private bucket, never to the public one | |
| C3 | Work and personal references | Recorded against the application, contactable | |
| C4 | ID proof | PAN masked at rest; no Aadhaar number stored anywhere | |
| C5 | Police check, optional | Absent does not block; present is recorded with its date | |
| C6 | Approve and convert to a tenancy | The application's figures carry into the lease unedited | |

## D — Money

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| D1 | Rent billed on schedule | Invoice per tenancy per period, once | |
| D2 | Payment through the aggregator | Settled state polled as well as pushed; no double credit | |
| D3 | TDS on rent over the s.194-I threshold | Deducted at the right rate; the certificate is issuable | |
| D4 | Platform fee | Taken from the firm's share, never the owner's | |
| D5 | Refund a deposit at exit | Deductions itemised; the balance reaches the tenant | |

## E — The edges that break apps

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| E1 | Cold start with no network | The last known state, not a sign-in screen | |
| E2 | Token expiring mid-request | Refreshed once, request retried once, no sign-out | |
| E3 | Two devices signed into one account | Both see the same firm; neither sees the other's drafts | |
| E4 | Sign out and back in | Every gate that was passed stays passed | |
| E5 | An organisation with nothing in it | Empty states say what to do, not "0" | |
| E6 | A request the server refuses | The server's own words, at the field that caused it | |
