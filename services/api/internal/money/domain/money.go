// Package domain holds the money module's aggregates and rules: the chart of
// accounts from ADR-0006, the posting template for every money event, and the
// arithmetic that turns an event into a balanced set of postings.
//
// Nothing here touches a database or a framework — the boundary test in
// internal/platform/arch enforces that — because the rule this package encodes
// is the one a dispute is argued over years later, and it has to be readable
// and testable on its own.
package domain

import (
	"fmt"
	"strconv"
)

// Minor is an amount in the currency's minor unit: paise, for the only currency
// this product handles.
//
// int64, never a float, and never a decimal string. 27500 rupees prorated across
// 17 of 31 days is 1508064.516… paise, and every representation that can hold
// that fraction eventually disagrees with itself about what it holds.
//
// The full money standard — the rounding rule, where rounding is permitted, the
// largest-remainder allocation, and the serialisation rule for JSON, CSV and PDF
// — is ADR-0007's to write (issue #8). This type is deliberately the smallest
// thing ADR-0006 needs: a unit, a name and the absence of a float.
type Minor int64

// Rupees renders the amount as a plain decimal: 2500000 becomes "25000.00". No
// symbol, no grouping and no locale — presentation belongs to whatever is
// presenting, and ADR-0007 fixes the export format.
func (m Minor) Rupees() string {
	sign := ""
	v := int64(m)
	if v < 0 {
		sign, v = "-", -v
	}
	return sign + strconv.FormatInt(v/100, 10) + "." + fmt.Sprintf("%02d", v%100)
}

func (m Minor) String() string { return m.Rupees() }

// Side is the direction of a posting. There are no negative postings: a refund
// is a debit, not a credit of minus, so that "what has this account been
// charged" never has to be reconstructed from signs.
type Side string

const (
	Debit  Side = "debit"
	Credit Side = "credit"
)

// Opposite is what a reversing entry does to every line of the entry it
// reverses. It is the whole of the correction mechanism.
func (s Side) Opposite() Side {
	if s == Debit {
		return Credit
	}
	return Debit
}

// Signed is the arithmetic every balance sums: debits positive, credits
// negative. It matches the generated signed_minor column in ledger_postings, and
// the contract test asserts the two agree — a sign convention that exists twice
// is a sign convention that will differ once.
func Signed(side Side, amount Minor) Minor {
	if side == Debit {
		return amount
	}
	return -amount
}

// Currency is fixed. ADR-0007 owns multi-currency and rejects it for now; the
// schema carries the same CHECK, so a second currency fails in both places
// rather than in neither.
const Currency = "INR"
