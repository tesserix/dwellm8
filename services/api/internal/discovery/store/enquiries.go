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

// ErrNoEnquiry is no such enquiry, including one hidden by policy.
var ErrNoEnquiry = errors.New("enquiry: no such enquiry")

// ErrListingNotLive is an enquiry against a listing that is not being
// advertised — answered honestly rather than queued into silence (#137).
var ErrListingNotLive = errors.New("enquiry: the listing is no longer available")

// Enquiries is the enquiry store. Two pools: creation comes from an anonymous
// prospect and runs on the platform pool; everything the owner does runs on
// the request pool under ordinary tenancy.
type Enquiries struct {
	pool     tenancy.Pool
	platform tenancy.PlatformPool
}

// NewEnquiries builds the store.
func NewEnquiries(p tenancy.Pool, plat tenancy.PlatformPool) *Enquiries {
	return &Enquiries{pool: p, platform: plat}
}

// Enquiry is a row as the owner's pipeline sees it.
type Enquiry struct {
	ID           string
	TenantID     string
	ListingID    string
	ProspectID   string
	Kind         string
	State        string
	Message      string
	ScheduledFor *time.Time
	CreatedAt    time.Time

	// Denormalised for the pipeline view.
	Headline string
	UnitID   string
	// ContactMasked is filled by the service from the prospect store; the
	// enquiries table deliberately does not hold it.
	ContactMasked string
}

// Create writes the enquiry from a verified prospect and publishes
// discovery.enquiry.received in the same transaction.
//
// Platform pool, because the caller is a stranger: the row belongs to the
// listing's organisation, which is read here rather than trusted from input.
// The database trigger re-checks the verification — defence in depth against
// this method being called with an unverified prospect id.
//
// Idempotent per (prospect, listing, kind) while the enquiry is open: the same
// person tapping twice is one enquiry, not spam to the owner.
func (s *Enquiries) Create(ctx context.Context, listingID, prospectID, kind, message string,
	scheduledFor *time.Time) (Enquiry, error) {
	var e Enquiry
	err := tenancy.Platform(ctx, s.platform, "recording a prospect's enquiry (ADR-0019)",
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

			// The open-enquiry dedupe.
			err = scanEnquiry(tx.QueryRow(ctx, selectEnquiry+`
				 WHERE e.prospect_id = $1 AND e.listing_id = $2 AND e.kind = $3
				   AND e.state IN ('new', 'owner_responded', 'scheduled')
				 ORDER BY e.created_at DESC LIMIT 1`, prospectID, listingID, kind), &e)
			if err == nil {
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}

			err = scanEnquiry(tx.QueryRow(ctx, `
				INSERT INTO enquiries (tenant_id, listing_id, prospect_id, kind, message, scheduled_for)
				VALUES ($1, $2, $3, $4, $5, $6)
				RETURNING id::text, tenant_id::text, listing_id::text, prospect_id::text,
				          kind, state, coalesce(message, ''), scheduled_for, created_at,
				          '', ''`,
				tenant, listingID, prospectID, kind, nullText(message), scheduledFor), &e)
			if err != nil {
				return err
			}

			env, err := events.New(string(domain.EventEnquiryReceived), tenant,
				events.Subject{Kind: "enquiry", ID: e.ID},
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
	if isCheckViolation(err) {
		return Enquiry{}, ErrNotVerified
	}
	return e, err
}

// ForProspect lists a prospect's own enquiries — their timeline, on the
// platform pool because the caller is the prospect.
func (s *Enquiries) ForProspect(ctx context.Context, prospectID string) ([]Enquiry, error) {
	var out []Enquiry
	err := tenancy.Platform(ctx, s.platform, "reading a prospect's own enquiries (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			return s.list(ctx, tx, &out, ` WHERE e.prospect_id = $1 ORDER BY e.created_at DESC`, prospectID)
		})
	return out, err
}

// ForOwner lists the organisation's pipeline, under ordinary tenancy.
func (s *Enquiries) ForOwner(ctx context.Context, state string) ([]Enquiry, error) {
	var out []Enquiry
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if state != "" {
			return s.list(ctx, tx, &out,
				` WHERE e.tenant_id = current_tenant_id() AND e.state = $1 ORDER BY e.created_at DESC`, state)
		}
		return s.list(ctx, tx, &out,
			` WHERE e.tenant_id = current_tenant_id() ORDER BY e.created_at DESC`)
	})
	return out, err
}

// Get reads one enquiry under ordinary tenancy — the owner's view.
func (s *Enquiries) Get(ctx context.Context, id string) (Enquiry, error) {
	var e Enquiry
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return scanEnquiry(tx.QueryRow(ctx, selectEnquiry+` WHERE e.id = $1`, id), &e)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Enquiry{}, ErrNoEnquiry
	}
	return e, err
}

// Move advances an enquiry's state and publishes the responded event when the
// move is the owner's first engagement.
func (s *Enquiries) Move(ctx context.Context, id, to string, actor events.Actor) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var from string
		err := tx.QueryRow(ctx, `SELECT state FROM enquiries WHERE id = $1 FOR UPDATE`, id).Scan(&from)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoEnquiry
		}
		if err != nil {
			return err
		}
		if !enquiryMoves[from][to] {
			return fmt.Errorf("%w: an enquiry in %s cannot become %s", ErrNoEnquiry, from, to)
		}
		if _, err := tx.Exec(ctx,
			`UPDATE enquiries SET state = $2, updated_at = now() WHERE id = $1`, id, to); err != nil {
			return err
		}
		if to != "owner_responded" {
			return nil
		}
		env, err := events.New(string(domain.EventEnquiryResponded), tenant.String(),
			events.Subject{Kind: "enquiry", ID: id}, actor, struct{}{})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
}

// enquiryMoves is the pipeline: forward only, and spam is reachable from
// anywhere open because that judgement can arrive late.
var enquiryMoves = map[string]map[string]bool{
	"new":             {"owner_responded": true, "closed": true, "spam": true},
	"owner_responded": {"scheduled": true, "completed": true, "closed": true, "spam": true},
	"scheduled":       {"completed": true, "closed": true, "spam": true},
	"completed":       {"closed": true},
}

// Bridge is a masked connection between the two sides (#138).
type Bridge struct {
	ID          string
	EnquiryID   string
	Provider    string
	ProviderRef string
	ProxyMasked string
	ExpiresAt   time.Time
}

// OpenBridge records the provider's masked connection for an enquiry. The
// database trigger refuses it while the enquiry is unanswered: an unanswered
// enquiry is one side, not two.
func (s *Enquiries) OpenBridge(ctx context.Context, b Bridge) (string, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return "", tenancy.ErrNoTenant
	}
	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO contact_bridges (tenant_id, enquiry_id, provider, provider_ref,
			                             proxy_masked, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (enquiry_id) DO UPDATE SET expires_at = EXCLUDED.expires_at
			RETURNING id::text`,
			tenant.String(), b.EnquiryID, b.Provider, b.ProviderRef, b.ProxyMasked, b.ExpiresAt).
			Scan(&id)
	})
	if isCheckViolation(err) {
		return "", fmt.Errorf("%w: respond to the enquiry first", ErrNoEnquiry)
	}
	return id, err
}

const selectEnquiry = `
	SELECT e.id::text, e.tenant_id::text, e.listing_id::text, e.prospect_id::text,
	       e.kind, e.state, coalesce(e.message, ''), e.scheduled_for, e.created_at,
	       coalesce(l.headline, ''), coalesce(l.unit_id::text, '')
	  FROM enquiries e
	  LEFT JOIN listings l ON l.id = e.listing_id`

func (s *Enquiries) list(ctx context.Context, tx pgx.Tx, out *[]Enquiry, tail string, args ...any) error {
	rows, err := tx.Query(ctx, selectEnquiry+tail+` LIMIT 500`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var e Enquiry
		if err := scanEnquiry(rows, &e); err != nil {
			return err
		}
		*out = append(*out, e)
	}
	return rows.Err()
}

func scanEnquiry(row scannable, e *Enquiry) error {
	return row.Scan(&e.ID, &e.TenantID, &e.ListingID, &e.ProspectID,
		&e.Kind, &e.State, &e.Message, &e.ScheduledFor, &e.CreatedAt,
		&e.Headline, &e.UnitID)
}

func isCheckViolation(err error) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return false
	}
	return pg.Code == "23514" || strings.Contains(pg.Message, "verified")
}
