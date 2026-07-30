// Package tenancy is the only sanctioned way to reach the database.
//
// ADR-0003: every query runs inside a transaction whose session carries
// app.tenant_id, so PostgreSQL's row-level security can filter it. There is no
// exported way to obtain a transaction without a tenant, which is the point —
// a forgotten filter should be impossible rather than merely discouraged.
package tenancy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNoTenant is returned when a scoped operation is attempted without one.
// It is deliberately not a database error: the request never reaches the
// database.
var ErrNoTenant = errors.New("tenancy: no tenant in context")

type ctxKey struct{}

// ID is an organisation id. A distinct type, so a unit id or a user id cannot
// be passed where a tenant is expected.
type ID string

func (t ID) String() string { return string(t) }

// With returns a context carrying the tenant. Called once per request by the
// middleware that verified the token — never from a handler, and never from a
// client-supplied header.
func With(ctx context.Context, tenant ID) context.Context {
	return context.WithValue(ctx, ctxKey{}, tenant)
}

// From returns the tenant in the context, if there is one.
func From(ctx context.Context) (ID, bool) {
	t, ok := ctx.Value(ctxKey{}).(ID)
	return t, ok && t != ""
}

// Pool is the subset of pgxpool.Pool this package needs, so a test can
// substitute one without a database.
type Pool interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Scoped runs fn inside a transaction with app.tenant_id set to the context's
// tenant, and commits if fn returns nil.
//
// SET LOCAL semantics, not SET: the setting is scoped to this transaction, so
// a pooled connection cannot carry one request's tenant into the next request
// that picks it up. That failure would be intermittent and would err towards
// disclosure — the worst combination available.
func Scoped(ctx context.Context, p Pool, fn func(context.Context, pgx.Tx) error) error {
	tenant, ok := From(ctx)
	if !ok {
		return ErrNoTenant
	}

	tx, err := p.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("tenancy: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // a no-op once committed

	// set_config with a parameter, not string interpolation: a malformed
	// tenant reaches PostgreSQL as data and cannot become SQL.
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenant.String()); err != nil {
		return fmt.Errorf("tenancy: set tenant: %w", err)
	}

	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("tenancy: commit: %w", err)
	}
	return nil
}

// PlatformPool is a pool connected as dwellm8_platform — the role the policies
// exempt through is_platform_session().
//
// It is a distinct type rather than a plain Pool so that the privileged
// connection cannot be passed where the request pool is expected, or reached
// by a handler that happens to have a pool in scope. Constructing one is a
// deliberate act, visible in wiring.
type PlatformPool struct{ Pool }

// NewPlatformPool marks a pool as the privileged one. Call it once, in main,
// with a pool whose DSN uses dwellm8_platform.
func NewPlatformPool(p Pool) PlatformPool { return PlatformPool{Pool: p} }

// Platform runs fn in a transaction with no tenant set, for the few operations
// that create or span organisations — onboarding, platform reporting, an
// audited support session.
//
// It takes PlatformPool because being unscoped is not enough: the policies
// exempt a role, so the connection itself must be the privileged one. Passing
// the request pool here would simply return zero rows, which is a confusing
// way to fail.
//
// reason is not decoration. The caller writes it to the audit trail, and an
// exemption that leaves no trace is a back door.
func Platform(ctx context.Context, p PlatformPool, reason string, fn func(context.Context, pgx.Tx) error) error {
	if reason == "" {
		return errors.New("tenancy: a platform-scoped operation needs a reason for the audit trail")
	}

	tx, err := p.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("tenancy: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
