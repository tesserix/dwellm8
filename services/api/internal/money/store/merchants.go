package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Merchants is the store for the manager's own account with an aggregator
// (#269). One row per organisation per provider; the account number is the
// provider's to hold, and what is here is the mask.
type Merchants struct{ pool tenancy.Pool }

func NewMerchants(pool tenancy.Pool) *Merchants { return &Merchants{pool: pool} }

// ErrNoMerchant is a manager who has not connected that provider. It is
// distinct from an unverified account: one is nothing to fix yet, the other is
// an onboarding to finish.
var ErrNoMerchant = errors.New("no merchant account with that provider")

const merchantColumns = `provider, coalesce(merchant_ref, ''), country, business_name,
	business_type, state, coalesce(reason, ''), coalesce(settlement_holder, ''),
	coalesce(settlement_masked, ''), coalesce(settlement_ifsc, ''), settlement_currency,
	coalesce(settlement_swift, ''), verified_at`

func scanMerchant(row pgx.Row) (merchant.Account, error) {
	var a merchant.Account
	var verified *time.Time
	err := row.Scan(&a.Provider, &a.MerchantRef, &a.Country, &a.BusinessName,
		&a.BusinessType, &a.State, &a.Reason, &a.Settlement.Holder,
		&a.Settlement.Masked, &a.Settlement.IFSC, &a.Settlement.Currency,
		&a.Settlement.SwiftBIC, &verified)
	if err != nil {
		return merchant.Account{}, err
	}
	if verified != nil {
		a.VerifiedAt = *verified
	}
	return a, nil
}

// Connect writes the account the manager submitted to their provider. Pressing
// the button twice is the same account, updated — the unique index is what says
// so, and a second row would mean two answers to where rent settles.
func (s *Merchants) Connect(ctx context.Context, a merchant.Account) (merchant.Account, error) {
	if err := a.Validate(); err != nil {
		return merchant.Account{}, err
	}
	var out merchant.Account
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tenant, _ := tenancy.From(ctx)
		row := tx.QueryRow(ctx, `
			INSERT INTO merchant_accounts (
				tenant_id, provider, merchant_ref, country, business_name, business_type,
				state, reason, settlement_holder, settlement_masked, settlement_ifsc,
				settlement_currency, settlement_swift, verified_at)
			VALUES ($1, $2, nullif($3, ''), $4, $5, $6, $7, nullif($8, ''), nullif($9, ''),
				nullif($10, ''), nullif($11, ''), $12, nullif($13, ''),
				CASE WHEN $7 = 'verified' THEN now() END)
			ON CONFLICT (tenant_id, provider) DO UPDATE SET
				merchant_ref        = excluded.merchant_ref,
				country             = excluded.country,
				business_name       = excluded.business_name,
				business_type       = excluded.business_type,
				state               = excluded.state,
				reason              = excluded.reason,
				settlement_holder   = excluded.settlement_holder,
				settlement_masked   = excluded.settlement_masked,
				settlement_ifsc     = excluded.settlement_ifsc,
				settlement_currency = excluded.settlement_currency,
				settlement_swift    = excluded.settlement_swift,
				verified_at         = CASE WHEN excluded.state = 'verified'
				                     THEN coalesce(merchant_accounts.verified_at, now()) END,
				updated_at          = now()
			RETURNING `+merchantColumns,
			tenant.String(), a.Provider, a.MerchantRef, a.Country, a.BusinessName,
			a.BusinessType, string(a.State), a.Reason, a.Settlement.Holder,
			a.Settlement.Masked, a.Settlement.IFSC, a.Settlement.Currency, a.Settlement.SwiftBIC)
		var err error
		out, err = scanMerchant(row)
		return err
	})
	if err != nil {
		return merchant.Account{}, fmt.Errorf("connect %s merchant: %w", a.Provider, err)
	}
	return out, nil
}

// ForProvider reads one account.
func (s *Merchants) ForProvider(ctx context.Context, provider string) (merchant.Account, error) {
	var out merchant.Account
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+merchantColumns+`
			  FROM merchant_accounts WHERE provider = $1`, provider)
		var err error
		out, err = scanMerchant(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoMerchant
		}
		return err
	})
	if err != nil {
		return merchant.Account{}, err
	}
	return out, nil
}

// List is every provider this manager has connected.
func (s *Merchants) List(ctx context.Context) ([]merchant.Account, error) {
	var out []merchant.Account
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+merchantColumns+`
			  FROM merchant_accounts ORDER BY provider`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scanMerchant(rows)
			if err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list merchant accounts: %w", err)
	}
	return out, nil
}

// Record applies the provider's verdict. The transition is checked against the
// domain first: a webhook that says an account went back to unconnected is a
// mistranslation, and applying it would make an onboarded manager look new.
func (s *Merchants) Record(ctx context.Context, provider string, to merchant.State, reason string) (merchant.Account, error) {
	var out merchant.Account
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var from merchant.State
		err := tx.QueryRow(ctx, `
			SELECT state FROM merchant_accounts WHERE provider = $1 FOR UPDATE`, provider).Scan(&from)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoMerchant
		}
		if err != nil {
			return err
		}
		if err := from.CanMoveTo(to); err != nil {
			return err
		}
		row := tx.QueryRow(ctx, `
			UPDATE merchant_accounts
			   SET state       = $2,
			       reason      = nullif($3, ''),
			       verified_at = CASE WHEN $2 = 'verified' THEN now() ELSE verified_at END,
			       updated_at  = now()
			 WHERE provider = $1
			RETURNING `+merchantColumns, provider, string(to), reason)
		out, err = scanMerchant(row)
		return err
	})
	if err != nil {
		return merchant.Account{}, err
	}
	return out, nil
}
