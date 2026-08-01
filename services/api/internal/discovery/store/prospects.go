package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ErrNoProspect is an unknown or expired token. One answer whatever the cause:
// a distinguishable refusal is an oracle over other people's tokens.
var ErrNoProspect = errors.New("prospect: no such prospect")

// ErrNotVerified is a prospect trying to make contact before verifying a phone
// number — the one thing standing between an owner and a thousand fake
// enquiries.
var ErrNotVerified = errors.New("prospect: not verified")

// Prospects is the prospect store. It takes the platform pool because a
// prospect belongs to nobody: the rows are the platform's, admitted by policy
// only to a platform session, and reached only by presenting a token.
type Prospects struct{ pool tenancy.PlatformPool }

// NewProspects builds the store on the platform pool.
func NewProspects(p tenancy.PlatformPool) *Prospects { return &Prospects{pool: p} }

// Prospect is a browsing stranger's record.
type Prospect struct {
	ID            string
	Verified      bool
	ContactRef    string
	ContactMasked string
	ConvertedTo   string
	CreatedAt     time.Time
}

// Ensure finds or creates the prospect a token hash names, touching
// last_seen_at either way. The token itself never reaches this store — the
// service hashes it, so a database copy holds nobody's browsing key.
func (s *Prospects) Ensure(ctx context.Context, tokenHash []byte) (Prospect, error) {
	var p Prospect
	err := tenancy.Platform(ctx, s.pool, "resolving a prospect token (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			return scanProspect(tx.QueryRow(ctx, `
				INSERT INTO prospects (token_hash)
				VALUES ($1)
				ON CONFLICT (token_hash) DO UPDATE SET last_seen_at = now()
				RETURNING id::text, verified_at, coalesce(contact_ref, ''),
				          coalesce(contact_masked, ''), coalesce(converted_party_id::text, ''),
				          created_at`, tokenHash), &p)
		})
	return p, err
}

// ByToken resolves a token hash without creating anything.
func (s *Prospects) ByToken(ctx context.Context, tokenHash []byte) (Prospect, error) {
	var p Prospect
	err := tenancy.Platform(ctx, s.pool, "resolving a prospect token (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			return scanProspect(tx.QueryRow(ctx, `
				UPDATE prospects SET last_seen_at = now()
				 WHERE token_hash = $1
				 RETURNING id::text, verified_at, coalesce(contact_ref, ''),
				           coalesce(contact_masked, ''), coalesce(converted_party_id::text, ''),
				           created_at`, tokenHash), &p)
		})
	if errors.Is(err, pgx.ErrNoRows) {
		return Prospect{}, ErrNoProspect
	}
	return p, err
}

// Verify records the verification package: the timestamp, the masked-calling
// provider's reference and the masked display form arrive together or not at
// all — the constraint on the table says the same thing again.
func (s *Prospects) Verify(ctx context.Context, tokenHash []byte, contactRef, contactMasked string) error {
	return tenancy.Platform(ctx, s.pool, "verifying a prospect's phone (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				UPDATE prospects
				   SET verified_at = coalesce(verified_at, now()),
				       contact_ref = $2, contact_masked = $3, last_seen_at = now()
				 WHERE token_hash = $1`, tokenHash, contactRef, contactMasked)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNoProspect
			}
			return nil
		})
}

// MarkConverted points the prospect at the party they became. The row is kept,
// not replaced — that is what carries a shortlist across sign-up.
func (s *Prospects) MarkConverted(ctx context.Context, prospectID, partyID string) error {
	return tenancy.Platform(ctx, s.pool, "recording a prospect's conversion (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				UPDATE prospects SET converted_party_id = $2, converted_at = coalesce(converted_at, now())
				 WHERE id = $1 AND verified_at IS NOT NULL`, prospectID, partyID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNotVerified
			}
			return nil
		})
}

// ShortlistAdd saves a listing to the prospect's shortlist, idempotently.
func (s *Prospects) ShortlistAdd(ctx context.Context, prospectID, listingID string) error {
	return tenancy.Platform(ctx, s.pool, "updating a prospect's shortlist (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO prospect_shortlist (prospect_id, listing_id)
				VALUES ($1, $2) ON CONFLICT DO NOTHING`, prospectID, listingID)
			return err
		})
}

// ShortlistRemove takes one off.
func (s *Prospects) ShortlistRemove(ctx context.Context, prospectID, listingID string) error {
	return tenancy.Platform(ctx, s.pool, "updating a prospect's shortlist (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				DELETE FROM prospect_shortlist WHERE prospect_id = $1 AND listing_id = $2`,
				prospectID, listingID)
			return err
		})
}

// ShortlistItem is a saved listing with enough of its current truth to be
// honest: a shortlisted flat that has been let says so.
type ShortlistItem struct {
	ListingID string
	Headline  string
	Locality  string
	City      string
	RentMinor int64
	State     string
	AddedAt   time.Time
}

// Shortlist lists the prospect's saved listings, newest first.
func (s *Prospects) Shortlist(ctx context.Context, prospectID string) ([]ShortlistItem, error) {
	var out []ShortlistItem
	err := tenancy.Platform(ctx, s.pool, "reading a prospect's shortlist (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT l.id::text, l.headline, l.locality, l.city, l.rent_minor, l.state, sl.added_at
				  FROM prospect_shortlist sl
				  JOIN listings l ON l.id = sl.listing_id
				 WHERE sl.prospect_id = $1
				 ORDER BY sl.added_at DESC`, prospectID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var it ShortlistItem
				if err := rows.Scan(&it.ListingID, &it.Headline, &it.Locality, &it.City,
					&it.RentMinor, &it.State, &it.AddedAt); err != nil {
					return err
				}
				out = append(out, it)
			}
			return rows.Err()
		})
	return out, err
}

// MaskedContacts returns the masked display form per prospect id, for showing
// an owner who enquired. The raw number is not in this database at all; the
// masked form is the only thing there is to show.
func (s *Prospects) MaskedContacts(ctx context.Context, prospectIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(prospectIDs) == 0 {
		return out, nil
	}
	err := tenancy.Platform(ctx, s.pool, "showing enquirers' masked contacts to the listing owner (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT id::text, coalesce(contact_masked, '') FROM prospects WHERE id = ANY($1)`,
				prospectIDs)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id, masked string
				if err := rows.Scan(&id, &masked); err != nil {
					return err
				}
				out[id] = masked
			}
			return rows.Err()
		})
	return out, err
}

func scanProspect(row scannable, p *Prospect) error {
	var verifiedAt *time.Time
	if err := row.Scan(&p.ID, &verifiedAt, &p.ContactRef, &p.ContactMasked,
		&p.ConvertedTo, &p.CreatedAt); err != nil {
		return err
	}
	p.Verified = verifiedAt != nil
	return nil
}
