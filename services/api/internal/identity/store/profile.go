package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/identity/domain/registration"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ErrNoProfile is a firm that has not filled its registration in yet.
var ErrNoProfile = errors.New("identity: this firm has no profile yet")

// ManagerProfile is the statutory identity of a managing firm (#282).
type ManagerProfile struct {
	LegalName    string
	TradeName    string
	Constitution string
	PANMasked    string
	TAN          string
	GSTIN        string
	RegistrarID  string
	AddressLine1 string
	AddressLine2 string
	Locality     string
	City         string
	StateCode    string
	PINCode      string
	ContactEmail string
	ContactPhone string
	State        string
}

// Registration is one agent registration, in one state, for one window.
type Registration struct {
	ID         string
	Authority  string
	StateCode  string
	Number     string
	ValidFrom  time.Time
	ValidTo    time.Time
	VerifiedAt time.Time
}

// ManagerDocument is one piece of firm-level paperwork.
type ManagerDocument struct {
	Kind        string
	ObjectPath  string
	Filename    string
	ContentType string
	ExpiresOn   time.Time
	UploadedBy  string
	State       string
	Reason      string
}

// SaveManagerProfile writes the firm's registration details. One row per firm,
// so a correction corrects rather than adding a second version of the truth.
func (s *Principals) SaveManagerProfile(ctx context.Context, firm tenancy.ID, p ManagerProfile) error {
	return tenancy.Platform(ctx, s.platform, "saving a manager profile",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO manager_profiles (
					tenant_id, legal_name, trade_name, constitution,
					pan_masked, tan, gstin, registrar_id,
					address_line1, address_line2, locality, city, state_code, pin_code,
					contact_email, contact_phone, state)
				VALUES ($1::uuid, $2, nullif($3,''), $4,
				        nullif($5,''), nullif($6,''), nullif($7,''), nullif($8,''),
				        $9, nullif($10,''), nullif($11,''), $12, $13, $14,
				        $15, $16, 'submitted')
				ON CONFLICT (tenant_id) DO UPDATE SET
					legal_name = excluded.legal_name,
					trade_name = excluded.trade_name,
					constitution = excluded.constitution,
					pan_masked = excluded.pan_masked,
					tan = excluded.tan,
					gstin = excluded.gstin,
					registrar_id = excluded.registrar_id,
					address_line1 = excluded.address_line1,
					address_line2 = excluded.address_line2,
					locality = excluded.locality,
					city = excluded.city,
					state_code = excluded.state_code,
					pin_code = excluded.pin_code,
					contact_email = excluded.contact_email,
					contact_phone = excluded.contact_phone,
					-- Filing details is what leaves draft; approval and lapse are
					-- somebody else's decision and a correction must not undo it (#288).
					state = CASE WHEN manager_profiles.state = 'draft'
					             THEN 'submitted' ELSE manager_profiles.state END,
					updated_at = now()`,
				string(firm), p.LegalName, p.TradeName, p.Constitution,
				p.PANMasked, p.TAN, p.GSTIN, p.RegistrarID,
				p.AddressLine1, p.AddressLine2, p.Locality, p.City, p.StateCode, p.PINCode,
				p.ContactEmail, p.ContactPhone)
			if err != nil {
				return fmt.Errorf("identity: writing the manager profile: %w", err)
			}
			return nil
		})
}

// ManagerProfile reads it back.
func (s *Principals) ManagerProfile(ctx context.Context, firm tenancy.ID) (ManagerProfile, error) {
	var out ManagerProfile
	err := tenancy.Platform(ctx, s.platform, "reading a manager profile",
		func(ctx context.Context, tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `
				SELECT legal_name, coalesce(trade_name,''), constitution,
				       coalesce(pan_masked,''), coalesce(tan,''), coalesce(gstin,''),
				       coalesce(registrar_id,''), address_line1, coalesce(address_line2,''),
				       coalesce(locality,''), city, state_code, pin_code,
				       contact_email::text, contact_phone, state
				  FROM manager_profiles
				 WHERE tenant_id = $1::uuid`, string(firm)).
				Scan(&out.LegalName, &out.TradeName, &out.Constitution,
					&out.PANMasked, &out.TAN, &out.GSTIN,
					&out.RegistrarID, &out.AddressLine1, &out.AddressLine2,
					&out.Locality, &out.City, &out.StateCode, &out.PINCode,
					&out.ContactEmail, &out.ContactPhone, &out.State)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoProfile
			}
			if err != nil {
				return fmt.Errorf("identity: reading the manager profile: %w", err)
			}
			return nil
		})
	return out, err
}

// AddRegistration files one state's agent registration.
func (s *Principals) AddRegistration(ctx context.Context, firm tenancy.ID, r Registration) error {
	return tenancy.Platform(ctx, s.platform, "filing an agent registration",
		func(ctx context.Context, tx pgx.Tx) error {
			authority := r.Authority
			if authority == "" {
				authority = "rera"
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO manager_registrations (tenant_id, authority, state_code, number, valid_from, valid_to)
				VALUES ($1::uuid, $2, $3, $4, $5, $6)`,
				string(firm), authority, r.StateCode, r.Number, r.ValidFrom, r.ValidTo)
			if err != nil {
				return fmt.Errorf("identity: filing the registration: %w", err)
			}
			return nil
		})
}

// LiveRegistrations is what the firm holds on a given day — which is the only
// question s.9 asks, and it is not the same as what it has ever held.
func (s *Principals) LiveRegistrations(ctx context.Context, firm tenancy.ID, on time.Time) ([]Registration, error) {
	var out []Registration
	err := tenancy.Platform(ctx, s.platform, "reading agent registrations",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT id::text, authority, state_code, number, valid_from, valid_to,
				       coalesce(verified_at, 'epoch'::timestamptz)
				  FROM manager_registrations
				 WHERE tenant_id = $1::uuid AND validity @> $2::date
				 ORDER BY state_code`, string(firm), on)
			if err != nil {
				return fmt.Errorf("identity: reading registrations: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var r Registration
				if err := rows.Scan(&r.ID, &r.Authority, &r.StateCode, &r.Number,
					&r.ValidFrom, &r.ValidTo, &r.VerifiedAt); err != nil {
					return fmt.Errorf("identity: reading a registration: %w", err)
				}
				out = append(out, r)
			}
			return rows.Err()
		})
	return out, err
}

// RecordDocument notes an upload. The object is already in the bucket; this row
// is the only record that it exists.
func (s *Principals) RecordDocument(ctx context.Context, firm tenancy.ID, d ManagerDocument) error {
	return tenancy.Platform(ctx, s.platform, "recording a manager document",
		func(ctx context.Context, tx pgx.Tx) error {
			var expires *time.Time
			if !d.ExpiresOn.IsZero() {
				expires = &d.ExpiresOn
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO manager_documents (tenant_id, kind, object_path, filename, content_type, expires_on, uploaded_by)
				VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)`,
				string(firm), d.Kind, d.ObjectPath, d.Filename, d.ContentType, expires, d.UploadedBy)
			if err != nil {
				return fmt.Errorf("identity: recording the document: %w", err)
			}
			return nil
		})
}

// ReviewDocument accepts or refuses the newest document of a kind.
func (s *Principals) ReviewDocument(ctx context.Context, firm tenancy.ID, kind, state, reason string) error {
	return tenancy.Platform(ctx, s.platform, "reviewing a manager document",
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				UPDATE manager_documents
				   SET state = $3, reason = nullif($4,''), reviewed_at = now()
				 WHERE id = (SELECT id FROM manager_documents
				              WHERE tenant_id = $1::uuid AND kind = $2
				              ORDER BY created_at DESC LIMIT 1)`,
				string(firm), kind, state, reason)
			if err != nil {
				return fmt.Errorf("identity: reviewing the document: %w", err)
			}
			if tag.RowsAffected() == 0 {
				return fmt.Errorf("identity: no %s has been uploaded", kind)
			}
			return nil
		})
}

// HeldDocuments is what the firm actually holds, in the shape the checklist
// reads — newest of each kind, and whether anybody has accepted it.
func (s *Principals) HeldDocuments(ctx context.Context, firm tenancy.ID) (map[registration.Kind]registration.Held, error) {
	held := map[registration.Kind]registration.Held{}
	err := tenancy.Platform(ctx, s.platform, "reading manager documents",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT DISTINCT ON (kind) kind, state, coalesce(expires_on, 'epoch'::date)
				  FROM manager_documents
				 WHERE tenant_id = $1::uuid
				 ORDER BY kind, created_at DESC`, string(firm))
			if err != nil {
				return fmt.Errorf("identity: reading documents: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var kind, state string
				var expires time.Time
				if err := rows.Scan(&kind, &state, &expires); err != nil {
					return fmt.Errorf("identity: reading a document: %w", err)
				}
				h := registration.Held{Accepted: state == "accepted"}
				if expires.Year() > 1970 {
					h.ExpiresOn = expires
				}
				held[registration.Kind(kind)] = h
			}
			return rows.Err()
		})
	return held, err
}
