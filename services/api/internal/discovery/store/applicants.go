package store

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The applicant pack (#258): who is applying, who else moves in, and the
// effective dating that keeps a decision explicable months later. A change
// writes a new row and retires the one it replaces (ADR-0008 §5), so the pack
// the manager saw when they declined is still readable after the applicant
// corrects their employer.

// ErrNoProfile is no pack, including one hidden by policy.
var ErrNoProfile = errors.New("applicant: no pack")

// ErrAlreadySubmitted separates a pack already closed from one never started —
// the caller sees them identically otherwise, and they need different words.
var ErrAlreadySubmitted = errors.New("applicant: this pack has already been submitted")

// ErrPhone is a number that is not E.164 (ADR-0029). The column's CHECK says
// the same thing, but a 500 from a typed phone number is not an answer.
var ErrPhone = errors.New("applicant: that is not a mobile number in E.164")

var e164 = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)

// Profile is the applicant themselves.
type Profile struct {
	ID            string
	ApplicationID string
	FullName      string
	DateOfBirth   *time.Time
	Nationality   string
	TaxResidency  string
	Occupants     int
	Pets          bool
	PetsNote      string
	State         string
	SubmittedAt   time.Time
	Corrects      string
	CreatedAt     time.Time
}

// Person is anyone else the tenancy carries — a spouse, a child, a guarantor.
type Person struct {
	ID           string
	Role         string
	FullName     string
	Relationship string
	DateOfBirth  *time.Time
	Phone        string
	Email        string
}

// Applicants is the store.
type Applicants struct {
	pool     tenancy.Pool
	platform tenancy.PlatformPool
}

// NewApplicants builds the store.
func NewApplicants(p tenancy.Pool, plat tenancy.PlatformPool) *Applicants {
	return &Applicants{pool: p, platform: plat}
}

const selectProfile = `
	SELECT id::text, application_id::text, full_name, date_of_birth, nationality,
	       tax_residency, occupants, pets, coalesce(pets_note, ''), state,
	       submitted_at, coalesce(corrects::text, ''), created_at
	  FROM applicant_profiles`

const liveProfile = ` WHERE application_id = $1 AND retired_at IS NULL AND valid_to IS NULL`

// SaveProfile writes the pack, superseding whatever stood before it. Saving
// twice is a correction, not an edit: the earlier row is retired and named by
// the new one, so a decision taken on the earlier pack still reads as it did.
func (s *Applicants) SaveProfile(ctx context.Context, applicationID string, p Profile) (Profile, error) {
	var out Profile
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var tenant, property, unit string
		err := tx.QueryRow(ctx, `
			SELECT tenant_id::text, property_id::text, unit_id::text
			  FROM listing_applications WHERE id = $1`, applicationID).
			Scan(&tenant, &property, &unit)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoApplication
		}
		if err != nil {
			return err
		}

		var previous string
		err = tx.QueryRow(ctx, `SELECT id::text FROM applicant_profiles`+liveProfile, applicationID).
			Scan(&previous)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if previous != "" {
			if _, err := tx.Exec(ctx, `
				UPDATE applicant_profiles SET retired_at = now(), updated_at = now()
				 WHERE id = $1`, previous); err != nil {
				return err
			}
		}

		if err := scanProfile(tx.QueryRow(ctx, `
			INSERT INTO applicant_profiles (tenant_id, application_id, property_id, unit_id,
			                                full_name, date_of_birth, nationality, tax_residency,
			                                occupants, pets, pets_note, corrects)
			VALUES ($1, $2, $3, $4, $5, $6, coalesce(nullif($7, ''), 'IN'),
			        coalesce(nullif($8, ''), 'resident'), coalesce(nullif($9, 0), 1), $10, $11,
			        nullif($12, '')::uuid)
			RETURNING`+returningProfile,
			tenant, applicationID, property, unit, p.FullName, p.DateOfBirth, p.Nationality,
			p.TaxResidency, p.Occupants, p.Pets, nullText(p.PetsNote), previous), &out); err != nil {
			return err
		}
		if previous == "" {
			return nil
		}
		// The household travels with the pack. Correcting a tax residency must
		// not quietly drop the spouse and the children.
		_, err = tx.Exec(ctx, `
			INSERT INTO applicant_people (tenant_id, profile_id, role, full_name,
			                              relationship, date_of_birth, phone, email)
			SELECT tenant_id, $1, role, full_name, relationship, date_of_birth, phone, email
			  FROM applicant_people WHERE profile_id = $2`, out.ID, previous)
		return err
	})
	return out, err
}

const returningProfile = ` id::text, application_id::text, full_name, date_of_birth, nationality,
	       tax_residency, occupants, pets, coalesce(pets_note, ''), state,
	       submitted_at, coalesce(corrects::text, ''), created_at`

// Profile is the pack as it stands.
func (s *Applicants) Profile(ctx context.Context, applicationID string) (Profile, error) {
	var p Profile
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return scanProfile(tx.QueryRow(ctx, selectProfile+liveProfile, applicationID), &p)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Profile{}, ErrNoProfile
	}
	return p, err
}

// Submit closes the draft. A pack nobody started cannot be submitted, which is
// the difference between "they told us nothing" and "they told us nothing yet".
func (s *Applicants) Submit(ctx context.Context, applicationID string) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var state string
		err := tx.QueryRow(ctx, `SELECT state FROM applicant_profiles`+liveProfile, applicationID).
			Scan(&state)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoProfile
		}
		if err != nil {
			return err
		}
		if state == "submitted" {
			return ErrAlreadySubmitted
		}
		_, err = tx.Exec(ctx, `
			UPDATE applicant_profiles
			   SET state = 'submitted', submitted_at = now(), updated_at = now()`+
			liveProfile, applicationID)
		return err
	})
}

// SavePeople replaces the household in one go. The set is small and always
// edited whole, so a replace beats reconciling rows the applicant deleted.
func (s *Applicants) SavePeople(ctx context.Context, applicationID string, people []Person) error {
	for _, m := range people {
		if m.Phone != "" && !e164.MatchString(m.Phone) {
			return ErrPhone
		}
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var profile, tenant string
		err := tx.QueryRow(ctx, `
			SELECT id::text, tenant_id::text FROM applicant_profiles`+liveProfile, applicationID).
			Scan(&profile, &tenant)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoProfile
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM applicant_people WHERE profile_id = $1`, profile); err != nil {
			return err
		}
		for _, m := range people {
			if _, err := tx.Exec(ctx, `
				INSERT INTO applicant_people (tenant_id, profile_id, role, full_name,
				                              relationship, date_of_birth, phone, email)
				VALUES ($1, $2, $3, $4, $5, $6, nullif($7, ''), nullif($8, ''))`,
				tenant, profile, m.Role, m.FullName, nullText(m.Relationship),
				m.DateOfBirth, m.Phone, m.Email); err != nil {
				return err
			}
		}
		return nil
	})
}

// People is the household, guarantors last.
func (s *Applicants) People(ctx context.Context, applicationID string) ([]Person, error) {
	var out []Person
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT p.id::text, p.role, p.full_name, coalesce(p.relationship, ''),
			       p.date_of_birth, coalesce(p.phone, ''), coalesce(p.email, '')
			  FROM applicant_people p
			  JOIN applicant_profiles f ON f.id = p.profile_id
			 WHERE f.application_id = $1 AND f.retired_at IS NULL AND f.valid_to IS NULL
			 ORDER BY p.role, p.created_at`, applicationID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Person
			if err := rows.Scan(&m.ID, &m.Role, &m.FullName, &m.Relationship,
				&m.DateOfBirth, &m.Phone, &m.Email); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

func scanProfile(row pgx.Row, p *Profile) error {
	var submitted *time.Time
	err := row.Scan(&p.ID, &p.ApplicationID, &p.FullName, &p.DateOfBirth, &p.Nationality,
		&p.TaxResidency, &p.Occupants, &p.Pets, &p.PetsNote, &p.State,
		&submitted, &p.Corrects, &p.CreatedAt)
	if submitted != nil {
		p.SubmittedAt = *submitted
	}
	return err
}
