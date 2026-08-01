package service

import (
	"context"
	"fmt"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
)

// What an owner is shown for one property, one calendar month. Companion to
// statement.go's tenant-facing Statement — a different audience, so its own
// file, but the same ledger: nothing here is stored, everything is summed
// from ledger_postings via Ledger.Balances.

// OwnerLine is one account's movement within the month, signed the way a
// statement reads: income positive, a fee or expense negative.
type OwnerLine struct {
	Account     string
	Label       string
	AmountMinor domain.Minor
}

// OwnerStatement is a property's net position for one month, as the owner
// reads it: what rent came in, what was deducted, and what that nets to.
type OwnerStatement struct {
	PropertyID   string
	From, To     time.Time
	NetMinor     domain.Minor
	IncomeMinor  domain.Minor
	ExpenseMinor domain.Minor
	Lines        []OwnerLine
}

// OwnerStatement sums the owner-party postings against a property for the
// calendar month containing `for`.
//
// signed_minor is debit-positive by the schema's one sign convention
// (030_ledger.sql), regardless of account type — so a credit to an
// income account (rent recognised) and a debit to an expense account (a fee
// taken) both need negating to read as a statement: income positive, a
// deduction negative. That negation happens once, here, rather than in every
// caller that reads a Balance.
func (s *Statements) OwnerStatement(ctx context.Context, propertyID string, forMonth time.Time) (OwnerStatement, error) {
	from := time.Date(forMonth.Year(), forMonth.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)

	balances, err := s.ledger.Balances(ctx, store.BalanceQuery{
		Property: propertyID,
		Party:    domain.Party{Kind: domain.Owner},
		From:     from,
		AsOf:     to.AddDate(0, 0, -1),
	})
	if err != nil {
		return OwnerStatement{}, fmt.Errorf("money: owner statement for %s: %w", propertyID, err)
	}

	out := OwnerStatement{PropertyID: propertyID, From: from, To: to}
	for _, b := range balances {
		if b.AccountType != domain.Income && b.AccountType != domain.Expense {
			continue
		}
		amount := -b.Amount
		out.Lines = append(out.Lines, OwnerLine{Account: b.Account, Label: b.AccountName, AmountMinor: amount})
		out.NetMinor += amount
		if b.AccountType == domain.Income {
			out.IncomeMinor += amount
		} else {
			out.ExpenseMinor -= amount
		}
	}
	return out, nil
}
