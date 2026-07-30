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
	"bytes"
	"fmt"
	"strconv"
)

// Minor is an amount in the currency's minor unit: paise, for the only currency
// this product handles. ADR-0007 §1.
//
// int64, never a float, and never a decimal string. 27500 rupees prorated across
// 17 of 31 days is 1508064.516… paise, and every representation that can hold
// that fraction eventually disagrees with itself about what it holds.
//
// The rest of the standard is in this package too: mulDivRound is where every
// fraction is resolved, Allocate is how an amount is split without losing a
// paisa, and MarshalJSON is the wire form. Rounding happens once, at the posting
// boundary, and never on a value that will be divided again.
type Minor int64

// MaxSafeMinor is the largest amount this platform will carry: 2⁵³−1 paise,
// which is roughly ₹90 lakh crore. ADR-0007 §5.
//
// The bound is not the int64 range, and that is the point. Money crosses the API
// as a JSON number, and JSON numbers are float64 to every JavaScript client in
// existence; beyond 2⁵³ they stop being exact, so a value the ledger stored
// correctly would arrive in a browser as a different value with no error
// anywhere. Refusing the amount is the only way that failure is visible. A ₹100
// crore transaction is 10¹¹ paise and this ceiling is about 10¹⁶, so nothing
// this product will ever carry is being turned away.
const MaxSafeMinor Minor = 1<<53 - 1

// Valid reports whether the amount is inside the exactly-representable range.
// Every posting is checked on the way in, so nothing unserialisable can be
// written and then fail on the way out, when the transaction is long gone.
func (m Minor) Valid() error {
	if m > MaxSafeMinor || m < -MaxSafeMinor {
		return fmt.Errorf("money: %d paise is outside the range that survives a JSON round trip (±%d)",
			int64(m), int64(MaxSafeMinor))
	}
	return nil
}

// MarshalJSON writes the amount as a bare integer of paise. ADR-0007 §5.
//
// Never a decimal, never a string, and never a float. The API pairs it with an
// explicit currency — `{"amountMinor": 1508065, "currency": "INR"}` — because a
// number with no unit is the other half of every money bug.
func (m Minor) MarshalJSON() ([]byte, error) {
	if err := m.Valid(); err != nil {
		return nil, err
	}
	return strconv.AppendInt(nil, int64(m), 10), nil
}

// UnmarshalJSON accepts an integer of paise and rejects everything else.
//
// This is the strictest thing in the package, on purpose. A client that sends
// 27500.50 means rupees, and silently truncating that to 27500 paise — ₹275,
// which is what a permissive parser does — is a defect nobody finds until a
// statement is disputed. So a fraction or an exponent is an error naming the
// value, not a value.
func (m *Minor) UnmarshalJSON(data []byte) error {
	text := string(bytes.TrimSpace(data))
	if text == "null" {
		return nil // by convention, a no-op — the field keeps its zero value
	}
	if len(text) > 0 && text[0] == '"' {
		return fmt.Errorf("money: %s is a string; an amount is a JSON number of paise", text)
	}
	if bytes.ContainsAny([]byte(text), ".eE") {
		return fmt.Errorf("money: %s is not a whole number of paise — send the amount in the minor unit, "+
			"so 27500.50 rupees is 2750050", text)
	}
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return fmt.Errorf("money: %s is not an amount in paise", text)
	}
	out := Minor(v)
	if err := out.Valid(); err != nil {
		return err
	}
	*m = out
	return nil
}

// Rupees renders the amount as a plain decimal: 2500000 becomes "25000.00".
//
// This is the export form for CSV, PDF and anything else a person reads or a
// spreadsheet parses: always two decimal places, no symbol, no thousands
// grouping, no locale, and a leading minus for a negative. Grouping is left out
// because a comma inside a CSV field is a bug waiting for the one export nobody
// quotes, and Indian grouping (₹27,50,000) is not what any parser expects.
// ADR-0007 §5.
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

// Currency is fixed. ADR-0007 §1 rejects multi-currency: a second currency needs
// a second rounding rule, an FX rate with a date and a source, and a decision
// about which one a balance is stated in — none of which this product has a
// caller for. The schema carries the same CHECK, so a second currency fails in
// both places rather than in neither.
//
// It is carried alongside every amount all the same, in the database and on the
// wire. A single-currency system that stops saying which currency is a system
// that cannot add the second one without a migration of every stored number.
const Currency = "INR"
