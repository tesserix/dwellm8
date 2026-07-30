// Package domain holds the lease lifecycle. ADR-0010.
//
// The lease is the object every other module hangs off: invoicing bills against it,
// documents are generated for it, deposits are held under it, notifications are sent
// about it. So its states have to be fixed before any of those are built, and the
// transitions have to be the same set everywhere — in Go, so a handler can decide
// without a round trip, and in PostgreSQL, so a path that never went through Go
// cannot move a lease sideways.
//
// # The agreed term and the actual end are different facts
//
// A lease agreement says "1 July 2025 to 30 June 2026". That is what the parties
// signed and it never changes, whatever happens next. Whether the tenancy actually
// ran that long is a separate fact, and terminating early does not rewrite the
// agreement — it records that occupancy ceased sooner.
//
// So Term is the agreed period, immutable, and EndedOn is the date occupancy actually
// ceased. Occupancy() is the intersection, and it is the interval that matters for
// money: charge generation is the rent schedule intersected with it, so billing stops
// because the occupancy interval ended rather than because a flag was flipped and
// something remembered to check it.
//
// The first version of this model had one interval and terminated by closing it. That
// is wrong twice over: a fixed-term lease's interval is already closed, so there was
// nothing to close, and shortening it would have edited the agreement to say the
// parties had signed up for less than they did.
//
// "A terminated lease stops billing exactly once" is then a consequence rather than a
// discipline: there is no flag to check, and the entry's idempotency key (ADR-0002 §6)
// is what stops a period being billed twice.
package domain

import (
	"errors"
	"fmt"
	"sort"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// State is where a lease has got to.
//
// Eight stored states. The story listed eight and this is not the same eight — see
// Expiring and StateLapsed for the two differences and why.
type State string

const (
	// StateDraft is a lease being written. It bills nothing and it is not a tenancy:
	// two competing drafts on one flat are legitimate, which is why the no-double-let
	// constraint does not cover this state.
	StateDraft State = "draft"

	// StatePendingSignature is a lease sent for signature and not yet signed. Still
	// not a tenancy, still bills nothing.
	StatePendingSignature State = "pending_signature"

	// StateActive is a live tenancy. Rent accrues.
	StateActive State = "active"

	// StateInNotice is a live tenancy with notice served, by either side. Rent still
	// accrues — notice is not an ending, it is the announcement of one — and the
	// difference matters because a lease in notice is still billable and a terminated
	// one is not.
	StateInNotice State = "in_notice"

	// StateRenewed is a tenancy that continued under a successor lease. Terminal, and
	// deliberately distinct from terminated: the tenant did not leave, so the deposit
	// carries forward rather than becoming settleable.
	StateRenewed State = "renewed"

	// StateTerminated is a tenancy that ended. Rent has stopped and the deposit is
	// settleable; the money is not yet settled.
	StateTerminated State = "terminated"

	// StateSettled is a terminated tenancy whose final accounts are closed and whose
	// deposit has been dealt with. The end.
	StateSettled State = "settled"

	// StateLapsed is a lease that was never signed. It is not in the story's list and
	// it is the reason the list needed changing: without it an unsigned draft has to
	// end up "terminated", which conflates a tenancy that ran and ended with one that
	// never started. Those differ in every report, and in whether there is a deposit
	// to settle at all.
	StateLapsed State = "lapsed"
)

// The transition table. ADR-0010 §3.
//
// The same set exists as lease_transition_allowed() in the schema, and the store
// contract test evaluates the function over all 64 ordered pairs — the price of two
// copies, paid the same way ADR-0011 §3 pays it.
var transitions = map[State][]State{
	StateDraft:            {StatePendingSignature, StateLapsed},
	StatePendingSignature: {StateActive, StateLapsed},
	StateActive:           {StateInNotice, StateRenewed, StateTerminated},
	// in_notice → active is the only backward edge in the machine, and it is here
	// because withdrawing notice is a real thing tenants and owners do: notice is
	// served, terms are renegotiated, the tenancy continues. Refusing it would mean
	// the workaround is a new lease, which loses the ledger history the tenancy
	// already has.
	//
	// It is the one transition that requires a recorded reason, because it is the one
	// that makes the history non-monotonic and therefore the one somebody will later
	// ask about.
	StateInNotice:   {StateTerminated, StateActive},
	StateTerminated: {StateSettled},
	StateRenewed:    nil,
	StateSettled:    nil,
	StateLapsed:     nil,
}

// States returns every state, ordered.
func States() []State {
	out := make([]State, 0, len(transitions))
	for s := range transitions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Terminal reports whether nothing further can happen.
func (s State) Terminal() bool {
	next, known := transitions[s]
	return known && len(next) == 0
}

// Billable reports whether rent accrues in this state.
//
// Only two states bill, and in_notice is one of them: notice announces an ending and
// does not perform it. Getting this wrong in the other direction — stopping the rent
// when notice is served — is the more expensive mistake, because it is a month of
// rent the owner never sees and the tenant never disputes.
func (s State) Billable() bool { return s == StateActive || s == StateInNotice }

// Tenancy reports whether this state represents an actual letting of the unit, as
// opposed to a document about one.
//
// It is what the no-double-let constraint is scoped to. A draft and a
// pending-signature lease are not tenancies, so two competing offers on one flat are
// legitimate; the moment one is signed, nothing else may overlap it.
func (s State) Tenancy() bool {
	switch s {
	case StateActive, StateInNotice, StateRenewed, StateTerminated, StateSettled:
		return true
	}
	return false
}

// DepositSettleable reports whether the deposit may now be returned or applied.
//
// Terminated only. A renewed lease's deposit carries into its successor — the tenant
// has not left — and returning it at renewal would mean collecting it again a day
// later. A lapsed lease never held one.
func (s State) DepositSettleable() bool { return s == StateTerminated }

func (s State) String() string { return string(s) }

// CanTransition reports whether a lease may move from one state to another.
//
// from == to is a permitted no-op, for the reason ADR-0011 §3 gives: a redelivered
// event or a retried activity asks for the state the lease is already in.
func CanTransition(from, to State) bool {
	if from == to {
		_, known := transitions[from]
		return known
	}
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// Actor is who may cause a transition. Recorded on every one, because "who ended this
// tenancy" is the first question in any dispute about it.
type Actor string

const (
	// ActorOwner is the owner or a management firm acting under a delegated grant with
	// lease.write. ADR-0005 decides whether the grant permits it; this records which
	// side acted.
	ActorOwner Actor = "owner"
	// ActorTenant is the tenant. A tenant may serve notice and may sign; a tenant may
	// not activate a lease or settle one.
	ActorTenant Actor = "tenant"
	// ActorSystem is the clock: a lease that expires from pending_signature, or one
	// that reaches its own end date.
	ActorSystem Actor = "system"
	// ActorPlatform is an audited support action. It can do anything the owner can,
	// and every use is written to audit_events.
	ActorPlatform Actor = "platform"
)

// permitted is who may cause each transition. It is data rather than a switch in a
// handler, because the interesting question is "may a tenant do this" and that has to
// be answerable without reading the handler.
var permitted = map[[2]State][]Actor{
	{StateDraft, StatePendingSignature}: {ActorOwner, ActorPlatform},
	{StateDraft, StateLapsed}:           {ActorOwner, ActorSystem, ActorPlatform},
	// Activation is the countersignature, so both sides and the system that observes
	// the last signature landing.
	{StatePendingSignature, StateActive}: {ActorOwner, ActorTenant, ActorSystem, ActorPlatform},
	// The story's edge case: a lease whose tenant never signs. The clock does this,
	// which is why ActorSystem is here and why nothing bills — a lapsed lease was
	// never a tenancy.
	{StatePendingSignature, StateLapsed}: {ActorSystem, ActorOwner, ActorPlatform},
	// Either side may serve notice. That is the point of a notice period.
	{StateActive, StateInNotice}:     {ActorOwner, ActorTenant, ActorPlatform},
	{StateActive, StateRenewed}:      {ActorOwner, ActorPlatform},
	{StateActive, StateTerminated}:   {ActorOwner, ActorPlatform},
	{StateInNotice, StateTerminated}: {ActorOwner, ActorSystem, ActorPlatform},
	// Withdrawing notice needs both sides to have agreed, and the owner is the one who
	// records it. A tenant unilaterally un-serving notice is a negotiation, not a
	// transition.
	{StateInNotice, StateActive}:    {ActorOwner, ActorPlatform},
	{StateTerminated, StateSettled}: {ActorOwner, ActorPlatform},
}

// Actors returns every actor, ordered.
func Actors() []Actor {
	out := []Actor{ActorOwner, ActorTenant, ActorSystem, ActorPlatform}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// MayTransition reports whether this actor may cause this transition.
func MayTransition(from, to State, by Actor) bool {
	if from == to {
		return CanTransition(from, to)
	}
	for _, a := range permitted[[2]State{from, to}] {
		if a == by {
			return true
		}
	}
	return false
}

// Event is what a transition publishes. The names are ADR-0001's, which already
// declared lease.tenancy.started, lease.tenancy.ended and lease.notice.served before
// this ADR existed; the other three are additive under the same
// <module>.<aggregate>.<past-tense-verb> rule.
type Event string

const (
	EventStarted         Event = "lease.tenancy.started"
	EventNoticeServed    Event = "lease.notice.served"
	EventNoticeWithdrawn Event = "lease.notice.withdrawn"
	EventEnded           Event = "lease.tenancy.ended"
	EventRenewed         Event = "lease.tenancy.renewed"
	EventLapsed          Event = "lease.tenancy.lapsed"
	EventSettled         Event = "lease.tenancy.settled"
)

// events is what each transition publishes, or nothing.
//
// draft → pending_signature publishes nothing on purpose: nobody outside the lease
// module cares that a document was sent, and an event with no consumer is an event
// somebody will later wire something to by accident.
var events = map[[2]State]Event{
	{StatePendingSignature, StateActive}: EventStarted,
	{StateActive, StateInNotice}:         EventNoticeServed,
	{StateInNotice, StateActive}:         EventNoticeWithdrawn,
	{StateActive, StateTerminated}:       EventEnded,
	{StateInNotice, StateTerminated}:     EventEnded,
	{StateActive, StateRenewed}:          EventRenewed,
	{StateDraft, StateLapsed}:            EventLapsed,
	{StatePendingSignature, StateLapsed}: EventLapsed,
	{StateTerminated, StateSettled}:      EventSettled,
}

// EventFor returns the event a transition publishes, and false if it publishes none.
func EventFor(from, to State) (Event, bool) {
	e, ok := events[[2]State{from, to}]
	return e, ok
}

// Events returns every event this module publishes, ordered.
func Events() []Event {
	seen := map[Event]bool{}
	for _, e := range events {
		seen[e] = true
	}
	out := make([]Event, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SettlementDecision is what happens to money when a tenancy ends on a date that has
// already been billed.
//
// ADR-0010's failure scenario: a termination effective before the last invoiced period
// must require an explicit decision rather than silently deleting charges. Half of
// that is already impossible — ADR-0006 §3 revokes DELETE on the ledger and refuses
// it again in a policy — so what is left is that somebody has to *decide*, and a
// closed vocabulary is how a decision is distinguishable from a shrug.
type SettlementDecision string

const (
	// DecisionNone is a termination that bills nothing retrospectively, because its
	// effective date is after everything already invoiced.
	DecisionNone SettlementDecision = "none"
	// DecisionAdjust raises a credit note against the over-billed period. The
	// receivable comes down and the invoice stays, per ADR-0006 §3.
	DecisionAdjust SettlementDecision = "adjust"
	// DecisionRefund returns money the tenant already paid for a period they will not
	// occupy.
	DecisionRefund SettlementDecision = "refund"
	// DecisionForfeit keeps it, which is what a lock-in clause is for. It is a
	// decision with a name so that an owner cannot make it by default.
	DecisionForfeit SettlementDecision = "forfeit"
)

var decisions = map[SettlementDecision]bool{
	DecisionNone: true, DecisionAdjust: true, DecisionRefund: true, DecisionForfeit: true,
}

// Decisions returns every decision, ordered.
func Decisions() []SettlementDecision {
	out := make([]SettlementDecision, 0, len(decisions))
	for d := range decisions {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Lease is the aggregate, as far as the lifecycle is concerned.
type Lease struct {
	ID       string
	TenantID string
	Property string
	Unit     string

	State State
	// Term is the period the parties agreed, as an ADR-0008 half-open interval. It is
	// what the agreement says and it never changes.
	Term effective.Interval
	// EndedOn is the exclusive date occupancy actually ceased, when it ceased before
	// the agreed term ran out. Zero while the tenancy is running or ran to term.
	//
	// Separate from Term because they are different facts: an early exit does not mean
	// the parties agreed to a shorter tenancy.
	EndedOn effective.Date

	// NoticeDays is the notice either side must give.
	NoticeDays int
	// LockInUntil is the date before which the tenant may not leave without a
	// decision. Zero when there is no lock-in.
	LockInUntil effective.Date

	// Renews is the lease this one succeeds, if any.
	Renews string
}

// ErrTransition is what a caller checks for to tell an illegal move from a bad input.
var ErrTransition = errors.New("lease: not a permitted transition")

// ErrDecisionRequired is the failure scenario's error. Distinguishable, because the
// caller's response is to go and ask a person rather than to retry.
var ErrDecisionRequired = errors.New("lease: a retrospective termination needs an explicit decision")

// Occupancy is the interval money is computed over: the agreed term, cut short if
// occupancy ceased early.
//
// This is what charge generation intersects the rent schedule with, and what the
// no-double-let constraint compares — a tenancy that ended in June does not block a
// new one starting in July, even though the agreement ran to December.
func (l Lease) Occupancy() effective.Interval {
	if l.EndedOn.Zero() {
		return l.Term
	}
	if !l.Term.To().Zero() && !l.Term.To().After(l.EndedOn) {
		return l.Term // ran to term, or beyond it; the agreement is the shorter one
	}
	cut, err := effective.Between(l.Term.From(), l.EndedOn)
	if err != nil {
		// EndedOn at or before the start is refused by Validate and by the schema, so
		// this is unreachable; returning the agreed term is the conservative answer
		// because it bills more rather than less.
		return l.Term
	}
	return cut
}

// Termination is what ending a tenancy requires.
type Termination struct {
	// EffectiveOn is the last day of the tenancy plus one — the exclusive bound the
	// term closes at, per ADR-0008. A tenancy ending on 20 June closes at 21 June.
	EffectiveOn effective.Date
	By          Actor
	Reason      string
	Decision    SettlementDecision
}

// Terminate closes the tenancy's term and returns the state and event.
//
// lastInvoicedThrough is the day up to which charges have already been raised, and it
// is a parameter rather than something this function looks up: the lease module does
// not read the ledger's tables (ADR-0001), and the schema's trigger asks the ledger
// the same question for anything that did not come through here.
func (l Lease) Terminate(t Termination, lastInvoicedThrough effective.Date) (Lease, Event, error) {
	if !MayTransition(l.State, StateTerminated, t.By) {
		return l, "", fmt.Errorf("%w: a %s may not terminate a %s lease", ErrTransition, t.By, l.State)
	}
	if t.EffectiveOn.Zero() {
		return l, "", errors.New("lease: a termination must say when the tenancy ends")
	}
	// The end must fall after the start and no later than the agreed end: a tenancy
	// cannot cease before it began, and ceasing after the agreement ran out is the
	// agreement running out rather than a termination.
	if !t.EffectiveOn.After(l.Term.From()) {
		return l, "", fmt.Errorf("%w: a tenancy starting %s cannot cease %s",
			ErrTransition, l.Term.From(), t.EffectiveOn)
	}
	if !l.Term.To().Zero() && t.EffectiveOn.After(l.Term.To()) {
		return l, "", fmt.Errorf("%w: the agreed term ends %s and the termination is dated %s",
			ErrTransition, l.Term.To(), t.EffectiveOn)
	}
	if t.Reason == "" {
		return l, "", errors.New("lease: a termination must say why — it is the first question " +
			"in any dispute about the tenancy")
	}
	if !decisions[t.Decision] {
		return l, "", fmt.Errorf("lease: %q is not a settlement decision", t.Decision)
	}

	// The failure scenario. A termination effective before the last invoiced period
	// leaves charges raised for time the tenant will not occupy, and the only wrong
	// answer is to quietly remove them.
	retrospective := !lastInvoicedThrough.Zero() && t.EffectiveOn.Before(lastInvoicedThrough)
	if retrospective && t.Decision == DecisionNone {
		return l, "", fmt.Errorf("%w: charges are raised through %s and the tenancy is ending %s, "+
			"so an over-billed period exists — adjust it, refund it, or forfeit it, but say which",
			ErrDecisionRequired, lastInvoicedThrough, t.EffectiveOn)
	}
	if !retrospective && t.Decision != DecisionNone {
		return l, "", fmt.Errorf("lease: a %s decision was made and nothing was over-billed — "+
			"charges run to %s and the tenancy ends %s", t.Decision, lastInvoicedThrough, t.EffectiveOn)
	}

	out := l
	out.State = StateTerminated
	// The agreement is left exactly as signed; what is recorded is that occupancy
	// ceased.
	out.EndedOn = t.EffectiveOn
	return out, EventEnded, nil
}

// Expiring reports whether the tenancy ends within `within` days of asOf.
//
// Derived, and deliberately not a state — which is the second place this diverges from
// the story's list. `expiring` is a fact about a clock, not about anything that
// happened, so storing it would require a job to move leases into it and a lease that
// should be expiring and is not would be a silent bug. Same argument ADR-0012 §7 makes
// about ageing buckets: a stored bucket is wrong the day after it is written.
//
// It is a method rather than a column, and the schema exposes the same thing as a
// view, so the renewal reminder and the owner's dashboard read one definition.
func (l Lease) Expiring(asOf effective.Date, within int) bool {
	if !l.State.Billable() || l.Term.To().Zero() {
		return false
	}
	if !asOf.Before(l.Term.To()) {
		return false // already over
	}
	return !l.Term.To().After(asOf.AddDays(within))
}

// InLockIn reports whether the tenancy is still inside its lock-in on asOf.
func (l Lease) InLockIn(asOf effective.Date) bool {
	if l.LockInUntil.Zero() {
		return false
	}
	return asOf.Before(l.LockInUntil)
}

// Renew produces the successor lease and the predecessor's new state.
//
// The successor is a new lease that names its predecessor, never a mutated row. The
// ledger history a tenancy accumulates hangs off the lease id, so mutating the row
// would silently re-attribute two years of postings to a different set of terms.
//
// The successor's term begins exactly where the predecessor's ends, which is ADR-0008's
// half-open interval doing the work: no gap for a day of unbilled occupancy, no
// overlap for the no-double-let constraint to refuse.
func (l Lease) Renew(until effective.Date, by Actor) (predecessor Lease, successor Lease, err error) {
	if !MayTransition(l.State, StateRenewed, by) {
		return l, successor, fmt.Errorf("%w: a %s may not renew a %s lease", ErrTransition, by, l.State)
	}
	if l.Term.To().Zero() {
		return l, successor, errors.New("lease: an open-ended tenancy has nothing to renew from — " +
			"a renewal starts where the term ends, and this one does not end")
	}

	var term effective.Interval
	if until.Zero() {
		term, err = effective.Since(l.Term.To())
	} else {
		term, err = effective.Between(l.Term.To(), until)
	}
	if err != nil {
		return l, successor, err
	}

	predecessor = l
	predecessor.State = StateRenewed
	successor = Lease{
		TenantID: l.TenantID, Property: l.Property, Unit: l.Unit,
		State: StateActive, Term: term,
		NoticeDays: l.NoticeDays, Renews: l.ID,
	}
	return predecessor, successor, nil
}

// Validate asserts what the schema will assert, naming the field rather than the
// constraint.
func (l Lease) Validate() error {
	if l.TenantID == "" {
		return errors.New("lease: a lease with no organisation")
	}
	if l.Property == "" || l.Unit == "" {
		return errors.New("lease: a lease must name the unit it lets — a tenancy of a whole " +
			"building is a tenancy of a unit that happens to be the whole building")
	}
	if _, known := transitions[l.State]; !known {
		return fmt.Errorf("lease: unknown state %q", l.State)
	}
	if !l.Term.Valid() {
		return errors.New("lease: a lease must have a term, or it bills nothing and expires never")
	}
	if l.NoticeDays < 0 {
		return fmt.Errorf("lease: a notice period of %d days", l.NoticeDays)
	}
	if !l.LockInUntil.Zero() && l.LockInUntil.Before(l.Term.From()) {
		return fmt.Errorf("lease: a lock-in ending %s on a tenancy starting %s has already expired",
			l.LockInUntil, l.Term.From())
	}
	if !l.EndedOn.Zero() && !l.EndedOn.After(l.Term.From()) {
		return fmt.Errorf("lease: occupancy ceased %s on a tenancy starting %s", l.EndedOn, l.Term.From())
	}
	if !l.EndedOn.Zero() && !l.Term.To().Zero() && l.EndedOn.After(l.Term.To()) {
		return fmt.Errorf("lease: occupancy ceased %s and the agreed term ended %s — the agreement "+
			"running out is not a termination", l.EndedOn, l.Term.To())
	}
	if !l.Term.To().Zero() && !l.LockInUntil.Zero() && l.Term.To().Before(l.LockInUntil) {
		return fmt.Errorf("lease: a lock-in to %s on a tenancy ending %s locks the tenant in "+
			"past the end of their own lease", l.LockInUntil, l.Term.To())
	}
	if (l.State == StateRenewed) && l.Term.To().Zero() {
		return errors.New("lease: a renewed tenancy has an end date, because its successor starts there")
	}
	return nil
}
