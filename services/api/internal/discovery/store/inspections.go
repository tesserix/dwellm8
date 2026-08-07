package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ErrSlotClash is a second slot at the same time for the same listing.
var ErrSlotClash = errors.New("inspection: a slot already exists at that time")

// ErrSlotFull is the last place going to somebody else. The caller offers the
// alternatives that ride along rather than dropping the prospect silently
// (#139's race).
var ErrSlotFull = errors.New("inspection: the slot is full or no longer open")

// ErrNoSlot is no such open slot.
var ErrNoSlot = errors.New("inspection: no such slot")

// ErrNotReschedulable is an inspection that is not in a state to move — never
// booked, already concluded, or already past.
var ErrNotReschedulable = errors.New("inspection: not open to change")

// Inspections is the viewing-slot store (#139–#141). Owner operations run
// under ordinary tenancy; the prospect side runs on the platform pool like the
// rest of the anonymous funnel.
type Inspections struct {
	pool     tenancy.Pool
	platform tenancy.PlatformPool
}

// NewInspections builds the store.
func NewInspections(p tenancy.Pool, plat tenancy.PlatformPool) *Inspections {
	return &Inspections{pool: p, platform: plat}
}

// Slot is a published viewing time.
type Slot struct {
	ID           string
	ListingID    string
	StartsAt     time.Time
	DurationMins int
	Capacity     int
	Booked       int
	AssignedTo   string
	// MeetingPoint is blank on the prospect side until a booking confirms —
	// the approximate location becomes exact only then.
	MeetingPoint string
	State        string
	// Zone is the clock the viewing happens on — the series', or the country's
	// default where a slot was published on its own (#334).
	Zone string
}

// SlotDraft is what the owner publishes.
type SlotDraft struct {
	StartsAt     time.Time
	DurationMins int
	Capacity     int
	AssignedTo   string
	MeetingPoint string
}

// PublishSlot offers a viewing time on a listing.
func (s *Inspections) PublishSlot(ctx context.Context, listingID string, d SlotDraft) (string, error) {
	if d.DurationMins == 0 {
		d.DurationMins = 30
	}
	if d.Capacity == 0 {
		d.Capacity = 1
	}
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return "", tenancy.ErrNoTenant
	}
	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT true FROM listings WHERE id = $1`, listingID).Scan(&exists); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoListing
			}
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO inspection_slots (tenant_id, listing_id, starts_at, duration_mins,
			                              capacity, assigned_to, meeting_point)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id`,
			tenant.String(), listingID, d.StartsAt, d.DurationMins, d.Capacity,
			nullText(d.AssignedTo), nullText(d.MeetingPoint)).Scan(&id)
	})
	if isUniqueViolation(err, "inspection_slots_one_per_time") {
		return "", ErrSlotClash
	}
	return id, err
}

// CloseSlot stops new bookings. Existing bookings stand — the owner moves them
// individually, so nobody's confirmed viewing evaporates as a side effect.
func (s *Inspections) CloseSlot(ctx context.Context, slotID string) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE inspection_slots SET state = 'closed', updated_at = now()
			  WHERE id = $1 AND state = 'open'`, slotID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSlot
		}
		return nil
	})
}

// OwnerSlots lists a listing's slots with their fill, under ordinary tenancy.
func (s *Inspections) OwnerSlots(ctx context.Context, listingID string) ([]Slot, error) {
	var out []Slot
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, selectSlot+`
			 WHERE listing_id = $1 ORDER BY starts_at`, listingID)
		if err != nil {
			return err
		}
		return scanSlots(rows, &out)
	})
	return out, err
}

// OpenSlots is what a prospect chooses from: open, future, on a live listing.
// The meeting point is withheld here — it arrives with a confirmed booking.
func (s *Inspections) OpenSlots(ctx context.Context, listingID string) ([]Slot, error) {
	var out []Slot
	err := tenancy.Platform(ctx, s.platform, "listing open viewing slots to a prospect (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, selectSlot+`
				 WHERE listing_id = $1 AND state = 'open' AND booked < capacity
				   AND starts_at > now()
				   AND EXISTS (SELECT 1 FROM listings l WHERE l.id = listing_id
				                  AND l.state = 'live' AND l.published_at IS NOT NULL)
				 ORDER BY starts_at LIMIT 50`, listingID)
			if err != nil {
				return err
			}
			if err := scanSlots(rows, &out); err != nil {
				return err
			}
			for i := range out {
				out[i].MeetingPoint = ""
			}
			return nil
		})
	return out, err
}

// Booked is a confirmed booking: the enquiry, the exact meeting point, and the
// slot it holds.
type Booked struct {
	Enquiry      Enquiry
	StartsAt     time.Time
	MeetingPoint string
	// MeetingLink is where an online viewing happens (#331); it replaces the
	// meeting point rather than joining it.
	MeetingLink string
}

// Book takes a place on a slot for a verified prospect.
//
// The capacity race is settled by the conditional UPDATE: of two prospects
// racing for the last place, one increments the counter and one matches no row
// — and the loser gets ErrSlotFull with the adjacent alternatives, never
// silence. The enquiry trigger re-checks prospect verification.
func (s *Inspections) Book(ctx context.Context, listingID, prospectID, slotID string) (Booked, []Slot, error) {
	var (
		out  Booked
		alts []Slot
	)
	err := tenancy.Platform(ctx, s.platform, "booking a viewing for a prospect (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			var tenant, listingState string
			err := tx.QueryRow(ctx,
				`SELECT tenant_id::text, state FROM listings WHERE id = $1 AND published_at IS NOT NULL`,
				listingID).Scan(&tenant, &listingState)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoListing
			}
			if err != nil {
				return err
			}
			if listingState != string(domain.StateLive) {
				return ErrListingNotLive
			}

			var meeting *string
			err = tx.QueryRow(ctx, `
				UPDATE inspection_slots SET booked = booked + 1, updated_at = now()
				 WHERE id = $1 AND listing_id = $2 AND state = 'open'
				   AND booked < capacity AND starts_at > now()
				 RETURNING starts_at, meeting_point`,
				slotID, listingID).Scan(&out.StartsAt, &meeting)
			if errors.Is(err, pgx.ErrNoRows) {
				// The loser's answer: the next open times, in the same query's
				// transaction so they are not full too by the time they render.
				rows, qerr := tx.Query(ctx, selectSlot+`
					 WHERE listing_id = $1 AND state = 'open' AND booked < capacity
					   AND starts_at > now() AND id <> $2
					 ORDER BY starts_at LIMIT 3`, listingID, slotID)
				if qerr != nil {
					return qerr
				}
				if qerr := scanSlots(rows, &alts); qerr != nil {
					return qerr
				}
				for i := range alts {
					alts[i].MeetingPoint = ""
				}
				return ErrSlotFull
			}
			if err != nil {
				return err
			}
			if meeting != nil {
				out.MeetingPoint = *meeting
			}

			err = scanEnquiry(tx.QueryRow(ctx, `
				INSERT INTO enquiries (tenant_id, listing_id, prospect_id, kind, state,
				                       scheduled_for, slot_id)
				VALUES ($1, $2, $3, 'inspection', 'scheduled', $4, $5)
				RETURNING id::text, tenant_id::text, listing_id::text, prospect_id::text,
				          kind, state, coalesce(message, ''), scheduled_for, created_at, '', ''`,
				tenant, listingID, prospectID, out.StartsAt, slotID), &out.Enquiry)
			if err != nil {
				return err
			}

			env, err := events.New(string(domain.EventInspectionBooked), tenant,
				events.Subject{Kind: "enquiry", ID: out.Enquiry.ID},
				events.Actor{Kind: events.ActorSystem},
				struct {
					ListingID string    `json:"listing_id"`
					SlotID    string    `json:"slot_id"`
					StartsAt  time.Time `json:"starts_at"`
				}{listingID, slotID, out.StartsAt})
			if err != nil {
				return err
			}
			return events.Append(ctx, tx, env)
		})
	if isCheckViolation(err) {
		return Booked{}, nil, ErrNotVerified
	}
	return out, alts, err
}

// Reschedule moves a booking to another slot: the new place is taken first,
// the old released, and the enquiry keeps its identity — the original context
// is not lost to a time change (#140's edge case).
func (s *Inspections) Reschedule(ctx context.Context, prospectID, enquiryID, newSlotID string) (Booked, []Slot, error) {
	var (
		out  Booked
		alts []Slot
	)
	err := tenancy.Platform(ctx, s.platform, "rescheduling a prospect's viewing (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			var tenant, listingID, oldSlot string
			err := tx.QueryRow(ctx, `
				SELECT tenant_id::text, listing_id::text, coalesce(slot_id::text, '')
				  FROM enquiries
				 WHERE id = $1 AND prospect_id = $2 AND kind = 'inspection' AND state = 'scheduled'
				 FOR UPDATE`, enquiryID, prospectID).Scan(&tenant, &listingID, &oldSlot)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotReschedulable
			}
			if err != nil {
				return err
			}

			var meeting *string
			err = tx.QueryRow(ctx, `
				UPDATE inspection_slots SET booked = booked + 1, updated_at = now()
				 WHERE id = $1 AND listing_id = $2 AND state = 'open'
				   AND booked < capacity AND starts_at > now()
				 RETURNING starts_at, meeting_point`,
				newSlotID, listingID).Scan(&out.StartsAt, &meeting)
			if errors.Is(err, pgx.ErrNoRows) {
				rows, qerr := tx.Query(ctx, selectSlot+`
					 WHERE listing_id = $1 AND state = 'open' AND booked < capacity
					   AND starts_at > now() AND id <> $2
					 ORDER BY starts_at LIMIT 3`, listingID, newSlotID)
				if qerr != nil {
					return qerr
				}
				if qerr := scanSlots(rows, &alts); qerr != nil {
					return qerr
				}
				for i := range alts {
					alts[i].MeetingPoint = ""
				}
				return ErrSlotFull
			}
			if err != nil {
				return err
			}
			if meeting != nil {
				out.MeetingPoint = *meeting
			}

			if oldSlot != "" {
				if _, err := tx.Exec(ctx, `
					UPDATE inspection_slots SET booked = greatest(booked - 1, 0), updated_at = now()
					 WHERE id = $1`, oldSlot); err != nil {
					return err
				}
			}
			err = scanEnquiry(tx.QueryRow(ctx, `
				UPDATE enquiries SET slot_id = $2, scheduled_for = $3, updated_at = now()
				 WHERE id = $1
				 RETURNING id::text, tenant_id::text, listing_id::text, prospect_id::text,
				           kind, state, coalesce(message, ''), scheduled_for, created_at, '', ''`,
				enquiryID, newSlotID, out.StartsAt), &out.Enquiry)
			if err != nil {
				return err
			}

			env, err := events.New(string(domain.EventInspectionRescheduled), tenant,
				events.Subject{Kind: "enquiry", ID: enquiryID},
				events.Actor{Kind: events.ActorSystem},
				struct {
					FromSlotID string    `json:"from_slot_id,omitempty"`
					ToSlotID   string    `json:"to_slot_id"`
					StartsAt   time.Time `json:"starts_at"`
				}{oldSlot, newSlotID, out.StartsAt})
			if err != nil {
				return err
			}
			return events.Append(ctx, tx, env)
		})
	return out, alts, err
}

// CancelByProspect releases the place and closes the visit, named against the
// prospect. Platform pool: the caller holds a token, not a session.
func (s *Inspections) CancelByProspect(ctx context.Context, prospectID, enquiryID string) error {
	return tenancy.Platform(ctx, s.platform, "cancelling a prospect's viewing (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			return cancel(ctx, tx, enquiryID, prospectID, "cancelled_by_prospect")
		})
}

// CancelByOwner is the owner's cancellation, under ordinary tenancy.
func (s *Inspections) CancelByOwner(ctx context.Context, enquiryID string) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return cancel(ctx, tx, enquiryID, "", "cancelled_by_owner")
	})
}

// cancel is the shared shape: close the visit with its outcome, release the
// slot, publish the fact. prospectID narrows the prospect's own cancellation;
// empty means the owner side, where tenancy has already scoped the row.
func cancel(ctx context.Context, tx pgx.Tx, enquiryID, prospectID, outcome string) error {
	var tenant, listing, prospect, slot string
	err := tx.QueryRow(ctx, `
		UPDATE enquiries
		   SET state = 'closed', outcome = $2, outcome_at = now(), updated_at = now()
		 WHERE id = $1 AND kind IN ('inspection', 'online_inspection') AND state = 'scheduled'
		   AND ($3 = '' OR prospect_id = $3::uuid)
		 RETURNING tenant_id::text, listing_id::text, prospect_id::text,
		           coalesce(slot_id::text, '')`,
		enquiryID, outcome, prospectID).Scan(&tenant, &listing, &prospect, &slot)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotReschedulable
	}
	if err != nil {
		return err
	}
	if slot != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE inspection_slots SET booked = greatest(booked - 1, 0), updated_at = now()
			 WHERE id = $1`, slot); err != nil {
			return err
		}
	}
	env, err := events.New(string(domain.EventInspectionCancelled), tenant,
		events.Subject{Kind: "enquiry", ID: enquiryID},
		events.Actor{Kind: events.ActorSystem},
		struct {
			By         string `json:"by"`
			ListingID  string `json:"listing_id"`
			ProspectID string `json:"prospect_id"`
		}{outcome, listing, prospect})
	if err != nil {
		return err
	}
	return events.Append(ctx, tx, env)
}

// Outcome is what the agent records while still standing there (#141).
type Outcome struct {
	Outcome    string
	Objections []string
	Note       string
}

// RecordOutcome concludes a viewing with its structured result. The vocabulary
// is the schema's; a no-show is an outcome like any other, so a phantom
// viewing cannot linger as "scheduled".
func (s *Inspections) RecordOutcome(ctx context.Context, enquiryID string, o Outcome, actor events.Actor) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE enquiries
			   SET state = 'completed', outcome = $2, objections = $3,
			       outcome_note = $4, outcome_at = now(), updated_at = now()
			 WHERE id = $1 AND kind IN ('inspection', 'online_inspection')
			   AND state IN ('scheduled', 'owner_responded')`,
			enquiryID, o.Outcome, nullStrings(o.Objections), nullText(o.Note))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotReschedulable
		}
		env, err := events.New(string(domain.EventInspectionCompleted), tenant.String(),
			events.Subject{Kind: "enquiry", ID: enquiryID}, actor,
			struct {
				Outcome    string   `json:"outcome"`
				Objections []string `json:"objections,omitempty"`
			}{o.Outcome, o.Objections})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
}

// Feedback is the pattern the owner sees (#141): counts, never an individual
// prospect.
type Feedback struct {
	Completed  int
	Interested int
	NoShows    int
	Objections map[string]int
}

// ListingFeedback aggregates concluded viewings for one listing.
func (s *Inspections) ListingFeedback(ctx context.Context, listingID string) (Feedback, error) {
	out := Feedback{Objections: map[string]int{}}
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE outcome IS NOT NULL),
			       count(*) FILTER (WHERE outcome IN ('interested', 'applied')),
			       count(*) FILTER (WHERE outcome = 'prospect_no_show')
			  FROM enquiries
			 WHERE listing_id = $1 AND kind = 'inspection'`, listingID).
			Scan(&out.Completed, &out.Interested, &out.NoShows); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT objection, count(*)
			  FROM enquiries, unnest(objections) AS objection
			 WHERE listing_id = $1 AND kind = 'inspection'
			 GROUP BY objection ORDER BY count(*) DESC`, listingID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			var n int
			if err := rows.Scan(&name, &n); err != nil {
				return err
			}
			out.Objections[name] = n
		}
		return rows.Err()
	})
	return out, err
}

// DayView is the assigned side's day: every scheduled viewing, in order.
func (s *Inspections) DayView(ctx context.Context, on time.Time) ([]Booked, error) {
	var out []Booked
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, selectEnquiry+`
			 WHERE e.kind IN ('inspection', 'online_inspection') AND e.state = 'scheduled'
			   AND e.scheduled_for >= $1 AND e.scheduled_for < $1 + interval '1 day'
			 ORDER BY e.scheduled_for`, on)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b Booked
			if err := scanEnquiry(rows, &b.Enquiry); err != nil {
				return err
			}
			if b.Enquiry.ScheduledFor != nil {
				b.StartsAt = *b.Enquiry.ScheduledFor
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

const selectSlot = `
	SELECT id::text, listing_id::text, starts_at, duration_mins, capacity, booked,
	       coalesce(assigned_to, ''), coalesce(meeting_point, ''), state,
	       coalesce((SELECT sc.zone FROM inspection_schedules sc WHERE sc.id = schedule_id),
	                'Asia/Kolkata')
	  FROM inspection_slots`

func scanSlots(rows pgx.Rows, out *[]Slot) error {
	defer rows.Close()
	for rows.Next() {
		var sl Slot
		if err := rows.Scan(&sl.ID, &sl.ListingID, &sl.StartsAt, &sl.DurationMins,
			&sl.Capacity, &sl.Booked, &sl.AssignedTo, &sl.MeetingPoint, &sl.State,
			&sl.Zone); err != nil {
			return err
		}
		*out = append(*out, sl)
	}
	return rows.Err()
}

func nullStrings(ss []string) any {
	if len(ss) == 0 {
		return nil
	}
	return ss
}
