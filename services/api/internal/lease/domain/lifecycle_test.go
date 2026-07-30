package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// ADR-0010, tested without a database: the state machine, who may move it, what each
// move publishes, and the two money rules that hang off it.
//
// The schema holds the same transition table and refuses the same moves for anything
// that did not come through here; the store contract test compares the two over all
// 64 pairs. What is here is the part that has judgement in it.

func day(y int, m time.Month, d int) effective.Date { return effective.Day(y, m, d) }

// A live tenancy of one flat, running to 30 June 2026 — so the term closes at 1 July,
// which is ADR-0008's exclusive bound.
func active(t *testing.T) domain.Lease {
	t.Helper()
	term, err := effective.Between(day(2025, time.July, 1), day(2026, time.July, 1))
	if err != nil {
		t.Fatalf("term: %v", err)
	}
	l := domain.Lease{
		ID: "lease-1", TenantID: "org-1", Property: "prop-1", Unit: "unit-101",
		State: domain.StateActive, Term: term, NoticeDays: 60,
	}
	if err := l.Validate(); err != nil {
		t.Fatalf("the fixture is invalid: %v", err)
	}
	return l
}

// The primary acceptance scenario. An active lease terminated with effect from
// 20 June: charges stop after the prorated final period, the deposit becomes
// settleable, and lease.tenancy.ended is published once.
func TestTerminatingAnActiveLeaseStopsBillingAndSettlesTheDeposit(t *testing.T) {
	l := active(t)
	// Charges have been raised through 30 June — the month was invoiced in advance,
	// which is how Indian rent works.
	billedThrough := day(2026, time.July, 1)

	// Ending on 20 June means the term closes at 21 June: the tenant occupies the
	// 20th.
	end := day(2026, time.June, 21)

	out, ev, err := l.Terminate(domain.Termination{
		EffectiveOn: end, By: domain.ActorOwner,
		Reason: "tenant relocating", Decision: domain.DecisionAdjust,
	}, billedThrough)
	if err != nil {
		t.Fatalf("Terminate: %v", err)
	}

	if out.State != domain.StateTerminated {
		t.Fatalf("state is %s, want terminated", out.State)
	}
	if ev != domain.EventEnded {
		t.Errorf("published %s, want %s", ev, domain.EventEnded)
	}

	// The agreement is left exactly as the parties signed it. An early exit does not
	// mean they agreed to a shorter tenancy.
	if !out.Term.To().Equal(day(2026, time.July, 1)) {
		t.Errorf("the agreed term now ends %s — terminating rewrote the agreement", out.Term.To())
	}
	if !out.EndedOn.Equal(end) {
		t.Errorf("occupancy ceased %s, want %s", out.EndedOn, end)
	}

	// Billing follows the occupancy interval, which is the agreement cut short. The
	// 20th is inside it and the 21st is not.
	occ := out.Occupancy()
	if !occ.Contains(day(2026, time.June, 20)) {
		t.Errorf("the last day of the tenancy is not inside the occupancy %s, so it would not be billed", occ)
	}
	if occ.Contains(day(2026, time.June, 21)) {
		t.Errorf("the day after the tenancy ended is still inside the occupancy %s, so it would be billed", occ)
	}
	if out.State.Billable() {
		t.Error("a terminated lease still reports itself billable")
	}
	if !out.State.DepositSettleable() {
		t.Error("the deposit is not settleable after termination, which is when it becomes so")
	}

	// Exactly one event: EventFor is a lookup, so a transition cannot publish twice
	// and cannot publish two different things.
	got, ok := domain.EventFor(domain.StateActive, domain.StateTerminated)
	if !ok || got != domain.EventEnded {
		t.Errorf("EventFor(active→terminated) = %q, %v", got, ok)
	}
	if _, ok := domain.EventFor(domain.StateTerminated, domain.StateSettled); !ok {
		t.Error("settling publishes nothing, so nobody learns the tenancy is closed")
	}
}

// The failure scenario. A termination effective before the last invoiced period must
// require an explicit decision rather than silently deleting charges.
func TestARetrospectiveTerminationRequiresADecision(t *testing.T) {
	l := active(t)
	billedThrough := day(2026, time.July, 1) // charges raised through June

	// Ending on 20 May, when May and June have already been billed.
	end := day(2026, time.May, 21)

	_, _, err := l.Terminate(domain.Termination{
		EffectiveOn: end, By: domain.ActorOwner,
		Reason: "tenant left early", Decision: domain.DecisionNone,
	}, billedThrough)
	if err == nil {
		t.Fatal("a termination that leaves an over-billed period was accepted with no decision — " +
			"the charges would sit against a tenant who does not occupy the flat")
	}
	if !errors.Is(err, domain.ErrDecisionRequired) {
		t.Errorf("the error is not distinguishable as needing a person: %v", err)
	}
	t.Logf("refused: %v", err)

	// Each of the three real decisions is accepted, because each is a different answer
	// somebody has to give.
	for _, d := range []domain.SettlementDecision{
		domain.DecisionAdjust, domain.DecisionRefund, domain.DecisionForfeit,
	} {
		if _, _, err := l.Terminate(domain.Termination{
			EffectiveOn: end, By: domain.ActorOwner, Reason: "early exit", Decision: d,
		}, billedThrough); err != nil {
			t.Errorf("decision %s was refused: %v", d, err)
		}
	}

	// And the reverse: a decision on a termination that over-billed nothing is also
	// refused, because it would credit a period nobody was charged for.
	if _, _, err := l.Terminate(domain.Termination{
		EffectiveOn: day(2026, time.July, 1), By: domain.ActorOwner,
		Reason: "term ran out", Decision: domain.DecisionRefund,
	}, day(2026, time.July, 1)); err == nil {
		t.Error("a refund was accepted on a tenancy that ended exactly where billing stopped")
	}

	// A termination with no reason is refused whatever the dates say.
	if _, _, err := l.Terminate(domain.Termination{
		EffectiveOn: day(2026, time.June, 21), By: domain.ActorOwner, Decision: domain.DecisionNone,
	}, day(2026, time.June, 1)); err == nil {
		t.Error("a termination with no reason was accepted")
	}
}

// The agreed term and the actual occupancy, which are different facts and are the
// correction that model needed.
func TestOccupancyIsTheAgreementCutShort(t *testing.T) {
	agreed, _ := effective.Between(day(2025, time.July, 1), day(2026, time.July, 1))
	base := domain.Lease{
		ID: "l", TenantID: "o", Property: "p", Unit: "u",
		State: domain.StateActive, Term: agreed,
	}

	// Running: occupancy is the agreement.
	if got := base.Occupancy(); !got.To().Equal(agreed.To()) {
		t.Errorf("a running tenancy's occupancy ends %s, want the agreed %s", got.To(), agreed.To())
	}

	// Ended early: occupancy is shorter, and the agreement is untouched.
	early := base
	early.EndedOn = day(2026, time.June, 21)
	if got := early.Occupancy(); !got.To().Equal(early.EndedOn) {
		t.Errorf("an early exit's occupancy ends %s, want %s", got.To(), early.EndedOn)
	}
	if !early.Term.To().Equal(agreed.To()) {
		t.Error("recording an early exit changed the agreed term")
	}

	// Ran to term: the agreement is the shorter of the two, so occupancy is the
	// agreement. This is the case a naive min() gets right and a naive "prefer
	// EndedOn" gets wrong by extending the tenancy past what was signed.
	toTerm := base
	toTerm.EndedOn = day(2026, time.July, 1)
	if got := toTerm.Occupancy(); !got.To().Equal(agreed.To()) {
		t.Errorf("occupancy ends %s, want the agreed %s", got.To(), agreed.To())
	}

	// A periodic tenancy with no agreed end: occupancy is open until it ceases.
	periodic := base
	periodic.Term, _ = effective.Since(day(2025, time.July, 1))
	if !periodic.Occupancy().Open() {
		t.Error("a periodic tenancy's occupancy is bounded")
	}
	periodic.EndedOn = day(2026, time.June, 21)
	if got := periodic.Occupancy(); got.Open() || !got.To().Equal(periodic.EndedOn) {
		t.Errorf("a periodic tenancy that ceased has occupancy %s", got)
	}

	// And the shapes Validate refuses, because the schema refuses them too.
	for _, tc := range []struct {
		name string
		mut  func(*domain.Lease)
	}{
		{"occupancy ceasing before the tenancy began", func(l *domain.Lease) {
			l.EndedOn = day(2025, time.January, 1)
		}},
		{"occupancy ceasing after the agreement ran out", func(l *domain.Lease) {
			l.EndedOn = day(2027, time.January, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := base
			tc.mut(&l)
			if err := l.Validate(); err == nil {
				t.Errorf("accepted: %s", tc.name)
			}
		})
	}
}

// The state machine, stated as a table so a change to the rule shows up as a change
// to this table.
func TestTheTransitionTable(t *testing.T) {
	legal := map[[2]domain.State]bool{
		{domain.StateDraft, domain.StatePendingSignature}:  true,
		{domain.StateDraft, domain.StateLapsed}:            true,
		{domain.StatePendingSignature, domain.StateActive}: true,
		{domain.StatePendingSignature, domain.StateLapsed}: true,
		{domain.StateActive, domain.StateInNotice}:         true,
		{domain.StateActive, domain.StateRenewed}:          true,
		{domain.StateActive, domain.StateTerminated}:       true,
		{domain.StateInNotice, domain.StateTerminated}:     true,
		{domain.StateInNotice, domain.StateActive}:         true,
		{domain.StateTerminated, domain.StateSettled}:      true,
	}

	for _, from := range domain.States() {
		for _, to := range domain.States() {
			want := legal[[2]domain.State{from, to}] || from == to
			if got := domain.CanTransition(from, to); got != want {
				t.Errorf("%s -> %s: got %v, want %v", from, to, got, want)
			}
		}
	}

	// Terminal states absorb: a late event cannot revive a settled tenancy.
	for _, s := range []domain.State{domain.StateRenewed, domain.StateSettled, domain.StateLapsed} {
		if !s.Terminal() {
			t.Errorf("%s is not terminal", s)
		}
		for _, to := range domain.States() {
			if to == s {
				continue
			}
			if domain.CanTransition(s, to) {
				t.Errorf("%s -> %s is permitted and %s is terminal", s, to, s)
			}
		}
	}

	// The one backward edge, and the reason it exists.
	if !domain.CanTransition(domain.StateInNotice, domain.StateActive) {
		t.Error("notice cannot be withdrawn, so the workaround is a new lease and the tenancy " +
			"loses its ledger history")
	}
	if ev, ok := domain.EventFor(domain.StateInNotice, domain.StateActive); !ok ||
		ev != domain.EventNoticeWithdrawn {
		t.Error("withdrawing notice publishes nothing, so anything that reacted to the notice " +
			"never learns it was withdrawn")
	}
}

// Which states bill, which are tenancies, and which make a deposit settleable. Each
// of the three is a different question and getting them confused is a money bug.
func TestWhatEachStateMeansForMoney(t *testing.T) {
	for _, tc := range []struct {
		state                             domain.State
		billable, tenancy, depositSettles bool
	}{
		{domain.StateDraft, false, false, false},
		{domain.StatePendingSignature, false, false, false},
		{domain.StateActive, true, true, false},
		// Notice announces an ending and does not perform it. Stopping the rent here
		// is a month the owner never sees and the tenant never disputes.
		{domain.StateInNotice, true, true, false},
		// A renewed tenancy's deposit carries into its successor: the tenant has not
		// left, and returning it at renewal means collecting it again a day later.
		{domain.StateRenewed, false, true, false},
		{domain.StateTerminated, false, true, true},
		{domain.StateSettled, false, true, false},
		// A lapsed lease was never a tenancy, so it is not one for the no-double-let
		// constraint and there is no deposit to settle.
		{domain.StateLapsed, false, false, false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			if got := tc.state.Billable(); got != tc.billable {
				t.Errorf("Billable() = %v, want %v", got, tc.billable)
			}
			if got := tc.state.Tenancy(); got != tc.tenancy {
				t.Errorf("Tenancy() = %v, want %v", got, tc.tenancy)
			}
			if got := tc.state.DepositSettleable(); got != tc.depositSettles {
				t.Errorf("DepositSettleable() = %v, want %v", got, tc.depositSettles)
			}
		})
	}
}

// Who may do what. The interesting question is "may a tenant do this", and it has to
// be answerable without reading a handler.
func TestWhoMayMoveALease(t *testing.T) {
	// A tenant may serve notice and may sign. That is all.
	if !domain.MayTransition(domain.StateActive, domain.StateInNotice, domain.ActorTenant) {
		t.Error("a tenant cannot serve notice, which is what a notice period is for")
	}
	if !domain.MayTransition(domain.StatePendingSignature, domain.StateActive, domain.ActorTenant) {
		t.Error("a tenant cannot sign their own lease")
	}
	for _, to := range []domain.State{domain.StateTerminated, domain.StateRenewed, domain.StateLapsed} {
		if domain.MayTransition(domain.StateActive, to, domain.ActorTenant) {
			t.Errorf("a tenant may move an active lease to %s", to)
		}
	}
	if domain.MayTransition(domain.StateTerminated, domain.StateSettled, domain.ActorTenant) {
		t.Error("a tenant may settle their own deposit")
	}
	// Withdrawing notice is recorded by the owner: a tenant un-serving notice
	// unilaterally is a negotiation, not a transition.
	if domain.MayTransition(domain.StateInNotice, domain.StateActive, domain.ActorTenant) {
		t.Error("a tenant withdrew their own notice, which is a negotiation rather than a transition")
	}

	// The clock lapses an unsigned lease, and only the clock and the owner.
	if !domain.MayTransition(domain.StatePendingSignature, domain.StateLapsed, domain.ActorSystem) {
		t.Error("an unsigned lease cannot expire, so it sits pending forever")
	}
	if domain.MayTransition(domain.StateActive, domain.StateTerminated, domain.ActorSystem) {
		t.Error("the clock terminated a live tenancy on its own")
	}

	// Platform may do anything an owner may, because a support session has to be able
	// to unstick a tenancy — and every use is audited.
	for pair := range map[[2]domain.State]bool{
		{domain.StateActive, domain.StateTerminated}:  true,
		{domain.StateInNotice, domain.StateActive}:    true,
		{domain.StateTerminated, domain.StateSettled}: true,
	} {
		if !domain.MayTransition(pair[0], pair[1], domain.ActorPlatform) {
			t.Errorf("platform may not move %s -> %s, so a stuck tenancy needs a migration", pair[0], pair[1])
		}
	}

	// And nobody may make an illegal move, whoever they are.
	for _, a := range domain.Actors() {
		if domain.MayTransition(domain.StateSettled, domain.StateActive, a) {
			t.Errorf("%s re-opened a settled tenancy", a)
		}
	}
}

// The story's edge case: a renewal at a revised rent links to the previous lease and
// preserves its ledger history.
func TestRenewalIsANewLeaseLinkedToItsPredecessor(t *testing.T) {
	l := active(t)

	pred, succ, err := l.Renew(day(2027, time.July, 1), domain.ActorOwner)
	if err != nil {
		t.Fatalf("Renew: %v", err)
	}

	if pred.State != domain.StateRenewed {
		t.Errorf("the predecessor is %s, want renewed — and renewed is not terminated, because "+
			"the tenant did not leave", pred.State)
	}
	if pred.ID == "" || succ.Renews != pred.ID {
		t.Errorf("the successor names %q as its predecessor, want %q", succ.Renews, pred.ID)
	}
	if succ.ID != "" {
		t.Error("the successor arrived with an id — it is a new row and the database assigns it")
	}
	// The ledger history hangs off the lease id, so the predecessor's id must not have
	// moved: that is what "preserves its ledger history" means.
	if pred.ID != l.ID {
		t.Error("renewal changed the predecessor's id, re-attributing its postings")
	}
	// The successor's term begins exactly where the predecessor's ends. ADR-0008's
	// half-open interval: no gap for a day of unbilled occupancy, no overlap for the
	// no-double-let constraint to refuse.
	if !pred.Term.Meets(succ.Term) {
		t.Errorf("%s and %s do not meet — either a day is unbilled or two tenancies overlap",
			pred.Term, succ.Term)
	}
	if succ.State != domain.StateActive {
		t.Errorf("the successor is %s, want active", succ.State)
	}
	if succ.NoticeDays != l.NoticeDays {
		t.Error("the successor lost the notice period, which is a term of the tenancy rather than " +
			"of the document")
	}
	if err := succ.Validate(); err != nil {
		t.Errorf("the successor is invalid: %v", err)
	}

	// An open-ended tenancy has nothing to renew from: the successor would start where
	// the term ends, and it does not end.
	openEnded := l
	openEnded.Term, _ = effective.Since(day(2025, time.July, 1))
	if _, _, err := openEnded.Renew(day(2027, time.July, 1), domain.ActorOwner); err == nil {
		t.Error("an open-ended tenancy was renewed")
	}
	// And a tenant cannot renew their own lease.
	if _, _, err := l.Renew(day(2027, time.July, 1), domain.ActorTenant); err == nil {
		t.Error("a tenant renewed their own lease")
	}
}

// `expiring` is derived, not stored — the second place this diverges from the story's
// list of states. A stored one needs a job to maintain it, and a lease that should be
// expiring and is not would be a silent bug.
func TestExpiringIsDerivedFromTheClock(t *testing.T) {
	l := active(t) // term ends 1 July 2026, so the tenancy runs to 30 June

	for _, tc := range []struct {
		asOf   effective.Date
		within int
		want   bool
	}{
		{day(2026, time.January, 1), 60, false},
		{day(2026, time.May, 1), 60, false},  // 61 days out
		{day(2026, time.May, 2), 60, true},   // 60 days out
		{day(2026, time.June, 30), 60, true}, // the last day
		{day(2026, time.July, 1), 60, false}, // already over
		{day(2026, time.August, 1), 60, false},
	} {
		if got := l.Expiring(tc.asOf, tc.within); got != tc.want {
			t.Errorf("on %s, expiring within %d days = %v, want %v", tc.asOf, tc.within, got, tc.want)
		}
	}

	// A lease that is not billable is not expiring, whatever its dates say: a draft
	// with a past end date is not a tenancy running out.
	draft := l
	draft.State = domain.StateDraft
	if draft.Expiring(day(2026, time.June, 1), 60) {
		t.Error("a draft reports itself expiring")
	}
	// Nor is an open-ended one — it has no end to approach.
	openEnded := l
	openEnded.Term, _ = effective.Since(day(2025, time.July, 1))
	if openEnded.Expiring(day(2026, time.June, 1), 60) {
		t.Error("an open-ended tenancy reports itself expiring")
	}
}

// Lock-in, and the shapes of it that are contradictions.
func TestLockIn(t *testing.T) {
	l := active(t)
	l.LockInUntil = day(2026, time.January, 1)
	if err := l.Validate(); err != nil {
		t.Fatalf("a valid lock-in was refused: %v", err)
	}
	if !l.InLockIn(day(2025, time.December, 31)) {
		t.Error("the day before the lock-in ends is not inside it")
	}
	if l.InLockIn(day(2026, time.January, 1)) {
		t.Error("the day the lock-in ends is still inside it — the bound is exclusive, like every " +
			"other date bound in this product")
	}

	// A lock-in that outlasts the tenancy locks the tenant in past the end of their own
	// lease, which is not a term anybody can have agreed to.
	bad := l
	bad.LockInUntil = day(2027, time.January, 1)
	if err := bad.Validate(); err == nil {
		t.Error("a lock-in outlasting the tenancy was accepted")
	} else if !strings.Contains(err.Error(), "past the end") {
		t.Errorf("refused for the wrong reason: %v", err)
	}

	// One that has already expired when the tenancy starts is a data-entry error.
	stale := l
	stale.LockInUntil = day(2025, time.January, 1)
	if err := stale.Validate(); err == nil {
		t.Error("a lock-in that expired before the tenancy started was accepted")
	}
}

// Every event this module publishes is one ADR-0001 declared or an addition under the
// same naming rule. A name invented here that ADR-0002's subject convention cannot
// carry is a consumer that never receives anything.
func TestEveryEventFollowsTheNamingRule(t *testing.T) {
	declared := map[domain.Event]bool{
		domain.EventStarted: true, domain.EventEnded: true, domain.EventNoticeServed: true,
	}
	for _, e := range domain.Events() {
		parts := strings.Split(string(e), ".")
		if len(parts) != 3 {
			t.Errorf("%q is not <module>.<aggregate>.<verb>", e)
			continue
		}
		if parts[0] != "lease" {
			t.Errorf("%q is published by the lease module and does not say so", e)
		}
		// Past tense, which is ADR-0002 §2: an event is a fact, not a command.
		//
		// "-ed" is a heuristic rather than the rule, and English has irregular past
		// participles — "withdrawn" is one. Naming them here is honest: the check is a
		// spelling test with an exception list, not a grammar engine, and a new
		// irregular form has to be added deliberately rather than slipping through.
		irregular := map[string]bool{"withdrawn": true}
		if !strings.HasSuffix(parts[2], "ed") && !irregular[parts[2]] {
			t.Errorf("%q is not past tense — an event is a fact rather than an instruction. "+
				"If it is an irregular participle, add it to the list in this test", e)
		}
		if declared[e] {
			t.Logf("%s (declared in ADR-0001)", e)
		} else {
			t.Logf("%s (added by ADR-0010)", e)
		}
	}
	// The three ADR-0001 named must all still exist.
	for e := range declared {
		found := false
		for _, got := range domain.Events() {
			if got == e {
				found = true
			}
		}
		if !found {
			t.Errorf("%q was declared in ADR-0001 and is no longer published", e)
		}
	}
}

// A lease must name the unit it lets and must have a term. Both are the kind of thing
// a draft-saving screen will try to write empty.
func TestLeaseValidation(t *testing.T) {
	good := active(t)
	for _, tc := range []struct {
		name string
		mut  func(*domain.Lease)
		want string
	}{
		{"no organisation", func(l *domain.Lease) { l.TenantID = "" }, "organisation"},
		{"no unit", func(l *domain.Lease) { l.Unit = "" }, "unit"},
		{"no term", func(l *domain.Lease) { l.Term = effective.Interval{} }, "term"},
		{"an invented state", func(l *domain.Lease) { l.State = "signed_ish" }, "unknown state"},
		{"a negative notice period", func(l *domain.Lease) { l.NoticeDays = -30 }, "notice"},
		{"renewed with no end date", func(l *domain.Lease) {
			l.State = domain.StateRenewed
			l.Term, _ = effective.Since(day(2025, time.July, 1))
		}, "successor starts there"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := good
			tc.mut(&l)
			err := l.Validate()
			if err == nil {
				t.Fatalf("accepted: %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}
