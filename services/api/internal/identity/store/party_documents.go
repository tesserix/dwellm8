package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// PartyDocument is one piece of the landlord's own paperwork (#318).
//
// Dates are strings for the reason the tax profile's are: they arrive from a
// form as YYYY-MM-DD, are compared as dates by the database, and a time.Time
// round trip through a timezone is how an expiry moves a day.
type PartyDocument struct {
	Kind        string
	ObjectPath  string
	Filename    string
	ContentType string

	IssuingCountry string
	// ADR-0013. The mask, never the number — the column has a CHECK to match.
	NumberMasked string

	IssuedOn  string
	ExpiresOn string

	// What the copy is worth: none, self, notary, indian_mission, apostille.
	Attestation string
	AttestedOn  string
	AttestedBy  string

	State      string
	Reason     string
	UploadedBy string
	CreatedAt  string
}

// SavePartyDocument records a copy that has already reached the bucket. The
// shape rules — a mask rather than a number, an endorsement naming who made it —
// are the table's, so a caller cannot route around them.
func (s *Principals) SavePartyDocument(ctx context.Context, org tenancy.ID, party string, d PartyDocument) error {
	attestation := d.Attestation
	if attestation == "" {
		attestation = "none"
	}
	return tenancy.Platform(ctx, s.platform, "recording an owner document",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO party_documents (
					tenant_id, party_id, kind, object_path, filename, content_type,
					issuing_country, number_masked, issued_on, expires_on,
					attestation, attested_on, attested_by, uploaded_by)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6,
				        nullif($7,''), nullif($8,''), nullif($9,'')::date, nullif($10,'')::date,
				        $11, nullif($12,'')::date, nullif($13,''), $14)`,
				string(org), party, d.Kind, d.ObjectPath, d.Filename, d.ContentType,
				d.IssuingCountry, d.NumberMasked, d.IssuedOn, d.ExpiresOn,
				attestation, d.AttestedOn, d.AttestedBy, d.UploadedBy)
			if err != nil {
				return fmt.Errorf("identity: recording an owner document: %w", err)
			}
			return nil
		})
}

// PartyDocuments is everything held for one landlord, newest first — the order
// the onboarding checklist reads, where a re-upload supersedes what it replaces.
func (s *Principals) PartyDocuments(ctx context.Context, org tenancy.ID, party string) ([]PartyDocument, error) {
	var out []PartyDocument
	err := tenancy.Platform(ctx, s.platform, "reading owner documents",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT kind, object_path, filename, content_type,
				       coalesce(issuing_country,''), coalesce(number_masked,''),
				       coalesce(to_char(issued_on,'YYYY-MM-DD'),''),
				       coalesce(to_char(expires_on,'YYYY-MM-DD'),''),
				       attestation, coalesce(to_char(attested_on,'YYYY-MM-DD'),''),
				       coalesce(attested_by,''), state, coalesce(reason,''),
				       uploaded_by, to_char(created_at,'YYYY-MM-DD')
				  FROM party_documents
				 WHERE tenant_id = $1::uuid AND party_id = $2::uuid
				 ORDER BY created_at DESC`, string(org), party)
			if err != nil {
				return fmt.Errorf("identity: reading owner documents: %w", err)
			}
			defer rows.Close()
			for rows.Next() {
				var d PartyDocument
				if err := rows.Scan(&d.Kind, &d.ObjectPath, &d.Filename, &d.ContentType,
					&d.IssuingCountry, &d.NumberMasked, &d.IssuedOn, &d.ExpiresOn,
					&d.Attestation, &d.AttestedOn, &d.AttestedBy, &d.State, &d.Reason,
					&d.UploadedBy, &d.CreatedAt); err != nil {
					return fmt.Errorf("identity: reading an owner document: %w", err)
				}
				out = append(out, d)
			}
			return rows.Err()
		})
	return out, err
}
