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
type grantCtxKey struct{}
type residentCtxKey struct{}

// ID is an organisation id. A distinct type, so a unit id or a user id cannot
// be passed where a tenant is expected.
type ID string

func (t ID) String() string { return string(t) }

// GrantID identifies a delegation grant (ADR-0005) — the mechanism by which a
// management firm reaches units it does not own.
//
// A distinct type from ID on purpose: the tenant of a delegated request stays
// the firm's own organisation. Acting under a grant is not becoming the owner,
// and a grant id must never be assignable to a tenant.
type GrantID string

func (g GrantID) String() string { return string(g) }

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

// WithGrant returns a context acting under a delegation grant, in addition to
// its own tenant. The tenant is unchanged: the firm remains itself, and the
// grant is what lets it see across.
//
// The grant id here is a claim, not a permission. PostgreSQL checks it on every
// row: that the grant exists, names this tenant as its grantee, is live, covers
// the property and carries the permission the policy asks for. Quoting somebody
// else's grant id yields nothing, because the policy on delegation_grants hides
// the row the check would need.
func WithGrant(ctx context.Context, grant GrantID) context.Context {
	return context.WithValue(ctx, grantCtxKey{}, grant)
}

// GrantFrom returns the grant the context is acting under, if any.
func GrantFrom(ctx context.Context) (GrantID, bool) {
	g, ok := ctx.Value(grantCtxKey{}).(GrantID)
	return g, ok && g != ""
}

// ResidentID is the party id of a renter reading their own tenancy. ADR-0029.
//
// A distinct type from ID for the same reason GrantID is: the tenant of a
// resident request stays the landlord's organisation. A renter is not a small
// organisation, and a party id must never be assignable to a tenant.
type ResidentID string

func (r ResidentID) String() string { return string(r) }

// WithResident narrows a context to one renter, in addition to its tenant.
//
// The organisation is unchanged: the rows still belong to the landlord, and
// this says which of them are this person's. Both settings are checked by
// PostgreSQL on every row — a party id quoted by somebody who is not on that
// lease yields nothing, because the policy on lease_parties hides the row the
// check would need.
//
// Called once per request by the middleware that resolved the sign-in, never
// from a handler and never from a client-supplied value.
func WithResident(ctx context.Context, party ResidentID) context.Context {
	return context.WithValue(ctx, residentCtxKey{}, party)
}

// ResidentFrom returns the renter the context is acting as, if any.
func ResidentFrom(ctx context.Context) (ResidentID, bool) {
	r, ok := ctx.Value(residentCtxKey{}).(ResidentID)
	return r, ok && r != ""
}

// Pool is the subset of pgxpool.Pool this package needs, so a test can
// substitute one without a database.
type Pool interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Scoped runs fn inside a transaction with app.tenant_id set to the context's
// tenant — and app.grant_id set too, when the request is acting under a
// delegation — and commits if fn returns nil.
//
// SET LOCAL semantics, not SET: the setting is scoped to this transaction, so
// a pooled connection cannot carry one request's tenant into the next request
// that picks it up. That failure would be intermittent and would err towards
// disclosure — the worst combination available.
//
// The transaction is READ COMMITTED, which is PostgreSQL's default and, for
// ADR-0005, load-bearing: each statement takes a fresh snapshot, so a grant
// revoked by its owner mid-transaction stops the very next statement. At
// REPEATABLE READ the open transaction would keep the access it started with.
// Both were measured; see ADR-0005.
func Scoped(ctx context.Context, p Pool, fn func(context.Context, pgx.Tx) error) error {
	tenant, ok := From(ctx)
	if !ok {
		return ErrNoTenant
	}
	grant, _ := GrantFrom(ctx)       // empty when the request is acting for itself
	resident, _ := ResidentFrom(ctx) // empty for everybody who is not a renter

	tx, err := p.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("tenancy: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // a no-op once committed

	// set_config with parameters, not string interpolation: a malformed tenant
	// or grant reaches PostgreSQL as data and cannot become SQL.
	//
	// All three settings are written every time, the grant and the resident to
	// '' when absent. The empty string is why current_grant_id() and
	// current_resident_party_id() coerce '' to NULL rather than casting it — an
	// unset grant must deny quietly, and an unset resident must leave every
	// ADR-0029 policy inert, rather than raising.
	//
	// Writing all three unconditionally is what stops one request's resident
	// leaking into the next request that picks the connection up. A conditional
	// write would leave the previous value in place for exactly the request that
	// is not a renter — the widest possible failure, arriving intermittently.
	if _, err := tx.Exec(ctx,
		`SELECT set_config('app.tenant_id', $1, true),
		        set_config('app.grant_id', $2, true),
		        set_config('app.resident_party_id', $3, true)`,
		tenant.String(), grant.String(), resident.String()); err != nil {
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

// Public runs fn in a deliberately unscoped transaction on the request pool,
// for the one read path that serves strangers: the listing site (ADR-0019).
//
// This is not a bypass. The connection is the ordinary request role, every
// policy still applies, and an unscoped session matches only the public branch
// on listings — live, published rows — and sees nothing anywhere else. The
// settings are still written, to '', for Scoped's reason: a pooled connection
// must not carry the previous request's tenant into this one, which here would
// turn an anonymous search into somebody's scoped read.
//
// Writes have no public branch by construction, so fn is enforced read-only at
// the database as well as by convention.
func Public(ctx context.Context, p Pool, fn func(context.Context, pgx.Tx) error) error {
	tx, err := p.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("tenancy: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`SELECT set_config('app.tenant_id', '', true),
		        set_config('app.grant_id', '', true),
		        set_config('app.resident_party_id', '', true)`); err != nil {
		return fmt.Errorf("tenancy: clear scope: %w", err)
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
