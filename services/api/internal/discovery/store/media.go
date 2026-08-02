package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Listing media (#136): membership, order and moderation. The bytes live in
// GCS; nothing here ever holds them, and a guest only ever receives a
// short-lived signed URL minted by the service for live media of a live
// listing.

// ErrNoMedia is no such photo, including one hidden by policy.
var ErrNoMedia = errors.New("media: not found")

// Media is one photo's row.
type Media struct {
	ID          string
	ListingID   string
	ObjectPath  string
	ContentType string
	Position    int
	State       string
}

// MediaStore reads and writes listing_media.
type MediaStore struct {
	pool     tenancy.Pool
	platform tenancy.PlatformPool
}

// NewMedia builds the store.
func NewMedia(p tenancy.Pool, plat tenancy.PlatformPool) *MediaStore {
	return &MediaStore{pool: p, platform: plat}
}

// Record writes the row confirming an upload landed — the documents pattern:
// the client attests after its PUT succeeds, and an upload never confirmed
// leaves nothing behind but an unreferenced object. Idempotent per object.
func (s *MediaStore) Record(ctx context.Context, listingID, objectPath, contentType string,
	position int) (string, error) {
	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var tenant string
		if err := tx.QueryRow(ctx,
			`SELECT tenant_id::text FROM listings WHERE id = $1`, listingID).Scan(&tenant); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoListing
			}
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO listing_media (tenant_id, listing_id, object_path, content_type, position)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (listing_id, object_path) DO UPDATE SET updated_at = now()
			RETURNING id::text`,
			tenant, listingID, objectPath, contentType, position).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("media: recording: %w", err)
	}
	return id, nil
}

// Reorder rewrites the positions in one statement — the cover is whatever
// lands at position zero, which makes "choose the cover" and "reorder" the
// same deliberate act (#136).
func (s *MediaStore) Reorder(ctx context.Context, listingID string, orderedIDs []string) error {
	if len(orderedIDs) == 0 {
		return nil
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE listing_media m
			   SET position = x.pos - 1, updated_at = now()
			  FROM (SELECT unnest($2::uuid[]) AS id, generate_subscripts($2::uuid[], 1) AS pos) x
			 WHERE m.id = x.id AND m.listing_id = $1 AND m.state <> 'removed'`,
			listingID, orderedIDs)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoMedia
		}
		return nil
	})
}

// Report flags a photo from the public side — anyone can pull the cord, and
// the moderation queue is where the judgement happens, not here.
func (s *MediaStore) Report(ctx context.Context, listingID, mediaID string) error {
	return tenancy.Platform(ctx, s.platform, "reporting listing media (#136)",
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				UPDATE listing_media
				   SET state = 'reported', reported_at = coalesce(reported_at, now()), updated_at = now()
				 WHERE id = $1 AND listing_id = $2 AND state = 'live'`, mediaID, listingID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				// Already reported or removed: the reporter's goal is met either
				// way, and a distinguishable answer would be an oracle.
				return nil
			}
			return nil
		})
}

// Takedown removes a photo from public delivery immediately, with the reason
// on the row and the fact on the outbox — the audited act #136 demands.
func (s *MediaStore) Takedown(ctx context.Context, listingID, mediaID, reason string) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE listing_media
			   SET state = 'removed', removed_at = now(), removed_reason = $3, updated_at = now()
			 WHERE id = $1 AND listing_id = $2 AND state <> 'removed'`,
			mediaID, listingID, reason)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoMedia
		}
		env, err := events.New("discovery.media.removed", tenant.String(),
			events.Subject{Kind: "listing_media", ID: mediaID},
			events.Actor{Kind: events.ActorSystem},
			struct {
				ListingID string `json:"listing_id"`
				Reason    string `json:"reason"`
			}{listingID, reason})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
}

// ReportedItem is one row of the moderation queue: the photo, where it hangs,
// and how long it has waited.
type ReportedItem struct {
	Media
	Headline   string
	ReportedAt string
}

// ReviewQueue is the moderation queue (#143): every reported photo on the
// organisation's listings, oldest report first — the one the reporter has
// been waiting on longest is the one reviewed next.
func (s *MediaStore) ReviewQueue(ctx context.Context) ([]ReportedItem, error) {
	var out []ReportedItem
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT m.id::text, m.listing_id::text, m.object_path, m.content_type,
			       m.position, m.state, coalesce(l.headline, ''),
			       to_char(m.reported_at, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
			  FROM listing_media m
			  LEFT JOIN listings l ON l.id = m.listing_id
			 WHERE m.tenant_id = current_tenant_id() AND m.state = 'reported'
			 ORDER BY m.reported_at
			 LIMIT 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it ReportedItem
			if err := rows.Scan(&it.ID, &it.ListingID, &it.ObjectPath, &it.ContentType,
				&it.Position, &it.State, &it.Headline, &it.ReportedAt); err != nil {
				return err
			}
			out = append(out, it)
		}
		return rows.Err()
	})
	return out, err
}

// Clear returns a reported photo to live — the reviewer's judgement that the
// report was wrong, as deliberate as the takedown.
func (s *MediaStore) Clear(ctx context.Context, listingID, mediaID string) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE listing_media SET state = 'live', updated_at = now()
			 WHERE id = $1 AND listing_id = $2 AND state = 'reported'`, mediaID, listingID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoMedia
		}
		return nil
	})
}

// ForListing is the owner's view: everything not removed, in order.
func (s *MediaStore) ForListing(ctx context.Context, listingID string) ([]Media, error) {
	var out []Media
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.list(ctx, tx, listingID, false, &out)
	})
	return out, err
}

// PublicForListing is what a stranger may see: live media of a live,
// published listing — both conditions checked here, in one place, however
// the caller got the listing id.
func (s *MediaStore) PublicForListing(ctx context.Context, listingID string) ([]Media, error) {
	var out []Media
	err := tenancy.Platform(ctx, s.platform, "minting public photo URLs (#136)",
		func(ctx context.Context, tx pgx.Tx) error {
			var live bool
			err := tx.QueryRow(ctx, `
				SELECT state = 'live' AND published_at IS NOT NULL
				  FROM listings WHERE id = $1`, listingID).Scan(&live)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && !live) {
				return nil // an unpublished listing's photos are as private as the flat
			}
			if err != nil {
				return err
			}
			return s.list(ctx, tx, listingID, true, &out)
		})
	return out, err
}

func (s *MediaStore) list(ctx context.Context, tx pgx.Tx, listingID string, liveOnly bool,
	out *[]Media) error {
	guard := `state <> 'removed'`
	if liveOnly {
		guard = `state = 'live'`
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, listing_id::text, object_path, content_type, position, state
		  FROM listing_media
		 WHERE listing_id = $1 AND `+guard+`
		 ORDER BY position, created_at`, listingID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.ListingID, &m.ObjectPath, &m.ContentType,
			&m.Position, &m.State); err != nil {
			return err
		}
		*out = append(*out, m)
	}
	return rows.Err()
}
