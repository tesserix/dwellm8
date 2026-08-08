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

**Status vocabulary.** `not run` — never exercised. `pass` — seen working, on
device unless the row says otherwise. `pass — unit` — proved by a test in the
repo, not yet on device. `#nnn` — failing, with the issue that holds it.
`blocked: …` — cannot be run, and why.

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
| A3 | Wrong code five times | Each refusal reads as itself; the fifth closes the code | covered by `verification.TestCheckRefusals`; on device it needs a fresh unverified account |
| A4 | Ask for a sixth code inside the hour | 429 carrying the wait; the screen counts down in minutes | covered by `TestIssuingIsCappedOverTheHourNotJustTheMinute`; device run needs a fresh account |
| A5 | Code older than ten minutes | "that code has expired", not "not right" | covered by `TestCheckRefusals`/an expired code; device run needs a fresh account |
| A6 | Right code | Gate passes to Name your firm | pass |
| A7 | Relaunch the app after verifying | Opens past the code screen, offline as well | |
| A8 | Name the firm | Organisation minted; gate passes to registration | pass |
| A9 | File statutory details as a sole proprietor | Own PAN asked for, not an entity PAN; state and PIN validated | pass — #286 |
| A10 | Save with a malformed PAN / GSTIN / PIN | Refused per field, at the field | pass — #287 |
| A10a | Search for the registered office | Picking a match fills line, locality, city, state code and PIN | pass |
| A10b | Search while the geocoder is down | "unavailable", and the fields stay typeable by hand | pass — #285 |
| A11 | Add a property, "It's mine" | Property owned by the firm; `grant_id` empty on every later call | pass — property `KVH` minted under the firm, no grant |
| A12 | Add units to that property | Units listed under the property, addressable in a tenancy | pass — a unit let from a date ahead says so in its pill (#304) and in its own row (#314); the three tiles partition the units (#311) |
| A13 | Onboard a tenant into a unit | Tenancy live on unit 101; rent, deposit and dates as entered | pass — first tenancy activates through the tax gate, so `active` not `pending_signature` |
| A14 | Today screen after the first tenancy | Rent roll and arrears real, not the demonstration figures | rent roll live; a tenancy that has not started is counted apart (#305) and named on the screen (#308); no sync queue is claimed (#309). The date and four tiles still demo — #291, #251 |
| A17 | Collections with every tenancy square | Both tabs readable: arrears empty, the roster still carries the tenancy | pass — the roster tab reads its own endpoint (#313); before that it borrowed the arrears list and showed nothing |
| A18 | Profile and portfolio switcher | Names the signed-in manager, states the mandate, offers a way out | pass — #315, #316. Log out is wired but deliberately not pressed: signing back in is the user's to do |
| A19 | Onboard a new owner, property, units and first tenancy in one pass | Every detail entered is reviewable before it lands, and the owner is identified well enough to deduct on | part pass — owner, property, 3 units and an active tenancy all landed live. The review omitted the deposit (#317, fixed). No owner identity is collected at all — no PAN, and no TRC, foreign TIN or rule 37BC particulars for an owner abroad, so a section 195 tenancy is unassessable (#318) |
| A15 | Connect the firm's own Cashfree account | Merchant recorded against this organisation only | refusals tested locally only — the live call opens a real merchant account with Cashfree, so it is the user's to run |
| A16 | Record an offline rent payment | Ledger entry and receipt; arrears fall by the amount | pass live — ₹50,000 deposit receipted against the KVH tenancy before its term starts (#303), the retry returned the same payment, and the feed carries `money.payment.received` |

## B — Firm managing for other owners

The nationwide firm: staff, roles, and property that belongs to somebody else.

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| B1 | Create the firm account and verify it | Same gates as A1–A6, independent of A's state | blocked: creating an account is the user's to do, so B2–B10 wait on it |
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
| C1 | Applicant supplies five years of addresses | Gaps flagged rather than accepted silently | blocked: no applicant-pack surface is mounted yet — #258–#266 |
| C2 | Six months of bank statements, three of payslips | Stored to the private bucket, never to the public one | |
| C3 | Work and personal references | Recorded against the application, contactable | |
| C4 | ID proof | PAN masked at rest; no Aadhaar number stored anywhere | |
| C5 | Police check, optional | Absent does not block; present is recorded with its date | |
| C6 | Approve and convert to a tenancy | The application's figures carry into the lease unedited | |

## D — Money

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| D1 | Rent billed on schedule | Invoice per tenancy per period, once | waiting on the 02:30 IST billing run after the term starts on 2026-08-10 |
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
| E4 | Sign out and back in | Every gate that was passed stays passed | blocked: signing back in means entering a password, which is the user's to do |
| E5 | An organisation with nothing in it | Empty states say what to do, not "0" | pass — Collect (both tabs), Jobs (all three), Inspect and the switcher each name what fills them. #313 was this row failing on the roster tab |
| E6 | A request the server refuses | The server's own words, at the field that caused it | |

## F — Describing what is let

Listing surface: the flat, the block, what is around it, and who asks to see it.

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| F1 | Describe a property — floors, lift, parking, power backup | Saved against the property and read back on reopen | not run |
| F2 | Describe a flat — bedrooms, bathrooms, facing, furnishing | Only the features the record can hold are offered (#354) | not run |
| F3 | Describe a flat with nothing filled in | Saves as unknown rather than as zero | not run |
| F4 | What is nearby — school, metro, hospital with distances | Each entry carries a kind and a distance; order is stable | not run |
| F5 | Correct a description already saved | The screen names what changed, and offers only fields the record holds (#354) | not run |
| F6 | Hostel or PG property — allocate beds by floor and room | Beds addressable individually; an allocated bed cannot be double-let | not run |
| F7 | Bed allocation on a property that is neither hostel nor PG | Says so plainly rather than showing an empty grid | not run |
| F8 | Set viewing times | Slots stored per property; a past slot cannot be offered | not run |
| F9 | Two enquiries on one listing | Both appear as leads, newest first, each traceable to its listing | not run |
| F10 | Book a viewing into a slot | Slot marked taken; the lead's state advances | not run |

## G — Agreements, deeds and documents

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| G1 | Generate the management agreement | PDF renders with the firm's and the owner's real particulars | not run |
| G2 | Preview an agreement that cannot render | Says so, rather than spinning (fixed 2dfca3a — re-verify) | not run |
| G3 | Send an agreement to be signed | State moves off draft; the signed copy can be filed against it | not run |
| G4 | File a signed paper copy | Stored to the private bucket; the agreement reads as executed | not run |
| G5 | Upload ownership evidence (sale deed, tax receipt, khata) | Ownership reads as proven; a property with none reads as not proven | not run |
| G6 | Let a unit on a property with no ownership evidence | Warned before the tenancy, not after | not run |
| G7 | Open a document template and fill it | Placeholders resolve from the record, none left literal | not run |
| G8 | Applicant pack — download everything filed for one tenancy | One archive, private-bucket links only, no public URL | not run |
| G9 | A blob upload the signer cannot write | Fails loudly rather than returning a link to nothing (#352) | not run |

## H — Jobs, vendors and inspections

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| H1 | Raise a job against a unit | Appears on Jobs with who bears the cost stated | not run |
| H2 | Dispatch a vendor | Vendor recorded on the job; the timeline gains the dispatch | not run |
| H3 | Schedule a visit | Date lands on the job and on the tenant's side | not run |
| H4 | Job cost above the manager's spend authority | Approval requested rather than the job proceeding (same rule as B8) | not run |
| H5 | Close a job | Timeline complete; the job leaves the open tile | not run |
| H6 | Run a move-in inspection | Report queued; summary readable by the owner | not run |
| H7 | Move-out inspection against the move-in | Differences itemised, and feed the deposit deduction (D5) | not run |
| H8 | Define a process and run its checklist | Steps tick in order; a half-done run resumes where it stopped | not run |

## I — Compliance, society and what runs by itself

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| I1 | Compliance register with a lapsed certificate | Names the certificate, the days lapsed, and who owns it | not run |
| I2 | Add a certificate with an expiry ahead | Falls out of the lapsed list; reminder scheduled | not run |
| I3 | Society maintenance due | Amount and month stated; payable from the right book | not run |
| I4 | Post a society notice | Reaches residents of that property only | not run |
| I5 | Reminders list | Each reminder names its subject and its date, none orphaned | not run |
| I6 | Automations — what runs by itself, and its limits | Every automation states its cap; none can be enabled without one | not run |

## J — Team, roles and delegation

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| J1 | Hire a manager with terms and a spend cap | Staff row; the cap is the one enforced in H4 | not run |
| J2 | Assign properties to that manager | They see those and no others (same rule as B7) | not run |
| J3 | Roles — what each role may do | The list matches what the API enforces, not a longer one | not run |
| J4 | Rota — move-ins, move-outs, who covers | Hours per person add up; no double-booking | not run |
| J5 | Mark somebody as no longer with the firm | Access ends at once; their history stays readable | not run |
| J6 | Switcher between own book and each mandate | `X-Dwellm8-Grant` set only under a grant (same rule as B5) | not run |

## K — Money surfaces

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| K1 | Where rent settles — own account, own aggregator | Recorded against this organisation only (same rule as A15) | not run |
| K2 | Payout run for one owner | Rent less fee and TDS, the split shown before it is released | not run |
| K3 | Tax on rent — the two questions before a tenancy starts | Category decides the rate; an unanswerable owner blocks the tenancy | not run |
| K4 | Receipt an offline payment twice with the same reference | The second returns the first payment, not a duplicate | not run |
| K5 | Arrear screen for a tenancy behind on rent | Position, receipts and what is owed agree with the ledger | not run |
| K6 | Message a tenant in arrears | What was said is on record against the tenancy | not run |

## L — Retirement and closure (#356)

Deactivating rather than deleting: the row stops being usable, its history stays.

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| L1 | End a tenancy | Tenancy closed with its date; the unit becomes lettable again | blocked: no PM screen calls the retire endpoints yet |
| L2 | Retire a unit with a live tenancy | Refused, naming the tenancy | blocked, as L1 |
| L3 | Retire a property with no live tenancies | Property unreachable from the portfolio; receipts still readable | blocked, as L1 |
| L4 | Retire an owner with a live mandate | Refused until the mandate is revoked | blocked, as L1 |
| L5 | Close the firm's account | Every book becomes read-only; nothing is deleted | blocked, as L1 |
| L6 | Sign in after closure | Told the account is closed, not shown an empty app | blocked, as L1 |

## M — The way in (shared across every Dwellm8 app)

`packages/mobile-shared` — the same screen and the same session hook serve PM,
`live`, and anything after them. A row here failing fails every app at once.

| # | Scenario | Expected | Status |
|---|----------|----------|--------|
| M1 | Sign in with the right password | Session stored in the keychain; the app opens past the gate | blocked: no password for the test account until the M4 link is opened |
| M2 | Sign in with a wrong password | The provider's own refusal, at the form | pass — unit (`SignIn.test.tsx`) |
| M3 | Password under six characters | Button stays disabled; nothing is sent | pass — unit |
| M4 | Forgot password with a known address | Reset mail arrives; the link sets a new password | part pass — sent live to `samyak.rout@gmail.com` 2026-08-08, GIP accepted; the link itself is the user's to open |
| M5 | Forgot password with an address that has no account | Same neutral answer as M4; no enumeration oracle | pass — on device against live GIP, and unit |
| M6 | Forgot password with the field empty | Asks for the address rather than sending nothing | pass — on device, and unit |
| M7 | Reset offered while creating an account | It is not | pass — on device, and unit |
| M8 | A build with no GIP tenant configured | Says so plainly instead of failing at the first request | pass — unit |
| M9 | Relaunch after signing in | Restored from the keychain, no sign-in screen | not run — waits on M1 |
| M10 | Token a minute from expiry | Refreshed before the request, not after a 401 | pass — `session.test.ts` |
| M11 | Sign out | Keychain cleared; a relaunch lands on sign-in | not run |
| M12 | Sign in on a device with no keychain entitlement | Regression guard for the entitlement lost at prebuild | fixed — `app.json` carries it |
