package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/workflow"
)

// ADR-0015's acceptance criteria, tested where they can be tested: the ordering,
// what may still be undone, who is told, and whether a key survives a retry.
//
// Temporal's own guarantee — that a killed worker resumes from the last completed
// activity — is not re-tested here; it is the product's guarantee, not ours. What
// is ours, and what is tested, is that resuming produces no duplicate side effect,
// and that is a property of where the idempotency key is computed rather than of
// the executor.

const payoutID = "7c9e6679-7425-40de-944b-e07fc1f90ae7"

// recorder captures what actually ran, which is the only way to assert an order.
type recorder struct {
	did  []string
	undo []string
	keys map[string]string
}

func newRecorder() *recorder { return &recorder{keys: map[string]string{}} }

func (r *recorder) step(name string, fail error) workflow.Step {
	return workflow.Step{
		Name: name,
		Do: func(_ context.Context, key string) error {
			r.did = append(r.did, name)
			r.keys[name] = key
			return fail
		},
		Undo: func(_ context.Context, key string) error {
			r.undo = append(r.undo, name)
			if got := r.keys[name]; got != "" && got != key {
				return fmt.Errorf("compensation got key %q and the step got %q", key, got)
			}
			return nil
		},
	}
}

// payout builds the acceptance scenario's shape: two reversible postings, then the
// bank transfer, which is irreversible and therefore last.
func payout(t *testing.T, r *recorder, failAt string, failure error) *workflow.Saga {
	t.Helper()
	s, err := workflow.New(workflow.OpPayoutExecute, payoutID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fail := func(name string) error {
		if name == failAt {
			return failure
		}
		return nil
	}
	transferFail := fail("bank_transfer")
	return s.
		Step(workflow.Step{Name: "read_bank_account", ReadOnly: true,
			Do: func(_ context.Context, key string) error {
				r.did = append(r.did, "read_bank_account")
				r.keys["read_bank_account"] = key
				return fail("read_bank_account")
			}}).
		Step(r.step("post_platform_fee", fail("post_platform_fee"))).
		Step(r.step("post_payout_entry", fail("post_payout_entry"))).
		PointOfNoReturn().
		Step(workflow.Step{Name: "bank_transfer",
			Do: func(_ context.Context, key string) error {
				r.did = append(r.did, "bank_transfer")
				r.keys["bank_transfer"] = key
				return transferFail
			}})
}

// The primary acceptance scenario. A payout fails after the fee posting and before
// the bank transfer: the postings are reversed, the world is where it started, and
// both the owner and operations are told.
func TestAPayoutThatFailsBeforeTheTransferReversesItsPostings(t *testing.T) {
	r := newRecorder()
	boom := errors.New("the owner's bank account was rejected by the sponsor bank")

	res, err := payout(t, r, "post_payout_entry", boom).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.Outcome != workflow.Compensated {
		t.Fatalf("outcome is %s, want compensated — the failure was before the point of no return, "+
			"so the world can be put back", res.Outcome)
	}
	if res.PastNoReturn {
		t.Error("the run claims it passed the point of no return, and the transfer never ran")
	}
	// The fee posting is reversed. That is the criterion.
	if want := []string{"post_platform_fee"}; !equal(res.Compensated, want) {
		t.Errorf("reversed %v, want %v — the fee posting must not survive a payout that did not happen",
			res.Compensated, want)
	}
	// And the irreversible step never ran, because it is last.
	for _, name := range r.did {
		if name == "bank_transfer" {
			t.Error("the bank transfer ran despite an earlier step failing")
		}
	}

	// Both audiences, per the criterion, and they are told different things.
	audiences := map[string]string{}
	for _, n := range res.Notifications {
		audiences[n.Audience] = n.Message
	}
	for _, who := range []string{"owner", "operations"} {
		if audiences[who] == "" {
			t.Errorf("nobody told the %s that a payout was reversed", who)
		}
	}
	if audiences["owner"] == audiences["operations"] {
		t.Error("the owner and operations got the same message — an owner needs to know the money did " +
			"not move, operations needs the step and the error")
	}
	if !strings.Contains(audiences["operations"], "post_payout_entry") {
		t.Errorf("the operations message does not name the failing step: %q", audiences["operations"])
	}
	t.Logf("owner:      %s", audiences["owner"])
	t.Logf("operations: %s", audiences["operations"])
}

// Compensations run in reverse. A later step's effect can depend on an earlier
// one's, so undoing the earlier first can leave the later compensation with
// nothing coherent to act on.
func TestCompensationsRunInReverseOrder(t *testing.T) {
	r := newRecorder()
	s, err := workflow.New(workflow.OpMandateCreate, payoutID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.Step(r.step("one", nil)).
		Step(r.step("two", nil)).
		Step(r.step("three", nil)).
		Step(r.step("four", errors.New("no")))

	res, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if want := []string{"one", "two", "three", "four"}; !equal(r.did, want) {
		t.Fatalf("ran %v, want %v", r.did, want)
	}
	if want := []string{"three", "two", "one"}; !equal(res.Compensated, want) {
		t.Fatalf("compensated %v, want %v — reverse of the order they ran in", res.Compensated, want)
	}
	if !equal(r.undo, res.Compensated) {
		t.Errorf("the result says %v was reversed and %v actually was", res.Compensated, r.undo)
	}
}

// The scenario the story does not name, and the one that matters more.
//
// The bank transfer itself fails. Whether the money left is unknowable from here —
// a timeout is not a decline — so the design refuses to guess. It escalates rather
// than compensating, because compensating would reverse a fee for a payout that
// may well have gone out, and the next run would send it again.
func TestATransferThatMayHaveLandedEscalatesInsteadOfCompensating(t *testing.T) {
	r := newRecorder()
	s := payout(t, r, "bank_transfer", errors.New("context deadline exceeded"))
	res, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.WorkflowID != s.WorkflowID() {
		t.Errorf("the result names workflow %q and the saga is %q", res.WorkflowID, s.WorkflowID())
	}

	if res.Outcome != workflow.Escalated {
		t.Fatalf("outcome is %s, want escalated — a transfer that timed out may have landed, and "+
			"compensating it would reverse the fee for a payout that went out", res.Outcome)
	}
	if !res.PastNoReturn {
		t.Error("the run does not record that it reached the point of no return, and the schema's " +
			"constraint is checked against that flag")
	}
	if len(r.undo) != 0 {
		t.Errorf("compensated %v after the point of no return — nothing there can be undone", r.undo)
	}
	var toOps string
	for _, n := range res.Notifications {
		if n.Audience == "operations" {
			toOps = n.Message
		}
	}
	if !strings.Contains(toOps, res.WorkflowID) {
		t.Errorf("the escalation does not name the workflow, so nobody can find it: %q", toOps)
	}
	t.Logf("escalation: %s", toOps)
}

// A compensation that cannot be applied is the worst state available: money moved
// and the record does not agree. It must not be reported as a clean failure,
// because a workflow that failed cleanly is one nobody goes looking for.
func TestAFailedCompensationEscalates(t *testing.T) {
	stubborn := errors.New("the ledger refused the reversing entry")
	s, err := workflow.New(workflow.OpMandateCreate, payoutID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ok := func(_ context.Context, _ string) error { return nil }
	s.Step(workflow.Step{Name: "first", Do: ok,
		Undo: func(_ context.Context, _ string) error { return stubborn }}).
		Step(workflow.Step{Name: "second", Do: ok,
			Undo: func(_ context.Context, _ string) error { return nil }}).
		Step(workflow.Step{Name: "third", Do: func(_ context.Context, _ string) error {
			return errors.New("no")
		}, Undo: ok})

	res, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Outcome != workflow.Escalated {
		t.Fatalf("outcome is %s, want escalated", res.Outcome)
	}
	// The compensation that worked still ran: stopping at the first failure would
	// leave steps standing that could have been undone.
	if want := []string{"second"}; !equal(res.Compensated, want) {
		t.Errorf("reversed %v, want %v — a failed compensation must not abandon the others",
			res.Compensated, want)
	}
	if _, named := res.CompensationErrs["first"]; !named {
		t.Error("the step whose compensation failed is not named, so nobody knows what to fix")
	}
	var toOps string
	for _, n := range res.Notifications {
		if n.Audience == "operations" {
			toOps = n.Message
		}
	}
	if !strings.Contains(toOps, "first") {
		t.Errorf("the escalation does not name the uncompensated step: %q", toOps)
	}
	t.Logf("escalation: %s", toOps)
}

// The declaration-time rules. Each of these is a mistake that reads perfectly well
// in review and is wrong at 3am, so it is refused when the saga is built rather
// than discovered when it runs.
func TestTheDeclarationRules(t *testing.T) {
	ok := func(_ context.Context, _ string) error { return nil }

	for _, tc := range []struct {
		name  string
		build func() (*workflow.Saga, error)
		want  string
	}{
		{
			name: "a state-changing step before the boundary with no compensation",
			build: func() (*workflow.Saga, error) {
				s, err := workflow.New(workflow.OpPayoutExecute, payoutID)
				if err != nil {
					return nil, err
				}
				return s.Step(workflow.Step{Name: "post", Do: ok}), nil
			},
			want: "cannot be undone",
		},
		{
			name: "a step after the boundary offering a compensation",
			build: func() (*workflow.Saga, error) {
				s, err := workflow.New(workflow.OpPayoutExecute, payoutID)
				if err != nil {
					return nil, err
				}
				return s.PointOfNoReturn().
					Step(workflow.Step{Name: "transfer", Do: ok, Undo: ok}), nil
			},
			want: "after the point of no return",
		},
		{
			name: "read-only and compensating at the same time",
			build: func() (*workflow.Saga, error) {
				s, err := workflow.New(workflow.OpPayoutExecute, payoutID)
				if err != nil {
					return nil, err
				}
				return s.Step(workflow.Step{Name: "read", ReadOnly: true, Do: ok, Undo: ok}), nil
			},
			want: "read-only",
		},
		{
			name: "two steps with one name, which is one idempotency key",
			build: func() (*workflow.Saga, error) {
				s, err := workflow.New(workflow.OpPayoutExecute, payoutID)
				if err != nil {
					return nil, err
				}
				return s.Step(workflow.Step{Name: "post", Do: ok, Undo: ok}).
					Step(workflow.Step{Name: "post", Do: ok, Undo: ok}), nil
			},
			want: "same",
		},
		{
			name: "the boundary declared twice",
			build: func() (*workflow.Saga, error) {
				s, err := workflow.New(workflow.OpPayoutExecute, payoutID)
				if err != nil {
					return nil, err
				}
				return s.PointOfNoReturn().PointOfNoReturn(), nil
			},
			want: "twice",
		},
		{
			name: "a boundary on an operation whose spec says it has none",
			build: func() (*workflow.Saga, error) {
				s, err := workflow.New(workflow.OpAgreementESign, payoutID)
				if err != nil {
					return nil, err
				}
				return s.PointOfNoReturn(), nil
			},
			want: "point of no return",
		},
		{
			name: "an irreversible operation that never says where the boundary is",
			build: func() (*workflow.Saga, error) {
				s, err := workflow.New(workflow.OpPayoutExecute, payoutID)
				if err != nil {
					return nil, err
				}
				return s.Step(workflow.Step{Name: "post", Do: ok, Undo: ok}), nil
			},
			want: "never says where",
		},
		{
			name: "no steps at all",
			build: func() (*workflow.Saga, error) {
				return workflow.New(workflow.OpAgreementESign, payoutID)
			},
			want: "no steps",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := tc.build()
			if err != nil {
				t.Fatalf("building: %v", err)
			}
			_, err = s.Run(context.Background())
			if err == nil {
				t.Fatalf("accepted: %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			} else {
				t.Logf("refused: %v", err)
			}
		})
	}
}

// The property behind "no duplicate side effects on resume". Temporal makes the
// workflow resumable; what stops a resumed activity being a second request is that
// its key came from the workflow, not from inside the activity.
func TestAnIdempotencyKeyIsTheSameOnEveryAttempt(t *testing.T) {
	id, err := workflow.ID(workflow.OpPayoutExecute, payoutID)
	if err != nil {
		t.Fatalf("ID: %v", err)
	}

	first, err := workflow.IdempotencyKey(id, "bank_transfer")
	if err != nil {
		t.Fatalf("IdempotencyKey: %v", err)
	}
	for range 100 {
		again, err := workflow.IdempotencyKey(id, "bank_transfer")
		if err != nil {
			t.Fatalf("IdempotencyKey: %v", err)
		}
		if again != first {
			t.Fatalf("two calls produced %q and %q — a retry would be a second request at the provider",
				first, again)
		}
	}
	// Different steps of one workflow must not share a key, or the second is
	// deduplicated away by the provider and reported as done.
	other, _ := workflow.IdempotencyKey(id, "post_platform_fee")
	if other == first {
		t.Error("two steps of one workflow share an idempotency key")
	}
	// And the same step of two subjects must not.
	otherID, _ := workflow.ID(workflow.OpPayoutExecute, "9f1b7a0c-0000-4000-8000-000000000001")
	otherSubject, _ := workflow.IdempotencyKey(otherID, "bank_transfer")
	if otherSubject == first {
		t.Error("the same step of two payouts shares an idempotency key — one owner's transfer would " +
			"be deduplicated against another's")
	}
	t.Logf("key: %s", first)
}

// The compensation receives the key its step received, so a compensation retried
// five times is one correction rather than five reversing entries.
func TestACompensationReusesItsStepsKey(t *testing.T) {
	var stepKey, undoKey string
	s, err := workflow.New(workflow.OpMandateCreate, payoutID)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.Step(workflow.Step{
		Name: "post",
		Do:   func(_ context.Context, key string) error { stepKey = key; return nil },
		Undo: func(_ context.Context, key string) error { undoKey = key; return nil },
	}).Step(workflow.Step{
		Name: "boom",
		Do:   func(_ context.Context, _ string) error { return errors.New("no") },
		Undo: func(_ context.Context, _ string) error { return nil },
	})

	if _, err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stepKey == "" || undoKey != stepKey {
		t.Fatalf("the step used %q and its compensation used %q — a retried compensation would write "+
			"a second reversing entry", stepKey, undoKey)
	}
}

// A run's recorded state comes from its outcome rather than from a caller choosing,
// so the two vocabularies cannot drift.
func TestTheRecordedStateIsDerivedFromTheOutcome(t *testing.T) {
	for outcome, want := range map[workflow.Outcome]workflow.RunState{
		workflow.Completed:   workflow.StateCompleted,
		workflow.Compensated: workflow.StateCompensated,
		workflow.Escalated:   workflow.StateEscalated,
	} {
		got, err := workflow.StateFor(outcome)
		if err != nil {
			t.Fatalf("StateFor(%s): %v", outcome, err)
		}
		if got != want {
			t.Errorf("outcome %s records as %s, want %s", outcome, got, want)
		}
	}
	if _, err := workflow.StateFor("invented"); err == nil {
		t.Error("an unknown outcome mapped to a state")
	}
	// `running` and `compensating` are in-flight, and `escalated` is terminal —
	// it is terminal because it is waiting for a person, which is why it is not
	// called "failed".
	if workflow.StateRunning.Terminal() || workflow.StateCompensating.Terminal() {
		t.Error("an in-flight state reports itself terminal")
	}
	if !workflow.StateEscalated.Terminal() {
		t.Error("escalated is not terminal, so nothing would ever stop watching it")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
