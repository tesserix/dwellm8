package tenancy

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The audited support path. Issue #226, and the threat model's §3.1.
//
// Platform() takes a reason and, until this file existed, discarded it — so
// every privileged action stated its reason to the compiler and to nobody else.
// A support path that is unlogged is the exact shape of the incident that cannot
// be investigated afterwards.
//
// # Why this is a second function rather than a change to the first
//
// Platform() is used by machines as well as by people: the webhook inbox records
// a delivery before it knows whose money it concerns, reconciliation sweeps every
// organisation, the test harness seeds fixtures. None of those has a human actor,
// and an audit row per seeded fixture would bury the rows that matter under the
// rows that do not.
//
// So the line is drawn where it actually falls — machine paths keep Platform(),
// human paths use Support() and cannot avoid naming themselves. The arch test in
// internal/platform/arch is what stops a module's handlers drifting back to
// Platform() once this is inconvenient.

// ActorKind is who is acting, matching the audit_events CHECK.
type ActorKind string

const (
	// ActorSupport is a Dwellm8 employee acting on an organisation's data. The
	// only kind Support() accepts: the others do not go through a support path.
	ActorSupport ActorKind = "support"
	// ActorUser is a person acting on their own organisation's data.
	ActorUser ActorKind = "user"
)

// Act is what a support action must declare before it is allowed to happen.
//
// Every field is required, and that is the design: an audit row that says
// "support did something to an organisation for a reason" is not an audit row.
// The question afterwards is always *who*, *to what*, and *why*, and a schema
// that permits two of the three answers none of them.
type Act struct {
	// ActorID is the employee. Not a name — an id that resolves to one.
	ActorID string
	// ActorKind is normally ActorSupport.
	ActorKind ActorKind
	// TenantID is the organisation being acted on. audit_events.tenant_id is
	// NOT NULL, so an action spanning organisations is one Act per organisation
	// rather than one row that names none of them.
	TenantID ID
	// Module is which part of the product, matching the audit_events CHECK.
	Module string
	// Action is what was done, in the past tense: "refunded", "unlocked".
	Action string
	// SubjectKind and SubjectID are what it was done to.
	SubjectKind, SubjectID string
	// Reason is why. Free text, and the thing a reviewer actually reads.
	Reason string
	// GrantID is the delegation grant relied on, where one was.
	GrantID string
}

// modules matches the audit_events CHECK. Duplicated deliberately, like every
// other vocabulary in this codebase, and the store contract test is the price.
var modules = []string{"identity", "property", "lease", "money",
	"maintenance", "community", "discovery", "notify"}

// ErrAct is a support action that cannot be audited, and therefore cannot happen.
var ErrAct = errors.New("tenancy: a support action must say who, to what, and why")

// Validate refuses an action that would produce an unusable audit row.
func (a Act) Validate() error {
	switch {
	case a.ActorID == "":
		return fmt.Errorf("%w: no actor", ErrAct)
	case a.ActorKind != ActorSupport && a.ActorKind != ActorUser:
		return fmt.Errorf("%w: %q is not an actor kind", ErrAct, a.ActorKind)
	case a.TenantID == "":
		return fmt.Errorf("%w: no organisation — an action spanning several is one call per "+
			"organisation, not one row naming none of them", ErrAct)
	case !slices.Contains(modules, a.Module):
		return fmt.Errorf("%w: %q is not a module", ErrAct, a.Module)
	case strings.TrimSpace(a.Action) == "":
		return fmt.Errorf("%w: no action", ErrAct)
	case strings.TrimSpace(a.SubjectKind) == "" || strings.TrimSpace(a.SubjectID) == "":
		return fmt.Errorf("%w: nothing was acted on", ErrAct)
	case strings.TrimSpace(a.Reason) == "":
		return fmt.Errorf("%w: no reason — the reason is the whole point of the record", ErrAct)
	}
	return nil
}

// Support runs a privileged action and writes its audit row in the same
// transaction.
//
// **In the same transaction**, which is the only part of this that is subtle. An
// audit written afterwards records actions that were rolled back; one written
// before records actions that never happened. Inside, the two commit together or
// neither does — so the audit trail cannot claim something the database does not
// show, in either direction.
//
// The row is written *first* within that transaction, so that a fn which fails
// on a constraint still leaves no audit row, and a fn which succeeds cannot
// leave one behind by returning early.
func Support(ctx context.Context, p PlatformPool, a Act, fn func(context.Context, pgx.Tx) error) error {
	if err := a.Validate(); err != nil {
		return err
	}

	tx, err := p.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("tenancy: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (tenant_id, actor_id, actor_kind, module, action,
		                          subject_kind, subject_id, reason, grant_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		a.TenantID.String(), a.ActorID, string(a.ActorKind), a.Module, a.Action,
		a.SubjectKind, a.SubjectID, a.Reason, nullID(a.GrantID)); err != nil {
		return fmt.Errorf("tenancy: recording the support action: %w", err)
	}

	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("tenancy: commit: %w", err)
	}
	return nil
}

// Modules returns the audit vocabulary, for the contract test.
func Modules() []string { return append([]string(nil), modules...) }

func nullID(s string) any {
	if s == "" {
		return nil
	}
	return s
}
