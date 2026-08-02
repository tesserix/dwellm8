package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Prospect push tokens (#126). The token is the key: a device that a new
// prospect signs into simply moves, and a token Expo declares dead is
// disabled rather than retried forever.

// PushTokens reads and writes prospect_push_tokens on the platform pool.
type PushTokens struct{ pool tenancy.PlatformPool }

// NewPushTokens builds the store.
func NewPushTokens(p tenancy.PlatformPool) *PushTokens { return &PushTokens{pool: p} }

// Register upserts the token against the prospect, re-enabling it if a
// previous owner's copy had been disabled.
func (s *PushTokens) Register(ctx context.Context, prospectID, token, platform string) error {
	return tenancy.Platform(ctx, s.pool, "registering a push token (#126)",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO prospect_push_tokens (expo_token, prospect_id, platform)
				VALUES ($1, $2, $3)
				ON CONFLICT (expo_token) DO UPDATE
				   SET prospect_id = EXCLUDED.prospect_id, platform = EXCLUDED.platform,
				       disabled_at = NULL, updated_at = now()`,
				token, prospectID, platform)
			return err
		})
}

// Drop removes the prospect's own registration — sign-out, or a declined
// permission the app reports honestly.
func (s *PushTokens) Drop(ctx context.Context, prospectID, token string) error {
	return tenancy.Platform(ctx, s.pool, "dropping a push token (#126)",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				DELETE FROM prospect_push_tokens
				 WHERE expo_token = $1 AND prospect_id = $2`, token, prospectID)
			return err
		})
}

// Disable marks tokens Expo declared dead. Not a delete: the row is the
// record of why this prospect stopped hearing from us.
func (s *PushTokens) Disable(ctx context.Context, tokens []string) error {
	if len(tokens) == 0 {
		return nil
	}
	return tenancy.Platform(ctx, s.pool, "disabling dead push tokens (#126)",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				UPDATE prospect_push_tokens SET disabled_at = now(), updated_at = now()
				 WHERE expo_token = ANY($1) AND disabled_at IS NULL`, tokens)
			return err
		})
}
