package workflow_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/workflow"
)

// The list, and whether it was derived from the rule or collected by memory.
//
// A durable-operation list is the kind of thing that is right the day it is written
// and wrong six months later, in both directions: an operation that should be a
// workflow and is a handler, or a handler that got wrapped in a workflow because
// workflows felt safer. Neither shows up as a failure. These tests are what make
// the list an artefact rather than a note.

func TestEveryOperationSaysWhichHalfOfTheRulePutsItOnTheList(t *testing.T) {
	ops := workflow.Operations()
	if len(ops) == 0 {
		t.Fatal("no durable operations — ADR-0015's whole content is this list")
	}
	for _, op := range ops {
		spec, ok := workflow.Lookup(op)
		if !ok {
			t.Fatalf("%s is listed and has no spec", op)
		}
		if err := spec.Validate(); err != nil {
			t.Errorf("%s: %v", op, err)
		}
		// The clause has to be a sentence, not a label. "multi-system" is a
		// category; what a reader needs is what breaks.
		if len(spec.Because) < 40 {
			t.Errorf("%s is on the list because %q, which is a label rather than a failure",
				op, spec.Because)
		}
	}
}

// The story's own list, checked against ours. It names mandate creation, debits,
// payouts, refunds, stamping and eSign, and every one of them must be here — a
// standard that quietly omitted one of the operations it was written for would be
// worse than no standard, because it would look complete.
func TestTheOperationsTheStoryNamesAreAllOnTheList(t *testing.T) {
	for _, op := range []workflow.Operation{
		workflow.OpMandateCreate,
		workflow.OpAutopayDebit,
		workflow.OpPayoutExecute,
		workflow.OpRefundIssue,
		workflow.OpAgreementStamp,
		workflow.OpAgreementESign,
	} {
		if _, ok := workflow.Lookup(op); !ok {
			t.Errorf("%s is named in the story and is not on the durable list", op)
		}
	}
}

// Every operation resolves to a task queue, and every domain has exactly one. A
// worker fleet is configured per queue, so a queue nobody can enumerate is a queue
// nobody deploys a worker for — and the symptom is a workflow that stays queued
// with nothing anywhere reporting an error.
func TestEveryDomainIsOneTaskQueueAndEveryQueueIsEnumerable(t *testing.T) {
	queues := map[string][]workflow.Operation{}
	for _, op := range workflow.Operations() {
		spec, _ := workflow.Lookup(op)
		q := spec.TaskQueue()
		if !strings.HasPrefix(q, "dwellm8-") {
			t.Errorf("%s serves queue %q, which is not this product's", op, q)
		}
		queues[q] = append(queues[q], op)
	}
	if len(queues) != len(workflow.Domains()) {
		t.Errorf("%d queues for %d domains", len(queues), len(workflow.Domains()))
	}
	for _, d := range workflow.Domains() {
		if _, ok := queues["dwellm8-"+string(d)]; !ok {
			t.Errorf("domain %s has no queue, so its operations have no worker", d)
		}
	}
	for q, ops := range queues {
		t.Logf("%s: %v", q, ops)
	}
}

// The workflow id is the support call's entry point. It has to be constructible
// from an operation and a subject id and nothing else, because the alternative is a
// lookup table and a lookup table can be missing exactly when somebody needs it.
func TestTheWorkflowIDIsDeterministicAndDerivedFromTheSubject(t *testing.T) {
	const subject = "7c9e6679-7425-40de-944b-e07fc1f90ae7"

	id, err := workflow.ID(workflow.OpPayoutExecute, subject)
	if err != nil {
		t.Fatalf("ID: %v", err)
	}
	if !strings.Contains(id, subject) {
		t.Errorf("the workflow id %q does not contain the payout id, so an agent holding one "+
			"cannot construct it", id)
	}
	for range 50 {
		again, _ := workflow.ID(workflow.OpPayoutExecute, subject)
		if again != id {
			t.Fatalf("two calls produced %q and %q — a second start would be a second workflow "+
				"rather than a no-op", id, again)
		}
	}
	// Two operations on the same subject are different workflows: a payout and a
	// refund of the same payment must not collide.
	refund, _ := workflow.ID(workflow.OpRefundIssue, subject)
	if refund == id {
		t.Error("a payout and a refund of the same subject share a workflow id, so the second would " +
			"be silently dropped as a duplicate")
	}

	for _, tc := range []struct{ name, op, subject string }{
		{"an operation that is not durable", "payout.invent", subject},
		{"no subject, so every run of the operation collides", string(workflow.OpPayoutExecute), ""},
		{"a subject carrying the separator", string(workflow.OpPayoutExecute), "a:b"},
	} {
		if _, err := workflow.ID(workflow.Operation(tc.op), tc.subject); err == nil {
			t.Errorf("accepted: %s", tc.name)
		}
	}
	if _, err := workflow.ID("payout.invent", subject); !errors.Is(err, workflow.ErrNotDurable) {
		t.Error("an unlisted operation is not reported as not-durable, so a caller cannot tell it apart " +
			"from a bad subject")
	}
	t.Logf("id: %s", id)
}

// Escalation must happen inside the operation's own budget, or it escalates to
// nobody: by the time the deadline passes the workflow is already over and there is
// nothing left to look at.
func TestAnOperationEscalatesBeforeItGivesUp(t *testing.T) {
	for _, op := range workflow.Operations() {
		spec, _ := workflow.Lookup(op)
		if spec.Escalate >= spec.Timeout {
			t.Errorf("%s escalates after %s and gives up after %s", op, spec.Escalate, spec.Timeout)
		}
		// A money operation that can wait more than two days before telling anybody
		// is an operation whose failure is discovered by the customer. Documents are
		// exempt: an eSign genuinely waits on people.
		if spec.Domain != workflow.DomainDocument && spec.Escalate > 24*time.Hour {
			t.Errorf("%s waits %s before telling anybody, and it moves money", op, spec.Escalate)
		}
	}
}

// The two spec combinations that are contradictions. Both read perfectly well.
func TestASpecCannotContradictItself(t *testing.T) {
	for _, tc := range []struct {
		name string
		spec workflow.Spec
		want string
	}{
		{
			name: "an operation that does not say why it is on the list",
			spec: workflow.Spec{Op: "payout.execute", Domain: workflow.DomainPayout,
				Timeout: time.Hour, Escalate: time.Minute},
			want: "no clause of the rule",
		},
		{
			name: "an operation whose name does not match its domain",
			spec: workflow.Spec{Op: "payout.execute", Domain: workflow.DomainRefund,
				Because: strings.Repeat("a reason long enough to be a sentence. ", 2),
				Timeout: time.Hour, Escalate: time.Minute},
			want: "does not say so",
		},
		{
			name: "an operation that can wait forever without telling anybody",
			spec: workflow.Spec{Op: "payout.execute", Domain: workflow.DomainPayout,
				Because: strings.Repeat("a reason long enough to be a sentence. ", 2),
				Timeout: time.Hour},
			want: "never tell anybody",
		},
		{
			name: "an operation that escalates after it has already given up",
			spec: workflow.Spec{Op: "payout.execute", Domain: workflow.DomainPayout,
				Because:  strings.Repeat("a reason long enough to be a sentence. ", 2),
				Timeout:  time.Hour,
				Escalate: 2 * time.Hour},
			want: "never escalates",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.spec.Validate()
			if err == nil {
				t.Fatalf("accepted: %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A process that serves a money workflow refuses to start without Temporal.
//
// This is the deliberate divergence from the pattern in production for HomeChef,
// where an absent Temporal falls back to inline execution. For a notification that
// is the right trade. For a payout, "Temporal was not configured so we did it
// inline" is a payout with no compensation and no record — and nothing errors, so
// it is indistinguishable from a healthy deploy.
func TestAMissingTemporalIsAStartupFailureRatherThanAFallback(t *testing.T) {
	good := workflow.Config{HostPort: "temporal.dwellm8.svc.cluster.local:7233", Namespace: "dwellm8"}
	if err := good.Validate(); err != nil {
		t.Fatalf("a valid configuration was refused: %v", err)
	}

	for _, tc := range []struct {
		name string
		cfg  workflow.Config
	}{
		{"no host, which would otherwise mean inline execution", workflow.Config{Namespace: "dwellm8"}},
		{"no namespace", workflow.Config{HostPort: "temporal:7233"}},
		{"another product's namespace", workflow.Config{HostPort: "temporal:7233", Namespace: "homechef"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if err == nil {
				t.Fatalf("accepted: %s", tc.name)
			}
			if !errors.Is(err, workflow.ErrNotConfigured) {
				t.Errorf("the error is not distinguishable as a configuration failure: %v", err)
			}
			t.Logf("refused: %v", err)
		})
	}
}

// The retry tiers, and the one property that distinguishes them. Compensation is
// the most patient because a failed compensation is the worst state in the system;
// an internal call is the least because if the database is still refusing after a
// few seconds the problem is not transient.
func TestTheRetryTiersArePatientInTheRightOrder(t *testing.T) {
	tiers := []struct {
		name string
		r    workflow.Retry
	}{
		{"internal", workflow.RetryInternal},
		{"provider", workflow.RetryProvider},
		{"compensation", workflow.RetryCompensation},
	}
	for i, tc := range tiers {
		if tc.r.Initial <= 0 || tc.r.Max <= 0 {
			t.Errorf("%s has no intervals", tc.name)
		}
		if tc.r.Coefficient <= 1 {
			t.Errorf("%s does not back off (coefficient %v), so a struggling dependency is hammered",
				tc.name, tc.r.Coefficient)
		}
		// No attempt cap anywhere: the question for a money call is never "how many
		// times" but "for how long", and the window is the activity's.
		if tc.r.Attempts != 0 {
			t.Errorf("%s caps attempts at %d — a cap turns a slow provider into a lost payment",
				tc.name, tc.r.Attempts)
		}
		if i > 0 && tc.r.Max < tiers[i-1].r.Max {
			t.Errorf("%s is less patient than %s", tc.name, tiers[i-1].name)
		}
	}
}
