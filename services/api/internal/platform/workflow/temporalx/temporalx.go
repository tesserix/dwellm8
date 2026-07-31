// Package temporalx is the only place the Temporal SDK is imported. ADR-0015 §1.
//
// Everything about which order steps run in, what may still be undone and who is
// told lives in internal/platform/workflow, where it is testable without a
// cluster. This package converts that standard into SDK calls and nothing more:
// a saga's Step becomes an ExecuteActivity, a Retry becomes a RetryPolicy, and a
// Spec's task queue becomes a worker.
package temporalx

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	sdkclient "go.temporal.io/sdk/client"
	sdktemporal "go.temporal.io/sdk/temporal"
	sdkworkflow "go.temporal.io/sdk/workflow"

	"github.com/tesserix/dwellm8/services/api/internal/platform/workflow"
)

// Dial connects to Temporal. It refuses a configuration that would let a money
// operation run anywhere but a workflow — see workflow.Config.Validate.
func Dial(cfg workflow.Config, log *slog.Logger) (sdkclient.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	opts := sdkclient.Options{
		HostPort:  cfg.HostPort,
		Namespace: cfg.Namespace,
		Logger:    newLogger(log),
	}
	if cfg.TLS {
		opts.ConnectionOptions = sdkclient.ConnectionOptions{TLS: &tls.Config{MinVersion: tls.VersionTLS12}}
	}
	c, err := sdkclient.Dial(opts)
	if err != nil {
		return nil, fmt.Errorf("temporalx: dialling %s: %w", cfg.HostPort, err)
	}
	return c, nil
}

// retryPolicy converts a policy expressed as data into the SDK's type.
//
// Attempts of zero stays zero, which the SDK reads as unlimited — the standard's
// deliberate default for a money call, where the question is how long rather
// than how many times.
func retryPolicy(r workflow.Retry) *sdktemporal.RetryPolicy {
	return &sdktemporal.RetryPolicy{
		InitialInterval:    r.Initial,
		BackoffCoefficient: r.Coefficient,
		MaximumInterval:    r.Max,
		MaximumAttempts:    int32(r.Attempts),
	}
}

// Call is one activity invocation: its registered name, how patient to be, and
// the arguments, which must be deterministic — they are recomputed on replay.
type Call struct {
	Name    string
	Retry   workflow.Retry
	Timeout time.Duration
	Args    any
}

// StepDef is a saga step expressed as activities rather than closures, so the
// declaration can be built inside a workflow and survive a replay.
type StepDef struct {
	Name string
	Do   Call
	// Undo is nil for a step with nothing to reverse. The standard refuses a nil
	// before the point of no return, so this is checked there rather than here.
	Undo     *Call
	ReadOnly bool
}

// Input is what starts a durable operation. It is the workflow's only argument,
// so everything a replay needs must be in it.
type Input struct {
	Op       workflow.Operation `json:"op"`
	Subject  string             `json:"subject"`
	TenantID string             `json:"tenant_id"`
	// Params is the operation's own payload, opaque to this package.
	Params map[string]any `json:"params,omitempty"`
}

// Definition builds one operation's steps. It must be a pure function of its
// input: it runs again on every replay, and a step list that differs between
// runs is the non-determinism ADR-0015 §3 exists to prevent.
type Definition struct {
	Op    workflow.Operation
	Build func(in Input) ([]StepDef, error)
}

// Registry holds the definitions a worker serves.
type Registry struct {
	defs map[workflow.Operation]Definition
}

func NewRegistry() *Registry {
	return &Registry{defs: map[workflow.Operation]Definition{}}
}

// Register adds a definition, refusing an operation the standard does not list.
// A workflow for an operation nobody declared durable is one with no timeout, no
// escalation and no place in the catalogue a support call reads.
func (r *Registry) Register(d Definition) error {
	if _, ok := workflow.Lookup(d.Op); !ok {
		return fmt.Errorf("temporalx: %q is not on the durable list, so it has no spec: %w",
			d.Op, workflow.ErrNotDurable)
	}
	if d.Build == nil {
		return fmt.Errorf("temporalx: %s has no step builder", d.Op)
	}
	if _, taken := r.defs[d.Op]; taken {
		return fmt.Errorf("temporalx: %s is registered twice", d.Op)
	}
	r.defs[d.Op] = d
	return nil
}

// Definitions returns everything registered, for the worker to mount.
func (r *Registry) Definitions() []Definition {
	out := make([]Definition, 0, len(r.defs))
	for _, d := range r.defs {
		out = append(out, d)
	}
	return out
}

// WorkflowName is the name an operation is registered under. It is the operation
// itself, so a workflow found in the UI names the thing it is doing.
func WorkflowName(op workflow.Operation) string { return string(op) }

// build turns a Definition into the SDK workflow function.
//
// The shell is deliberately thin, and everything it does is either an SDK call
// or a call into the standard. Nothing here reads a clock, a random number or
// the outside world: on replay the same input must produce the same step list,
// or a compensation runs against a world that never had its step.
func build(d Definition) func(sdkworkflow.Context, Input) (Report, error) {
	return func(ctx sdkworkflow.Context, in Input) (Report, error) {
		spec, ok := workflow.Lookup(in.Op)
		if !ok {
			return Report{}, fmt.Errorf("temporalx: %q is not on the durable list", in.Op)
		}

		steps, err := d.Build(in)
		if err != nil {
			return Report{}, err
		}

		saga, err := workflow.New(in.Op, in.Subject)
		if err != nil {
			return Report{}, err
		}

		// The boundary is declared before the first step that cannot be undone,
		// which is what Saga.PointOfNoReturn means: everything already declared
		// is compensable, everything after it is retried until it succeeds.
		declared := false
		for _, sd := range steps {
			if spec.HasPointOfNoReturn && !declared && sd.Undo == nil && !sd.ReadOnly {
				saga.PointOfNoReturn()
				declared = true
			}

			step := workflow.Step{Name: sd.Name, ReadOnly: sd.ReadOnly}
			step.Do = activityStep(ctx, sd.Do)
			if sd.Undo != nil {
				// A compensation is retried far more patiently than the step it
				// undoes: money moved and the record uncorrected is the one state
				// nothing downstream can reason about.
				undo := *sd.Undo
				if undo.Retry == (workflow.Retry{}) {
					undo.Retry = workflow.RetryCompensation
				}
				step.Undo = workflow.CompensateFunc(activityStep(ctx, undo))
			}
			saga.Step(step)
		}

		// The operation's whole budget is the execution timeout set at Start, so
		// nothing is imposed here: a run that exceeds it escalates rather than
		// failing, because a workflow that fails while money may be in flight
		// destroys the only record that it is.
		res, err := saga.Run(sdkContext{ctx})
		if err != nil {
			return Report{}, err
		}
		return reportOf(res), nil
	}
}

// activityStep is the conversion the whole adapter exists for: a step becomes
// one ExecuteActivity, carrying the idempotency key the standard derived.
//
// The key is passed rather than generated inside the activity. An activity that
// mints its own key produces a different one on every retry, and the provider
// sees two requests where the workflow intended one.
func activityStep(ctx sdkworkflow.Context, c Call) workflow.StepFunc {
	return func(_ context.Context, key string) error {
		timeout := c.Timeout
		if timeout <= 0 {
			timeout = 2 * time.Minute
		}
		actx := sdkworkflow.WithActivityOptions(ctx, sdkworkflow.ActivityOptions{
			StartToCloseTimeout: timeout,
			RetryPolicy:         retryPolicy(c.Retry),
		})
		return sdkworkflow.ExecuteActivity(actx, c.Name, ActivityArgs{
			IdempotencyKey: key,
			Args:           c.Args,
		}).Get(actx, nil)
	}
}

// ActivityArgs is what every activity receives. The key is first because it is
// the field an activity may never invent.
type ActivityArgs struct {
	IdempotencyKey string `json:"idempotency_key"`
	Args           any    `json:"args,omitempty"`
}

// sdkContext adapts a workflow context to context.Context so the standard's
// Step signature — which knows nothing about the SDK — can be honoured.
//
// The values are the workflow's; the deadline is not, because a workflow's
// timeout is enforced by the server rather than by a Go deadline, and a
// cancellable context here would let a compensation be cut short.
type sdkContext struct{ ctx sdkworkflow.Context }

func (s sdkContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (s sdkContext) Done() <-chan struct{}       { return nil }
func (s sdkContext) Err() error                  { return nil }
func (s sdkContext) Value(key any) any           { return s.ctx.Value(key) }

// Start begins a durable operation, or returns the run already in flight.
//
// Starting the same operation for the same subject twice is a no-op rather than
// a second workflow: the id is derived, Temporal rejects a duplicate, and a
// double-tapped button is therefore safe without any deduplication of our own.
func Start(ctx context.Context, c sdkclient.Client, in Input) (sdkclient.WorkflowRun, error) {
	spec, ok := workflow.Lookup(in.Op)
	if !ok {
		return nil, fmt.Errorf("temporalx: %q is not on the durable list: %w", in.Op, workflow.ErrNotDurable)
	}
	id, err := workflow.ID(in.Op, in.Subject)
	if err != nil {
		return nil, err
	}

	run, err := c.ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{
		ID:        id,
		TaskQueue: spec.TaskQueue(),
		// The operation's whole budget, after which the standard escalates.
		WorkflowExecutionTimeout: spec.Timeout,
		// A duplicate id finds the running one rather than starting a second, and
		// a completed one may not be re-run: a payout that succeeded must not be
		// startable again by the same button.
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	}, WorkflowName(in.Op), in)
	if err != nil {
		return nil, fmt.Errorf("temporalx: starting %s for %s: %w", in.Op, in.Subject, err)
	}
	return run, nil
}
