package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/identity/domain/verification"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ErrNoCodePending is asked-to-confirm with nothing in flight.
var ErrNoCodePending = errors.New("identity: no code was sent")

// IssueEmailCode draws a code, stores its hash and returns the code — the only
// moment it exists in the clear. The caller mails it and forgets it.
//
// Issuing supersedes whatever was in flight: the person is looking at the
// newest email, so an older code left alive would keep a forwarded message
// working as a key.
func (s *Principals) IssueEmailCode(ctx context.Context, p auth.Principal, email string) (string, error) {
	code, err := verification.NewCode()
	if err != nil {
		return "", err
	}
	hash := verification.Hash(string(p.Surface), p.UID, code)

	err = tenancy.Platform(ctx, s.platform, "issuing an email code",
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				UPDATE identity_email_codes
				   SET consumed_at = now()
				 WHERE surface = $1 AND gip_uid = $2 AND consumed_at IS NULL`,
				string(p.Surface), p.UID); err != nil {
				return fmt.Errorf("identity: retiring the previous code: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO identity_email_codes (surface, gip_uid, email, code_hash, expires_at)
				VALUES ($1, $2, $3, $4, now() + $5::interval)`,
				string(p.Surface), p.UID, email, hash, verification.TTL.String()); err != nil {
				return fmt.Errorf("identity: writing the code: %w", err)
			}
			return nil
		})
	if err != nil {
		return "", err
	}
	return code, nil
}

// PendingEmailCode is the code in flight, for a caller deciding whether a
// resend is allowed.
func (s *Principals) PendingEmailCode(ctx context.Context, p auth.Principal) (verification.Pending, error) {
	var out verification.Pending
	err := tenancy.Platform(ctx, s.platform, "reading the code in flight",
		func(ctx context.Context, tx pgx.Tx) error {
			var consumed *time.Time
			err := tx.QueryRow(ctx, `
				SELECT code_hash, attempts, sent_at, expires_at, consumed_at
				  FROM identity_email_codes
				 WHERE surface = $1 AND gip_uid = $2
				 ORDER BY sent_at DESC
				 LIMIT 1`, string(p.Surface), p.UID).
				Scan(&out.Hash, &out.Attempts, &out.SentAt, &out.ExpiresAt, &consumed)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoCodePending
			}
			if err != nil {
				return fmt.Errorf("identity: reading the code: %w", err)
			}
			if consumed != nil {
				out.ConsumedAt = *consumed
			}
			return nil
		})
	return out, err
}

// EmailCodesIssued counts what this sign-in has drawn inside the window and
// when the oldest of them was sent, which is what the hourly cap is decided on.
func (s *Principals) EmailCodesIssued(ctx context.Context, p auth.Principal, window time.Duration) (int, time.Time, error) {
	var n int
	var oldest *time.Time
	err := tenancy.Platform(ctx, s.platform, "counting codes issued",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT count(*), min(sent_at)
				  FROM identity_email_codes
				 WHERE surface = $1 AND gip_uid = $2
				   AND sent_at > now() - $3::interval`,
				string(p.Surface), p.UID, window.String()).Scan(&n, &oldest)
		})
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("identity: counting codes issued: %w", err)
	}
	if oldest == nil {
		return n, time.Time{}, nil
	}
	return n, *oldest, nil
}

// ConsumeEmailCode checks a code and, if it is right, marks the email verified.
//
// The refusal is carried out of the transaction rather than returned from it,
// because returning it would roll the attempt counter back with it — and a
// wrong guess that costs nothing is a million guesses.
func (s *Principals) ConsumeEmailCode(ctx context.Context, p auth.Principal, code string) error {
	var refused error
	err := tenancy.Platform(ctx, s.platform, "confirming an email code",
		func(ctx context.Context, tx pgx.Tx) error {
			var id string
			var pending verification.Pending
			var consumed *time.Time
			// FOR UPDATE: two tabs confirming at once must not each see four
			// attempts used and let a fifth guess through twice.
			err := tx.QueryRow(ctx, `
				SELECT id::text, code_hash, attempts, sent_at, expires_at, consumed_at
				  FROM identity_email_codes
				 WHERE surface = $1 AND gip_uid = $2
				 ORDER BY sent_at DESC
				 LIMIT 1
				   FOR UPDATE`, string(p.Surface), p.UID).
				Scan(&id, &pending.Hash, &pending.Attempts, &pending.SentAt, &pending.ExpiresAt, &consumed)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoCodePending
			}
			if err != nil {
				return fmt.Errorf("identity: reading the code: %w", err)
			}
			if consumed != nil {
				pending.ConsumedAt = *consumed
			}

			if _, err := tx.Exec(ctx,
				`UPDATE identity_email_codes SET attempts = attempts + 1 WHERE id = $1::uuid`, id); err != nil {
				return fmt.Errorf("identity: counting the attempt: %w", err)
			}

			if refused = verification.Check(pending, string(p.Surface), p.UID, code, time.Now()); refused != nil {
				return nil
			}

			if _, err := tx.Exec(ctx,
				`UPDATE identity_email_codes SET consumed_at = now() WHERE id = $1::uuid`, id); err != nil {
				return fmt.Errorf("identity: consuming the code: %w", err)
			}
			// The principal may not exist yet — the firm gate runs before
			// onboarding — so this is an upsert rather than an update.
			if _, err := tx.Exec(ctx, `
				INSERT INTO identity_principals (surface, gip_uid, email, sign_in_provider, email_verified_at)
				VALUES ($1, $2, nullif($3,''), nullif($4,''), now())
				ON CONFLICT (surface, gip_uid) DO UPDATE
				   SET email_verified_at = now(),
				       email = coalesce(identity_principals.email, excluded.email),
				       last_seen_at = now()`,
				string(p.Surface), p.UID, p.Email, p.SignInProvider); err != nil {
				return fmt.Errorf("identity: marking the email verified: %w", err)
			}
			return nil
		})
	if err != nil {
		return err
	}
	return refused
}

// EmailVerified is what the firm gate asks before it will mint an organisation.
func (s *Principals) EmailVerified(ctx context.Context, p auth.Principal) (bool, error) {
	var verified bool
	err := tenancy.Platform(ctx, s.platform, "reading email verification",
		func(ctx context.Context, tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `
				SELECT email_verified_at IS NOT NULL
				  FROM identity_principals
				 WHERE surface = $1 AND gip_uid = $2`,
				string(p.Surface), p.UID).Scan(&verified)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("identity: reading the principal: %w", err)
			}
			return nil
		})
	return verified, err
}
