// Package store reads and writes who has signed in, and what they may act as.
// ADR-0027 §6.
//
// It is the only place `identity_principals` is reachable from: the table has no
// tenant_id — a person signing into Find is nobody's tenant yet — so row-level
// security has nothing to scope it by, and the privilege is the boundary
// instead. `dwellm8_app` has it revoked; only `dwellm8_identity` holds it.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Principals is the identity module's store.
type Principals struct{ platform tenancy.PlatformPool }

// New takes the platform pool.
//
// The privileged one, and it has to be: a sign-in arrives before anybody knows
// which organisation it belongs to, so there is no tenant to scope the session
// by — the same argument ADR-0011 §5 makes for the webhook inbox. The narrowness
// comes from this being the only package that holds it for these tables.
func New(p tenancy.PlatformPool) *Principals { return &Principals{platform: p} }

// ErrUnknownPrincipal is a verified token for somebody this platform has never
// seen. Distinguishable, because the answer is to onboard them rather than to
// refuse them.
var ErrUnknownPrincipal = errors.New("identity: no principal for that sign-in")

// Membership is one organisation a person may act in.
type Membership struct {
	TenantID tenancy.ID
	Role     string
}

// Person is a principal and everything they may act as.
type Person struct {
	PartyID     string
	Surface     auth.Surface
	Staff       bool
	Phone       string
	Email       string
	Memberships []Membership
}

// Organisation returns the one organisation this person acts in, and false when
// that is not a single answer.
//
// A person with one membership is the ordinary case and needs no choice made for
// them. A person with several — a manager who is also a landlord — must say
// which they are acting as, and picking the first would be the platform choosing
// silently on their behalf.
func (p Person) Organisation() (tenancy.ID, bool) {
	if len(p.Memberships) != 1 {
		return "", false
	}
	return p.Memberships[0].TenantID, true
}

// Lookup finds a verified principal and what they may act as.
//
// The key is (surface, uid) rather than uid, because a uid is unique within a
// GIP tenant and not across them. Looking up by uid alone would eventually
// return the wrong person.
func (s *Principals) Lookup(ctx context.Context, p auth.Principal) (Person, error) {
	surface := string(p.Surface)
	if p.Staff {
		surface = "staff"
	}

	var out Person
	err := tenancy.Platform(ctx, s.platform, "resolving a sign-in",
		func(ctx context.Context, tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `
				UPDATE identity_principals
				   SET last_seen_at = now()
				 WHERE surface = $1 AND gip_uid = $2 AND disabled_at IS NULL
				RETURNING party_id::text, coalesce(phone, ''), coalesce(email::text, '')`,
				surface, p.UID).Scan(&out.PartyID, &out.Phone, &out.Email)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownPrincipal
			}
			if err != nil {
				return fmt.Errorf("identity: reading the principal: %w", err)
			}

			rows, err := tx.Query(ctx, `
				SELECT tenant_id::text, role
				  FROM organisation_members
				 WHERE party_id = $1::uuid AND validity @> current_date
				 ORDER BY role`, out.PartyID)
			if err != nil {
				return fmt.Errorf("identity: reading memberships: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var m Membership
				var id string
				if err := rows.Scan(&id, &m.Role); err != nil {
					return err
				}
				m.TenantID = tenancy.ID(id)
				out.Memberships = append(out.Memberships, m)
			}
			return rows.Err()
		})
	if err != nil {
		return Person{}, err
	}
	out.Surface, out.Staff = p.Surface, p.Staff
	return out, nil
}

// Onboarding is a first sign-in becoming somebody with an organisation.
type Onboarding struct {
	Principal auth.Principal
	// OrganisationName is what the person calls themselves or their firm.
	OrganisationName string
	// Kind is the organisation's kind, which follows from the surface they
	// signed into: Own makes an owner, Ops an agency, Pro a vendor.
	Kind string
	// Slug is the URL-safe handle, unique across the platform.
	Slug string
	// Role is what they are in the organisation they are creating. The person
	// who creates one is its owner; everybody else is invited into an existing
	// one, which is a different story.
	Role string
}

// Onboard creates the principal, the organisation and the membership in one
// transaction, and reports whether it created anything.
//
// **One transaction**, and it is the whole point. Three separate writes leave
// three ways to fail: a principal with no organisation cannot do anything, an
// organisation with no member is unreachable by anybody and invisible to
// support, and a membership pointing at either is a foreign-key error somebody
// discovers later. Committing together means a committed organisation always has
// somebody who can reach it.
//
// **Idempotent by membership**: a principal who already holds a live membership
// in an organisation of this kind gets that organisation back, not a second
// one. A retried request, a re-opened app and a double-tap are the same call,
// and a person's second property joins their existing organisation — creating
// another would split one landlord's books in two.
func (s *Principals) Onboard(ctx context.Context, o Onboarding) (Person, bool, error) {
	if err := o.validate(); err != nil {
		return Person{}, false, err
	}
	surface := string(o.Principal.Surface)
	if o.Principal.Staff {
		return Person{}, false, errors.New("identity: staff do not onboard into an organisation — " +
			"they are outside every surface, and giving them one would make the product owner " +
			"a member of a customer")
	}

	var out Person
	created := false
	err := tenancy.Platform(ctx, s.platform, "onboarding a first sign-in",
		func(ctx context.Context, tx pgx.Tx) error {
			// The principal first: everything else points at its party.
			err := tx.QueryRow(ctx, `
				INSERT INTO identity_principals (surface, gip_uid, phone, email, sign_in_provider)
				VALUES ($1, $2, nullif($3, ''), nullif($4, ''), nullif($5, ''))
				ON CONFLICT (surface, gip_uid) DO UPDATE
				    SET last_seen_at = now()
				RETURNING party_id::text`,
				surface, o.Principal.UID, o.Principal.Phone, o.Principal.Email,
				o.Principal.SignInProvider).Scan(&out.PartyID)
			if err != nil {
				return fmt.Errorf("identity: writing the principal: %w", err)
			}

			var existingID, existingRole string
			err = tx.QueryRow(ctx, `
				SELECT m.tenant_id::text, m.role
				  FROM organisation_members m
				  JOIN organisations org ON org.id = m.tenant_id
				 WHERE m.party_id = $1::uuid AND org.kind = $2
				   AND m.validity @> current_date
				 ORDER BY m.created_at
				 LIMIT 1`, out.PartyID, o.Kind).Scan(&existingID, &existingRole)
			switch {
			case err == nil:
				out.Memberships = []Membership{{TenantID: tenancy.ID(existingID), Role: existingRole}}
				return nil
			case !errors.Is(err, pgx.ErrNoRows):
				return fmt.Errorf("identity: looking for an existing organisation: %w", err)
			}

			var tenantID string
			err = tx.QueryRow(ctx, `
				INSERT INTO organisations (slug, name, kind, state)
				VALUES ($1, $2, $3, 'onboarding')
				RETURNING id::text`, o.Slug, o.OrganisationName, o.Kind).Scan(&tenantID)
			if err != nil {
				return fmt.Errorf("identity: creating the organisation: %w", err)
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO organisation_members (tenant_id, party_id, role, created_by)
				VALUES ($1, $2::uuid, $3, $2::uuid)`,
				tenantID, out.PartyID, o.Role); err != nil {
				return fmt.Errorf("identity: writing the membership: %w", err)
			}

			// The fact, in the same transaction (ADR-0002) — the authz
			// projector turns it into the organisation's first edge, which is
			// what makes the creator able to pass an enforced check at all.
			env, err := events.New("identity.organisation.created", tenantID,
				events.Subject{Kind: "organisation", ID: tenantID},
				events.Actor{Kind: events.ActorUser, ID: out.PartyID},
				struct {
					PartyID string `json:"party_id"`
					Role    string `json:"role"`
					Kind    string `json:"kind"`
					Slug    string `json:"slug"`
				}{out.PartyID, o.Role, o.Kind, o.Slug})
			if err != nil {
				return err
			}
			if err := events.Append(ctx, tx, env); err != nil {
				return err
			}

			created = true
			out.Memberships = []Membership{{TenantID: tenancy.ID(tenantID), Role: o.Role}}
			return nil
		})
	if err != nil {
		return Person{}, false, err
	}
	out.Surface = o.Principal.Surface
	out.Phone, out.Email = o.Principal.Phone, o.Principal.Email
	return out, created, nil
}

// ErrOnboarding is an onboarding that cannot produce a usable organisation.
var ErrOnboarding = errors.New("identity: that is not enough to create an organisation")

var kindForSurface = map[auth.Surface]string{
	auth.SurfaceOwn: "owner",
	auth.SurfaceOps: "agency",
	auth.SurfacePro: "vendor",
}

// KindFor returns the organisation kind a surface creates, and false where that
// surface does not create organisations at all.
//
// Live and Find do not: a tenant belongs to their landlord's organisation and a
// prospect belongs to nobody. Letting either create one would produce an
// organisation of one person that owns nothing, and then a second when they are
// actually invited.
func KindFor(s auth.Surface) (string, bool) {
	k, ok := kindForSurface[s]
	return k, ok
}

func (o Onboarding) validate() error {
	kind, creates := KindFor(o.Principal.Surface)
	switch {
	case o.Principal.UID == "":
		return fmt.Errorf("%w: no principal", ErrOnboarding)
	case !creates:
		return fmt.Errorf("%w: the %s app does not create organisations — a tenant belongs to "+
			"their landlord's and a prospect to nobody", ErrOnboarding, o.Principal.Surface)
	case o.Kind != "" && o.Kind != kind:
		return fmt.Errorf("%w: the %s app creates a %q, not a %q",
			ErrOnboarding, o.Principal.Surface, kind, o.Kind)
	case o.OrganisationName == "":
		return fmt.Errorf("%w: no name", ErrOnboarding)
	case o.Slug == "":
		return fmt.Errorf("%w: no slug", ErrOnboarding)
	case o.Role == "":
		return fmt.Errorf("%w: no role", ErrOnboarding)
	}
	return nil
}

// Fill sets the fields that follow from the surface rather than from the form.
func (o Onboarding) Fill() Onboarding {
	if kind, ok := KindFor(o.Principal.Surface); ok && o.Kind == "" {
		o.Kind = kind
	}
	if o.Role == "" {
		o.Role = "owner"
	}
	return o
}
