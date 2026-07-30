# ADR-0019 — Public listing surface, anonymous access and prospect identity

- **Status**: Accepted
- **Date**: 2026-07-30
- **Deciders**: Discovery, Platform
- **Issue**: [#132](https://github.com/tesserix/dwellm8/issues/132)
- **Related**: [ADR-0003](0003-tenancy-and-row-level-security.md) (the rule this makes its only exception to), [ADR-0013](0013-kyc-data-handling.md) (why no phone number is stored), [ADR-0021](0021-demo-sandbox-architecture.md) (the same token pattern), [ADR-0009](0009-property-block-unit-model.md) (the unit a listing advertises)

---

## Context

The listing site is the only surface used by people who are not customers and may never
sign in. How much a prospect can do anonymously, when they must verify a phone number,
and how that record later becomes a tenant, has to be settled before the funnel is built
— because both failure modes are expensive. A wall at the front kills the funnel; no wall
makes an owner's phone a spam target.

Underneath it there is a harder problem. **ADR-0003's whole design is that an unscoped
session sees nothing**: `current_tenant_id()` is NULL when unset, so every policy denies.
An anonymous visitor *is* an unscoped session. Serving them anything at all means opening
the first read path in this schema that does not fail closed.

---

## Decision

**One publicly readable table, whose rows opt in; a prospect who belongs to no
organisation and whose phone number is not in this database; verification enforced at the
point of contact rather than at the door; and a masked connection that opens only once
both sides have engaged.**

### 1. The hole, and the three things that make it narrow

`listings` has a fourth branch in its `USING` clause that mentions no tenant:

```sql
OR (state = 'live' AND published_at IS NOT NULL)
```

That is the exception. It is kept survivable by three constraints, and all three are
enforced:

1. **One table.** Assertion 17 refuses a `PERMISSIVE` read policy anywhere else that
   mentions neither `current_tenant_id()` nor `is_platform_session()`. The failure it
   prevents is silent and total — a new table given a convenience policy while somebody is
   debugging is a table the whole internet can read, and nothing about it looks wrong from
   the application.
2. **The row opts in.** Publication is an act by the owner, not a default, and assertion 17
   also refuses a public branch that is not scoped to a published live row. Pausing removes
   a listing from public view immediately, which is what makes publication an act rather
   than a flag set once.
3. **Read-only by construction.** `WITH CHECK` has no public branch at all. Publishing is a
   write and a write always needs a tenant, and assertion 17 refuses a `WITH CHECK` that
   does not mention `current_tenant_id()`.

The listing carries denormalised public fields — locality, rent, photographs — so an
anonymous reader never needs to join to `properties` or `units`, which deny. The address
is deliberately not among them: a listing shows a locality until an inspection is booked,
which is the industry norm and is also what stops the site being a map of which flats are
empty.

`property_id` and `unit_id` are present because the product needs them, and they lead
nowhere: a uuid is opaque when every other table denies.

### 2. The leak this design caused, and how it was found

The first version of the `enquiries` policy had a delegated branch reading:

```sql
OR EXISTS (SELECT 1 FROM listings l WHERE l.id = listing_id)
```

The reasoning was that a firm which can see the listing may answer its enquiries. It is a
leak. **`listings` is publicly readable, so the subquery was true for everybody** — an
anonymous visitor could read every enquiry on every published listing, with names and
messages.

The isolation test found it, not the assertion, and that is worth stating plainly:
assertion 17 checks that a policy *mentions* the tenant, and this one did. It cannot see
that a branch reaches a publicly-readable table through a subquery.

The general lesson, which is the real cost of §1: **once one table is world-readable, any
policy that reaches it through a subquery inherits that.** Every existing policy was
written when nothing was public. The fix here was to drop the branch — a management firm
reads enquiries through the owner's session, and a proper delegated branch needs the
property denormalised onto the enquiry, which is a later story.

### 3. What is anonymous, and where the wall is

Browsing and searching are anonymous. Shortlisting is anonymous and lives in browser
local state — which is what makes it genuinely anonymous rather than anonymous-looking:
there is no row until there is a reason for one.

The wall is at contact. `enquiries_verification_point` refuses an enquiry, inspection or
callback from an unverified prospect:

```
ERROR:  enquiry … is from an unverified prospect: browsing and shortlisting need no
        account, and making contact needs a verified phone number — which is the only
        thing standing between an owner and a thousand fake enquiries
```

A trigger rather than a convention, because the bot case is the one where the application
layer is being probed.

### 4. A prospect belongs to nobody

`prospects` has **no `tenant_id`**, and that is a decision rather than an omission. A
prospect is browsing the whole site; attributing them to the first owner whose listing
they opened would be wrong, and it would leak their interest in the others to that owner.
So the row is platform-owned, assertion 12 requires its writes to be platform-only, and
the schema audit needed the exemption argued for.

`enquiries` is the first row in the funnel that does belong to somebody — the owner whose
listing it is — and it has ordinary tenancy. That is the line the exemption stops at.

Identity is an opaque token stored hashed, the same shape ADR-0021 uses for a demo
visitor and for the same reason: a copy of the database must not hand out somebody's
browsing history.

**The prospect row survives sign-up.** The story's edge case is that a prospect who signs
up must not lose their shortlist or enquiry history, so conversion points the row at a
party (`converted_party_id`) rather than replacing it. The shortlist and the enquiries
still reference the prospect, so nothing has to be migrated.

### 5. No phone number is in this database

`contact_ref` is the masked-calling provider's token and `contact_masked` is the display
form; the raw number is held by the provider. This goes further than the story's edge case
— "neither number may be exposed before both have engaged" — because it is cheaper to go
further: **there is nothing here to expose at any point.**

The timing rule still exists, and it is what `contact_bridges_mutual` enforces: a bridge
opens only once the enquiry has left `new`, so a prospect cannot dial an owner off the
back of an unanswered enquiry.

`prospects_verification_shape` makes verification a package — timestamp, provider
reference and masked form together or not at all. A half-verified prospect is one who can
be called and not displayed, or displayed and not called, and either is a bug in whatever
wrote them.

### 6. Anti-abuse, and the shared-mobile-network problem

The story's failure scenario has a sting in it: rate-limit a thousand bot enquiries
*without* blocking a genuine prospect on a shared mobile network. Indian mobile carriers
CGNAT aggressively, so per-IP limiting blocks a building.

So the bound that matters is **per verified contact**, not per address. Verification is
already required to enquire (§3), and a verified contact is a real cost to acquire at
scale. Edge rate limiting on *creation* still belongs at the edge and is ADR-0018's; what
this ADR fixes is that the expensive action is behind the verification, so the edge only
has to survive browsing traffic.

### 7. SEO

`listings.indexable` is a column rather than a template condition, so what a search engine
may see is data and can be reported on. Only a live published listing is indexable;
nothing about a prospect, an enquiry or an owner is reachable to index at all, because
none of it is publicly readable.

### 8. What fails the build

- `internal/platform/tenancy/isolationtest` — an anonymous session seeing a published
  listing, not seeing a draft, not seeing a paused one; seeing zero rows in thirteen other
  tables including the two this funnel adds; unable to edit or create a listing; an
  enquiry refused from an unverified prospect and accepted from a verified one; a
  half-verified prospect refused; and a contact bridge refused until the owner responds.

CI plants three failures and expects red.

---

## Alternatives considered

### A. A separate read-only replica or a denormalised public table — rejected

Publish listings into a table with no row-level security at all, or into a separate
database the site reads.

Rejected because it doubles the write path for the thing most likely to be wrong — a
listing that is paused here and live there is worse than no listing site — and because the
public table would still need to be kept in step with `state` and `published_at`. The
policy branch is one clause and cannot drift from the row it is about.

It becomes the right answer if listing traffic ever threatens the primary, and the switch
is a read path change rather than a data model one.

### B. No anonymous browsing; sign in to see anything — rejected

It would keep ADR-0003 absolute, which has real value.

Rejected because it is a wall at the front of the funnel, which is half the failure the
story names. Nobody signs up to find out whether a flat exists.

### C. Store the prospect's phone number, encrypted — rejected

It would let the product call a prospect directly and avoid a provider dependency.

Rejected because a masked-calling provider is needed anyway — the owner's number must not
be exposed either — and once the provider holds one side it may as well hold both. Storing
a number we do not need is the thing ADR-0013 exists to argue against, and the encryption
would be the same dormant-encryption trap.

### D. Attribute a prospect to the owner whose listing they opened — rejected

It would let a prospect be an ordinary tenant-scoped row and avoid the assertion-12
exemption.

Rejected because it is false and it leaks. A prospect looking at six flats belongs to
none of the six owners, and giving the first one the row would tell them about the other
five.

### E. Per-IP rate limiting as the primary defence — rejected

The obvious anti-abuse measure.

Rejected for §6's reason: Indian carriers CGNAT, so a per-IP limit tight enough to stop a
bot blocks a residential building. It survives as an edge measure against browsing floods,
and the enquiry path is defended by verification instead.

---

## Consequences

**What is now true.** Exactly one table is readable without a tenant, only for rows their
owner published, and only for reading — with an assertion refusing a second one, an
unpublished one, or a public write. Browsing and shortlisting need no account. Making
contact needs a verified phone, enforced by a trigger. Neither party's number is in this
database. A prospect who signs up keeps their shortlist and their history. And an
anonymous session sees zero rows in every other table, asserted against thirteen of them.

**What this costs.** The subquery hazard in §2 is permanent: every policy in this schema
was written when nothing was public, and any future one that reaches `listings` through a
subquery inherits its visibility. Assertion 17 cannot catch that, and the only thing that
did was a test that counted rows an anonymous session could see. `enquiries` has no
delegated branch, so a management firm cannot answer enquiries under its own session yet.
And the masked-calling provider becomes a hard dependency of the funnel — if it is down,
no owner and prospect can be connected, and there is no fallback that does not involve
exposing a number.

**What is not decided.** The provider itself, and its webhook path. Listing media and
where it is stored, which is the object-storage question ADR-0021 also leaves open.
Bot protection and edge rate limiting, which are ADR-0018's. The conversion flow that
turns `converted_party_id` into an actual account. A delegated branch for enquiries,
which needs the property denormalised onto them. And listing quality moderation, which is
the difference between a listing site and a spam site and has no issue yet.
