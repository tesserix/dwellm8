package dpdp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Erasure, and the three answers it can have. ADR-0026 §3.
//
// The retention matrix in docs/data-retention.md is the authority for the
// periods; this is the same matrix as code, so a request can be answered without
// a person reading a table and deciding.

// Class is a group of records that share a retention rule. Coarser than tables
// on purpose: "the rent ledger" is one answer to a data principal, not eleven.
type Class string

const (
	// ClassFinancial is the ledger and everything posted to it: journal entries,
	// postings, payments, settlements.
	ClassFinancial Class = "financial"
	// ClassTax is what a tax authority may ask for: TDS obligations, the
	// certificates behind them, and the facts that chose the section.
	ClassTax Class = "tax"
	// ClassAgreement is the executed tenancy: the lease, who was on it, what the
	// rent was.
	ClassAgreement Class = "agreement"
	// ClassKYC is identity verification under ADR-0013 — results and masked
	// references, never the document.
	ClassKYC Class = "kyc"
	// ClassContact is the funnel: prospects, enquiries, shortlists, the masked
	// call bridge. Held on consent and on nothing else.
	ClassContact Class = "contact"
	// ClassSupport is conversations the person started.
	ClassSupport Class = "support"
	// ClassAudit is the security record of who did what. Held because erasing
	// the audit trail on request is how an attacker would erase the audit trail.
	ClassAudit Class = "audit"
)

// Classes returns every class, ordered.
func Classes() []Class {
	return []Class{ClassAgreement, ClassAudit, ClassContact, ClassFinancial,
		ClassKYC, ClassSupport, ClassTax}
}

// Retention is how long a class is kept once the relationship ends, and under
// what.
type Retention struct {
	Class Class
	// Years after the anchor date. Zero means nothing requires it to be kept.
	Years int
	// Statute is what requires it. Empty where nothing does.
	Statute string
	// Anchor names the date the clock runs from, in words the requester reads.
	Anchor string
}

// matrix is docs/data-retention.md, as data. The two must agree, and the test
// that reads the document is what keeps them agreeing.
var matrix = map[Class]Retention{
	ClassFinancial: {Class: ClassFinancial, Years: 8,
		Statute: "Section 128(5), Companies Act 2013, read with Rule 6F, Income-tax Rules 1962",
		Anchor:  "the end of the financial year the entry falls in"},
	ClassTax: {Class: ClassTax, Years: 8,
		Statute: "Section 149 and Rule 31A, Income-tax Act 1961 — the period in which an assessment may be reopened",
		Anchor:  "the end of the financial year the deduction falls in"},
	ClassAgreement: {Class: ClassAgreement, Years: 12,
		Statute: "Articles 65 and 66, Limitation Act 1963 — the period in which a suit concerning immovable property may be brought",
		Anchor:  "the end of the tenancy"},
	ClassKYC: {Class: ClassKYC, Years: 5,
		Statute: "Section 12, Prevention of Money-Laundering Act 2002 — see docs/india-property-compliance.md §9 on whether Dwellm8 is a reporting entity",
		Anchor:  "the end of the relationship"},
	ClassAudit: {Class: ClassAudit, Years: 8,
		Statute: "Retained as the security record; erasing it on request is how an audit trail gets erased",
		Anchor:  "the event"},
	ClassContact: {Class: ClassContact},
	ClassSupport: {Class: ClassSupport},
}

// RetentionFor returns the rule for a class.
func RetentionFor(c Class) (Retention, bool) {
	r, ok := matrix[c]
	return r, ok
}

// Action is what happens to a class in response to an erasure request.
type Action string

const (
	// Erase deletes it.
	Erase Action = "erase"
	// Retain keeps it, with a statute named. The requester is told, which is
	// the half that is usually missing.
	Retain Action = "retain"
	// Defer waits. Something is unresolved — a dispute, money in flight, a
	// certificate the landlord is still owed — and erasing now would destroy the
	// evidence in an argument that is still running.
	Defer Action = "defer"
)

// Entanglement is a reason a request cannot be answered yet.
type Entanglement struct {
	// What it is, in the requester's words.
	What string
	// Reference identifies it — a dispute id, a lease, an obligation.
	Reference string
}

// Request is an erasure request, scoped to one organisation.
//
// Scoped deliberately, and it is the story's first edge case: the same person
// may be a tenant of one organisation and an owner in another. A request made to
// one is answered by one, and reaching across would hand an organisation the
// power to erase another's records.
type Subject struct {
	PartyID  string
	TenantID string

	// RelationshipEndedOn is when the last tenancy, mandate or engagement with
	// this organisation ended. Zero while any is live, which by itself defers
	// everything with a retention period.
	RelationshipEndedOn effective.Date

	// OpenDisputes, UnsettledMoney and OutstandingObligations are the three
	// things that defer. Each is a list rather than a flag so the answer can name
	// them: "we cannot erase yet" is not an answer, "we cannot erase yet because
	// of dispute D-1042" is.
	OpenDisputes           []Entanglement
	UnsettledMoney         []Entanglement
	OutstandingObligations []Entanglement

	// Present is which classes actually hold data for this person. A class that
	// holds nothing produces no line in the answer, because listing every class
	// the platform *could* hold is noise dressed as transparency.
	Present []Class
}

// Outcome is what happens to one class.
type Outcome struct {
	Class  Class
	Action Action
	// Until is the date retention expires, where it is known.
	Until effective.Date
	// Because is the sentence the requester is given. Every outcome has one,
	// including the erasures.
	Because string
	// Blocking names what is deferring this class, where something is.
	Blocking []Entanglement
}

// Assess answers an erasure request: what goes, what stays, what waits, and why
// in each case.
//
// It decides nothing about *when* the erasure runs — that is the endpoint's
// business (MVP 2) — and everything about what the answer is.
func Assess(s Subject, on effective.Date) ([]Outcome, error) {
	if s.PartyID == "" || s.TenantID == "" {
		return nil, fmt.Errorf("%w: an erasure request must name a person and the organisation "+
			"it is made to — the same person may be a tenant of one and an owner of another",
			ErrConsent)
	}
	if on.Zero() {
		return nil, fmt.Errorf("%w: an assessment must say which date it is made on", ErrConsent)
	}

	blocking := append(append(append([]Entanglement(nil), s.OpenDisputes...),
		s.UnsettledMoney...), s.OutstandingObligations...)

	out := make([]Outcome, 0, len(s.Present))
	for _, c := range s.Present {
		r, known := matrix[c]
		if !known {
			return nil, fmt.Errorf("%w: %q is not a retention class", ErrConsent, c)
		}
		out = append(out, assessClass(c, r, s, blocking, on))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Class < out[j].Class })
	return out, nil
}

func assessClass(c Class, r Retention, s Subject, blocking []Entanglement, on effective.Date) Outcome {
	// Nothing requires it: it goes, whatever else is unresolved. A dispute about
	// rent is not a reason to keep somebody's marketing preferences.
	if r.Years == 0 {
		return Outcome{Class: c, Action: Erase,
			Because: fmt.Sprintf("Nothing requires %s data to be kept, so it is erased.", c)}
	}

	// The story's failure scenario. Something is unresolved, so this is deferred
	// with a reason rather than silently ignored or blindly executed.
	if len(blocking) > 0 {
		return Outcome{Class: c, Action: Defer, Blocking: blocking,
			Because: fmt.Sprintf("%s data is not erased yet: %s. It is reassessed when that is "+
				"resolved, and you will be told either way.", titled(c), list(blocking))}
	}

	if s.RelationshipEndedOn.Zero() {
		return Outcome{Class: c, Action: Defer,
			Because: fmt.Sprintf("%s data cannot be erased while the relationship is still "+
				"running. The retention period starts at %s.", titled(c), r.Anchor)}
	}

	end := s.RelationshipEndedOn.Time()
	until := effective.Day(end.Year()+r.Years, end.Month(), end.Day())
	if !on.Before(until) {
		return Outcome{Class: c, Action: Erase, Until: until,
			Because: fmt.Sprintf("%s data was retained under %s until %s. That period has passed, "+
				"so it is erased.", titled(c), r.Statute, until)}
	}
	return Outcome{Class: c, Action: Retain, Until: until,
		Because: fmt.Sprintf("%s data is retained until %s under %s, counted from %s. It is not "+
			"used for anything else in the meantime.", titled(c), until, r.Statute, r.Anchor)}
}

// Erasable reports whether the request can be completed in full today.
func Erasable(outcomes []Outcome) bool {
	for _, o := range outcomes {
		if o.Action != Erase {
			return false
		}
	}
	return len(outcomes) > 0
}

// Deferred returns the outcomes that are waiting on something.
func Deferred(outcomes []Outcome) []Outcome {
	var out []Outcome
	for _, o := range outcomes {
		if o.Action == Defer {
			out = append(out, o)
		}
	}
	return out
}

func titled(c Class) string {
	switch c {
	case ClassKYC:
		return "KYC"
	default:
		return strings.ToUpper(string(c)[:1]) + string(c)[1:]
	}
}

func list(e []Entanglement) string {
	parts := make([]string, 0, len(e))
	for _, x := range e {
		if x.Reference != "" {
			parts = append(parts, fmt.Sprintf("%s (%s)", x.What, x.Reference))
			continue
		}
		parts = append(parts, x.What)
	}
	return strings.Join(parts, "; ")
}
