package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Applications (#142): the formal step between enquiry and lease. The schema
// owns the two hard rules — one open application per prospect per listing,
// one accepted per unit — and this store surfaces them as named refusals.

// ErrNoApplication is no such application, including one hidden by policy.
var ErrNoApplication = errors.New("application: not found")

// ErrAlreadyAccepted is the story's failure scenario: a second acceptance for
// a unit, blocked naming the first.
var ErrAlreadyAccepted = errors.New("application: another application is already accepted for this unit")

// ErrNotDecidable is a decision on an application not open to one.
var ErrNotDecidable = errors.New("application: not open to a decision")

// declinedRetention is #213's working default until the matrix lands.
const declinedRetention = 180 * 24 * time.Hour

// Application is one row.
type Application struct {
	ID            string
	ListingID     string
	PropertyID    string
	UnitID        string
	ProspectID    string
	State         string
	MoveIn        time.Time
	TermMonths    int
	OfferMinor    *int64
	Note          string
	DeclineReason string
	LeaseID       string
	CreatedAt     time.Time
	// Headline and ContactMasked are the review view's, filled by joins and
	// the service respectively.
	Headline      string
	ContactMasked string
}

// Applications is the store.
type Applications struct {
	pool     tenancy.Pool
	platform tenancy.PlatformPool
}

// NewApplications builds the store.
func NewApplications(p tenancy.Pool, plat tenancy.PlatformPool) *Applications {
	return &Applications{pool: p, platform: plat}
}

// Apply files an application from a verified prospect against a live listing.
// Idempotent while open: applying twice is the same application.
func (s *Applications) Apply(ctx context.Context, listingID, prospectID string, moveIn time.Time,
	termMonths int, offerMinor *int64, note string) (Application, error) {
	var a Application
	err := tenancy.Platform(ctx, s.platform, "filing a prospect's application (#142)",
		func(ctx context.Context, tx pgx.Tx) error {
			var tenant, property, unit, state string
			err := tx.QueryRow(ctx, `
				SELECT tenant_id::text, property_id::text, unit_id::text, state
				  FROM listings WHERE id = $1 AND published_at IS NOT NULL`, listingID).
				Scan(&tenant, &property, &unit, &state)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoListing
			}
			if err != nil {
				return err
			}
			if state != "live" {
				return ErrListingNotLive
			}

			err = scanApplication(tx.QueryRow(ctx, selectApplication+`
				 WHERE a.prospect_id = $1 AND a.listing_id = $2
				   AND a.state IN ('submitted', 'under_review')
				 LIMIT 1`, prospectID, listingID), &a)
			if err == nil {
				return nil // the open application they already have
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}

			if termMonths == 0 {
				termMonths = 11
			}
			err = scanApplication(tx.QueryRow(ctx, `
				INSERT INTO listing_applications (tenant_id, listing_id, property_id, unit_id,
				                                  prospect_id, move_in, term_months, offer_minor, note)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				RETURNING id::text, listing_id::text, property_id::text, unit_id::text,
				          prospect_id::text, state, move_in, term_months, offer_minor,
				          coalesce(note, ''), coalesce(decline_reason, ''),
				          coalesce(lease_id::text, ''), created_at, ''`,
				tenant, listingID, property, unit, prospectID, moveIn, termMonths,
				offerMinor, nullText(note)), &a)
			if err != nil {
				return err
			}
			env, err := events.New("discovery.application.submitted", tenant,
				events.Subject{Kind: "application", ID: a.ID},
				events.Actor{Kind: events.ActorSystem},
				struct {
					ListingID string `json:"listing_id"`
					MoveIn    string `json:"move_in"`
				}{listingID, moveIn.Format("2006-01-02")})
			if err != nil {
				return err
			}
			return events.Append(ctx, tx, env)
		})
	if isCheckViolation(err) {
		return Application{}, ErrNotVerified
	}
	return a, err
}

// Withdraw is the applicant's own exit, while the application is open.
func (s *Applications) Withdraw(ctx context.Context, prospectID, applicationID string) error {
	return tenancy.Platform(ctx, s.platform, "withdrawing a prospect's application (#142)",
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				UPDATE listing_applications
				   SET state = 'withdrawn', updated_at = now()
				 WHERE id = $1 AND prospect_id = $2 AND state IN ('submitted', 'under_review')`,
				applicationID, prospectID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNoApplication
			}
			return nil
		})
}

// ForProspect is the applicant's own timeline.
func (s *Applications) ForProspect(ctx context.Context, prospectID string) ([]Application, error) {
	var out []Application
	err := tenancy.Platform(ctx, s.platform, "reading a prospect's applications (#142)",
		func(ctx context.Context, tx pgx.Tx) error {
			return s.list(ctx, tx, &out,
				` WHERE a.prospect_id = $1 ORDER BY a.created_at DESC`, prospectID)
		})
	return out, err
}

// ForOwner is the review queue, under ordinary tenancy.
func (s *Applications) ForOwner(ctx context.Context, state string) ([]Application, error) {
	var out []Application
	// No tenant predicate: RLS is the boundary, and repeating it here hid the
	// applications a firm reaches through its mandate (#259).
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if state != "" {
			return s.list(ctx, tx, &out, ` WHERE a.state = $1 ORDER BY a.created_at`, state)
		}
		return s.list(ctx, tx, &out, ` ORDER BY a.created_at`)
	})
	return out, err
}

// Get is one application, the owner's view.
func (s *Applications) Get(ctx context.Context, id string) (Application, error) {
	var a Application
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return scanApplication(tx.QueryRow(ctx, selectApplication+` WHERE a.id = $1`, id), &a)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Application{}, ErrNoApplication
	}
	return a, err
}

// Review moves a submitted application under review.
func (s *Applications) Review(ctx context.Context, id string) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE listing_applications SET state = 'under_review', updated_at = now()
			 WHERE id = $1 AND state = 'submitted'`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotDecidable
		}
		return nil
	})
}

// Accept records the acceptance and the lease it minted, in one transaction —
// the caller creates the draft first and hands its id in, so an acceptance
// with no lease cannot exist (the schema says the same thing again).
func (s *Applications) Accept(ctx context.Context, id, leaseID string) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE listing_applications
			   SET state = 'accepted', lease_id = $2, decided_at = now(), updated_at = now()
			 WHERE id = $1 AND state IN ('submitted', 'under_review')`, id, leaseID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotDecidable
		}
		env, err := events.New("discovery.application.accepted", tenant.String(),
			events.Subject{Kind: "application", ID: id},
			events.Actor{Kind: events.ActorSystem},
			struct {
				LeaseID string `json:"lease_id"`
			}{leaseID})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
	if isUniqueViolation(err, "applications_one_accepted_per_unit") {
		first, _ := s.acceptedOnSameUnit(ctx, id)
		if first != "" {
			return fmt.Errorf("%w: application %s — decline it explicitly first", ErrAlreadyAccepted, first)
		}
		return ErrAlreadyAccepted
	}
	return err
}

// Decline records the decision, its reason, and the retention clock.
func (s *Applications) Decline(ctx context.Context, id, reason string) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE listing_applications
			   SET state = 'declined', decline_reason = $2, decided_at = now(),
			       retain_until = (now() + $3::interval)::date, updated_at = now()
			 WHERE id = $1 AND state IN ('submitted', 'under_review', 'accepted')`,
			id, reason, declinedRetention.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotDecidable
		}
		env, err := events.New("discovery.application.declined", tenant.String(),
			events.Subject{Kind: "application", ID: id},
			events.Actor{Kind: events.ActorSystem},
			struct {
				Reason string `json:"reason"`
			}{reason})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
}

// SweepRetention deletes declined applications whose clock has passed — the
// retention promise (#213's working default), platform-side on the poll
// job's heartbeat. The DELETE policy is the second signature on this act.
func (s *Applications) SweepRetention(ctx context.Context) (int, error) {
	var n int
	err := tenancy.Platform(ctx, s.platform, "purging declined applications past retention (#142/#213)",
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				DELETE FROM listing_applications
				 WHERE state = 'declined' AND retain_until IS NOT NULL
				   AND retain_until <= CURRENT_DATE`)
			if err != nil {
				return err
			}
			n = int(tag.RowsAffected())
			return nil
		})
	return n, err
}

// acceptedOnSameUnit names the acceptance that blocked this one.
func (s *Applications) acceptedOnSameUnit(ctx context.Context, id string) (string, error) {
	var first string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT other.id::text
			  FROM listing_applications mine
			  JOIN listing_applications other
			    ON other.unit_id = mine.unit_id AND other.state = 'accepted' AND other.id <> mine.id
			 WHERE mine.id = $1 LIMIT 1`, id).Scan(&first)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return first, err
}

const applicationColumns = `
	a.id::text, a.listing_id::text, a.property_id::text, a.unit_id::text,
	a.prospect_id::text, a.state, a.move_in, a.term_months, a.offer_minor,
	coalesce(a.note, ''), coalesce(a.decline_reason, ''),
	coalesce(a.lease_id::text, ''), a.created_at`

const selectApplication = `
	SELECT ` + applicationColumns + `, coalesce(l.headline, '')
	  FROM listing_applications a
	  LEFT JOIN listings l ON l.id = a.listing_id`

func scanApplication(row scannable, a *Application) error {
	return row.Scan(&a.ID, &a.ListingID, &a.PropertyID, &a.UnitID, &a.ProspectID,
		&a.State, &a.MoveIn, &a.TermMonths, &a.OfferMinor, &a.Note,
		&a.DeclineReason, &a.LeaseID, &a.CreatedAt, &a.Headline)
}

func (s *Applications) list(ctx context.Context, tx pgx.Tx, out *[]Application,
	tail string, args ...any) error {
	rows, err := tx.Query(ctx, selectApplication+tail+` LIMIT 500`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a Application
		if err := scanApplication(rows, &a); err != nil {
			return err
		}
		*out = append(*out, a)
	}
	return rows.Err()
}
