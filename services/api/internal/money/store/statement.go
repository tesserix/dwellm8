package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// What one tenancy owes, and what has happened on it. Issue #51.
//
// Everything here is derived from postings. There is no "amount due" column to
// read and none to go stale, which is ADR-0006 §5 and is also the only reason a
// tenant and their landlord can be shown the same number: both sums run over the
// same rows, and neither is a cache of the other.
//
// The breakdown is by the *entry kind* that raised each receivable line, not by
// a category column. A charge knows what it was — an invoice, a late fee, a
// correction — and asking the ledger is what stops a second vocabulary growing
// beside ADR-0006's.

// Statement is a tenancy's position, split into the parts a person recognises.
//
// Every field is signed the way a tenant reads it: positive means owed. Paid and
// Advance are therefore reported as the positive amounts they are, and Due is
// what remains after both.
type Statement struct {
	// Rent and LateFee are what has been charged, ever, on this tenancy.
	Rent    domain.Minor
	LateFee domain.Minor
	// Adjustment is everything else that moved the receivable: a write-off, a
	// refund, a reversing correction. One bucket rather than a list of kinds the
	// tenant would have to interpret, and the lines below say which was which.
	Adjustment domain.Minor
	// Paid is what has been received against the receivable.
	Paid domain.Minor
	// Advance is money held that no charge has claimed yet — a tenant who paid
	// early or paid round. It is a liability of the landlord's, not income.
	Advance domain.Minor
	// Deposit is the security deposit held, derived from deposit_liability.
	Deposit domain.Minor
	// Due is Rent + LateFee + Adjustment − Paid − Advance: the number on the
	// screen, and the number the pay button is for.
	Due domain.Minor
}

// Charge is one ledger event on the tenancy, as it affected what the tenant
// owes.
type Charge struct {
	EntryID    string
	Kind       domain.EventKind
	OccurredOn time.Time
	// AmountMinor is signed: positive increases what is owed.
	AmountMinor domain.Minor
	Memo        string
	// Reversed is set on an entry that a later reversing entry corrected, so the
	// history can show it struck through rather than silently dropping a line
	// the tenant may have a copy of.
	Reversed bool
}

// Statement derives what a tenancy owes as of a date.
//
// Two queries, not one: the receivable and the advance are the tenant's own
// party lines, and the deposit is a third account with the same party. Combining
// them into a single grouped scan is possible and produces a query nobody can
// read six months later, for one round trip.
func (l *Ledger) Statement(ctx context.Context, leaseID, partyID string, asOf time.Time) (Statement, error) {
	var s Statement
	err := tenancy.Scoped(ctx, l.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT p.account_code, e.entry_kind, sum(p.signed_minor)
			  FROM ledger_postings p
			  JOIN journal_entries e ON e.id = p.entry_id AND e.tenant_id = p.tenant_id
			 WHERE e.lease_id    = $1::uuid
			   AND p.party_kind  = 'tenant'
			   AND p.party_id    = $2::uuid
			   AND p.account_code = ANY($4)
			   AND e.occurred_on <= $3::date
			 GROUP BY p.account_code, e.entry_kind`,
			leaseID, partyID, asOf,
			[]string{domain.TenantReceivable, domain.TenantAdvance, domain.DepositLiability})
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var account, kind string
			var amount domain.Minor
			if err := rows.Scan(&account, &kind, &amount); err != nil {
				return err
			}
			switch account {
			case domain.TenantAdvance, domain.DepositLiability:
				// Liabilities, held as credits, so the signed sum is negative and
				// the amount a person would recognise is its opposite.
				if account == domain.TenantAdvance {
					s.Advance -= amount
				} else {
					s.Deposit -= amount
				}
			default:
				switch domain.EventKind(kind) {
				case domain.KindInvoice:
					s.Rent += amount
				case domain.KindLateFee:
					s.LateFee += amount
				case domain.KindPayment, domain.KindPaymentWithTDS:
					// Credited, so the signed sum is negative and Paid is positive.
					s.Paid -= amount
				default:
					s.Adjustment += amount
				}
			}
		}
		return rows.Err()
	})
	if err != nil {
		return Statement{}, fmt.Errorf("money: deriving the statement for lease %s: %w", leaseID, err)
	}

	s.Due = s.Rent + s.LateFee + s.Adjustment - s.Paid - s.Advance
	if s.Due < 0 {
		// Owed less than nothing is owed nothing; the excess is already reported
		// as Advance, and showing a negative due would read as a refund the
		// tenant is not being sent.
		s.Due = 0
	}
	return s, nil
}

// Charges lists the ledger events on a tenancy, most recent first.
//
// Bounded by limit because this is the screen a tenant opens on a phone: six
// months of a monthly tenancy is six rows, and the tenancy that has forty is the
// one where an unbounded query matters.
func (l *Ledger) Charges(ctx context.Context, leaseID, partyID string, limit int) ([]Charge, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []Charge
	err := tenancy.Scoped(ctx, l.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT e.id::text, e.entry_kind, e.occurred_on, sum(p.signed_minor),
			       coalesce(max(e.memo), ''),
			       EXISTS (SELECT 1 FROM journal_entries r
			                WHERE r.reverses_entry_id = e.id AND r.tenant_id = e.tenant_id)
			  FROM ledger_postings p
			  JOIN journal_entries e ON e.id = p.entry_id AND e.tenant_id = p.tenant_id
			 WHERE e.lease_id   = $1::uuid
			   AND p.party_kind = 'tenant'
			   AND p.party_id   = $2::uuid
			 GROUP BY e.id, e.entry_kind, e.occurred_on, e.tenant_id
			 ORDER BY e.occurred_on DESC, e.id
			 LIMIT $3`, leaseID, partyID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var c Charge
			var kind string
			if err := rows.Scan(&c.EntryID, &kind, &c.OccurredOn, &c.AmountMinor,
				&c.Memo, &c.Reversed); err != nil {
				return err
			}
			c.Kind = domain.EventKind(kind)
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("money: reading the charges on lease %s: %w", leaseID, err)
	}
	return out, nil
}

// ByLease lists a tenancy's payments, most recent first.
//
// Every attempt, not only the successful ones. A tenant whose UPI mandate failed
// on the 5th needs to see that it failed, or the first they know of it is a late
// fee — and a history that showed only what worked would be the product hiding
// the thing the person most needs to act on.
func (s *Payments) ByLease(ctx context.Context, leaseID string, limit int) ([]collect.Payment, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []collect.Payment
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+paymentColumns+`
			  FROM payments p
			 WHERE p.lease_id = $1::uuid
			 ORDER BY p.created_at DESC
			 LIMIT $2`, leaseID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var p collect.Payment
			var kind, method, status string
			if err := rows.Scan(&p.ID, &p.TenantID, &p.Property, &p.Unit, &p.MandateID, &p.Lease,
				&kind, &p.PayerID, &p.Amount, &method, &p.Provider,
				&p.ProviderOrderID, &p.ProviderPaymentID, &status, &p.FailureCode,
				&p.IdempotencyKey, &p.EntryID, &p.CreatedAt, &p.Bearer); err != nil {
				return err
			}
			p.PayerKind, p.Method, p.Status = domain.PartyKind(kind), collect.Method(method), collect.Status(status)
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("money: reading the payments on lease %s: %w", leaseID, err)
	}
	return out, nil
}

// ByID reads one payment.
func (s *Payments) ByID(ctx context.Context, id string) (collect.Payment, error) {
	return s.one(ctx, `WHERE p.id = $1::uuid`, id)
}
