package temporalx

import (
	"fmt"
	"log/slog"

	sdkactivity "go.temporal.io/sdk/activity"
	sdkclient "go.temporal.io/sdk/client"
	sdkworker "go.temporal.io/sdk/worker"
	sdkworkflow "go.temporal.io/sdk/workflow"

	"github.com/tesserix/dwellm8/services/api/internal/platform/workflow"
)

// Activity is one registered activity: the name a workflow calls it by, and the
// function that does the work.
//
// The function takes the arguments it declares and must present ActivityArgs's
// idempotency key to whatever it calls. It may be retried at any point, and the
// key is what makes the retry the same request rather than a second one.
type Activity struct {
	Name string
	Fn   any
}

// Workers serves every task queue the registered operations need.
//
// One worker per domain rather than one for everything, because the task queue
// is the unit of isolation: a payout backlog must not stop a mandate being
// created, and a poison workflow in one domain must not starve the others.
type Workers struct {
	client     sdkclient.Client
	log        *slog.Logger
	byQueue    map[string]sdkworker.Worker
	activities []Activity
}

func NewWorkers(c sdkclient.Client, log *slog.Logger) *Workers {
	return &Workers{client: c, log: log, byQueue: map[string]sdkworker.Worker{}}
}

// AddActivity registers an activity on every queue built after it. Activities
// are shared: a ledger posting is the same code whichever domain needs it.
func (w *Workers) AddActivity(a Activity) error {
	if a.Name == "" || a.Fn == nil {
		return fmt.Errorf("temporalx: an activity needs a name and a function, got %q", a.Name)
	}
	w.activities = append(w.activities, a)
	return nil
}

// Mount registers every definition in the registry on its operation's queue.
func (w *Workers) Mount(r *Registry) error {
	for _, d := range r.Definitions() {
		spec, ok := workflow.Lookup(d.Op)
		if !ok {
			return fmt.Errorf("temporalx: %q is not on the durable list", d.Op)
		}
		queue := spec.TaskQueue()

		wk, exists := w.byQueue[queue]
		if !exists {
			wk = sdkworker.New(w.client, queue, sdkworker.Options{})
			for _, a := range w.activities {
				wk.RegisterActivityWithOptions(a.Fn, sdkactivity.RegisterOptions{Name: a.Name})
			}
			w.byQueue[queue] = wk
		}

		wk.RegisterWorkflowWithOptions(build(d), sdkworkflow.RegisterOptions{Name: WorkflowName(d.Op)})
		w.log.Info("workflow registered", "operation", d.Op, "queue", queue)
	}
	return nil
}

// Start runs every worker. It returns once they are all running; Stop ends them.
func (w *Workers) Start() error {
	if len(w.byQueue) == 0 {
		// A worker process serving nothing is one that looks healthy and does no
		// work, which is the failure mode ADR-0015 §11 refuses to ship.
		return fmt.Errorf("temporalx: no task queues to serve — nothing was registered")
	}
	for queue, wk := range w.byQueue {
		if err := wk.Start(); err != nil {
			return fmt.Errorf("temporalx: starting the worker for %s: %w", queue, err)
		}
	}
	return nil
}

// Stop drains the workers, letting activities in flight finish.
func (w *Workers) Stop() {
	for _, wk := range w.byQueue {
		wk.Stop()
	}
}

// Queues reports what this process serves, for a log line at boot that an
// operator can compare against what they expected to deploy.
func (w *Workers) Queues() []string {
	out := make([]string, 0, len(w.byQueue))
	for q := range w.byQueue {
		out = append(out, q)
	}
	return out
}
