package temporalx

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	sdkworkflow "go.temporal.io/sdk/workflow"

	"github.com/tesserix/dwellm8/services/api/internal/platform/workflow"
)

// The adapter is a shell, so what is worth testing is that it does not quietly
// change the standard's meaning: the operations it accepts, the retry policy it
// converts, and where it puts the point of no return.

// quick keeps the test environment from spending real patience on a retry.
var quick = workflow.Retry{Initial: time.Millisecond, Coefficient: 1, Max: time.Millisecond, Attempts: 1}

func TestRegisterRefusesAnOperationTheStandardDoesNotList(t *testing.T) {
	r := NewRegistry()
	err := r.Register(Definition{
		Op:    workflow.Operation("money.something.invented"),
		Build: func(Input) ([]StepDef, error) { return nil, nil },
	})
	if !errors.Is(err, workflow.ErrNotDurable) {
		t.Fatalf("err = %v, want ErrNotDurable — an unlisted operation has no timeout and no escalation", err)
	}
}

func TestRegisterRefusesADefinitionWithNoSteps(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Definition{Op: workflow.OpPayoutExecute}); err == nil {
		t.Fatal("accepted a definition with no step builder")
	}
}

func TestRegisterRefusesTheSameOperationTwice(t *testing.T) {
	r := NewRegistry()
	d := Definition{
		Op:    workflow.OpPayoutExecute,
		Build: func(Input) ([]StepDef, error) { return nil, nil },
	}
	if err := r.Register(d); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := r.Register(d); err == nil {
		t.Fatal("registered the same operation twice, so which one runs is undefined")
	}
}

// Attempts of zero must stay zero, which the SDK reads as unlimited. Converting
// it to a small number would cap a money call the standard deliberately bounds
// by time instead — the payout would stop retrying and nothing would say so.
func TestRetryPolicyKeepsAnUncappedAttemptCount(t *testing.T) {
	p := retryPolicy(workflow.RetryProvider)
	if p.MaximumAttempts != 0 {
		t.Fatalf("MaximumAttempts = %d, want 0 (unlimited)", p.MaximumAttempts)
	}
	if p.InitialInterval != workflow.RetryProvider.Initial {
		t.Errorf("InitialInterval = %v, want %v", p.InitialInterval, workflow.RetryProvider.Initial)
	}
	if p.MaximumInterval != workflow.RetryProvider.Max {
		t.Errorf("MaximumInterval = %v, want %v", p.MaximumInterval, workflow.RetryProvider.Max)
	}
	if p.BackoffCoefficient != workflow.RetryProvider.Coefficient {
		t.Errorf("BackoffCoefficient = %v, want %v", p.BackoffCoefficient, workflow.RetryProvider.Coefficient)
	}
}

// A compensation with no policy of its own gets the patient one. Falling back to
// the ordinary provider policy would let a compensation give up, which is the
// one failure the standard says must escalate instead.
func TestACompensationDefaultsToThePatientPolicy(t *testing.T) {
	if workflow.RetryCompensation.Max <= workflow.RetryProvider.Max {
		t.Fatal("the compensation policy is not more patient than the provider one, " +
			"so this adapter's default would not mean what it says")
	}
}

// The workflow name is the operation, so a run found in a support call names the
// thing it is doing rather than a Go function.
func TestWorkflowNameIsTheOperation(t *testing.T) {
	if got := WorkflowName(workflow.OpPayoutExecute); got != "payout.execute" {
		t.Fatalf("name = %q, want payout.execute", got)
	}
}

// Every operation on the durable list must have a task queue a worker can serve.
// A queue that is empty or malformed is a workflow that waits forever with
// nothing reporting an error.
func TestEveryOperationHasAServableQueue(t *testing.T) {
	seen := map[string]bool{}
	for _, op := range workflow.Operations() {
		spec, ok := workflow.Lookup(op)
		if !ok {
			t.Fatalf("%s is listed and has no spec", op)
		}
		q := spec.TaskQueue()
		if q == "" || q == "dwellm8-" {
			t.Errorf("%s has queue %q", op, q)
		}
		seen[q] = true
	}
	if len(seen) == 0 {
		t.Fatal("no queues at all, so this guard proves nothing")
	}
}

// ADR-0015's primary acceptance scenario, run through the SDK's own test
// environment: a payout fails after the fee is posted and before the transfer.
// The posting must be reversed, and the transfer must not have happened.
func TestAFailureBeforeTheTransferReversesTheFee(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()

	var order []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, a ActivityArgs) error { order = append(order, "read"); return nil },
		activity.RegisterOptions{Name: "read-account"})
	env.RegisterActivityWithOptions(
		func(_ context.Context, a ActivityArgs) error { order = append(order, "post-fee"); return nil },
		activity.RegisterOptions{Name: "post-fee"})
	env.RegisterActivityWithOptions(
		func(_ context.Context, a ActivityArgs) error { order = append(order, "reverse-fee"); return nil },
		activity.RegisterOptions{Name: "reverse-fee"})
	env.RegisterActivityWithOptions(
		func(_ context.Context, a ActivityArgs) error { order = append(order, "transfer"); return nil },
		activity.RegisterOptions{Name: "transfer"})
	// The step that fails: still before the transfer, so everything so far can
	// be undone and the world returns to where it started.
	env.RegisterActivityWithOptions(
		func(_ context.Context, a ActivityArgs) error {
			order = append(order, "reserve")
			return errors.New("the balance moved under us")
		},
		activity.RegisterOptions{Name: "reserve-funds"})
	env.RegisterActivityWithOptions(
		func(_ context.Context, a ActivityArgs) error { order = append(order, "release"); return nil },
		activity.RegisterOptions{Name: "release-funds"})

	def := Definition{
		Op: workflow.OpPayoutExecute,
		Build: func(Input) ([]StepDef, error) {
			return []StepDef{
				{Name: "read-account", Do: Call{Name: "read-account"}, ReadOnly: true},
				{Name: "post-fee", Do: Call{Name: "post-fee"}, Undo: &Call{Name: "reverse-fee", Retry: quick}},
				{Name: "reserve-funds", Do: Call{Name: "reserve-funds", Retry: quick},
					Undo: &Call{Name: "release-funds", Retry: quick}},
				{Name: "transfer", Do: Call{Name: "transfer", Retry: quick}},
			}, nil
		},
	}

	env.RegisterWorkflowWithOptions(build(def), sdkworkflow.RegisterOptions{Name: "payout.execute"})
	env.ExecuteWorkflow("payout.execute", Input{
		Op: workflow.OpPayoutExecute, Subject: "payout-1", TenantID: "t-1",
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("the workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the workflow errored instead of compensating: %v", err)
	}

	var res Report
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("reading the result: %v", err)
	}

	// The money never left, and the fee posting was reversed.
	if slices.Contains(order, "transfer") {
		t.Fatalf("the transfer ran after an earlier step failed: %v", order)
	}
	if !slices.Contains(order, "reverse-fee") {
		t.Fatalf("the fee was not reversed; activities ran in order %v", order)
	}
	// Reverse order: a later step's compensation runs before an earlier one's,
	// because the later effect may depend on the earlier.
	if i, j := slices.Index(order, "release"), slices.Index(order, "reverse-fee"); i > j {
		t.Errorf("compensations ran in declaration order rather than reverse: %v", order)
	}
	if res.Outcome != workflow.Compensated {
		t.Errorf("outcome = %q, want compensated", res.Outcome)
	}
	if res.FailedStep != "reserve-funds" {
		t.Errorf("failed step = %q, want reserve-funds", res.FailedStep)
	}
	if res.Err == "" {
		t.Error("the result carries no error text, so an incident has nothing to read")
	}
	if res.PastNoReturn {
		t.Error("a failure before the transfer was recorded as past the point of no return")
	}
}

// The other half of the boundary: when the irreversible step itself fails, the
// external effect may or may not have landed. Compensating on that assumption is
// what pays somebody twice, so the standard escalates and leaves the run visible.
func TestAFailedTransferEscalatesRatherThanCompensating(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()

	var order []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, a ActivityArgs) error { order = append(order, "post-fee"); return nil },
		activity.RegisterOptions{Name: "post-fee"})
	env.RegisterActivityWithOptions(
		func(_ context.Context, a ActivityArgs) error { order = append(order, "reverse-fee"); return nil },
		activity.RegisterOptions{Name: "reverse-fee"})
	env.RegisterActivityWithOptions(
		func(_ context.Context, a ActivityArgs) error {
			order = append(order, "transfer")
			return errors.New("the bank timed out")
		},
		activity.RegisterOptions{Name: "transfer"})

	def := Definition{
		Op: workflow.OpPayoutExecute,
		Build: func(Input) ([]StepDef, error) {
			return []StepDef{
				{Name: "post-fee", Do: Call{Name: "post-fee"}, Undo: &Call{Name: "reverse-fee", Retry: quick}},
				{Name: "transfer", Do: Call{Name: "transfer", Retry: quick}},
			}, nil
		},
	}

	env.RegisterWorkflowWithOptions(build(def), sdkworkflow.RegisterOptions{Name: "payout.execute"})
	env.ExecuteWorkflow("payout.execute", Input{
		Op: workflow.OpPayoutExecute, Subject: "payout-2", TenantID: "t-1",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("the workflow failed instead of escalating: %v", err)
	}
	var res Report
	if err := env.GetWorkflowResult(&res); err != nil {
		t.Fatalf("reading the result: %v", err)
	}

	if slices.Contains(order, "reverse-fee") {
		t.Errorf("the fee was reversed after a transfer that may have landed: %v", order)
	}
	if res.Outcome != workflow.Escalated {
		t.Errorf("outcome = %q, want escalated", res.Outcome)
	}
	if !res.PastNoReturn {
		t.Error("a failed transfer was not recorded as past the point of no return")
	}
	if !res.Escalated() {
		t.Error("Escalated() is false for an escalated run, so no alert would fire")
	}
}

// The key an activity receives is derived from the workflow and the step, and is
// the same on every retry. An activity that saw a different key each time would
// present a new request to the provider and debit the tenant twice.
func TestEveryAttemptCarriesTheSameIdempotencyKey(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()

	var keys []string
	var attempts int
	env.RegisterActivityWithOptions(
		func(_ context.Context, a ActivityArgs) error {
			keys = append(keys, a.IdempotencyKey)
			attempts++
			if attempts < 3 {
				return errors.New("the provider timed out")
			}
			return nil
		},
		activity.RegisterOptions{Name: "collect"})

	def := Definition{
		Op: workflow.OpAutopayDebit,
		Build: func(Input) ([]StepDef, error) {
			return []StepDef{{Name: "collect", Do: Call{Name: "collect",
				Retry: workflow.Retry{Initial: time.Millisecond, Coefficient: 1, Max: time.Millisecond, Attempts: 5}}}}, nil
		},
	}
	env.RegisterWorkflowWithOptions(build(def), sdkworkflow.RegisterOptions{Name: "collect.autopay_debit"})
	env.ExecuteWorkflow("collect.autopay_debit", Input{
		Op: workflow.OpAutopayDebit, Subject: "mandate-1", TenantID: "t-1",
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow: %v", err)
	}
	if len(keys) < 2 {
		t.Fatalf("the activity ran %d times, so nothing about retries was tested", len(keys))
	}
	for i, k := range keys {
		if k != keys[0] {
			t.Fatalf("attempt %d presented key %q, attempt 0 presented %q — a retry became a "+
				"second request", i, k, keys[0])
		}
	}
	want := "dwellm8:collect.autopay_debit:mandate-1#collect"
	if keys[0] != want {
		t.Errorf("key = %q, want %q", keys[0], want)
	}
}

// A saga built the way the adapter builds one must satisfy the standard: the
// step list is refused if a compensable step follows the point of no return.
// Testing it here keeps the adapter honest about the order it declares things in.
func TestTheStandardStillRefusesACompensationAfterTheBoundary(t *testing.T) {
	s, err := workflow.New(workflow.OpPayoutExecute, "payout-1")
	if err != nil {
		t.Fatalf("new saga: %v", err)
	}
	noop := func(context.Context, string) error { return nil }

	s.Step(workflow.Step{Name: "post-fee", Do: noop, Undo: noop})
	s.PointOfNoReturn()
	s.Step(workflow.Step{Name: "transfer", Do: noop, Undo: noop})

	if _, err := s.Run(context.Background()); err == nil {
		t.Fatal("the standard accepted a compensation after the point of no return")
	}
}
