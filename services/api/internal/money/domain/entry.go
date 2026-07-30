package domain

import (
	"errors"
	"fmt"
	"time"
)

// An entry and its postings. ADR-0006 §3.
//
// An entry is one money event: what happened, when it happened for accounting
// purposes, what caused it, and the lines it posts. Once written it is never
// changed — the schema revokes UPDATE and DELETE and refuses both again in a
// policy — so the only correction is Reverse().

// EventKind is the closed vocabulary of money events. It matches
// journal_entries.entry_kind and posting_templates.event_kind; the contract test
// asserts all three agree.
type EventKind string

const (
	KindInvoice           EventKind = "invoice"
	KindLateFee           EventKind = "late_fee"
	KindPayment           EventKind = "payment"
	KindPaymentWithTDS    EventKind = "payment_with_tds"
	KindSettlement        EventKind = "settlement"
	KindDepositCollection EventKind = "deposit_collection"
	KindDepositRefund     EventKind = "deposit_refund"
	KindPayout            EventKind = "payout"
	KindPlatformFee       EventKind = "platform_fee"
	KindGSTRemittance     EventKind = "gst_remittance"
	KindRefund            EventKind = "refund"
	KindWriteOff          EventKind = "write_off"
	KindReversal          EventKind = "reversal"
)

// ReversalReason is why a correction was made. Closed, because "adjustment" is
// what every reason becomes when the field is free text, and the whole value of
// a reversing entry is that it says what went wrong.
type ReversalReason string

const (
	ReasonDuplicate          ReversalReason = "duplicate"
	ReasonWrongAmount        ReversalReason = "wrong_amount"
	ReasonWrongAccount       ReversalReason = "wrong_account"
	ReasonWrongParty         ReversalReason = "wrong_party"
	ReasonWrongPeriod        ReversalReason = "wrong_period"
	ReasonProviderChargeback ReversalReason = "provider_chargeback"
	ReasonOperatorError      ReversalReason = "operator_error"
	ReasonSettlementMismatch ReversalReason = "settlement_mismatch"
)

var reversalReasons = map[ReversalReason]bool{
	ReasonDuplicate: true, ReasonWrongAmount: true, ReasonWrongAccount: true,
	ReasonWrongParty: true, ReasonWrongPeriod: true, ReasonProviderChargeback: true,
	ReasonOperatorError: true, ReasonSettlementMismatch: true,
}

// Party is whose balance a posting moves.
type Party struct {
	Kind PartyKind
	ID   string
}

// Posting is one line. Amount is always positive; Side carries the direction.
type Posting struct {
	Account string
	Side    Side
	Amount  Minor
	Party   Party
	Memo    string
}

// Signed is this line's contribution to a balance: debits positive.
func (p Posting) Signed() Minor { return Signed(p.Side, p.Amount) }

// Entry is one money event and the lines it posts.
//
// Property and Unit place the money. Both are optional at this level and the
// rule is the schema's: a unit needs a property, and a posting with no property
// is the organisation's own — a GST remittance, say — which no delegated session
// ever sees. Every posting of an entry shares the place, so a payout batch
// spanning three buildings is three entries. Issue #189 is where that stops
// being free.
type Entry struct {
	Kind            EventKind
	TemplateVersion int
	OccurredOn      time.Time
	Property        string
	Unit            string
	SourceKind      string
	SourceID        string
	IdempotencyKey  string
	Memo            string
	Postings        []Posting

	// A reversal names what it reverses and why. Set by Reverse() and by
	// nothing else.
	Reverses       string
	ReversalReason ReversalReason
}

// Totals returns the entry's debits and credits.
func (e Entry) Totals() (debits, credits Minor) {
	for _, p := range e.Postings {
		if p.Side == Debit {
			debits += p.Amount
		} else {
			credits += p.Amount
		}
	}
	return debits, credits
}

// ErrUnbalanced is what a caller checks for when it wants to distinguish "this
// entry is wrong" from "this input was wrong".
var ErrUnbalanced = errors.New("money: entry does not balance")

// Validate asserts everything the database will assert, before the database is
// asked.
//
// It duplicates the schema on purpose. The deferred constraint trigger is the
// authority — it catches anything written by any path, including psql — but it
// fires at COMMIT, and an error from COMMIT names a transaction rather than the
// line that was wrong. This one names the line.
func (e Entry) Validate() error {
	if _, ok := templates[e.Kind]; !ok && e.Kind != KindReversal {
		return fmt.Errorf("money: unknown event kind %q", e.Kind)
	}
	if len(e.Postings) < 2 {
		return fmt.Errorf("money: entry %s has %d posting(s): an entry with one line is a balance nobody can explain",
			e.Kind, len(e.Postings))
	}
	for i, p := range e.Postings {
		acct, ok := Lookup(p.Account)
		if !ok {
			return fmt.Errorf("money: posting %d names account %q, which is not in the chart", i, p.Account)
		}
		if p.Amount <= 0 {
			return fmt.Errorf("money: posting %d on %s is %s: amounts are positive and the side carries the direction",
				i, p.Account, p.Amount)
		}
		// ADR-0007 §5: nothing is written that cannot be read back exactly.
		if err := p.Amount.Valid(); err != nil {
			return fmt.Errorf("posting %d on %s: %w", i, p.Account, err)
		}
		if p.Side != Debit && p.Side != Credit {
			return fmt.Errorf("money: posting %d on %s has side %q", i, p.Account, p.Side)
		}
		switch {
		case acct.Party == NoParty && p.Party.ID != "":
			return fmt.Errorf("money: posting %d on %s carries a party, and that account is not kept per party",
				i, p.Account)
		case acct.Party != NoParty && p.Party.ID == "":
			return fmt.Errorf("money: posting %d on %s has no %s: a balance nobody is on the hook for cannot be chased",
				i, p.Account, acct.Party)
		case acct.Party != NoParty && p.Party.Kind != acct.Party:
			return fmt.Errorf("money: posting %d on %s names a %s, and that account is kept per %s",
				i, p.Account, p.Party.Kind, acct.Party)
		}
	}
	if e.Unit != "" && e.Property == "" {
		return errors.New("money: an entry against a unit must name the unit's property")
	}
	if (e.Kind == KindReversal) != (e.Reverses != "") {
		return errors.New("money: a reversal names the entry it reverses, and nothing else does")
	}
	if e.Kind == KindReversal && !reversalReasons[e.ReversalReason] {
		return fmt.Errorf("money: reversal reason %q is not one of the recorded reasons", e.ReversalReason)
	}
	if debits, credits := e.Totals(); debits != credits {
		return fmt.Errorf("%w: debits %s, credits %s", ErrUnbalanced, debits, credits)
	}
	return nil
}

// Reverse produces the correcting entry: every line of the original on the
// opposite side, same amounts, same place, with a reason.
//
// This is the only correction there is. The original is not touched — the link
// lives on the correcting entry — which is what lets the table be immutable and
// the history stay readable.
func Reverse(original Entry, originalID string, reason ReversalReason, key string) (Entry, error) {
	if originalID == "" {
		return Entry{}, errors.New("money: a reversal must name the entry it reverses")
	}
	if original.Kind == KindReversal {
		return Entry{}, errors.New("money: reversing a reversal — post the original event again instead, " +
			"so the history reads as what happened rather than as two corrections")
	}
	if !reversalReasons[reason] {
		return Entry{}, fmt.Errorf("money: reversal reason %q is not one of the recorded reasons", reason)
	}

	out := Entry{
		Kind:            KindReversal,
		TemplateVersion: original.TemplateVersion,
		OccurredOn:      original.OccurredOn,
		Property:        original.Property,
		Unit:            original.Unit,
		SourceKind:      original.SourceKind,
		SourceID:        original.SourceID,
		IdempotencyKey:  key,
		Memo:            original.Memo,
		Reverses:        originalID,
		ReversalReason:  reason,
		Postings:        make([]Posting, 0, len(original.Postings)),
	}
	for _, p := range original.Postings {
		p.Side = p.Side.Opposite()
		out.Postings = append(out.Postings, p)
	}
	return out, out.Validate()
}
