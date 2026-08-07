package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Viewing requests (#331). What a prospect asks for when the published times do
// not suit: a private viewing at a time of their own, or a video walkthrough
// instead of an attendance. A request needs no slot to exist first, so a
// by-appointment-only listing is not a dead end.

// ErrPastTime is a proposed time that is no good — behind us, or absent.
var ErrPastTime = errors.New("inspection: a viewing needs a time still to come")

// ErrTooManyRequests is the spam ceiling, enforced by the database.
var ErrTooManyRequests = errors.New("inspection: two open viewing requests on one listing is the limit")

// ErrNoProposal is no such open proposed time.
var ErrNoProposal = errors.New("inspection: no such open proposed time")

// ErrNotAnswerable is a request that has already been settled — accepted,
// declined, or countered once already.
var ErrNotAnswerable = errors.New("inspection: the request is not open to that answer")

// ErrNotARequest is a kind that is neither a private nor an online viewing.
var ErrNotARequest = errors.New("inspection: a viewing request is either private or online")

// Proposal is a time one side put forward. It deliberately carries no meeting
// point: where the viewing happens is disclosed on confirmation only.
type Proposal struct {
	ID           string
	EnquiryID    string
	ProposedBy   string
	StartsAt     time.Time
	DurationMins int
	State        string
}

// Confirmation is what the owner attaches to an answer — the place for a
// private viewing, the link for an online one.
type Confirmation struct {
	MeetingPoint string
	MeetingLink  string
	AssignedTo   string
}

// Request is an enquiry with the times proposed against it.
type Request struct {
	Enquiry   Enquiry
	Proposals []Proposal
	Outcome   string
	// Withheld from the prospect until the viewing is confirmed.
	MeetingPoint string
	MeetingLink  string
}

// RequestViewing records a prospect's request with the times they can make.
// Platform pool and the same verification trigger as a booking: there is no new
// path around the verified-prospect rule.
func (s *Inspections) RequestViewing(ctx context.Context, listingID, prospectID, kind, message string,
	at []time.Time) (Request, error) {
	if kind != "inspection" && kind != "online_inspection" {
		return Request{}, ErrNotARequest
	}
	if len(at) == 0 || len(at) > 3 {
		return Request{}, fmt.Errorf("%w: propose between one and three times", ErrPastTime)
	}
	for _, t := range at {
		if !t.After(time.Now()) {
			return Request{}, ErrPastTime
		}
	}

	var out Request
	err := tenancy.Platform(ctx, s.platform, "recording a prospect's viewing request (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			var tenant, state string
			err := tx.QueryRow(ctx,
				`SELECT tenant_id::text, state FROM listings WHERE id = $1 AND published_at IS NOT NULL`,
				listingID).Scan(&tenant, &state)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoListing
			}
			if err != nil {
				return err
			}
			if state != string(domain.StateLive) {
				return ErrListingNotLive
			}

			if err := scanEnquiry(tx.QueryRow(ctx, `
				INSERT INTO enquiries (tenant_id, listing_id, prospect_id, kind, message)
				VALUES ($1, $2, $3, $4, $5)
				RETURNING id::text, tenant_id::text, listing_id::text, prospect_id::text,
				          kind, state, coalesce(message, ''), scheduled_for, created_at, '', ''`,
				tenant, listingID, prospectID, kind, nullText(message)), &out.Enquiry); err != nil {
				return err
			}

			for _, t := range at {
				var p Proposal
				if err := tx.QueryRow(ctx, `
					INSERT INTO inspection_proposals (tenant_id, enquiry_id, proposed_by, starts_at)
					VALUES ($1, $2, 'prospect', $3)
					ON CONFLICT (enquiry_id, starts_at) DO NOTHING
					RETURNING id::text, enquiry_id::text, proposed_by, starts_at, duration_mins, state`,
					tenant, out.Enquiry.ID, t).
					Scan(&p.ID, &p.EnquiryID, &p.ProposedBy, &p.StartsAt, &p.DurationMins, &p.State); err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						continue // the same time twice in one request is one time
					}
					return err
				}
				out.Proposals = append(out.Proposals, p)
			}

			env, err := events.New(string(domain.EventInspectionRequested), tenant,
				events.Subject{Kind: "enquiry", ID: out.Enquiry.ID},
				events.Actor{Kind: events.ActorSystem},
				struct {
					ListingID  string `json:"listing_id"`
					ProspectID string `json:"prospect_id"`
					Kind       string `json:"kind"`
				}{listingID, prospectID, kind})
			if err != nil {
				return err
			}
			return events.Append(ctx, tx, env)
		})
	if isRequestCeiling(err) {
		return Request{}, ErrTooManyRequests
	}
	if isCheckViolation(err) {
		return Request{}, ErrNotVerified
	}
	return out, err
}

// AcceptRequest takes one of the proposed times. For a private viewing the slot
// and the booking are written in the same transaction: a slot with nobody in it
// is a phantom viewing.
func (s *Inspections) AcceptRequest(ctx context.Context, enquiryID, proposalID string,
	c Confirmation, actor events.Actor) (Booked, error) {
	var out Booked
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = confirm(ctx, tx, enquiryID, "", proposalID, "prospect", c, actor)
		return err
	})
	return out, err
}

// AcceptCounter is the prospect's side of the one round trip: they take the
// time the owner offered, with the confirmation the owner attached to it.
func (s *Inspections) AcceptCounter(ctx context.Context, prospectID, enquiryID, proposalID string) (Booked, error) {
	var out Booked
	err := tenancy.Platform(ctx, s.platform, "accepting an owner's counter-offered time (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			var err error
			out, err = confirm(ctx, tx, enquiryID, prospectID, proposalID, "owner",
				Confirmation{}, events.Actor{Kind: events.ActorSystem})
			return err
		})
	return out, err
}

// confirm settles a request on one proposed time. by names which side put that
// time forward, so the prospect cannot accept their own proposal on the owner's
// behalf and the owner cannot accept a time they themselves offered.
func confirm(ctx context.Context, tx pgx.Tx, enquiryID, prospectID, proposalID, by string,
	c Confirmation, actor events.Actor) (Booked, error) {
	var out Booked
	var tenant, listingID, kind string
	err := tx.QueryRow(ctx, `
		SELECT tenant_id::text, listing_id::text, kind
		  FROM enquiries
		 WHERE id = $1 AND kind IN ('inspection', 'online_inspection')
		   AND state IN ('new', 'owner_responded') AND slot_id IS NULL
		   AND ($2 = '' OR prospect_id = $2::uuid)
		 FOR UPDATE`, enquiryID, prospectID).Scan(&tenant, &listingID, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotAnswerable
	}
	if err != nil {
		return out, err
	}

	var duration int
	var place, link *string
	err = tx.QueryRow(ctx, `
		UPDATE inspection_proposals SET state = 'accepted', updated_at = now()
		 WHERE id = $1 AND enquiry_id = $2 AND state = 'open' AND proposed_by = $3
		 RETURNING starts_at, duration_mins, meeting_point, meeting_link`,
		proposalID, enquiryID, by).Scan(&out.StartsAt, &duration, &place, &link)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNoProposal
	}
	if err != nil {
		return out, err
	}
	if !out.StartsAt.After(time.Now()) {
		return out, ErrPastTime
	}
	// The owner's own answer carries the confirmation; the prospect's
	// acceptance takes what the counter already carried.
	if c.MeetingPoint == "" && place != nil {
		c.MeetingPoint = *place
	}
	if c.MeetingLink == "" && link != nil {
		c.MeetingLink = *link
	}

	if _, err := tx.Exec(ctx, `
		UPDATE inspection_proposals SET state = 'superseded', updated_at = now()
		 WHERE enquiry_id = $1 AND state = 'open'`, enquiryID); err != nil {
		return out, err
	}

	var slotID any
	if kind == "inspection" {
		var id string
		if err := tx.QueryRow(ctx, `
			INSERT INTO inspection_slots (tenant_id, listing_id, starts_at, duration_mins,
			                              capacity, booked, assigned_to, meeting_point)
			VALUES ($1, $2, $3, $4, 1, 1, $5, $6)
			RETURNING id::text`,
			tenant, listingID, out.StartsAt, duration,
			nullText(c.AssignedTo), nullText(c.MeetingPoint)).Scan(&id); err != nil {
			if isUniqueViolation(err, "inspection_slots_one_per_time") {
				return out, ErrSlotClash
			}
			return out, err
		}
		slotID, out.MeetingPoint = id, c.MeetingPoint
	} else {
		out.MeetingLink = c.MeetingLink
	}

	if err := scanEnquiry(tx.QueryRow(ctx, `
		UPDATE enquiries
		   SET state = 'scheduled', scheduled_for = $2, slot_id = $3,
		       meeting_link = coalesce($4, meeting_link), updated_at = now()
		 WHERE id = $1
		 RETURNING id::text, tenant_id::text, listing_id::text, prospect_id::text,
		           kind, state, coalesce(message, ''), scheduled_for, created_at, '', ''`,
		enquiryID, out.StartsAt, slotID, nullText(c.MeetingLink)), &out.Enquiry); err != nil {
		return out, err
	}

	env, err := events.New(string(domain.EventInspectionConfirmed), tenant,
		events.Subject{Kind: "enquiry", ID: enquiryID}, actor,
		struct {
			ListingID  string `json:"listing_id"`
			ProspectID string `json:"prospect_id"`
			Kind       string `json:"kind"`
		}{listingID, out.Enquiry.ProspectID, kind})
	if err != nil {
		return out, err
	}
	return out, events.Append(ctx, tx, env)
}

// CounterRequest offers a time of the owner's own. One counter per request:
// this is a round trip, not a negotiation thread.
func (s *Inspections) CounterRequest(ctx context.Context, enquiryID string, at time.Time,
	c Confirmation, actor events.Actor) (Proposal, error) {
	if !at.After(time.Now()) {
		return Proposal{}, ErrPastTime
	}
	var out Proposal
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var tenant, listingID, prospectID string
		err := tx.QueryRow(ctx, `
			SELECT tenant_id::text, listing_id::text, prospect_id::text
			  FROM enquiries
			 WHERE id = $1 AND kind IN ('inspection', 'online_inspection')
			   AND state IN ('new', 'owner_responded') AND slot_id IS NULL
			   AND NOT EXISTS (SELECT 1 FROM inspection_proposals p
			                    WHERE p.enquiry_id = enquiries.id AND p.proposed_by = 'owner')
			 FOR UPDATE`, enquiryID).Scan(&tenant, &listingID, &prospectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAnswerable
		}
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE inspection_proposals SET state = 'superseded', updated_at = now()
			 WHERE enquiry_id = $1 AND state = 'open'`, enquiryID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO inspection_proposals (tenant_id, enquiry_id, proposed_by, starts_at,
			                                  meeting_point, meeting_link)
			VALUES ($1, $2, 'owner', $3, $4, $5)
			RETURNING id::text, enquiry_id::text, proposed_by, starts_at, duration_mins, state`,
			tenant, enquiryID, at, nullText(c.MeetingPoint), nullText(c.MeetingLink)).
			Scan(&out.ID, &out.EnquiryID, &out.ProposedBy, &out.StartsAt, &out.DurationMins,
				&out.State); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE enquiries SET state = 'owner_responded', updated_at = now() WHERE id = $1`,
			enquiryID); err != nil {
			return err
		}

		env, err := events.New(string(domain.EventInspectionCountered), tenant,
			events.Subject{Kind: "enquiry", ID: enquiryID}, actor,
			struct {
				ListingID  string `json:"listing_id"`
				ProspectID string `json:"prospect_id"`
			}{listingID, prospectID})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
	return out, err
}

// DeclineRequest closes the request with a reason. The prospect is told, and
// the request stops counting against their ceiling.
func (s *Inspections) DeclineRequest(ctx context.Context, enquiryID string, actor events.Actor) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var tenant, listingID, prospectID string
		err := tx.QueryRow(ctx, `
			UPDATE enquiries
			   SET state = 'closed', outcome = 'declined_by_owner', outcome_at = now(),
			       updated_at = now()
			 WHERE id = $1 AND kind IN ('inspection', 'online_inspection')
			   AND state IN ('new', 'owner_responded') AND slot_id IS NULL
			 RETURNING tenant_id::text, listing_id::text, prospect_id::text`, enquiryID).
			Scan(&tenant, &listingID, &prospectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAnswerable
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE inspection_proposals SET state = 'declined', updated_at = now()
			 WHERE enquiry_id = $1 AND state = 'open'`, enquiryID); err != nil {
			return err
		}

		env, err := events.New(string(domain.EventInspectionDeclined), tenant,
			events.Subject{Kind: "enquiry", ID: enquiryID}, actor,
			struct {
				ListingID  string `json:"listing_id"`
				ProspectID string `json:"prospect_id"`
			}{listingID, prospectID})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
}

// ProspectRequests is the prospect's own view: their requests, the times still
// on the table, and — once confirmed — where to turn up or which link to open.
func (s *Inspections) ProspectRequests(ctx context.Context, prospectID string) ([]Request, error) {
	var out []Request
	err := tenancy.Platform(ctx, s.platform, "reading a prospect's viewing requests (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			return loadRequests(ctx, tx, &out, ` WHERE e.prospect_id = $1
				   AND EXISTS (SELECT 1 FROM inspection_proposals p WHERE p.enquiry_id = e.id)
				 ORDER BY e.created_at DESC`, prospectID)
		})
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Enquiry.State != "scheduled" {
			out[i].MeetingPoint, out[i].MeetingLink = "", ""
		}
	}
	return out, nil
}

// OwnerRequests is the manager's queue of requests still to answer, alongside
// the enquiries and callbacks they already see.
func (s *Inspections) OwnerRequests(ctx context.Context) ([]Request, error) {
	var out []Request
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return loadRequests(ctx, tx, &out, ` WHERE e.tenant_id = current_tenant_id()
			   AND e.state IN ('new', 'owner_responded', 'scheduled')
			   AND EXISTS (SELECT 1 FROM inspection_proposals p WHERE p.enquiry_id = e.id)
			 ORDER BY e.created_at DESC`)
	})
	return out, err
}

func loadRequests(ctx context.Context, tx pgx.Tx, out *[]Request, tail string, args ...any) error {
	rows, err := tx.Query(ctx, selectRequest+tail+` LIMIT 200`, args...)
	if err != nil {
		return err
	}
	byEnquiry := map[string]int{}
	func() {
		defer rows.Close()
		for rows.Next() {
			var r Request
			if err = rows.Scan(&r.Enquiry.ID, &r.Enquiry.TenantID, &r.Enquiry.ListingID,
				&r.Enquiry.ProspectID, &r.Enquiry.Kind, &r.Enquiry.State, &r.Enquiry.Message,
				&r.Enquiry.ScheduledFor, &r.Enquiry.CreatedAt, &r.Enquiry.Headline,
				&r.Enquiry.UnitID, &r.Outcome, &r.MeetingLink, &r.MeetingPoint); err != nil {
				return
			}
			byEnquiry[r.Enquiry.ID] = len(*out)
			*out = append(*out, r)
		}
		err = rows.Err()
	}()
	if err != nil || len(*out) == 0 {
		return err
	}

	ids := make([]string, 0, len(byEnquiry))
	for id := range byEnquiry {
		ids = append(ids, id)
	}
	prows, err := tx.Query(ctx, `
		SELECT id::text, enquiry_id::text, proposed_by, starts_at, duration_mins, state
		  FROM inspection_proposals
		 WHERE enquiry_id = ANY($1::uuid[]) ORDER BY starts_at`, ids)
	if err != nil {
		return err
	}
	defer prows.Close()
	for prows.Next() {
		var p Proposal
		if err := prows.Scan(&p.ID, &p.EnquiryID, &p.ProposedBy, &p.StartsAt,
			&p.DurationMins, &p.State); err != nil {
			return err
		}
		i, ok := byEnquiry[p.EnquiryID]
		if !ok {
			continue
		}
		(*out)[i].Proposals = append((*out)[i].Proposals, p)
	}
	return prows.Err()
}

const selectRequest = `
	SELECT e.id::text, e.tenant_id::text, e.listing_id::text, e.prospect_id::text,
	       e.kind, e.state, coalesce(e.message, ''), e.scheduled_for, e.created_at,
	       coalesce(l.headline, ''), coalesce(l.unit_id::text, ''),
	       coalesce(e.outcome, ''), coalesce(e.meeting_link, ''),
	       coalesce((SELECT s.meeting_point FROM inspection_slots s WHERE s.id = e.slot_id), '')
	  FROM enquiries e
	  LEFT JOIN listings l ON l.id = e.listing_id`

// isRequestCeiling separates the spam ceiling from the verification trigger:
// both are check violations, and only the message tells them apart.
func isRequestCeiling(err error) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return false
	}
	return pg.Code == "23514" && strings.Contains(pg.Message, "open viewing requests")
}
