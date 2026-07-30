// Package workflow is the durable-operation standard. ADR-0015.
//
// It answers three questions and deliberately does not answer a fourth. Which
// operations must be durable, how one is written, and how it is found during a
// support call — but not how Temporal is called. Nothing in this package imports
// the Temporal SDK, and an arch test in internal/platform/arch keeps it that way:
// the SDK is confined to one adapter package, for the same reason ADR-0011 confines
// Razorpay to internal/money/provider.
//
// That is not tidiness. The parts of this standard that can actually go wrong —
// the order compensations run in, what may still be undone, and where an
// idempotency key is computed — are pure logic, and pure logic can be tested
// without a cluster. A standard whose only test needs a Temporal test server is a
// standard nobody runs on a laptop.
//
// # The rule that generates the list
//
// An operation must be a durable workflow when it changes state in more than one
// system and a partial completion is not self-healing.
//
// Both halves matter. Multi-system alone is not enough: an invoice writes a row
// and an outbox row in one transaction, so a partial completion cannot exist.
// Not-self-healing alone is not enough either: a webhook delivery that fails is
// redelivered by the provider. It is the conjunction that produces the failure
// this ADR exists to prevent — tenant debited, owner not credited, nobody
// notified — and Operations() below is derived from the rule rather than
// collected by memory, with the clause that puts each one there written down.
//
// The negative half of the rule is load-bearing and is the mistake teams make in
// the month after adopting Temporal: wrapping a single-system transaction in a
// workflow adds a distributed failure mode to an operation that had none. See
// ADR-0015's alternative D.
package workflow

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Domain is the money or document area an operation belongs to. It fixes the task
// queue, so it is a closed set rather than a string: a typo in a task queue name
// produces a worker that is running, healthy, and subscribed to nothing.
type Domain string

const (
	DomainMandate  Domain = "mandate"
	DomainCollect  Domain = "collect"
	DomainPayout   Domain = "payout"
	DomainRefund   Domain = "refund"
	DomainDocument Domain = "document"
	DomainRecon    Domain = "recon"
)

// Operation is one durable operation. The name is stable forever: it is half of
// the workflow id, and a workflow id that changes is a workflow that cannot be
// found during the support call it was designed for.
type Operation string

const (
	// OpMandateCreate registers a UPI Autopay mandate: an order at the provider,
	// the payer's approval, and a mandate row here.
	OpMandateCreate Operation = "mandate.create"
	// OpMandateAmend changes a live mandate's amount or validity.
	OpMandateAmend Operation = "mandate.amend"
	// OpMandateRevoke ends one. Revocation that half-happened leaves a mandate
	// this system thinks is dead and the provider will still debit against.
	OpMandateRevoke Operation = "mandate.revoke"

	// OpAutopayDebit is a debit under a standing mandate, with no payer present to
	// see anything go wrong.
	OpAutopayDebit Operation = "collect.autopay_debit"

	// OpPayoutExecute disburses what an owner is owed: the fee, the ledger, and a
	// bank transfer. ADR-0015's primary acceptance scenario.
	OpPayoutExecute Operation = "payout.execute"

	// OpRefundIssue returns money to a payer.
	OpRefundIssue Operation = "refund.issue"
	// OpDepositRefund returns a security deposit at the end of a lease, net of
	// agreed deductions that are their own charges.
	OpDepositRefund Operation = "refund.deposit"
	// OpChargebackHandle answers a chargeback: the provider's process, the ledger,
	// and somebody being told.
	OpChargebackHandle Operation = "refund.chargeback"

	// OpAgreementStamp pays stamp duty and attaches the certificate.
	OpAgreementStamp Operation = "document.stamp"
	// OpAgreementESign runs an eSign ceremony across two or more signatories.
	OpAgreementESign Operation = "document.esign"

	// OpReconcileDay is ADR-0012's nightly run. It is here because ADR-0012
	// deferred the retry and backoff policy to this ADR, and because a day that
	// silently never ran is the failure that ADR's watchdog exists to catch.
	OpReconcileDay Operation = "recon.day"
)

// Spec is everything the standard fixes about one operation.
type Spec struct {
	Op     Operation
	Domain Domain

	// Because is the clause of the rule that puts this operation on the list. It
	// is a required field, so an operation cannot be added without saying which
	// half of the rule it satisfies — the discipline that stops the list growing
	// into "everything we felt nervous about".
	Because string

	// Compensable reports whether the operation has a reversible prologue at all.
	// False means every step is retry-until-success and there is nothing to undo.
	Compensable bool

	// HasPointOfNoReturn reports whether the operation contains an irreversible
	// external step. Everything before it may be compensated; nothing after it
	// can be, so everything after it must be retried until it succeeds.
	HasPointOfNoReturn bool

	// PlatformWide reports whether the operation belongs to the platform rather
	// than to one organisation. Such a run still carries an organisation — the
	// platform organisation, exactly as ADR-0002 §1 requires of a platform-level
	// event — so workflow_runs.tenant_id can stay NOT NULL and the table can keep
	// the ordinary strict policy instead of becoming a fifth nullable-tenant
	// table.
	PlatformWide bool

	// Timeout is the whole operation's budget. Beyond it the operation is not
	// failed — see Escalate.
	Timeout time.Duration

	// Escalate is how long a step may wait on something outside this system — a
	// provider callback, a signatory, a human — before an operator is told. It is
	// deliberately not a failure deadline: a workflow that fails while money may
	// be in flight destroys the only record that it is.
	Escalate time.Duration
}

var specs = map[Operation]Spec{
	OpMandateCreate: {
		Op: OpMandateCreate, Domain: DomainMandate,
		Because: "an order at the provider, an approval by the payer, and a row here; " +
			"a half-created mandate is one the provider will honour and this system will not bill against",
		Compensable: true, HasPointOfNoReturn: false,
		Timeout: 7 * 24 * time.Hour, Escalate: 24 * time.Hour,
	},
	OpMandateAmend: {
		Op: OpMandateAmend, Domain: DomainMandate,
		Because: "the provider's mandate and ours must agree on the amount; disagreeing means " +
			"debiting a tenant for a figure nobody approved",
		Compensable: true, HasPointOfNoReturn: false,
		Timeout: 7 * 24 * time.Hour, Escalate: 24 * time.Hour,
	},
	OpMandateRevoke: {
		Op: OpMandateRevoke, Domain: DomainMandate,
		Because: "revocation that half-happened leaves a mandate this system thinks is dead " +
			"and the provider will still debit against",
		Compensable: false, HasPointOfNoReturn: true,
		Timeout: 7 * 24 * time.Hour, Escalate: 4 * time.Hour,
	},
	OpAutopayDebit: {
		Op: OpAutopayDebit, Domain: DomainCollect,
		Because: "a debit at the provider, a posting here and a receipt to the tenant, with no payer " +
			"present to notice that the last two did not happen",
		Compensable: true, HasPointOfNoReturn: true,
		Timeout: 3 * 24 * time.Hour, Escalate: 6 * time.Hour,
	},
	OpPayoutExecute: {
		Op: OpPayoutExecute, Domain: DomainPayout,
		Because: "the fee, the ledger and a bank transfer; failing between them is the exact shape of " +
			"tenant debited, owner not credited, nobody notified",
		Compensable: true, HasPointOfNoReturn: true,
		Timeout: 3 * 24 * time.Hour, Escalate: 2 * time.Hour,
	},
	OpRefundIssue: {
		Op: OpRefundIssue, Domain: DomainRefund,
		Because: "the provider's refund and the reversing entry; a refund the ledger does not know " +
			"about is money gone with no explanation on any statement",
		Compensable: true, HasPointOfNoReturn: true,
		Timeout: 3 * 24 * time.Hour, Escalate: 4 * time.Hour,
	},
	OpDepositRefund: {
		Op: OpDepositRefund, Domain: DomainRefund,
		Because: "releasing a deposit liability and moving money out of the bank, at the moment a " +
			"tenant is least willing to be told to wait",
		Compensable: true, HasPointOfNoReturn: true,
		Timeout: 7 * 24 * time.Hour, Escalate: 4 * time.Hour,
	},
	OpChargebackHandle: {
		Op: OpChargebackHandle, Domain: DomainRefund,
		Because: "the provider's dispute process, a reversing entry and a deadline nobody controls; " +
			"missing the deadline loses the money by default",
		Compensable: false, HasPointOfNoReturn: true,
		Timeout: 30 * 24 * time.Hour, Escalate: 12 * time.Hour,
	},
	OpAgreementStamp: {
		Op: OpAgreementStamp, Domain: DomainDocument,
		Because: "stamp duty is paid to a government gateway and the certificate is attached here; " +
			"duty paid with no certificate stored is money spent and an unstamped agreement",
		Compensable: false, HasPointOfNoReturn: true,
		Timeout: 7 * 24 * time.Hour, Escalate: 4 * time.Hour,
	},
	OpAgreementESign: {
		Op: OpAgreementESign, Domain: DomainDocument,
		Because: "a ceremony across two or more signatories over days, where a lost half-signed " +
			"envelope has to be started again by people who already signed",
		Compensable: true, HasPointOfNoReturn: false,
		Timeout: 30 * 24 * time.Hour, Escalate: 48 * time.Hour,
	},
	OpReconcileDay: {
		Op: OpReconcileDay, Domain: DomainRecon, PlatformWide: true,
		Because: "ADR-0012 deferred its retry and backoff here, and a day that silently never ran " +
			"is what that ADR's watchdog exists to catch",
		Compensable: false, HasPointOfNoReturn: false,
		Timeout: 12 * time.Hour, Escalate: 3 * time.Hour,
	},
}

// Lookup returns the spec, or false. An operation not in this map is not a
// durable operation, and starting a workflow for one is a programming error
// rather than a configuration choice.
func Lookup(op Operation) (Spec, bool) {
	s, ok := specs[op]
	return s, ok
}

// Operations returns every durable operation, ordered.
func Operations() []Operation {
	out := make([]Operation, 0, len(specs))
	for op := range specs {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Domains returns every domain that has at least one operation, ordered. It is
// the set of task queues a worker fleet must cover, so a domain with no worker is
// findable rather than discovered by a stuck workflow.
func Domains() []Domain {
	seen := map[Domain]bool{}
	for _, s := range specs {
		seen[s.Domain] = true
	}
	out := make([]Domain, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Validate asserts a spec is coherent. Called for every entry by the contract
// test, because two of these combinations are contradictions that would read
// perfectly well in review.
func (s Spec) Validate() error {
	if s.Op == "" || s.Domain == "" {
		return errors.New("workflow: an operation must name itself and its domain")
	}
	if strings.TrimSpace(s.Because) == "" {
		return fmt.Errorf("workflow: %s is on the durable list with no clause of the rule behind it — "+
			"a list without reasons is a list that grows", s.Op)
	}
	// The prefix is not decoration: it is what makes the workflow id readable to a
	// support agent holding a payment id and nothing else.
	if !strings.HasPrefix(string(s.Op), string(s.Domain)+".") {
		return fmt.Errorf("workflow: operation %q is in domain %q and does not say so", s.Op, s.Domain)
	}
	if s.Timeout <= 0 {
		return fmt.Errorf("workflow: %s has no time budget", s.Op)
	}
	if s.Escalate <= 0 {
		return fmt.Errorf("workflow: %s can wait on the outside world and never tell anybody", s.Op)
	}
	// An operation that escalates only after its own budget has expired escalates
	// to nobody: the workflow is already over.
	if s.Escalate >= s.Timeout {
		return fmt.Errorf("workflow: %s escalates after %s and gives up after %s, so it never escalates",
			s.Op, s.Escalate, s.Timeout)
	}
	return nil
}

// TaskQueue is where this operation's workflows and activities are served.
//
// `dwellm8-<domain>`, matching the convention already in production for HomeChef
// (`homechef-orders`, `homechef-payouts`). One queue per domain rather than one
// per operation: a queue is a worker-fleet boundary, and eleven fleets to serve
// eleven operations is eleven things to watch for no isolation anybody wanted.
func (s Spec) TaskQueue() string { return "dwellm8-" + string(s.Domain) }

// ErrNotDurable is what a caller gets for an operation that is not on the list.
var ErrNotDurable = errors.New("workflow: not a durable operation")

// ID is the workflow id: `dwellm8:<operation>:<subject>`.
//
// Deterministic, and derived from the domain entity rather than generated. Two
// things follow, and both are the point:
//
// Starting the same operation for the same subject twice is a no-op rather than a
// second workflow, because Temporal rejects a duplicate id. That makes a
// producer-side retry — a double-tapped button, a redelivered event, a replayed
// queue — safe without any deduplication of our own. It is the same guarantee
// ADR-0011 §2 gets from a unique index, obtained the same way: by making the
// second attempt collide rather than by remembering the first.
//
// And a support agent holding a payout id can construct the workflow id without
// searching for it. A generated id would mean a lookup table, and a lookup table
// is a thing that can be missing exactly when somebody needs it.
func ID(op Operation, subject string) (string, error) {
	if _, ok := specs[op]; !ok {
		return "", fmt.Errorf("%w: %q", ErrNotDurable, op)
	}
	if subject == "" {
		return "", fmt.Errorf("workflow: %s needs the id of what it is operating on, or two of them "+
			"collide and the second is silently dropped", op)
	}
	if strings.ContainsAny(subject, ": ") {
		return "", fmt.Errorf("workflow: subject %q contains a separator, which would make two "+
			"different operations produce the same workflow id", subject)
	}
	return fmt.Sprintf("dwellm8:%s:%s", op, subject), nil
}

// Retry is a retry policy, as data. The SDK's own type is not used here because
// this package does not import the SDK; the adapter converts.
type Retry struct {
	Initial     time.Duration
	Coefficient float64
	Max         time.Duration
	// Attempts of zero means "no cap" — bounded by the activity's
	// schedule-to-close window instead. That is the right default for a money
	// call: the question is never "how many times" but "for how long".
	Attempts int
}

// The three tiers. There are three rather than one because the cost of retrying
// differs by three orders of magnitude between them, and rather than a dozen
// because a policy per activity is a policy nobody can compare.
var (
	// RetryInternal is for a call to our own database. Fast, and if it is still
	// failing after a minute the problem is not transient.
	RetryInternal = Retry{Initial: 100 * time.Millisecond, Coefficient: 2, Max: 5 * time.Second}

	// RetryProvider is for an external money call. The interval matters less than
	// the patience: the provider's own rate limits mean a tight loop is worse than
	// a slow one, and the idempotency key makes every attempt the same request.
	RetryProvider = Retry{Initial: time.Second, Coefficient: 2, Max: time.Minute}

	// RetryCompensation is the one that is different, and it is different because
	// a failed compensation is the worst state this system can be in: money moved
	// and the record of it was not corrected. So it is more patient than anything
	// else and it never gives up on its own — it escalates. See Saga.
	RetryCompensation = Retry{Initial: 2 * time.Second, Coefficient: 2, Max: 5 * time.Minute}
)

// IdempotencyKey is the key an activity presents to a provider or to a unique
// index, derived from the workflow rather than generated inside the activity.
//
// This is the single most important line in the package. An activity that
// generates its own key — uuid.New(), time.Now() — produces a different key on
// every retry, so the provider sees two requests and the tenant is debited twice.
// Derived from the workflow id and the step name, every attempt of every retry
// presents the same key, and the provider's own deduplication does the rest.
//
// Deliberately no attempt number and no timestamp. Both would make a retry a new
// request, which is exactly the bug.
//
// It composes with ADR-0011 §2: our key never expires, so a workflow resumed
// after a day of retries still cannot create a second collection, long after the
// provider's own 24-hour key has gone.
func IdempotencyKey(workflowID, step string) (string, error) {
	if workflowID == "" || step == "" {
		return "", errors.New("workflow: an idempotency key needs the workflow and the step, or a " +
			"retry becomes a second request")
	}
	if strings.Contains(step, "#") {
		return "", fmt.Errorf("workflow: step %q contains the separator", step)
	}
	return workflowID + "#" + step, nil
}

// Config is what a process needs to reach Temporal.
type Config struct {
	HostPort  string
	Namespace string
	TLS       bool
}

// ErrNotConfigured is returned when Temporal is absent.
var ErrNotConfigured = errors.New("workflow: Temporal is not configured")

// Validate refuses a configuration that would leave a money operation running
// somewhere other than a workflow.
//
// This is a deliberate divergence from the pattern already in production for
// HomeChef, where the Temporal client is opt-in and callers fall back to inline
// execution when it is absent. For a notification that is the right trade: an
// inline send is worse than a durable one and better than none.
//
// For money it is the failure this ADR exists to prevent. "Temporal was not
// configured, so we did the payout inline" is a payout with no compensation, no
// resumption and no record — and it would be indistinguishable from a healthy
// deploy, because nothing errored. So a process that serves any operation on the
// durable list refuses to start without a namespace, in the same way ADR-0011's
// empty webhook secret rejects every delivery rather than trusting every delivery.
func (c Config) Validate() error {
	if c.HostPort == "" {
		return fmt.Errorf("%w: no host and port. A money operation may not fall back to running "+
			"inline — that is a payout with no compensation and no record, and nothing would error",
			ErrNotConfigured)
	}
	if c.Namespace == "" {
		return fmt.Errorf("%w: no namespace", ErrNotConfigured)
	}
	// One namespace per product, which is the org's existing convention. Dwellm8's
	// workflows must not be registered into another product's namespace, where its
	// task queues would be served by workers that have never heard of them — a
	// workflow that stays queued forever, with nothing anywhere reporting an error.
	if c.Namespace != Namespace {
		return fmt.Errorf("%w: namespace is %q, and this product's is %q — a workflow registered in "+
			"another product's namespace waits on a queue no worker is serving",
			ErrNotConfigured, c.Namespace, Namespace)
	}
	return nil
}

// Namespace is Dwellm8's Temporal namespace. One per product, matching the
// convention already in production.
const Namespace = "dwellm8"
