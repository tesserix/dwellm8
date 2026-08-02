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
