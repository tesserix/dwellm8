package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Five years of address history (#259). The gaps are what the manager needs:
// a hole is reported so it gets asked about, and an overlap — a family home
// kept while renting — is ordinary. Detection lives here rather than in a
// constraint because a gap is a question, not a violation.

// historyMonths is the span an Indian rental history is judged over: two or
// three 11-month tenancies plus a family home.
const historyMonths = 60

// ErrAddressWindow is a window that cannot have happened.
var ErrAddressWindow = errors.New("applicant: an address must end after it began, and cannot begin in the future")

// ErrAddressShape is an Indian address missing what a letter would need.
var ErrAddressShape = errors.New("applicant: an Indian address needs a state code and a PIN")

// Address is one place the applicant lived, by month.
type Address struct {
	ID            string
	Kind          string
	Line1         string
	Locality      string
	City          string
	StateCode     string
	Pin           string
	Country       string
	From          time.Time
	To            *time.Time
	LandlordName  string
	LandlordPhone string
}

// Gap is a stretch of the last five years nobody has accounted for.
type Gap struct {
	From time.Time
	To   time.Time
}

// History is the set and what is missing from it.
type History struct {
	Addresses []Address
	Gaps      []Gap
	Complete  bool
}

// SaveAddresses replaces the history whole — it is edited as one list, and
// reconciling rows the applicant deleted would buy nothing.
func (s *Applicants) SaveAddresses(ctx context.Context, applicationID string, in []Address) error {
	for _, a := range in {
		// Named, because a manager fixing a five-row history needs to know
		// which row is wrong.
		if err := a.validate(); err != nil {
			return fmt.Errorf("%s: %w", a.Line1, err)
		}
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		profile, tenant, err := liveProfileOf(ctx, tx, applicationID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM applicant_addresses WHERE profile_id = $1`, profile); err != nil {
			return err
		}
		for _, a := range in {
			if _, err := tx.Exec(ctx, `
				INSERT INTO applicant_addresses (tenant_id, profile_id, kind, line1, locality,
				                                 city, state_code, pin, country, from_month,
				                                 to_month, landlord_name, landlord_phone)
				VALUES ($1, $2, $3, $4, nullif($5, ''), $6, nullif($7, ''), nullif($8, ''),
				        coalesce(nullif($9, ''), 'IN'), $10, $11, nullif($12, ''), nullif($13, ''))`,
				tenant, profile, a.Kind, a.Line1, a.Locality, a.City, a.StateCode, a.Pin,
				a.Country, firstOfMonth(a.From), firstOfMonthPtr(a.To),
				a.LandlordName, a.LandlordPhone); err != nil {
				return err
			}
		}
		return nil
	})
}

// Addresses is the history, newest first, with the gaps named.
func (s *Applicants) Addresses(ctx context.Context, applicationID string) (History, error) {
	var out History
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		profile, _, err := liveProfileOf(ctx, tx, applicationID)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT id::text, kind, line1, coalesce(locality, ''), city,
			       coalesce(state_code, ''), coalesce(pin, ''), country, from_month, to_month,
			       coalesce(landlord_name, ''), coalesce(landlord_phone, '')
			  FROM applicant_addresses WHERE profile_id = $1
			 ORDER BY from_month DESC`, profile)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Address
			if err := rows.Scan(&a.ID, &a.Kind, &a.Line1, &a.Locality, &a.City, &a.StateCode,
				&a.Pin, &a.Country, &a.From, &a.To, &a.LandlordName, &a.LandlordPhone); err != nil {
				return err
			}
			out.Addresses = append(out.Addresses, a)
		}
		return rows.Err()
	})
	if err != nil {
		return History{}, err
	}
	out.Gaps = gapsIn(out.Addresses, time.Now())
	out.Complete = len(out.Gaps) == 0
	return out, nil
}

// gapsIn walks the last five years month by month. Overlaps collapse for free
// that way, which is the behaviour wanted: a family home kept while renting
// covers the same months twice and leaves nothing missing.
func gapsIn(list []Address, now time.Time) []Gap {
	if len(list) == 0 {
		return nil
	}
	this := firstOfMonth(now)
	covered := make(map[time.Time]bool, historyMonths)
	for _, a := range list {
		to := this
		if a.To != nil {
			to = firstOfMonth(*a.To)
		}
		for m := firstOfMonth(a.From); !m.After(to); m = m.AddDate(0, 1, 0) {
			covered[m] = true
		}
	}

	var gaps []Gap
	var open *Gap
	for i := historyMonths - 1; i >= 0; i-- {
		m := this.AddDate(0, -i, 0)
		switch {
		case covered[m] && open != nil:
			gaps = append(gaps, *open)
			open = nil
		case !covered[m] && open == nil:
			open = &Gap{From: m, To: m}
		case !covered[m]:
			open.To = m
		}
	}
	if open != nil {
		gaps = append(gaps, *open)
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].From.After(gaps[j].From) })
	return gaps
}

func (a Address) validate() error {
	if a.To != nil && a.To.Before(a.From) {
		return ErrAddressWindow
	}
	if firstOfMonth(a.From).After(firstOfMonth(time.Now())) {
		return ErrAddressWindow
	}
	country := a.Country
	if country == "" {
		country = "IN"
	}
	if country == "IN" && (a.StateCode == "" || a.Pin == "") {
		return ErrAddressShape
	}
	return nil
}

// liveProfileOf is the pack every hanging row belongs to.
func liveProfileOf(ctx context.Context, tx pgx.Tx, applicationID string) (string, string, error) {
	var profile, tenant string
	err := tx.QueryRow(ctx, `
		SELECT id::text, tenant_id::text FROM applicant_profiles`+liveProfile, applicationID).
		Scan(&profile, &tenant)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNoProfile
	}
	return profile, tenant, err
}

func firstOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func firstOfMonthPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	m := firstOfMonth(*t)
	return &m
}
