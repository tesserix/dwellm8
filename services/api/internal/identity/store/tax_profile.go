package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ErrNoTaxProfile is a landlord nobody has asked yet. It is an error rather
// than a zero value because a blank profile deducts at section 206AA's floor,
// and a caller must choose that rather than receive it.
var ErrNoTaxProfile = errors.New("identity: this party has no tax profile")

// ErrNoOwnerOrg is a party who owns nothing. A tax profile belongs in the books
// the rent is credited to, so there is nowhere to put one.
var ErrNoOwnerOrg = errors.New("identity: this party owns no organisation")

// OwnerOrgOf is OwnerParty read the other way: from the person to the books.
// A tax profile is written under the owner's tenancy, never the firm's, however
// broad the mandate the firm is asking under.
func (s *Principals) OwnerOrgOf(ctx context.Context, party string) (string, error) {
	var org string
	err := tenancy.Platform(ctx, s.platform, "reading a party's owner organisation",
		func(ctx context.Context, tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `
				SELECT m.tenant_id::text
				  FROM organisation_members m
				 WHERE m.party_id = $1::uuid AND m.role = 'owner'
				   AND m.validity @> current_date
				 ORDER BY m.created_at
				 LIMIT 1`, party).Scan(&org)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoOwnerOrg
			}
			return err
		})
	return org, err
}

// TaxProfile is what a landlord has furnished for TDS purposes, as of a date.
// Rule37BCFurnished is derived by the database from the particulars themselves.
type TaxProfile struct {
	Residency        string
	ResidenceCountry string
	PANFurnished     bool

	// The rule 37BC(2) particulars. The TIN is masked here and everywhere —
	// ADR-0013 keeps identifiers masked or not at all.
	ForeignTINMasked string
	TRCNumber        string
	TRCValidFrom     string
	TRCValidTo       string
	ForeignAddress   string
	ForeignEmail     string
	ForeignPhone     string

	Source    string
	ValidFrom string

	Rule37BCFurnished bool
}

// SaveTaxProfile records what the landlord has furnished from ValidFrom on.
//
// It closes the previous row at that date rather than replacing it: a landlord
// who moves abroad in October was a resident in April, and April's deduction was
// correct at the rate it was made.
func (s *Principals) SaveTaxProfile(ctx context.Context, firm tenancy.ID, party string, p TaxProfile) error {
	from, err := time.Parse("2006-01-02", p.ValidFrom)
	if err != nil {
		return fmt.Errorf("identity: a tax profile must say what date it is true from: %w", err)
	}
	return tenancy.Platform(ctx, s.platform, "saving a tax profile",
		func(ctx context.Context, tx pgx.Tx) error {
			// The open row is closed at the new row's start, so the exclusion
			// constraint sees two adjacent windows rather than an overlap.
			if _, err := tx.Exec(ctx, `
				UPDATE party_tax_profiles
				   SET valid_to = $3
				 WHERE tenant_id = $1::uuid AND party_id = $2::uuid
				   AND retired_at IS NULL
				   AND (valid_to IS NULL OR valid_to > $3)
				   AND valid_from < $3`,
				string(firm), party, from); err != nil {
				return fmt.Errorf("identity: closing the previous tax profile: %w", err)
			}
			// A profile filed twice for the same day is a correction, not a
			// second truth: the exclusion constraint would refuse the overlap.
			if _, err := tx.Exec(ctx, `
				UPDATE party_tax_profiles SET retired_at = now()
				 WHERE tenant_id = $1::uuid AND party_id = $2::uuid
				   AND retired_at IS NULL AND valid_from >= $3`,
				string(firm), party, from); err != nil {
				return fmt.Errorf("identity: retiring a superseded tax profile: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO party_tax_profiles (
					tenant_id, party_id, tax_residency, residence_country, pan_furnished,
					foreign_tin_masked, trc_number, trc_valid_from, trc_valid_to,
					foreign_address, foreign_email, foreign_phone, source, valid_from)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5,
				        nullif($6,''), nullif($7,''), nullif($8,'')::date, nullif($9,'')::date,
				        nullif($10,''), nullif($11,''), nullif($12,''), $13, $14)`,
				string(firm), party, p.Residency, p.ResidenceCountry, p.PANFurnished,
				p.ForeignTINMasked, p.TRCNumber, p.TRCValidFrom, p.TRCValidTo,
				p.ForeignAddress, p.ForeignEmail, p.ForeignPhone, p.Source, from); err != nil {
				return fmt.Errorf("identity: writing the tax profile: %w", err)
			}
			return nil
		})
}

// TaxProfile reads what was true on a date, which is not always what is true now.
func (s *Principals) TaxProfile(ctx context.Context, firm tenancy.ID, party string, on time.Time) (TaxProfile, error) {
	var out TaxProfile
	err := tenancy.Platform(ctx, s.platform, "reading a tax profile",
		func(ctx context.Context, tx pgx.Tx) error {
			err := tx.QueryRow(ctx, `
				SELECT tax_residency, residence_country, pan_furnished,
				       coalesce(foreign_tin_masked,''), coalesce(trc_number,''),
				       coalesce(to_char(trc_valid_from,'YYYY-MM-DD'),''),
				       coalesce(to_char(trc_valid_to,'YYYY-MM-DD'),''),
				       coalesce(foreign_address,''), coalesce(foreign_email,''),
				       coalesce(foreign_phone,''), source,
				       to_char(valid_from,'YYYY-MM-DD'), rule_37bc_furnished
				  FROM party_tax_profiles
				 WHERE tenant_id = $1::uuid AND party_id = $2::uuid
				   AND retired_at IS NULL AND validity @> $3::date`,
				string(firm), party, on).Scan(
				&out.Residency, &out.ResidenceCountry, &out.PANFurnished,
				&out.ForeignTINMasked, &out.TRCNumber, &out.TRCValidFrom, &out.TRCValidTo,
				&out.ForeignAddress, &out.ForeignEmail, &out.ForeignPhone, &out.Source,
				&out.ValidFrom, &out.Rule37BCFurnished)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoTaxProfile
			}
			if err != nil {
				return fmt.Errorf("identity: reading the tax profile: %w", err)
			}
			return nil
		})
	return out, err
}
