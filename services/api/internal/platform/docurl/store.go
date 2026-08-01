package docurl

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Store persists the two things the signature cannot carry: which
// transactions have reached a terminal state, and who fetched what. Both
// tables are tenant-scoped under row-level security — the grant's Org is what
// opens the window, since the fetch itself arrives with no sign-in.
type Store struct{ pool tenancy.Pool }

func NewStore(p tenancy.Pool) *Store { return &Store{pool: p} }

// Revoke closes the window for one transaction: completion, cancellation,
// timeout or rejection — the reasons #212 names, and the table's CHECK
// enforces. Idempotent, because a webhook and a poller may both report the
// same terminal state; the first reason recorded is the one kept.
func (st *Store) Revoke(ctx context.Context, org tenancy.ID, txnID, reason string) error {
	ctx = tenancy.With(ctx, org)
	return tenancy.Scoped(ctx, st.pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO esign_docurl_revocations (tenant_id, txn_id, reason)
			VALUES ($1, $2, $3)
			ON CONFLICT (tenant_id, txn_id) DO NOTHING`,
			org.String(), txnID, reason)
		if err != nil {
			return fmt.Errorf("docurl: revoking %s: %w", txnID, err)
		}
		return nil
	})
}

// Revoked reports whether a grant's transaction has reached a terminal state.
func (st *Store) Revoked(ctx context.Context, g Grant) (bool, error) {
	ctx = tenancy.With(ctx, g.Org)
	var revoked bool
	err := tenancy.Scoped(ctx, st.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM esign_docurl_revocations WHERE txn_id = $1)`,
			g.TxnID).Scan(&revoked)
	})
	if err != nil {
		return false, fmt.Errorf("docurl: checking revocation of %s: %w", g.TxnID, err)
	}
	return revoked, nil
}

// Log records one fetch — served or refused — against its transaction, which
// is what makes a forwarded link accountable rather than forbidden. The rows
// are lease evidence: append-only, retained with the lease.
func (st *Store) Log(ctx context.Context, g Grant, sourceIP, userAgent, outcome string) error {
	ctx = tenancy.With(ctx, g.Org)
	return tenancy.Scoped(ctx, st.pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO esign_docurl_access_log
			       (tenant_id, txn_id, document_ref, source_ip, user_agent, outcome)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			g.Org.String(), g.TxnID, g.DocumentRef, sourceIP, userAgent, outcome)
		if err != nil {
			return fmt.Errorf("docurl: logging a fetch of %s: %w", g.TxnID, err)
		}
		return nil
	})
}
