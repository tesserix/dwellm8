package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// An owner's identity can exist before they do, exactly like a renter's
// (residents.go): the manager types their number, the organisation and the
// mandate are minted around the reservation, and the first Own sign-in with
// that verified number claims it. #240.

// managementPermissions is the full mandate a firm takes when it onboards the
// owner itself. identity.* is structurally absent from the vocabulary — a
// grant never confers control of the owner's account.
var managementPermissions = []string{
	"property.read", "property.write",
	"lease.read", "lease.write",
	"money.read", "money.collect", "money.payout",
	"maintenance.read", "maintenance.write",
	"document.read", "document.write",
	"community.read", "community.write",
}

// OwnerOnboarding is a manager onboarding an owner they manage for.
type OwnerOnboarding struct {
	// FirmOrgID is the managing firm — the grantee of the mandate.
	FirmOrgID string
	// ByPartyID is who pressed the button, for created_by and the events.
	ByPartyID string

	OwnerName string
	Phone     string
	Email     string
	OrgName   string
	OrgSlug   string
}

// OwnerOnboarded is what the flow produced, existing or new.
type OwnerOnboarded struct {
	PartyID    string
	OrgID      string
	GrantID    string
	CreatedOrg bool
}

// PreOnboardOwner reserves the owner's identity, their organisation and the
// firm's mandate over it, in one transaction.
//
// Idempotent the way Onboard is: a number that already claimed an owner
// organisation gets that organisation back — a second property joins it,
// which is what keeps one landlord's books in one place — and an existing
// live grant to the same firm is reused rather than doubled.
func (s *Principals) PreOnboardOwner(ctx context.Context, o OwnerOnboarding) (OwnerOnboarded, error) {
	if !e164.MatchString(o.Phone) {
		return OwnerOnboarded{}, fmt.Errorf("%w: %q", ErrPhone, o.Phone)
	}
	if o.OwnerName == "" || o.OrgName == "" || o.FirmOrgID == "" {
		return OwnerOnboarded{}, errors.New("identity: an owner onboarding names the owner, their organisation and the firm")
	}
	if o.OrgSlug == "" {
		o.OrgSlug = ownerSlug(o.OrgName)
	}

	var out OwnerOnboarded
	err := tenancy.Platform(ctx, s.platform, "pre-onboarding an owner",
		func(ctx context.Context, tx pgx.Tx) error {
			// The principal: a claimed Own sign-in already holding this number,
			// or a reservation the first sign-in will claim.
			err := tx.QueryRow(ctx, `
				SELECT party_id::text FROM identity_principals
				 WHERE surface = 'own' AND phone = $1`, o.Phone).Scan(&out.PartyID)
			if errors.Is(err, pgx.ErrNoRows) {
				err = tx.QueryRow(ctx, `
					INSERT INTO identity_principals (surface, gip_uid, phone, email, display_name, sign_in_provider)
					VALUES ('own', $1, $2, nullif($3, ''), nullif($4, ''), 'phone')
					ON CONFLICT (surface, gip_uid) DO UPDATE SET last_seen_at = identity_principals.last_seen_at
					RETURNING party_id::text`,
					pendingUID(o.Phone), o.Phone, o.Email, o.OwnerName).Scan(&out.PartyID)
			}
			if err != nil {
				return fmt.Errorf("identity: reserving the owner's identity: %w", err)
			}

			// Their organisation: the live owner-kind membership if one exists.
			err = tx.QueryRow(ctx, `
				SELECT m.tenant_id::text
				  FROM organisation_members m
				  JOIN organisations org ON org.id = m.tenant_id
				 WHERE m.party_id = $1::uuid AND org.kind = 'owner'
				   AND m.validity @> current_date
				 ORDER BY m.created_at
				 LIMIT 1`, out.PartyID).Scan(&out.OrgID)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if err := tx.QueryRow(ctx, `
					INSERT INTO organisations (slug, name, kind, state)
					VALUES ($1, $2, 'owner', 'onboarding')
					RETURNING id::text`, o.OrgSlug, o.OrgName).Scan(&out.OrgID); err != nil {
					return fmt.Errorf("identity: creating the owner's organisation: %w", err)
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO organisation_members (tenant_id, party_id, role, created_by)
					VALUES ($1, $2::uuid, 'owner', nullif($3, '')::uuid)`,
					out.OrgID, out.PartyID, o.ByPartyID); err != nil {
					return fmt.Errorf("identity: writing the owner's membership: %w", err)
				}
				env, err := events.New("identity.organisation.created", out.OrgID,
					events.Subject{Kind: "organisation", ID: out.OrgID},
					events.Actor{Kind: events.ActorUser, ID: o.ByPartyID},
					struct {
						PartyID string `json:"party_id"`
						Role    string `json:"role"`
						Kind    string `json:"kind"`
						Slug    string `json:"slug"`
					}{out.PartyID, "owner", "owner", o.OrgSlug})
				if err != nil {
					return err
				}
				if err := events.Append(ctx, tx, env); err != nil {
					return err
				}
				out.CreatedOrg = true
			case err != nil:
				return fmt.Errorf("identity: looking for the owner's organisation: %w", err)
			}

			// The mandate: a live grant to this firm, or a new portfolio-wide one.
			err = tx.QueryRow(ctx, `
				SELECT id::text FROM delegation_grants
				 WHERE tenant_id = $1::uuid AND grantee_org_id = $2::uuid
				   AND revoked_at IS NULL
				   AND (valid_to IS NULL OR valid_to > now())
				 ORDER BY created_at DESC
				 LIMIT 1`, out.OrgID, o.FirmOrgID).Scan(&out.GrantID)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				if err := tx.QueryRow(ctx, `
					INSERT INTO delegation_grants (tenant_id, grantee_org_id, permissions, created_by)
					VALUES ($1::uuid, $2::uuid, $3, nullif($4, '')::uuid)
					RETURNING id::text`,
					out.OrgID, o.FirmOrgID, managementPermissions, o.ByPartyID).Scan(&out.GrantID); err != nil {
					return fmt.Errorf("identity: minting the mandate: %w", err)
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO delegation_grant_scopes (grant_id, scope_kind, scope_id)
					VALUES ($1::uuid, 'portfolio', NULL)`, out.GrantID); err != nil {
					return fmt.Errorf("identity: scoping the mandate: %w", err)
				}
				env, err := events.New("identity.delegation.granted", out.OrgID,
					events.Subject{Kind: "delegation", ID: out.GrantID},
					events.Actor{Kind: events.ActorUser, ID: o.ByPartyID},
					struct {
						GranteeOrgID string   `json:"grantee_org_id"`
						Permissions  []string `json:"permissions"`
						Scope        string   `json:"scope"`
					}{o.FirmOrgID, managementPermissions, "portfolio"})
				if err != nil {
					return err
				}
				if err := events.Append(ctx, tx, env); err != nil {
					return err
				}
			case err != nil:
				return fmt.Errorf("identity: looking for the firm's mandate: %w", err)
			}
			return nil
		})
	if err != nil {
		return OwnerOnboarded{}, err
	}
	return out, nil
}

// ownerSlug derives a unique-enough handle, the same shape the self-service
// onboarding derives: lower-cased, dashed, randomness against collisions.
func ownerSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	base := strings.Trim(b.String(), "-")
	if len(base) > 40 {
		base = base[:40]
	}
	if base == "" {
		base = "owner"
	}
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("%s-%x", base, suffix)
}

// ClaimOwner attaches a verified Own sign-in to a pre-onboarded owner, the
// same move ClaimResident makes for a renter and safe for the same reason:
// the claim is conditioned on the reservation marker, so it can only ever
// take over a reservation.
func (s *Principals) ClaimOwner(ctx context.Context, p auth.Principal) (Person, error) {
	if p.Surface != auth.SurfaceOwn {
		return Person{}, fmt.Errorf("identity: %q is not the owner app", p.Surface)
	}
	if p.UID == "" || p.Phone == "" {
		return Person{}, ErrUnknownPrincipal
	}

	out := Person{Surface: auth.SurfaceOwn, Phone: p.Phone, Email: p.Email}
	err := tenancy.Platform(ctx, s.platform, "claiming an owner's first sign-in",
		func(ctx context.Context, tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `
				UPDATE identity_principals
				   SET gip_uid = $1, sign_in_provider = coalesce(nullif($3,''), sign_in_provider),
				       email = coalesce(nullif($4,'')::citext, email), last_seen_at = now()
				 WHERE surface = 'own' AND phone = $2 AND gip_uid = $5 AND disabled_at IS NULL
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

// Profile is the person as they present themselves: the verified anchors and
// the name they chose.
type Profile struct {
	PartyID     string
	Phone       string
	Email       string
	DisplayName string
}

// ProfileByParty reads a party's profile — the resident and owner surfaces'
// "me". LIMIT 1 for Contact's reason.
func (s *Principals) ProfileByParty(ctx context.Context, partyID string) (Profile, error) {
	out := Profile{PartyID: partyID}
	err := tenancy.Platform(ctx, s.platform, "reading a profile",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT coalesce(phone, ''), coalesce(email::text, ''), coalesce(display_name, '')
				  FROM identity_principals
				 WHERE party_id = $1::uuid
				 ORDER BY last_seen_at DESC NULLS LAST
				 LIMIT 1`, partyID).Scan(&out.Phone, &out.Email, &out.DisplayName)
		})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return Profile{}, fmt.Errorf("identity: reading the profile for %s: %w", partyID, err)
	}
	return out, nil
}

// UpdateProfileByParty is the person filling in their own PI after
// onboarding. Only the self-served fields move: the phone is the verified
// anchor and never edited here.
func (s *Principals) UpdateProfileByParty(ctx context.Context, partyID, displayName, email string) (Profile, error) {
	out := Profile{PartyID: partyID}
	err := tenancy.Platform(ctx, s.platform, "updating a profile",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				UPDATE identity_principals
				   SET display_name = coalesce(nullif($2, ''), display_name),
				       email = coalesce(nullif($3, '')::citext, email)
				 WHERE party_id = $1::uuid
				RETURNING coalesce(phone, ''), coalesce(email::text, ''), coalesce(display_name, '')`,
				partyID, displayName, email).Scan(&out.Phone, &out.Email, &out.DisplayName)
		})
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrUnknownPrincipal
	}
	if err != nil {
		return Profile{}, fmt.Errorf("identity: updating the profile for %s: %w", partyID, err)
	}
	return out, nil
}
