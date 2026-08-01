package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Periods is the period close (#190): the append-only close/reopen history,
// the pre-close checklist, and the audit pack. Enforcement itself lives in
// the database — the journal_entries_period_guard trigger — because the store
// is one of several writers and a rule enforced in Go binds only the Go that
// remembered.
type Periods struct{ pool tenancy.Pool }

func NewPeriods(pool tenancy.Pool) *Periods { return &Periods{pool: pool} }

// PeriodState is what a month is right now, per its latest history row.
type PeriodState struct {
	Month   time.Time // first day of the month, UTC
	Closed  bool
	Actor   string
	Reason  string
	ActedAt time.Time
	// History is every close and reopen, oldest first. A month that needed
	// three of them is a month somebody will ask about.
	History []PeriodAction
}

type PeriodAction struct {
	Action  string    `json:"action"`
	Actor   string    `json:"actor"`
	Reason  string    `json:"reason"`
	ActedAt time.Time `json:"acted_at"`
}

// Blocker is one pre-close check that refused.
type Blocker struct {
	Check  string `json:"check"`
	Count  int    `json:"count"`
	Detail string `json:"detail"`
}

// ErrBlocked carries the checklist that refused a close. Typed so the HTTP
// layer can render the list — a close blocked over a known gap must name the
// gap, not just say no.
type ErrBlocked struct{ Blockers []Blocker }

func (e ErrBlocked) Error() string {
	return fmt.Sprintf("period close blocked by %d outstanding check(s)", len(e.Blockers))
}

var (
	// ErrAlreadyClosed refuses a second close; ErrNotClosed refuses a reopen
	// of a month that is open. Both are conflicts, not races — the history
	// table's latest row is the state, and these keep it readable.
	ErrAlreadyClosed = errors.New("store: the period is already closed")
	ErrNotClosed     = errors.New("store: the period is not closed")
)

// State reads one month's standing.
func (s *Periods) State(ctx context.Context, month time.Time) (PeriodState, error) {
	out := PeriodState{Month: month}
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT action, actor, reason, acted_at
			  FROM ledger_period_closes
			 WHERE period_month = $1
			 ORDER BY acted_at, id`, month)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a PeriodAction
			if err := rows.Scan(&a.Action, &a.Actor, &a.Reason, &a.ActedAt); err != nil {
				return err
			}
			out.History = append(out.History, a)
		}
		return rows.Err()
	})
	if err != nil {
		return PeriodState{}, fmt.Errorf("reading the period state: %w", err)
	}
	if n := len(out.History); n > 0 {
		last := out.History[n-1]
		out.Closed = last.Action == "closed"
		out.Actor, out.Reason, out.ActedAt = last.Actor, last.Reason, last.ActedAt
	}
	return out, nil
}

// Close runs the checklist and, when it is clean, writes the close row. One
// transaction: the checks and the close see the same snapshot, so a payment
// that lands between them cannot slip under a close that already decided.
func (s *Periods) Close(ctx context.Context, month time.Time, actor, reason string) error {
	if reason == "" {
		reason = "month-end close"
	}
	end := month.AddDate(0, 1, 0)
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		closed, err := latestIsClosed(ctx, tx, month)
		if err != nil {
			return err
		}
		if closed {
			return ErrAlreadyClosed
		}

		blockers, err := checklist(ctx, tx, end)
		if err != nil {
			return err
		}
		if len(blockers) > 0 {
			return ErrBlocked{Blockers: blockers}
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO ledger_period_closes (tenant_id, period_month, action, actor, reason)
			VALUES (current_tenant_id(), $1, 'closed', $2, $3)`, month, actor, reason)
		if err != nil {
			return fmt.Errorf("writing the close: %w", err)
		}
		return nil
	})
}

// Reopen is rare, authorised at the route, and audited by construction: the
// reason lands in the same history the close did, and the schema refuses a
// reopen reason under eight characters.
func (s *Periods) Reopen(ctx context.Context, month time.Time, actor, reason string) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		closed, err := latestIsClosed(ctx, tx, month)
		if err != nil {
			return err
		}
		if !closed {
			return ErrNotClosed
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO ledger_period_closes (tenant_id, period_month, action, actor, reason)
			VALUES (current_tenant_id(), $1, 'reopened', $2, $3)`, month, actor, reason)
		if err != nil {
			return fmt.Errorf("writing the reopen: %w", err)
		}
		return nil
	})
}

func latestIsClosed(ctx context.Context, tx pgx.Tx, month time.Time) (bool, error) {
	var closed bool
	err := tx.QueryRow(ctx, `
		SELECT coalesce((
			SELECT action = 'closed' FROM ledger_period_closes
			 WHERE period_month = $1
			 ORDER BY acted_at DESC, id DESC LIMIT 1), false)`, month).Scan(&closed)
	if err != nil {
		return false, fmt.Errorf("reading the latest period action: %w", err)
	}
	return closed, nil
}

// checklist is the pre-close gate, over what this organisation can see.
//
// Two checks exist today. Two more are owed and have nowhere to look yet:
// held payouts wait on the payout run (#80), and unposted supplier bills wait
// on the bills subsystem (#186/#187) — each lands here as one more query the
// day its table exists.
func checklist(ctx context.Context, tx pgx.Tx, before time.Time) ([]Blocker, error) {
	var out []Blocker
	for _, c := range []struct {
		name, detail, sql string
	}{
		{
			name:   "settlement_lines_unposted",
			detail: "settlement lines attributed to this organisation but not yet posted — money the provider settled and the ledger does not show",
			sql: `SELECT count(*) FROM settlement_lines
			       WHERE settled_on < $1 AND (entry_id IS NULL OR match_class IS NULL)`,
		},
		{
			name:   "settlement_drift_open",
			detail: "open reconciliation drift touching this organisation — a disagreement with the provider's statement that would be closed over",
			sql: `SELECT count(*) FROM settlement_drift
			       WHERE state = 'open' AND as_of_date < $1`,
		},
	} {
		var n int
		if err := tx.QueryRow(ctx, c.sql, before).Scan(&n); err != nil {
			return nil, fmt.Errorf("pre-close check %s: %w", c.name, err)
		}
		if n > 0 {
			out = append(out, Blocker{Check: c.name, Count: n, Detail: c.detail})
		}
	}
	return out, nil
}

// IsClosedPeriod reports whether an error is the period guard refusing a
// posting dated inside a closed month — the journal_entries_period_guard
// trigger. Callers use it to offer the correction path instead of a plain
// failure, which is #190's primary scenario.
func IsClosedPeriod(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "P0001" &&
		strings.Contains(pg.Message, "closed period")
}

// ReverseAsOf is the closed-period correction: domain.Reverse dated on rather
// than at the original — the original's month is locked, so the correcting
// entry lands in the period that is open, still referencing what it corrects.
// For an open month, plain Reverse is right: a reversal nets out in the period
// the mistake was made.
func (l *Ledger) ReverseAsOf(ctx context.Context, originalID string,
	reason domain.ReversalReason, key string, on time.Time) (Posted, error) {
	original, _, err := l.read(ctx, `e.id = $1`, originalID)
	if err != nil {
		return Posted{}, err
	}
	reversal, err := domain.Reverse(original, originalID, reason, key)
	if err != nil {
		return Posted{}, err
	}
	reversal.OccurredOn = on
	return l.Post(ctx, reversal)
}

// AuditPack is the month, exportable: what an owner's chartered accountant
// asks for, reconciling to what the organisation was already shown.
type AuditPack struct {
	Month          string         `json:"month"`
	GeneratedAt    time.Time      `json:"generated_at"`
	State          []PeriodAction `json:"close_history"`
	TrialBalance   []TrialLine    `json:"trial_balance"`
	Entries        []PackEntry    `json:"entries"`
	Reconciliation ReconSummary   `json:"reconciliation"`
}

type TrialLine struct {
	AccountCode string `json:"account_code"`
	AccountName string `json:"account_name"`
	AccountType string `json:"account_type"`
	DebitMinor  int64  `json:"debit_amount_minor"`
	CreditMinor int64  `json:"credit_amount_minor"`
}

type PackEntry struct {
	ID             string        `json:"id"`
	Kind           string        `json:"kind"`
	OccurredOn     string        `json:"occurred_on"`
	PostedAt       time.Time     `json:"posted_at"`
	SourceKind     string        `json:"source_kind"`
	SourceID       string        `json:"source_id"`
	Memo           string        `json:"memo,omitempty"`
	ReversesEntry  string        `json:"reverses_entry_id,omitempty"`
	ReversalReason string        `json:"reversal_reason,omitempty"`
	Postings       []PackPosting `json:"postings"`
}

type PackPosting struct {
	AccountCode string `json:"account_code"`
	Side        string `json:"side"`
	AmountMinor int64  `json:"amount_minor"`
	Memo        string `json:"memo,omitempty"`
}

// ReconSummary is reconciliation as this organisation may see it: its own
// lines by match class, and its own drift. Batch totals span every
// organisation and belong to none (060's policy), so they are deliberately
// not here.
type ReconSummary struct {
	LinesByMatchClass map[string]int `json:"lines_by_match_class"`
	DriftOpen         int            `json:"drift_open"`
	DriftResolved     int            `json:"drift_resolved"`
}

// Pack assembles the audit pack for one month.
func (s *Periods) Pack(ctx context.Context, month time.Time) (AuditPack, error) {
	end := month.AddDate(0, 1, 0)
	out := AuditPack{Month: month.Format("2006-01"), GeneratedAt: time.Now().UTC()}

	state, err := s.State(ctx, month)
	if err != nil {
		return AuditPack{}, err
	}
	out.State = state.History

	err = tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if out.TrialBalance, err = trialBalance(ctx, tx, month, end); err != nil {
			return err
		}
		if out.Entries, err = packEntries(ctx, tx, month, end); err != nil {
			return err
		}
		out.Reconciliation, err = reconSummary(ctx, tx, month, end)
		return err
	})
	if err != nil {
		return AuditPack{}, fmt.Errorf("assembling the audit pack: %w", err)
	}
	return out, nil
}

func trialBalance(ctx context.Context, tx pgx.Tx, from, to time.Time) ([]TrialLine, error) {
	rows, err := tx.Query(ctx, `
		SELECT p.account_code, a.name, a.account_type,
		       coalesce(sum(p.amount_minor) FILTER (WHERE p.side = 'debit'), 0),
		       coalesce(sum(p.amount_minor) FILTER (WHERE p.side = 'credit'), 0)
		  FROM ledger_postings p
		  JOIN journal_entries e ON e.id = p.entry_id AND e.tenant_id = p.tenant_id
		  JOIN ledger_accounts a ON a.code = p.account_code
		 WHERE e.occurred_on >= $1 AND e.occurred_on < $2
		 GROUP BY 1, 2, 3
		 ORDER BY 1`, from, to)
	if err != nil {
		return nil, fmt.Errorf("trial balance: %w", err)
	}
	defer rows.Close()
	var out []TrialLine
	for rows.Next() {
		var l TrialLine
		if err := rows.Scan(&l.AccountCode, &l.AccountName, &l.AccountType,
			&l.DebitMinor, &l.CreditMinor); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func packEntries(ctx context.Context, tx pgx.Tx, from, to time.Time) ([]PackEntry, error) {
	rows, err := tx.Query(ctx, `
		SELECT e.id, e.entry_kind, e.occurred_on, e.posted_at, e.source_kind, e.source_id,
		       coalesce(e.memo, ''), coalesce(e.reverses_entry_id::text, ''),
		       coalesce(e.reversal_reason, ''),
		       p.account_code, p.side, p.amount_minor, coalesce(p.memo, '')
		  FROM journal_entries e
		  JOIN ledger_postings p ON p.entry_id = e.id AND p.tenant_id = e.tenant_id
		 WHERE e.occurred_on >= $1 AND e.occurred_on < $2
		 ORDER BY e.occurred_on, e.posted_at, e.id, p.side DESC, p.account_code`, from, to)
	if err != nil {
		return nil, fmt.Errorf("ledger detail: %w", err)
	}
	defer rows.Close()

	var out []PackEntry
	for rows.Next() {
		var (
			e          PackEntry
			p          PackPosting
			occurredOn time.Time
		)
		if err := rows.Scan(&e.ID, &e.Kind, &occurredOn, &e.PostedAt, &e.SourceKind, &e.SourceID,
			&e.Memo, &e.ReversesEntry, &e.ReversalReason,
			&p.AccountCode, &p.Side, &p.AmountMinor, &p.Memo); err != nil {
			return nil, err
		}
		e.OccurredOn = occurredOn.Format("2006-01-02")
		if n := len(out); n > 0 && out[n-1].ID == e.ID {
			out[n-1].Postings = append(out[n-1].Postings, p)
			continue
		}
		e.Postings = []PackPosting{p}
		out = append(out, e)
	}
	return out, rows.Err()
}

func reconSummary(ctx context.Context, tx pgx.Tx, from, to time.Time) (ReconSummary, error) {
	out := ReconSummary{LinesByMatchClass: map[string]int{}}
	rows, err := tx.Query(ctx, `
		SELECT coalesce(match_class, 'unmatched'), count(*)
		  FROM settlement_lines
		 WHERE settled_on >= $1 AND settled_on < $2
		 GROUP BY 1`, from, to)
	if err != nil {
		return out, fmt.Errorf("reconciliation summary: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var class string
		var n int
		if err := rows.Scan(&class, &n); err != nil {
			return out, err
		}
		out.LinesByMatchClass[class] = n
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	err = tx.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE state = 'open'),
		       count(*) FILTER (WHERE state <> 'open')
		  FROM settlement_drift
		 WHERE as_of_date >= $1 AND as_of_date < $2`, from, to).
		Scan(&out.DriftOpen, &out.DriftResolved)
	if err != nil {
		return out, fmt.Errorf("drift summary: %w", err)
	}
	return out, nil
}
