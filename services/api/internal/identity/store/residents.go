package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// A renter's identity exists before they do. ADR-0029 §2.
//
// Every other surface onboards forwards: somebody signs in, and an organisation
// is created for them. A tenant arrives backwards — their landlord types their
// mobile number into a lease, and the reminder that lands on their phone weeks
// later is the first thing they ever see of this product. So the party id has to
// exist at the moment the lease is written, and the sign-in has to attach itself
// to the one already there rather than mint a second.
//
// The join is the mobile number, which is the identifier Indian rental actually
// runs on and the one Identity Platform verifies by OTP. Until the sign-in
// happens the row's gip_uid is the reservation marker below, which is unique per
// number and so cannot collide with a real Google uid.

// pendingUID is the placeholder gip_uid a pre-registered renter carries.
//
// A prefix that no GIP uid can take: Google's are base64-ish and contain no
// colon, and the '+' of an E.164 number is not in that alphabet either. It has
// to be non-empty because the schema refuses a blank uid, and it has to be
// unique per person because (surface, gip_uid) is the key.
func pendingUID(phone string) string { return "phone:" + phone }

// e164 is the same shape the schema's CHECK enforces, applied before the insert
// so a bad number is a named refusal rather than a constraint violation.
var e164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// ErrPhone is a number that cannot identify a renter.
var ErrPhone = errors.New("identity: that is not a mobile number in E.164")

// EnsureResident returns the party id for a renter's mobile number, creating the
// pre-registration if this is the first tenancy they have ever been named on.
//
// Idempotent, and idempotent under concurrency: two landlords adding the same
// number at the same moment both insert, one loses on the unique index, and both
// receive the same party. The alternative — read, then insert — is correct in
// review and wrong under load, which is the same argument the payments store
// makes about idempotency keys.
func (s *Principals) EnsureResident(ctx context.Context, phone string) (string, error) {
	if !e164.MatchString(phone) {
		return "", fmt.Errorf("%w: %q", ErrPhone, phone)
	}

	var partyID string
	err := tenancy.Platform(ctx, s.platform, "pre-registering a renter named on a lease",
		func(ctx context.Context, tx pgx.Tx) error {
			// A claimed principal already holds this number, so its party is the
			// answer. Looked at first because the insert below would conflict on
			// the phone index rather than on the uid, and DO UPDATE cannot see
			// which of the two constraints it lost to.
			err := tx.QueryRow(ctx, `
				SELECT party_id::text FROM identity_principals
				 WHERE surface = 'live' AND phone = $1`, phone).Scan(&partyID)
			if err == nil {
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("identity: reading a renter by number: %w", err)
			}

			return tx.QueryRow(ctx, `
				INSERT INTO identity_principals (surface, gip_uid, phone, sign_in_provider)
				VALUES ('live', $1, $2, 'phone')
				ON CONFLICT (surface, gip_uid) DO UPDATE SET last_seen_at = identity_principals.last_seen_at
				RETURNING party_id::text`,
				pendingUID(phone), phone).Scan(&partyID)
		})
	if err != nil {
		return "", err
	}
	return partyID, nil
}

// ClaimResident resolves a verified Live sign-in to the party the landlord
// already named, and binds the two together the first time.
//
// Three outcomes, and they are deliberately distinct:
//
//   - The uid is already known. The ordinary case, every sign-in after the
//     first.
//   - The uid is new and the verified number matches a pre-registration. The
//     claim: the placeholder uid is replaced by the real one, and the party id
//     is unchanged — which is what makes six months of receipts still theirs.
//   - Neither. ErrUnknownPrincipal, because nobody has ever put this number on a
//     tenancy. A renter cannot onboard themselves: there is nothing for them to
//     be a tenant of, and creating an empty organisation for them is the failure
//     KindFor() already refuses.
//
// The number is the token's, verified by Identity Platform against an OTP, and
// never the client's claim about itself.
func (s *Principals) ClaimResident(ctx context.Context, p auth.Principal) (Person, error) {
	if p.Surface != auth.SurfaceLive {
		return Person{}, fmt.Errorf("identity: %q is not the tenant app", p.Surface)
	}
	if p.UID == "" {
		return Person{}, ErrUnknownPrincipal
	}

	out := Person{Surface: auth.SurfaceLive, Phone: p.Phone, Email: p.Email}
	err := tenancy.Platform(ctx, s.platform, "claiming a renter's first sign-in",
		func(ctx context.Context, tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `
				UPDATE identity_principals
				   SET last_seen_at = now()
				 WHERE surface = 'live' AND gip_uid = $1 AND disabled_at IS NULL
				RETURNING party_id::text, coalesce(phone, '')`,
				p.UID).Scan(&out.PartyID, &out.Phone)
			if err == nil {
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("identity: reading a renter's sign-in: %w", err)
			}
			if p.Phone == "" {
				// A Live token with no verified number cannot be matched to a
				// tenancy, and guessing is not available.
				return ErrUnknownPrincipal
			}

			// The claim. Conditioned on the placeholder uid so it can only ever
			// take over a reservation: a row already bound to somebody else's uid
			// does not match, so a second person verifying a recycled number does
			// not inherit the first one's tenancies.
			err = tx.QueryRow(ctx, `
				UPDATE identity_principals
				   SET gip_uid = $1, sign_in_provider = coalesce(nullif($3,''), sign_in_provider),
				       email = coalesce(nullif($4,'')::citext, email), last_seen_at = now()
				 WHERE surface = 'live' AND phone = $2 AND gip_uid = $5 AND disabled_at IS NULL
				RETURNING party_id::text, coalesce(phone, '')`,
				p.UID, p.Phone, p.SignInProvider, p.Email, pendingUID(p.Phone),
			).Scan(&out.PartyID, &out.Phone)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownPrincipal
			}
			return err
		})
	if err != nil {
		return Person{}, err
	}
	return out, nil
}
