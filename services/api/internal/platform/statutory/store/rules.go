// Package store reads the statutory rule registry. ADR-0023.
//
// Read-only by construction: the schema revokes INSERT, UPDATE and DELETE on
// statutory_rules from dwellm8_app, so there is no writer to write here. A rate
// change is a reviewed commit to the schema file, not an API call.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory"
)

// Querier is what this store needs: a pool or a transaction. Reference data
// carries no tenant, so this is not tenancy.Pool and no app.tenant_id is set.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Rules reads the registry.
type Rules struct{ q Querier }

// New takes anything that can query.
func New(q Querier) *Rules { return &Rules{q: q} }

// Load returns every live rule with its bands.
//
// The whole registry, in two queries, because it is small — tens of rows — and
// because a resolution per lookup would put a round trip inside a loop that bills
// a thousand tenancies. Callers hold the Table and reload it, rather than
// querying per invoice.
func (s *Rules) Load(ctx context.Context) ([]statutory.Rule, error) {
	rows, err := s.q.Query(ctx, `
		SELECT id, rule_type, jurisdiction, rule_key, value_kind,
		       coalesce(rate_bps, 0), coalesce(amount_minor, 0), coalesce(count_value, 0),
		       valid_from, valid_to,
		       coalesce(corrects::text, ''), statute_ref, coalesce(source_url, ''),
		       verification_status, coalesce(verified_by, ''), verified_on,
		       owner, review_due, enforcement, coalesce(note, '')
		  FROM statutory_rules
		 WHERE retired_at IS NULL
		 ORDER BY rule_type, jurisdiction, rule_key, valid_from`)
	if err != nil {
		return nil, fmt.Errorf("statutory: reading the registry: %w", err)
	}
	defer rows.Close()

	byID := map[string]*statutory.Rule{}
	var out []statutory.Rule
	for rows.Next() {
		var (
			r                 statutory.Rule
			from              time.Time
			to, verifiedOn    *time.Time
			reviewDue         time.Time
			kind, typ, juris  string
			verification, enf string
		)
		if err := rows.Scan(&r.ID, &typ, &juris, &r.Key, &kind,
			&r.RateBps, &r.AmountMinor, &r.CountValue,
			&from, &to, &r.Corrects, &r.StatuteRef, &r.SourceURL,
			&verification, &r.VerifiedBy, &verifiedOn,
			&r.Owner, &reviewDue, &enf, &r.Note); err != nil {
			return nil, fmt.Errorf("statutory: scanning a rule: %w", err)
		}
		r.Type = statutory.Type(typ)
		r.Jurisdiction = statutory.Jurisdiction(juris)
		r.Kind = statutory.Kind(kind)
		r.Verification = statutory.Verification(verification)
		r.Enforcement = statutory.Enforcement(enf)
		r.ReviewDue = day(reviewDue)
		if verifiedOn != nil {
			r.VerifiedOn = day(*verifiedOn)
		}
		if r.Validity, err = interval(from, to); err != nil {
			return nil, fmt.Errorf("statutory: rule %s: %w", r.ID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("statutory: reading the registry: %w", err)
	}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	if err := s.loadSlabs(ctx, byID); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Rules) loadSlabs(ctx context.Context, byID map[string]*statutory.Rule) error {
	rows, err := s.q.Query(ctx, `
		SELECT rule_id, seq, lower_minor, upper_minor,
		       coalesce(rate_bps, 0), coalesce(flat_minor, 0)
		  FROM statutory_rule_slabs
		 ORDER BY rule_id, lower_minor`)
	if err != nil {
		return fmt.Errorf("statutory: reading the bands: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			ruleID string
			s      statutory.Slab
			upper  *int64
		)
		if err := rows.Scan(&ruleID, &s.Seq, &s.LowerMinor, &upper, &s.RateBps, &s.FlatMinor); err != nil {
			return fmt.Errorf("statutory: scanning a band: %w", err)
		}
		if upper == nil {
			s.Top = true
		} else {
			s.UpperMinor = *upper
		}
		rule, ok := byID[ruleID]
		if !ok {
			// A band whose rule is retired. The ON DELETE CASCADE means it cannot be
			// orphaned, so this is history rather than a fault.
			continue
		}
		rule.Slabs = append(rule.Slabs, s)
	}
	return rows.Err()
}

// Table loads and indexes the registry in one call, which is what a caller
// actually wants.
func (s *Rules) Table(ctx context.Context) (*statutory.Table, error) {
	rules, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	return statutory.NewTable(rules)
}

// Due is one row of the review report.
type Due struct {
	ID           string
	Type         statutory.Type
	Jurisdiction statutory.Jurisdiction
	Key          string
	Owner        string
	ReviewDue    effective.Date
	DaysOverdue  int
	Verification statutory.Verification
	Enforcement  statutory.Enforcement
	StatuteRef   string
}

// ReviewDue reads the database's own view rather than filtering in Go.
//
// Two implementations of "due for review" would be one too many, and this is the
// one an operator can run by hand at three in the morning. Table.DueForReview
// answers the same question against a registry already in memory; the store test
// asserts they agree.
func (s *Rules) ReviewDue(ctx context.Context) ([]Due, error) {
	rows, err := s.q.Query(ctx, `
		SELECT id, rule_type, jurisdiction, rule_key, owner, review_due, days_overdue,
		       verification_status, enforcement, statute_ref
		  FROM statutory_rules_review_due
		 ORDER BY days_overdue DESC, rule_key`)
	if err != nil {
		return nil, fmt.Errorf("statutory: reading the review report: %w", err)
	}
	defer rows.Close()

	var out []Due
	for rows.Next() {
		var (
			d                    Due
			typ, juris, ver, enf string
			reviewDue            time.Time
		)
		if err := rows.Scan(&d.ID, &typ, &juris, &d.Key, &d.Owner, &reviewDue, &d.DaysOverdue,
			&ver, &enf, &d.StatuteRef); err != nil {
			return nil, fmt.Errorf("statutory: scanning the review report: %w", err)
		}
		d.Type = statutory.Type(typ)
		d.Jurisdiction = statutory.Jurisdiction(juris)
		d.Verification = statutory.Verification(ver)
		d.Enforcement = statutory.Enforcement(enf)
		d.ReviewDue = day(reviewDue)
		out = append(out, d)
	}
	return out, rows.Err()
}

// day drops the time the driver attaches to a `date`. UTC, because that is what
// the driver read it as: no timezone conversion happens, and none should — a date
// column has no instant to convert.
func day(t time.Time) effective.Date {
	return effective.Day(t.Year(), t.Month(), t.Day())
}

func interval(from time.Time, to *time.Time) (effective.Interval, error) {
	if to == nil {
		return effective.Since(day(from))
	}
	return effective.Between(day(from), day(*to))
}
