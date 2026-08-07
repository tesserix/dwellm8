package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ErrNoTaxFacts is a tenancy with nothing on file for the date asked about.
// A date before the tenancy's facts begin is the ordinary case: it is not a
// missing row, it is a question about a month this lease did not exist in.
var ErrNoTaxFacts = errors.New("lease: no tax facts in force on that date")

// TaxFacts reads the two facts that select the section, as at a date.
//
// Dated for the reason the rows are: a landlord who becomes non-resident in
// October leaves April's payment on section 194-I, and a deduction recomputed in
// November has to resolve April's facts rather than today's.
func (s *Leases) TaxFacts(ctx context.Context, leaseID string, on effective.Date) (tds.Facts, error) {
	var (
		out            tds.Facts
		deductor       string
		residency      string
		acknowledgedOn *time.Time
		acknowledgedBy *string
		from           time.Time
	)
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT deductor_class, landlord_residency, source,
			       acknowledged_on, acknowledged_by, valid_from
			  FROM lease_tax_facts
			 WHERE lease_id = $1::uuid AND retired_at IS NULL AND validity @> $2::date`,
			leaseID, on.Time()).Scan(&deductor, &residency, &out.Source,
			&acknowledgedOn, &acknowledgedBy, &from)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return tds.Facts{}, fmt.Errorf("%w: lease %s on %s", ErrNoTaxFacts, leaseID, on)
	}
	if err != nil {
		return tds.Facts{}, fmt.Errorf("lease: reading tax facts: %w", err)
	}

	out.Deductor = tds.DeductorClass(deductor)
	out.Residency = tds.Residency(residency)
	out.From = asDay(from)
	if acknowledgedOn != nil {
		out.AcknowledgedOn = asDay(*acknowledgedOn)
	}
	if acknowledgedBy != nil {
		out.AcknowledgedBy = *acknowledgedBy
	}
	return out, nil
}

// AnnualRentToOwner is the rent payable to one landlord across every tenancy of
// theirs in the financial year holding `on`, in paise.
//
// Aggregated to the payee rather than to the property, because that is what
// section 194-I's threshold is written against: two flats let by one owner are
// one threshold, and testing them separately is the classic under-deduction.
//
// A month counts where the tenancy is in force for any part of it, and where two
// rents overlap a month the higher is taken. Both roundings are deliberately
// upwards: this figure decides whether tax is deducted at all, and the direction
// that errs is the one that deducts.
func (s *Leases) AnnualRentToOwner(ctx context.Context, ownerParty string, on effective.Date) (int64, error) {
	start := financialYearStart(on)
	var total int64
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT coalesce(sum(rent.amount_minor), 0)::bigint
			  FROM leases l
			  CROSS JOIN LATERAL generate_series($2::date, ($2::date + interval '11 months')::date,
			                                     interval '1 month') AS g(month)
			  JOIN LATERAL (
			       SELECT rs.amount_minor
			         FROM rent_schedule rs
			        WHERE rs.lease_id = l.id AND rs.retired_at IS NULL
			          AND rs.validity && daterange(g.month::date, (g.month + interval '1 month')::date)
			        ORDER BY rs.amount_minor DESC
			        LIMIT 1) AS rent ON true
			 WHERE l.state IN ('active', 'in_notice', 'renewed', 'terminated', 'settled')
			   AND l.validity && daterange(g.month::date, (g.month + interval '1 month')::date)
			   AND EXISTS (
			       SELECT 1 FROM property_ownership o
			        WHERE o.owner_party_id = $1::uuid AND o.retired_at IS NULL
			          AND (o.unit_id = l.unit_id
			               OR (o.unit_id IS NULL AND o.property_id = l.property_id))
			          AND o.validity && daterange(g.month::date,
			                                      (g.month + interval '1 month')::date))`,
			ownerParty, start).Scan(&total)
	})
	if err != nil {
		return 0, fmt.Errorf("lease: aggregating the year's rent to %s: %w", ownerParty, err)
	}
	return total, nil
}

// financialYearStart is the 1 April on or before a date — India's tax year.
func financialYearStart(on effective.Date) time.Time {
	t := on.Time()
	year := t.Year()
	if t.Month() < time.April {
		year--
	}
	return time.Date(year, time.April, 1, 0, 0, 0, 0, time.UTC)
}

func asDay(t time.Time) effective.Date {
	return effective.Day(t.Year(), t.Month(), t.Day())
}
