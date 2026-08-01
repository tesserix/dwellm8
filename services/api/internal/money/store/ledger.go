package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Ledger is the posting engine. ADR-0006: the only way an amount is written, and
// the only way a balance is read.
type Ledger struct{ pool tenancy.Pool }

// NewLedger takes the request pool. Money is always somebody's, so never the
// platform pool.
func NewLedger(p tenancy.Pool) *Ledger { return &Ledger{pool: p} }

// ErrNoEntry is returned when no entry matches — including one hidden by policy.
var ErrNoEntry = errors.New("money: no such journal entry")

// Posted is the result of a post. Duplicate means the key had already posted this
// entry and nothing was written.
type Posted struct {
	ID        string
	Entry     domain.Entry
	Duplicate bool
}

// Post writes one entry and its lines in a single transaction.
//
// Idempotent on (tenant_id, idempotency_key): a redelivered webhook or a retried
// activity returns the entry that already exists rather than a second set of
// postings.
func (l *Ledger) Post(ctx context.Context, e domain.Entry) (Posted, error) {
	if err := e.Validate(); err != nil {
		return Posted{}, err
	}
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return Posted{}, tenancy.ErrNoTenant
	}

	var id string
	err := tenancy.Scoped(ctx, l.pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = postEntry(ctx, tx, tenant, e)
		return err
	})
	switch {
	case err == nil:
		return Posted{ID: id, Entry: e}, nil
	case isUniqueViolation(err, "journal_entries_idempotency"):
		// The guarantee working, not a failure: return what the key already posted.
		existing, id, lookupErr := l.byKey(ctx, e.IdempotencyKey)
		if lookupErr != nil {
			return Posted{}, fmt.Errorf("money: resolving a duplicate idempotency key: %w", lookupErr)
		}
		return Posted{ID: id, Entry: existing, Duplicate: true}, nil
	case isUniqueViolation(err, "journal_entries_one_reversal"):
		return Posted{}, fmt.Errorf("%w: %s", ErrAlreadyReversed, e.Reverses)
	default:
		return Posted{}, fmt.Errorf("money: posting a %s entry: %w", e.Kind, err)
	}
}

// postEntry writes the header and its lines inside a caller's transaction, so
// that a payment's capture and its posting can be one atomic act.
func postEntry(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, e domain.Entry) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO journal_entries (tenant_id, entry_kind, template_version, occurred_on,
		                             lease_id, source_kind, source_id, idempotency_key,
		                             memo, reverses_entry_id, reversal_reason)
		VALUES ($1, $2, $3, $4::date, nullif($5,'')::uuid, $6, $7, $8,
		        nullif($9,''), nullif($10,'')::uuid, nullif($11,''))
		RETURNING id`,
		tenant.String(), string(e.Kind), e.TemplateVersion, e.OccurredOn,
		e.Lease, e.SourceKind, e.SourceID, e.IdempotencyKey,
		e.Memo, e.Reverses, string(e.ReversalReason),
	).Scan(&id); err != nil {
		return "", err
	}
	return id, insertPostings(ctx, tx, tenant, id, e)
}

// The place lives on the lines, not on the header: it is what their policy is
// judged against, and every line of an entry shares it.
func insertPostings(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, entryID string, e domain.Entry) error {
	for i, p := range e.Postings {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ledger_postings (entry_id, tenant_id, property_id, unit_id,
			                             account_code, side, amount_minor, party_kind, party_id, memo)
			VALUES ($1, $2, nullif($3,'')::uuid, nullif($4,'')::uuid, $5, $6, $7, $8,
			        nullif($9,'')::uuid, nullif($10,''))`,
			entryID, tenant.String(), e.Property, e.Unit, p.Account, string(p.Side),
			int64(p.Amount), string(p.Party.Kind), p.Party.ID, p.Memo); err != nil {
			return fmt.Errorf("line %d on %s: %w", i, p.Account, err)
		}
	}
	return nil
}

// ErrAlreadyReversed is a second reversal of the same entry. One correction nets
// to the wrong side of the original twice over.
var ErrAlreadyReversed = errors.New("money: that entry has already been reversed")

// Reverse posts the correcting entry for originalID: every line on the opposite
// side, same amounts, same place, with a reason. The original is never touched.
func (l *Ledger) Reverse(ctx context.Context, originalID string, reason domain.ReversalReason, key string) (Posted, error) {
	original, err := l.Entry(ctx, originalID)
	if err != nil {
		return Posted{}, err
	}
	reversal, err := domain.Reverse(original, originalID, reason, key)
	if err != nil {
		return Posted{}, err
	}
	return l.Post(ctx, reversal)
}

// Entry reads one entry and its lines.
func (l *Ledger) Entry(ctx context.Context, id string) (domain.Entry, error) {
	e, _, err := l.read(ctx, `e.id = $1`, id)
	return e, err
}

func (l *Ledger) byKey(ctx context.Context, key string) (domain.Entry, string, error) {
	return l.read(ctx, `e.idempotency_key = $1`, key)
}

func (l *Ledger) read(ctx context.Context, where string, args ...any) (domain.Entry, string, error) {
	var e domain.Entry
	var id string
	err := tenancy.Scoped(ctx, l.pool, func(ctx context.Context, tx pgx.Tx) error {
		var kind, reason string
		var occurred time.Time
		if err := tx.QueryRow(ctx, `
			SELECT e.id, e.entry_kind, e.template_version, e.occurred_on, coalesce(e.lease_id::text,''),
			       e.source_kind, e.source_id, e.idempotency_key, coalesce(e.memo,''),
			       coalesce(e.reverses_entry_id::text,''), coalesce(e.reversal_reason,'')
			  FROM journal_entries e WHERE `+where, args...,
		).Scan(&id, &kind, &e.TemplateVersion, &occurred, &e.Lease, &e.SourceKind,
			&e.SourceID, &e.IdempotencyKey, &e.Memo, &e.Reverses, &reason); err != nil {
			return err
		}
		e.Kind, e.OccurredOn, e.ReversalReason = domain.EventKind(kind), occurred, domain.ReversalReason(reason)

		rows, err := tx.Query(ctx, `
			SELECT coalesce(property_id::text,''), coalesce(unit_id::text,''), account_code,
			       side, amount_minor, party_kind, coalesce(party_id::text,''), coalesce(memo,'')
			  FROM ledger_postings WHERE entry_id = $1 ORDER BY created_at, id`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p domain.Posting
			var side, party string
			if err := rows.Scan(&e.Property, &e.Unit, &p.Account, &side,
				&p.Amount, &party, &p.Party.ID, &p.Memo); err != nil {
				return err
			}
			p.Side, p.Party.Kind = domain.Side(side), domain.PartyKind(party)
			e.Postings = append(e.Postings, p)
		}
		return rows.Err()
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Entry{}, "", ErrNoEntry
	}
	if err != nil {
		return domain.Entry{}, "", fmt.Errorf("money: reading a journal entry: %w", err)
	}
	return e, id, nil
}

// BalanceQuery selects which postings a balance sums. Every field is optional and
// they combine: an empty query is the whole organisation, all time.
type BalanceQuery struct {
	Lease    string
	Property string
	Unit     string
	Account  string
	Party    domain.Party
	// AsOf bounds by journal_entries.occurred_on — the accounting date, not the
	// row's timestamp — so a backdated entry lands in the period it belongs to.
	AsOf time.Time
}

// Balance is one account's position for one party, derived. ADR-0006 §5: nothing
// stores this.
type Balance struct {
	Account     string
	AccountType domain.AccountType
	Party       domain.Party
	Amount      domain.Minor
	Debits      domain.Minor
	Credits     domain.Minor
	Postings    int
}

// Balances sums the matching postings, grouped by account and party.
//
// It reads ledger_postings joined to journal_entries rather than the
// ledger_balances view: the view has no date, and every question worth asking of
// a ledger is asked as of some date. Both run under the same policies.
func (l *Ledger) Balances(ctx context.Context, q BalanceQuery) ([]Balance, error) {
	where := []string{"true"}
	args := []any{}
	add := func(clause string, arg any) {
		args = append(args, arg)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if q.Lease != "" {
		add("e.lease_id = $%d::uuid", q.Lease)
	}
	if q.Property != "" {
		add("p.property_id = $%d::uuid", q.Property)
	}
	if q.Unit != "" {
		add("p.unit_id = $%d::uuid", q.Unit)
	}
	if q.Account != "" {
		add("p.account_code = $%d", q.Account)
	}
	if q.Party.ID != "" {
		add("p.party_id = $%d::uuid", q.Party.ID)
	}
	if q.Party.Kind != "" {
		add("p.party_kind = $%d", string(q.Party.Kind))
	}
	if !q.AsOf.IsZero() {
		add("e.occurred_on <= $%d::date", q.AsOf)
	}

	var out []Balance
	err := tenancy.Scoped(ctx, l.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT p.account_code, a.account_type, p.party_kind, coalesce(p.party_id::text,''),
			       sum(p.signed_minor),
			       coalesce(sum(p.amount_minor) FILTER (WHERE p.side = 'debit'), 0),
			       coalesce(sum(p.amount_minor) FILTER (WHERE p.side = 'credit'), 0),
			       count(*)
			  FROM ledger_postings p
			  JOIN journal_entries e ON e.id = p.entry_id AND e.tenant_id = p.tenant_id
			  JOIN ledger_accounts a ON a.code = p.account_code
			 WHERE `+strings.Join(where, " AND ")+`
			 GROUP BY p.account_code, a.account_type, p.party_kind, p.party_id
			 ORDER BY p.account_code, p.party_kind`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b Balance
			var kind, accountType string
			if err := rows.Scan(&b.Account, &accountType, &kind, &b.Party.ID,
				&b.Amount, &b.Debits, &b.Credits, &b.Postings); err != nil {
				return err
			}
			b.AccountType, b.Party.Kind = domain.AccountType(accountType), domain.PartyKind(kind)
			out = append(out, b)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("money: deriving a balance: %w", err)
	}
	return out, nil
}

// Position is what a party owes on a lease: the receivable less any advance held
// against it. Positive means arrears.
func (l *Ledger) Position(ctx context.Context, lease string, asOf time.Time) (domain.Minor, error) {
	balances, err := l.Balances(ctx, BalanceQuery{Lease: lease, AsOf: asOf})
	if err != nil {
		return 0, err
	}
	// Both are signed sums, debits positive, so the advance — a liability, held as
	// a credit — subtracts itself.
	var owed domain.Minor
	for _, b := range balances {
		if b.Account == domain.TenantReceivable || b.Account == domain.TenantAdvance {
			owed += b.Amount
		}
	}
	return owed, nil
}

// BilledThrough is the last day a lease charge has been raised for, or zero when
// none has.
//
// source_kind is filtered for the reason ADR-0006 §5's amendment gives: lease_id now
// names the tenancy an entry concerns, payments included, so an unfiltered maximum
// would move "billed through" every time a tenant paid.
func (l *Ledger) BilledThrough(ctx context.Context, lease string) (time.Time, error) {
	var through *time.Time
	err := tenancy.Scoped(ctx, l.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT max(occurred_on) FROM journal_entries
			 WHERE lease_id = $1 AND source_kind = 'lease_charge'`, lease).Scan(&through)
	})
	if err != nil || through == nil {
		return time.Time{}, err
	}
	return *through, nil
}
