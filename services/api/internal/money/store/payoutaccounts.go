package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// PayoutAccounts is the store for the impersonated-owner control (#227).
type PayoutAccounts struct{ pool tenancy.Pool }

func NewPayoutAccounts(pool tenancy.Pool) *PayoutAccounts { return &PayoutAccounts{pool: pool} }

// AccountChange is a decided change: masked and fingerprinted by the service,
// never carrying the number itself.
type AccountChange struct {
	OwnerPartyID string
	Masked       string
	IFSC         string
	Fingerprint  string
}

// Changed reports what a change did, for the response and the notification.
type Changed struct {
	ID        string
	OldMasked string // empty on the first account
	NewMasked string
	PayableAt time.Time
	Unchanged bool // the "change" named the account already on file
}

// ErrNoAccount is a payout asked about an owner who has no account on file.
var ErrNoAccount = errors.New("no payout account on file")

// Change closes the current account row, opens the new one, and writes the
// audit row and the outbox event in the same transaction — an audit that could
// record a change that rolled back, or miss one that committed, is the failure
// tenancy.Support documents, and the same rule holds here.
func (s *PayoutAccounts) Change(ctx context.Context, c AccountChange) (Changed, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return Changed{}, tenancy.ErrNoTenant
	}
	grant, _ := tenancy.GrantFrom(ctx)

	var out Changed
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var oldID, oldMasked, oldFP string
		err := tx.QueryRow(ctx, `
			SELECT id::text, masked_account, account_fp
			  FROM payout_accounts
			 WHERE owner_party_id = $1 AND valid_to IS NULL
			 FOR UPDATE`, c.OwnerPartyID).Scan(&oldID, &oldMasked, &oldFP)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			// The first account ever filed - nothing to close.
		case err != nil:
			return fmt.Errorf("reading the current account: %w", err)
		case oldFP == c.Fingerprint:
			// Naming the account already on file is not a change, and must not
			// open a new row: a fresh row would look like churn in the audit
			// trail without moving anything.
			out = Changed{ID: oldID, NewMasked: oldMasked, Unchanged: true}
			return nil
		default:
			if _, err := tx.Exec(ctx, `
				UPDATE payout_accounts SET valid_to = now() WHERE id = $1`, oldID); err != nil {
				return fmt.Errorf("closing the current account: %w", err)
			}
		}

		var newID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO payout_accounts
			       (tenant_id, owner_party_id, masked_account, ifsc, account_fp, changed_via_grant_id)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id::text`,
			tenant.String(), c.OwnerPartyID, c.Masked, c.IFSC, c.Fingerprint,
			nullUUID(string(grant))).Scan(&newID); err != nil {
			return fmt.Errorf("writing the new account: %w", err)
		}

		// When this account becomes payable: the first appearance of its
		// fingerprint plus the cool-off the schema function enforces.
		if err := tx.QueryRow(ctx, `
			SELECT min(valid_from) + interval '72 hours'
			  FROM payout_accounts
			 WHERE owner_party_id = $1 AND account_fp = $2`,
			c.OwnerPartyID, c.Fingerprint).Scan(&out.PayableAt); err != nil {
			return fmt.Errorf("computing the cool-off: %w", err)
		}

		// The audit names the acting organisation and the grant; the person
		// arrives when request principals carry a party uuid (#229).
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_events (tenant_id, actor_kind, module, action,
			                          subject_kind, subject_id, reason, detail, grant_id)
			VALUES ($1, 'user', 'money', 'payout_account.changed',
			        'payout_account', $2, 'owner payout account changed',
			        jsonb_build_object('old_masked', $3::text, 'new_masked', $4::text), $5)`,
			tenant.String(), newID, nullUUID(oldMasked), c.Masked,
			nullUUID(string(grant))); err != nil {
			return fmt.Errorf("recording the change: %w", err)
		}

		env, err := events.New("money.payout_account.changed", tenant.String(),
			events.Subject{Kind: "payout_account", ID: newID},
			events.Actor{Kind: events.ActorUser, ID: c.OwnerPartyID},
			struct {
				OwnerPartyID string    `json:"owner_party_id"`
				OldMasked    string    `json:"old_masked,omitempty"`
				NewMasked    string    `json:"new_masked"`
				PayableAt    time.Time `json:"payable_at"`
				GrantID      string    `json:"grant_id,omitempty"`
			}{c.OwnerPartyID, oldMasked, c.Masked, out.PayableAt, string(grant)})
		if err != nil {
			return err
		}
		if err := events.Append(ctx, tx, env); err != nil {
			return err
		}

		out.ID, out.OldMasked, out.NewMasked = newID, oldMasked, c.Masked
		return nil
	})
	return out, err
}

// Payable answers what the payout run must ask: the account to pay, or the
// hold. A hold is a result, not an absence — the run surfaces it (#227).
type Payable struct {
	AccountID string
	Held      bool
	Masked    string
	PayableAt time.Time
}

func (s *PayoutAccounts) Payable(ctx context.Context, ownerPartyID string) (Payable, error) {
	var p Payable
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var payable *string
		if err := tx.QueryRow(ctx,
			`SELECT payout_account_payable($1)::text`, ownerPartyID).Scan(&payable); err != nil {
			return err
		}
		if payable != nil {
			p.AccountID = *payable
			return tx.QueryRow(ctx, `
				SELECT masked_account FROM payout_accounts WHERE id = $1`,
				p.AccountID).Scan(&p.Masked)
		}
		p.Held = true
		err := tx.QueryRow(ctx, `
			SELECT cur.masked_account,
			       (SELECT min(f.valid_from) FROM payout_accounts f
			         WHERE f.owner_party_id = cur.owner_party_id
			           AND f.account_fp = cur.account_fp) + interval '72 hours'
			  FROM payout_accounts cur
			 WHERE cur.owner_party_id = $1 AND cur.valid_to IS NULL`,
			ownerPartyID).Scan(&p.Masked, &p.PayableAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoAccount
		}
		return err
	})
	return p, err
}
