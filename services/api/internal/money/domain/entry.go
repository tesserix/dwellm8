package domain

import (
	"errors"
	"fmt"
	"sort"
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
	KindInvoice        EventKind = "invoice"
	KindLateFee        EventKind = "late_fee"
	KindPayment        EventKind = "payment"
	KindPaymentWithTDS EventKind = "payment_with_tds"
	KindSettlement     EventKind = "settlement"
	// KindSettlementWithFee is a settlement the provider netted its charge out
	// of. Separate from KindSettlement for the same reason
	// KindPaymentWithTDS is separate from KindPayment: the event is the same and
	// the deduction is not, and a rule with an optional deduction in it is a rule
	// nobody can read. ADR-0012 §4.
	KindSettlementWithFee EventKind = "settlement_with_fee"
	// KindClearingWriteOff abandons a clearing balance reconciliation could never
	// account for. Without it the clearing account grows a permanent residue and
	// the ageing report never empties. ADR-0012 §7.
	KindClearingWriteOff  EventKind = "clearing_write_off"
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
	// ReasonWorkflowCompensated is a durable operation undoing its own earlier
	// step. ADR-0015 §4.
	//
	// It is its own reason rather than operator_error because nobody made an error:
	// the entry was correct when it was posted and a later step of the same
	// operation failed. Recording it as an operator's mistake would put a name
	// against a decision no person made, on a row that is immutable and will be
	// read during a dispute.
	ReasonWorkflowCompensated ReversalReason = "workflow_compensated"
)

var reversalReasons = map[ReversalReason]bool{
	ReasonDuplicate: true, ReasonWrongAmount: true, ReasonWrongAccount: true,
	ReasonWrongParty: true, ReasonWrongPeriod: true, ReasonProviderChargeback: true,
	ReasonOperatorError: true, ReasonSettlementMismatch: true,
	ReasonWorkflowCompensated: true,
}

// ReversalReasons returns every reason, ordered.
//
// It exists because the store contract test used to carry its own hand-written
// copy of this list, so a reason added here and not there was compared against
// nothing — the schema could refuse it and no build would notice. Same failure as
// a guard that only covers the tables its author had in mind.
func ReversalReasons() []ReversalReason {
	out := make([]ReversalReason, 0, len(reversalReasons))
	for r := range reversalReasons {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SourceLeaseCharge is the one source kind the schema checks: it must name a
// lease, and the retrospective-termination trigger reads only these. ADR-0010 §7.
const SourceLeaseCharge = "lease_charge"

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
	// Lease is the tenancy this entry concerns — charges and the payments against
	// them alike. Optional, and how a lease position is derived. ADR-0006 §5.
	Lease           string
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
	// One-directional, as the schema's CHECK is: a payment names its lease too.
	if e.SourceKind == SourceLeaseCharge && e.Lease == "" {
		return errors.New("money: an entry that calls itself a lease charge must name the lease")
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
		Lease:           original.Lease,
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
