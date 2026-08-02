package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Saved searches (#144). A prospect's own criteria, held platform-side like
// the shortlist, with a last-seen watermark so a fresh match is counted once
// — the no-resend rule enforced by arithmetic rather than by memory.

// ErrNoSearch is no such saved search for this prospect.
var ErrNoSearch = errors.New("search: not found")

// SavedSearch is one row, with the fresh-match count computed on read.
type SavedSearch struct {
	ID            string
	City          string
	Locality      string
	MaxRentMinor  int64
	Bedrooms      int
	AlertsEnabled bool
	LastSeenAt    time.Time
	CreatedAt     time.Time
	// NewMatches is how many live listings matching the criteria were
	// published after LastSeenAt.
	NewMatches int
}

// Searches reads and writes saved_searches on the platform pool.
type Searches struct{ pool tenancy.PlatformPool }

// NewSearches builds the store.
func NewSearches(p tenancy.PlatformPool) *Searches { return &Searches{pool: p} }

// Save records the criteria. The same criteria saved twice is the same
// search, returned again with its watermark intact.
func (s *Searches) Save(ctx context.Context, prospectID, city, locality string,
	maxRentMinor int64, bedrooms int) (string, error) {
	var id string
	err := tenancy.Platform(ctx, s.pool, "saving a search (#144)",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO saved_searches (prospect_id, city, locality, max_rent_minor, bedrooms)
				VALUES ($1, $2, nullif($3, ''), nullif($4, 0), nullif($5, -1))
				ON CONFLICT (prospect_id, city, coalesce(locality, ''),
				             coalesce(max_rent_minor, 0), coalesce(bedrooms, -1))
				DO UPDATE SET alerts_enabled = saved_searches.alerts_enabled
				RETURNING id::text`,
				prospectID, city, locality, maxRentMinor, normBedrooms(bedrooms)).Scan(&id)
		})
	return id, err
}

// Mine lists the prospect's searches, each with its fresh-match count.
func (s *Searches) Mine(ctx context.Context, prospectID string) ([]SavedSearch, error) {
	var out []SavedSearch
	err := tenancy.Platform(ctx, s.pool, "reading saved searches (#144)",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT ss.id::text, ss.city, coalesce(ss.locality, ''),
				       coalesce(ss.max_rent_minor, 0), coalesce(ss.bedrooms, -1),
				       ss.alerts_enabled, ss.last_seen_at, ss.created_at,
				       (SELECT count(*) FROM listings l
				         WHERE l.state = 'live' AND l.published_at > ss.last_seen_at
				           AND l.city = ss.city
				           AND (ss.locality IS NULL OR l.locality = ss.locality)
				           AND (ss.max_rent_minor IS NULL OR l.rent_minor <= ss.max_rent_minor)
				           AND (ss.bedrooms IS NULL OR l.bedrooms = ss.bedrooms))
				  FROM saved_searches ss
				 WHERE ss.prospect_id = $1
				 ORDER BY ss.created_at`, prospectID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var r SavedSearch
				if err := rows.Scan(&r.ID, &r.City, &r.Locality, &r.MaxRentMinor,
					&r.Bedrooms, &r.AlertsEnabled, &r.LastSeenAt, &r.CreatedAt,
					&r.NewMatches); err != nil {
					return err
				}
				out = append(out, r)
			}
			return rows.Err()
		})
	return out, err
}

// AlertTarget is one push to send: the search that matched and where to send.
type AlertTarget struct {
	SearchID   string
	ProspectID string
	Token      string
}

// AlertListing is what the notification says: the listing's own public face.
type AlertListing struct {
	ID       string
	Headline string
	Locality string
	City     string
	Rent     int64
}

// AlertsForListing finds every opted-in search a just-published listing
// satisfies, advances each one's alert watermark to that listing's
// publication moment, and returns the live tokens to push to. The watermark
// is what makes a NATS redelivery a no-op: the second delivery finds nothing
// newer than it — and it advances to the listing's own published_at rather
// than now(), so an event for a sibling listing still queued is not silently
// swallowed. A listing no longer live answers no targets: the alert would
// link to a page that 404s.
func (s *Searches) AlertsForListing(ctx context.Context, listingID string) (AlertListing, []AlertTarget, error) {
	var l AlertListing
	var out []AlertTarget
	err := tenancy.Platform(ctx, s.pool, "matching saved-search alerts (#144/#126)",
		func(ctx context.Context, tx pgx.Tx) error {
			var bedrooms int
			var published time.Time
			err := tx.QueryRow(ctx, `
				SELECT id::text, headline, locality, city, rent_minor,
				       coalesce(bedrooms, -1), published_at
				  FROM listings
				 WHERE id = $1 AND state = 'live' AND published_at IS NOT NULL`,
				listingID).Scan(&l.ID, &l.Headline, &l.Locality, &l.City, &l.Rent,
				&bedrooms, &published)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}

			rows, err := tx.Query(ctx, `
				WITH stamped AS (
					UPDATE saved_searches ss
					   SET last_alerted_at = GREATEST(ss.last_alerted_at, $5)
					 WHERE ss.alerts_enabled
					   AND ss.city = $1
					   AND (ss.locality IS NULL OR ss.locality = $2)
					   AND (ss.max_rent_minor IS NULL OR $3 <= ss.max_rent_minor)
					   AND (ss.bedrooms IS NULL OR ss.bedrooms = $4)
					   AND $5 > ss.last_alerted_at
					 RETURNING ss.id, ss.prospect_id
				)
				SELECT s.id::text, s.prospect_id::text, t.expo_token
				  FROM stamped s
				  JOIN prospect_push_tokens t
				    ON t.prospect_id = s.prospect_id AND t.disabled_at IS NULL`,
				l.City, l.Locality, l.Rent, bedrooms, published)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var a AlertTarget
				if err := rows.Scan(&a.SearchID, &a.ProspectID, &a.Token); err != nil {
					return err
				}
				out = append(out, a)
			}
			return rows.Err()
		})
	return l, out, err
}

// Seen advances the watermark: everything current is no longer news.
func (s *Searches) Seen(ctx context.Context, prospectID, id string) error {
	return s.exec(ctx, "marking a search seen (#144)", `
		UPDATE saved_searches SET last_seen_at = now()
		 WHERE id = $1 AND prospect_id = $2`, id, prospectID)
}

// SetAlerts flips alerting; the search itself is retained either way — the
// opt-out promise is about sending, not about forgetting.
func (s *Searches) SetAlerts(ctx context.Context, prospectID, id string, enabled bool) error {
	return s.exec(ctx, "setting search alerts (#144)", `
		UPDATE saved_searches SET alerts_enabled = $3
		 WHERE id = $1 AND prospect_id = $2`, id, prospectID, enabled)
}

// Delete removes the prospect's own search.
func (s *Searches) Delete(ctx context.Context, prospectID, id string) error {
	return s.exec(ctx, "deleting a saved search (#144)", `
		DELETE FROM saved_searches WHERE id = $1 AND prospect_id = $2`, id, prospectID)
}

func (s *Searches) exec(ctx context.Context, reason, sql string, args ...any) error {
	return tenancy.Platform(ctx, s.pool, reason,
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, sql, args...)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNoSearch
			}
			return nil
		})
}

// normBedrooms maps "not filtering" to the sentinel the dedupe index uses.
func normBedrooms(b int) int {
	if b <= 0 {
		return -1
	}
	return b
}
