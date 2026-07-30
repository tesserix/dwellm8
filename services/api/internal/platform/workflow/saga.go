package workflow

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// The compensation standard. ADR-0015 §4.
//
// The acceptance scenario this exists for: a payout fails after the fee has been
// posted and before the bank transfer. The posting must be reversed, the state
// must be consistent, and both the owner and operations must be told.
//
// # The irreversible step goes last
//
// The rule that makes the scenario above solvable at all, and it is a rule about
// the order steps are written in rather than about any code here.
//
// A completed bank transfer cannot be compensated. Asking for the money back is a
// new operation with its own failure modes, not an undo. So every step that can be
// undone happens first, and the irreversible external call is the final one.
// Ordered that way, "fee posted, transfer failed" is compensable: reverse the
// posting and the world is where it started. Ordered the other way — transfer,
// then post — the same failure leaves money gone with no way to un-send it and no
// entry explaining where it went.
//
// After the irreversible step succeeds there is nothing to compensate, so
// everything remaining must be retried until it succeeds. A workflow therefore has
// two phases and one boundary, and Saga makes the boundary explicit rather than
// leaving it in a comment: Step refuses a compensable step after
// PointOfNoReturn(), so the ordering cannot be got wrong quietly.
//
// # A failed compensation is worse than a failed step
//
// A step that fails leaves the world unchanged. A compensation that fails leaves
// money moved and the record of it uncorrected, which is the one state nothing
// downstream can reason about. So compensation uses RetryCompensation, never gives
// up on its own, and when it has exhausted its patience the saga does not report
// failure and finish — it escalates and stays visible. A workflow that failed
// cleanly is a workflow nobody is looking for.

// StepFunc does the work. It receives the idempotency key the workflow derived for
// it, and must present that key to whatever it calls — a provider, or a unique
// index. It must not generate one.
type StepFunc func(ctx context.Context, key string) error

// CompensateFunc undoes a step. It receives the same key its step received, so
// that a compensation retried five times is one correction.
//
// For a ledger step the compensation is a reversing entry, never a delete: ADR-0006
// §3 makes that the only correction there is, and ADR-0015 adds the reason
// `workflow_compensated` so the history says a workflow undid this rather than
// blaming an operator who was not involved.
type CompensateFunc func(ctx context.Context, key string) error

// Step is one unit of a durable operation.
type Step struct {
	Name string
	Do   StepFunc
	// Undo is nil for a step that cannot be undone. A nil Undo before the point of
	// no return is a step whose failure leaves the saga unable to return to its
	// starting state, so it is refused: see Saga.Step.
	Undo CompensateFunc

	// ReadOnly marks a step that changes nothing — reading an owner's bank
	// account, fetching a rate, asking the provider what is true. It needs no
	// compensation because there is nothing to undo.
	//
	// It exists as a flag rather than as "Undo may be nil" because those are two
	// different facts and a nil would conflate them: a step that changes nothing,
	// and a step that changes something and forgot how to undo it. The second is
	// the bug, and it is invisible if the first is spelled the same way.
	ReadOnly bool
}

// Outcome is how a saga ended.
type Outcome string

const (
	// Completed is every step done.
	Completed Outcome = "completed"
	// Compensated is a step failed and every earlier step was undone. The world is
	// where it started, and this is a successful outcome of an unsuccessful
	// operation.
	Compensated Outcome = "compensated"
	// Escalated is a compensation that could not be applied, or a step that failed
	// after the point of no return. Money has moved and the record does not agree,
	// and a person has been told. This is the only outcome that pages somebody.
	Escalated Outcome = "escalated"
)

// Notification is what the saga says happened, and to whom. Both audiences are
// required by the acceptance criterion, and they need different things: the owner
// needs to know their money did not move, and operations needs to know why.
type Notification struct {
	// Audience is "owner" or "operations".
	Audience string
	Outcome  Outcome
	Message  string
}

// Result is what a saga run produced.
type Result struct {
	// WorkflowID is the run this result belongs to. Carried on the result because
	// an escalation is read by a person who has to go and find the run, and a
	// result that does not name itself makes them search.
	WorkflowID string
	Outcome    Outcome
	// Completed is the steps that succeeded, in the order they ran.
	Completed []string
	// Compensated is the steps that were undone, in the order they were undone —
	// which is the reverse of the order they ran in.
	Compensated []string
	// FailedStep is the step whose failure ended the run, if one did.
	FailedStep string
	// Err is that step's error.
	Err error
	// CompensationErrs is every compensation that could not be applied. Non-empty
	// means Escalated, always.
	CompensationErrs map[string]error
	// PastNoReturn reports whether the irreversible step had already succeeded. It
	// is what the schema's constraint is checked against: a run that passed the
	// point of no return may not be recorded as compensated, because it cannot
	// have been.
	PastNoReturn bool
	// Notifications is what must be sent. The saga produces them rather than
	// sending them, so the notify module stays the only thing that sends anything
	// and so a test can assert on them.
	Notifications []Notification
}

// Saga is an ordered set of steps with their compensations.
//
// It is not a Temporal workflow and does not know about one. A Temporal workflow
// built on this is a thin shell that turns each Step into an ExecuteActivity call
// with the right options; everything about which order things happen in, what may
// still be undone and who is told lives here, where it is testable.
type Saga struct {
	spec       Spec
	workflowID string
	steps      []Step
	// noReturnAt is the index at which the irreversible step begins: len(steps) at
	// the moment PointOfNoReturn was called. -1 when it has not been called.
	noReturnAt int
	err        error
}

// New starts a saga for one operation and one subject.
func New(op Operation, subject string) (*Saga, error) {
	spec, ok := Lookup(op)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNotDurable, op)
	}
	id, err := ID(op, subject)
	if err != nil {
		return nil, err
	}
	return &Saga{spec: spec, workflowID: id, noReturnAt: -1}, nil
}

// WorkflowID is the deterministic id this saga runs under.
func (s *Saga) WorkflowID() string { return s.workflowID }

// Step appends a step. Errors are collected rather than returned so the
// declaration reads as a sequence; Run reports them.
func (s *Saga) Step(st Step) *Saga {
	switch {
	case st.Name == "":
		s.fail(errors.New("workflow: a step with no name cannot have an idempotency key, " +
			"so its retry would be a second request"))
	case strings.Contains(st.Name, "#"):
		s.fail(fmt.Errorf("workflow: step %q contains the key separator", st.Name))
	case st.Do == nil:
		s.fail(fmt.Errorf("workflow: step %q does nothing", st.Name))
	}
	for _, existing := range s.steps {
		if existing.Name == st.Name {
			// Two steps with one name share an idempotency key, so the second would
			// be deduplicated away by the provider and reported as done.
			s.fail(fmt.Errorf("workflow: step %q appears twice, and both would present the same "+
				"idempotency key", st.Name))
		}
	}

	if st.ReadOnly && st.Undo != nil {
		s.fail(fmt.Errorf("workflow: step %q is marked read-only and supplies a compensation, "+
			"so one of the two is wrong about whether it changes anything", st.Name))
	}

	switch {
	case st.ReadOnly:
		// Nothing to undo, wherever it sits.
	case s.noReturnAt >= 0:
		// Past the boundary, and nothing here can be undone. A step that offers a
		// compensation is claiming otherwise, and the claim is what has to be
		// refused: it would be believed at review time and false at 3am.
		if st.Undo != nil {
			s.fail(fmt.Errorf("workflow: step %q is after the point of no return and offers a "+
				"compensation — nothing after an irreversible step can be undone, so it must be "+
				"retried until it succeeds instead", st.Name))
		}
	case st.Undo == nil:
		s.fail(fmt.Errorf("workflow: step %q is before the point of no return, changes state and "+
			"cannot be undone, so a later failure could not return to the starting state — give it a "+
			"compensation, declare the point of no return above it, or mark it read-only", st.Name))
	}

	s.steps = append(s.steps, st)
	return s
}

// PointOfNoReturn marks where the irreversible step begins. Everything declared
// before it must be compensable; nothing after it may be.
func (s *Saga) PointOfNoReturn() *Saga {
	if s.noReturnAt >= 0 {
		s.fail(errors.New("workflow: the point of no return is declared twice, so one of the two " +
			"is wrong about what is reversible"))
		return s
	}
	if !s.spec.HasPointOfNoReturn {
		s.fail(fmt.Errorf("workflow: %s declares a point of no return and its spec says it has none",
			s.spec.Op))
		return s
	}
	s.noReturnAt = len(s.steps)
	return s
}

func (s *Saga) fail(err error) {
	if s.err == nil {
		s.err = err
	}
}

// Run executes the steps in order and compensates in reverse on failure.
//
// It is called from inside a Temporal workflow, where each Step.Do is an
// ExecuteActivity that Temporal has already made durable and resumable. Run itself
// holds no state between calls: after a worker is killed, Temporal replays the
// workflow, the completed activities return their recorded results without running
// again, and Run reaches the same point it was at. That is the mechanism behind
// this ADR's second acceptance criterion, and the property Run has to preserve is
// that it makes no decision from anything but its inputs and the step results.
func (s *Saga) Run(ctx context.Context) (Result, error) {
	if s.err != nil {
		return Result{}, s.err
	}
	if len(s.steps) == 0 {
		return Result{}, fmt.Errorf("workflow: %s has no steps", s.spec.Op)
	}
	if s.spec.HasPointOfNoReturn && s.noReturnAt < 0 {
		return Result{}, fmt.Errorf("workflow: %s has an irreversible step and never says where — "+
			"without the boundary a failure after it would try to compensate something that cannot be "+
			"undone", s.spec.Op)
	}

	res := Result{WorkflowID: s.workflowID, Outcome: Completed, CompensationErrs: map[string]error{}}

	for i, st := range s.steps {
		if s.noReturnAt >= 0 && i == s.noReturnAt {
			res.PastNoReturn = true
		}
		key, err := IdempotencyKey(s.workflowID, st.Name)
		if err != nil {
			return Result{}, err
		}
		if err := st.Do(ctx, key); err != nil {
			res.FailedStep, res.Err = st.Name, err
			// PastNoReturn is set when the irreversible step *begins*, and a failure
			// of that step is the ambiguous case: the external call may or may not
			// have landed. It is treated as though it did, because assuming it did
			// not is what double-pays somebody.
			if res.PastNoReturn {
				return s.escalate(res, fmt.Sprintf("step %q failed at or after the point of no return, "+
					"so the external effect may have landed and cannot be undone: %v", st.Name, err)), nil
			}
			s.compensate(ctx, i, &res)
			return res, nil
		}
		res.Completed = append(res.Completed, st.Name)
	}

	res.Notifications = append(res.Notifications, Notification{
		Audience: "owner", Outcome: Completed,
		Message: fmt.Sprintf("%s completed", s.spec.Op),
	})
	return res, nil
}

// compensate undoes every step before failedAt, in reverse.
//
// Reverse order is not a stylistic choice. A later step's effect may depend on an
// earlier one — a bank transfer against a balance the fee posting created — so
// undoing the earlier one first can leave the later compensation with nothing
// coherent to act on.
func (s *Saga) compensate(ctx context.Context, failedAt int, res *Result) {
	for i := failedAt - 1; i >= 0; i-- {
		st := s.steps[i]
		if st.Undo == nil {
			continue // only reachable past the boundary, which does not compensate
		}
		key, err := IdempotencyKey(s.workflowID, st.Name)
		if err != nil {
			res.CompensationErrs[st.Name] = err
			continue
		}
		if err := st.Undo(ctx, key); err != nil {
			res.CompensationErrs[st.Name] = err
			// Keep going. The alternative — stopping at the first failure — leaves
			// steps that could have been undone still standing, which makes the
			// inconsistency larger than it needs to be. Every failure is reported.
			continue
		}
		res.Compensated = append(res.Compensated, st.Name)
	}

	if len(res.CompensationErrs) > 0 {
		names := make([]string, 0, len(res.CompensationErrs))
		for n := range res.CompensationErrs {
			names = append(names, n)
		}
		sort.Strings(names)
		*res = s.escalate(*res, fmt.Sprintf("compensation failed for %s — money has moved and the "+
			"record does not agree", strings.Join(names, ", ")))
		return
	}

	res.Outcome = Compensated
	// Both audiences, because the acceptance criterion says both. They are told
	// different things on purpose: an owner needs to know the money did not move
	// and that nothing is half-done, and operations needs the step and the error.
	res.Notifications = append(res.Notifications,
		Notification{
			Audience: "owner", Outcome: Compensated,
			Message: fmt.Sprintf("%s did not complete and nothing was left half-done", s.spec.Op),
		},
		Notification{
			Audience: "operations", Outcome: Compensated,
			Message: fmt.Sprintf("%s failed at step %q (%v); %d step(s) reversed",
				s.spec.Op, res.FailedStep, res.Err, len(res.Compensated)),
		},
	)
}

// escalate is the outcome that pages somebody. It never reports success and it
// never reports a clean failure, because a workflow that failed cleanly is one
// nobody goes looking for.
func (s *Saga) escalate(res Result, why string) Result {
	res.Outcome = Escalated
	res.Notifications = append(res.Notifications,
		Notification{
			Audience: "operations", Outcome: Escalated,
			Message: fmt.Sprintf("%s [%s] needs a person: %s", s.spec.Op, s.workflowID, why),
		},
		Notification{
			Audience: "owner", Outcome: Escalated,
			Message: fmt.Sprintf("%s is being checked by our team", s.spec.Op),
		},
	)
	return res
}

// RunState mirrors workflow_runs.state. A run's state lives in our own tables and
// not only in Temporal, and ADR-0015 §7 is why: Temporal's retention is days, and
// a support call about a payout from last month has to have something to look at.
// Temporal is the executor; these tables are the record.
type RunState string

const (
	StateRunning      RunState = "running"
	StateCompleted    RunState = "completed"
	StateCompensating RunState = "compensating"
	StateCompensated  RunState = "compensated"
	StateEscalated    RunState = "escalated"
)

// StateFor maps an outcome onto the recorded state, so the two vocabularies cannot
// drift by anybody choosing.
func StateFor(o Outcome) (RunState, error) {
	switch o {
	case Completed:
		return StateCompleted, nil
	case Compensated:
		return StateCompensated, nil
	case Escalated:
		return StateEscalated, nil
	}
	return "", fmt.Errorf("workflow: no recorded state for outcome %q", o)
}

// RunStates returns every state, ordered, for the contract test.
func RunStates() []RunState {
	return []RunState{StateRunning, StateCompleted, StateCompensating, StateCompensated, StateEscalated}
}

// Terminal reports whether nothing further will happen to this run without a
// person. Escalated is terminal in that sense and is the reason it is not called
// "failed": it is waiting for somebody.
func (s RunState) Terminal() bool {
	return s == StateCompleted || s == StateCompensated || s == StateEscalated
}
