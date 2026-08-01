// Package store reads the section 197 certificates a rate override rests on.
// ADR-0025.
//
// Separate from statutory/store, and the separation is the point: that one reads
// reference data with no tenant and no row-level security, and this one reads a
// tenant's own records under both. A certificate is a fact about one landlord
// that one organisation holds, so it is queried in a scoped session like every
// other tenant table.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

// Querier is a scoped transaction or pool. Scoped, because the policy is what
// stops one organisation reading another's certificates.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Certificates reads section 197 certificates.
type Certificates struct{ q Querier }

// New takes anything that can query within a tenant's session.
func New(q Querier) *Certificates { return &Certificates{q: q} }

// ForParty returns the certificate in force for a payee and a section on a date,
// and false where none is.
//
// One or none, which the exclusion constraint guarantees: two live certificates
// for the same landlord, section and day cannot exist, so this does not have to
// decide between them.
func (c *Certificates) ForParty(
	ctx context.Context, partyID string, section tds.Section, on effective.Date,
) (tds.Certificate, bool, error) {
	rows, err := c.q.Query(ctx, `
		SELECT certificate_number, section, rate_bps, valid_from, valid_to, issued_on
		  FROM tds_certificates
		 WHERE party_id = $1
		   AND section = $2
		   AND retired_at IS NULL
		   AND validity @> $3::date`,
		partyID, string(section), on.Time())
	if err != nil {
		return tds.Certificate{}, false, fmt.Errorf("tds: reading certificates for %s: %w", partyID, err)
	}
	defer rows.Close()

	if !rows.Next() {
		return tds.Certificate{}, false, rows.Err()
	}
	var (
		out              tds.Certificate
		sec              string
		from, to, issued time.Time
	)
	if err := rows.Scan(&out.Number, &sec, &out.RateBps, &from, &to, &issued); err != nil {
		return tds.Certificate{}, false, fmt.Errorf("tds: scanning a certificate: %w", err)
	}
	out.Section = tds.Section(sec)
	out.IssuedOn = day(issued)
	if out.Validity, err = effective.Between(day(from), day(to)); err != nil {
		return tds.Certificate{}, false, fmt.Errorf("tds: certificate %s: %w", out.Number, err)
	}
	return out, true, rows.Err()
}

// Profile fills a payee's certificate in, leaving everything else as the caller
// set it.
//
// The PAN and filer flags are not read here: they belong to the payee's identity
// record and this package does not reach into another module's tables (ADR-0001
// §3). A caller assembles the profile and asks this only for the certificate.
func (c *Certificates) Profile(
	ctx context.Context, p tds.PayeeProfile, partyID string, section tds.Section, on effective.Date,
) (tds.PayeeProfile, error) {
	cert, found, err := c.ForParty(ctx, partyID, section, on)
	if err != nil {
		return p, err
	}
	if found {
		p.Certificate = &cert
	}
	return p, nil
}

func day(t time.Time) effective.Date {
	return effective.Day(t.Year(), t.Month(), t.Day())
}

// Expiring is a lower-deduction certificate running out. A landlord whose
// certificate lapses is deducted at the full rate from the next payment, which
// they discover on the payout rather than in time to renew.
type Expiring struct {
	PartyID           string
	CertificateNumber string
	Section           tds.Section
	ValidTo           effective.Date
	DaysRemaining     int
}

// Expiring lists live certificates lapsing within `within` days.
//
// Ordered by expiry, so the one with least time left is first — the order the
// work should actually be done in, which a screen should not have to re-derive.
func (c *Certificates) Expiring(ctx context.Context, on effective.Date, within int) ([]Expiring, error) {
	if within <= 0 {
		within = 45
	}
	rows, err := c.q.Query(ctx, `
		SELECT party_id::text, certificate_number, section, valid_to, (valid_to - $1::date)
		  FROM tds_certificates
		 WHERE retired_at IS NULL
		   AND valid_to > $1::date
		   AND valid_to <= ($1::date + $2)
		 ORDER BY valid_to`, on.Time(), within)
	if err != nil {
		return nil, fmt.Errorf("tds: reading expiring certificates: %w", err)
	}
	defer rows.Close()

	var out []Expiring
	for rows.Next() {
		var e Expiring
		var section string
		var validTo time.Time
		if err := rows.Scan(&e.PartyID, &e.CertificateNumber, &section, &validTo,
			&e.DaysRemaining); err != nil {
			return nil, err
		}
		e.Section, e.ValidTo = tds.Section(section), day(validTo)
		out = append(out, e)
	}
	return out, rows.Err()
}
