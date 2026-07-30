package store_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
)

// The drift check between the two copies of ADR-0006's rule.
//
// The chart of accounts and the posting templates exist in Go, so the module can
// compute an entry without a round trip, and in PostgreSQL, so the rule is
// inspectable in the database a dispute is being argued against. Two copies is a
// deliberate choice and this file is its price: every difference between them
// fails a build, in either direction.
//
// Without this the failure mode is not a crash. It is an account the Go code
// posts to and the reports have stopped summing, or a template whose GST line
// exists in one place and not the other — visible only as a number somebody
// eventually queries.

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set — skipping the ledger catalogue contract")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestTheGoChartMatchesTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	rows, err := p.Query(ctx, `
		SELECT code, name, account_type, normal_side, party_kind
		  FROM ledger_accounts ORDER BY code`)
	if err != nil {
		t.Fatalf("reading the chart: %v", err)
	}
	defer rows.Close()

	inDB := map[string]domain.Account{}
	normalSides := map[string]string{}
	for rows.Next() {
		var code, name, kind, side, party string
		if err := rows.Scan(&code, &name, &kind, &side, &party); err != nil {
			t.Fatalf("scan: %v", err)
		}
		inDB[code] = domain.Account{
			Code: code, Name: name,
			Type: domain.AccountType(kind), Party: domain.PartyKind(party),
		}
		normalSides[code] = side
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the chart: %v", err)
	}

	inGo := map[string]domain.Account{}
	for _, a := range domain.Chart() {
		inGo[a.Code] = a
	}

	for code, want := range inGo {
		got, ok := inDB[code]
		if !ok {
			t.Errorf("account %q is in the Go chart and not in ledger_accounts — every posting to it "+
				"would be refused by the foreign key, at runtime", code)
			continue
		}
		if got != want {
			t.Errorf("account %q differs:\n  database %+v\n  Go       %+v", code, got, want)
		}
		// The normal side is generated in both places from the same rule. If the
		// two ever disagree, every report that assumes the sign is backwards.
		if normalSides[code] != string(want.NormalSide()) {
			t.Errorf("account %q: the database's normal side is %q and Go's is %q",
				code, normalSides[code], want.NormalSide())
		}
	}
	for code := range inDB {
		if _, ok := inGo[code]; !ok {
			t.Errorf("account %q is in ledger_accounts and not in the Go chart — the module cannot "+
				"post to it, and a report that sums it will read zero forever", code)
		}
	}
}

func TestTheGoTemplatesMatchTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	rows, err := p.Query(ctx, `
		SELECT event_kind, seq, account_code, side, amount_role, optional
		  FROM posting_template_lines WHERE version = 1
		 ORDER BY event_kind, seq`)
	if err != nil {
		t.Fatalf("reading the templates: %v", err)
	}
	defer rows.Close()

	inDB := map[domain.EventKind][]domain.Line{}
	for rows.Next() {
		var kind, account, side, role string
		var seq int
		var optional bool
		if err := rows.Scan(&kind, &seq, &account, &side, &role, &optional); err != nil {
			t.Fatalf("scan: %v", err)
		}
		inDB[domain.EventKind(kind)] = append(inDB[domain.EventKind(kind)], domain.Line{
			Account: account, Side: domain.Side(side), Role: domain.Role(role), Optional: optional,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the templates: %v", err)
	}

	for _, kind := range domain.Kinds() {
		want, _ := domain.Template(kind)
		got, ok := inDB[kind]
		if !ok {
			t.Errorf("template %q exists in Go and not in posting_template_lines", kind)
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("template %q differs:\n  database %v\n  Go       %v", kind, got, want)
		}
	}
	for kind := range inDB {
		if _, ok := domain.Template(kind); !ok {
			t.Errorf("template %q exists in the database and not in Go", kind)
		}
	}

	// 'reversal' is the one event kind with a row in posting_templates and no
	// lines anywhere: it is the original entry with every side flipped, not a
	// rule about accounts. Asserting its emptiness is what stops somebody
	// helpfully giving it lines.
	var reversalLines int
	if err := p.QueryRow(ctx,
		`SELECT count(*) FROM posting_template_lines WHERE event_kind = 'reversal'`).Scan(&reversalLines); err != nil {
		t.Fatalf("counting reversal lines: %v", err)
	}
	if reversalLines != 0 {
		t.Errorf("the reversal template has %d lines — a reversal mirrors the entry it corrects, "+
			"and a template would let the two diverge", reversalLines)
	}
}

// The vocabularies: an event kind or a reversal reason that Go can produce and
// the schema's CHECK constraint refuses is an error nobody sees until the entry
// that needs it is written.
func TestTheGoVocabulariesAreAcceptedByTheSchema(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	def := func(name string) string {
		var out string
		if err := p.QueryRow(ctx,
			`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, name).Scan(&out); err != nil {
			t.Fatalf("reading constraint %s: %v", name, err)
		}
		return out
	}

	kinds := def("journal_entries_kind")
	var goKinds []string
	for _, k := range domain.Kinds() {
		goKinds = append(goKinds, string(k))
	}
	goKinds = append(goKinds, string(domain.KindReversal))
	sort.Strings(goKinds)
	for _, k := range goKinds {
		if !strings.Contains(kinds, "'"+k+"'") {
			t.Errorf("event kind %q is producible in Go and refused by journal_entries_kind", k)
		}
	}

	reasons := def("journal_entries_reversal_reason_check")
	for _, r := range []domain.ReversalReason{
		domain.ReasonDuplicate, domain.ReasonWrongAmount, domain.ReasonWrongAccount,
		domain.ReasonWrongParty, domain.ReasonWrongPeriod, domain.ReasonProviderChargeback,
		domain.ReasonOperatorError, domain.ReasonSettlementMismatch,
	} {
		if !strings.Contains(reasons, "'"+string(r)+"'") {
			t.Errorf("reversal reason %q is producible in Go and refused by the schema", r)
		}
	}
}
