package store_test

import (
	"context"
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/store"
)

// The contract between the Go registry and the one in PostgreSQL. ADR-0023.
//
// The rules live in the database, so the schema file is the single author; the
// vocabulary lives in both, so this file is the price of that. Every difference
// fails a build, in either direction — because the failure otherwise is not a
// crash but a rule type the Go code can never resolve, or one the database will
// never accept, discovered by whoever eventually queries the number.

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set — skipping the statutory registry contract")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// The values a CHECK (col IN ('a', 'b')) admits, read out of the constraint
// definition. Reading the constraint rather than a table means the assertion is
// about what the database will accept, which is the thing that matters.
func admitted(t *testing.T, p *pgxpool.Pool, table, column string) []string {
	t.Helper()
	var def string
	err := p.QueryRow(context.Background(), `
		SELECT pg_get_constraintdef(c.oid)
		  FROM pg_constraint c
		  JOIN pg_class t ON t.oid = c.conrelid
		 WHERE t.relname = $1 AND c.contype = 'c'
		   AND pg_get_constraintdef(c.oid) LIKE '%' || $2 || ' = ANY%'
		 LIMIT 1`, table, column).Scan(&def)
	if err != nil {
		t.Fatalf("reading the CHECK on %s.%s: %v", table, column, err)
	}
	var out []string
	for _, m := range regexp.MustCompile(`'([a-z0-9_]+)'::text`).FindAllStringSubmatch(def, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

func TestTheGoVocabularyMatchesTheDatabase(t *testing.T) {
	p := pool(t)

	var goTypes []string
	for _, typ := range statutory.Types() {
		goTypes = append(goTypes, string(typ))
	}
	sort.Strings(goTypes)
	if got := admitted(t, p, "statutory_rules", "rule_type"); strings.Join(got, ",") != strings.Join(goTypes, ",") {
		t.Errorf("rule types differ\n  database: %v\n  Go:       %v\n"+
			"a type only the database admits is a rule nothing resolves; one only Go knows is a "+
			"rule that can never be written", got, goTypes)
	}

	for _, c := range []struct {
		column string
		inGo   []string
	}{
		{"value_kind", []string{
			string(statutory.KindRate), string(statutory.KindAmount),
			string(statutory.KindCount), string(statutory.KindSlabs)}},
		{"verification_status", []string{
			string(statutory.Verified), string(statutory.NeedsBareActCheck),
			string(statutory.Unverified), string(statutory.Conflicting)}},
		{"enforcement", []string{
			string(statutory.Block), string(statutory.Warn), string(statutory.RecordOnly)}},
	} {
		want := append([]string(nil), c.inGo...)
		sort.Strings(want)
		if got := admitted(t, p, "statutory_rules", c.column); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s differs\n  database: %v\n  Go:       %v", c.column, got, want)
		}
	}
}

// Every seeded row survives the Go validation, which is the round trip that
// matters: a row the schema accepted and the registry refuses is a rule the
// product cannot use, and the schema is where rules are authored.
func TestTheSeededRegistryLoadsAndValidates(t *testing.T) {
	ctx := context.Background()
	table, err := store.New(pool(t)).Table(ctx)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}
	if len(table.Rules()) == 0 {
		t.Fatal("the registry is empty — the seed did not reach this database")
	}
}

// The story's primary scenario against the real seed: the Finance Act 2025 raised
// the 194-I threshold, and a deduction computed for March 2025 must still resolve
// the old one.
func TestTheThresholdInForceIsTheOneOnTheDate(t *testing.T) {
	ctx := context.Background()
	table, err := store.New(pool(t)).Table(ctx)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}

	for _, c := range []struct {
		on   effective.Date
		want int64
	}{
		{effective.Day(2025, 3, 31), 24000000},
		{effective.Day(2025, 4, 1), 60000000},
	} {
		got, err := table.Resolve(statutory.TDSThreshold, statutory.National, "tds.194i_annual", c.on)
		if err != nil {
			t.Fatalf("resolving on %s: %v", c.on, err)
		}
		amount, err := got.Rule.Amount()
		if err != nil {
			t.Fatalf("reading the amount: %v", err)
		}
		if amount != c.want {
			t.Errorf("on %s the 194-I threshold resolved to %d, want %d", c.on, amount, c.want)
		}
	}
}

// One national row, four state overrides, and no copies of the other twenty-four.
func TestAStateOverridesTheCentralRuleAndTheRestFallBack(t *testing.T) {
	ctx := context.Background()
	table, err := store.New(pool(t)).Table(ctx)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}
	on := effective.Day(2026, 7, 1)

	special, err := table.Resolve(statutory.GSTRegistrationThreshold, statutory.State("MZ"),
		"gst.aggregate_turnover_services", on)
	if err != nil {
		t.Fatalf("resolving a special-category state: %v", err)
	}
	if special.Scope != statutory.ScopeState {
		t.Errorf("Mizoram resolved nationally, so its lower threshold is not being applied")
	}

	ordinary, err := table.Resolve(statutory.GSTRegistrationThreshold, statutory.State("KA"),
		"gst.aggregate_turnover_services", on)
	if err != nil {
		t.Fatalf("resolving an ordinary state: %v", err)
	}
	if ordinary.Scope != statutory.ScopeNational {
		t.Errorf("Karnataka resolved to a state row that should not exist")
	}

	a, _ := special.Rule.Amount()
	b, _ := ordinary.Rule.Amount()
	if a >= b {
		t.Errorf("the special-category threshold (%d) is not below the central one (%d)", a, b)
	}
}

// The deposit cap has no national row on purpose, so an unlisted state is a gap
// rather than the Model Tenancy Act's two months applied to a state that never
// adopted it.
func TestAnUnlistedStateHasNoDepositCapRatherThanADefault(t *testing.T) {
	ctx := context.Background()
	table, err := store.New(pool(t)).Table(ctx)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}

	if _, err := table.Resolve(statutory.DepositCapMonths, statutory.State("GA"),
		"deposit.residential", effective.Day(2026, 7, 1)); !errors.Is(err, statutory.ErrNoRule) {
		t.Fatalf("Goa resolved a deposit cap: %v", err)
	}
	if _, err := table.Resolve(statutory.DepositCapMonths, statutory.State("MH"),
		"deposit.residential", effective.Day(2026, 7, 1)); err != nil {
		t.Fatalf("Maharashtra, which has a row, did not resolve: %v", err)
	}
}

// The view and the Go filter answer the same question. Two implementations is one
// too many, and this is what keeps them from drifting.
func TestTheReviewReportAgreesWithTheRegistry(t *testing.T) {
	ctx := context.Background()
	s := store.New(pool(t))
	table, err := s.Table(ctx)
	if err != nil {
		t.Fatalf("loading the registry: %v", err)
	}
	fromView, err := s.ReviewDue(ctx)
	if err != nil {
		t.Fatalf("reading the review report: %v", err)
	}

	now := time.Now().UTC()
	today := effective.Day(now.Year(), now.Month(), now.Day())
	inGo := map[string]bool{}
	for _, r := range table.DueForReview(today, 30) {
		inGo[r.ID] = true
	}
	inView := map[string]bool{}
	for _, d := range fromView {
		inView[d.ID] = true
	}

	for id := range inView {
		if !inGo[id] {
			t.Errorf("rule %s is due for review in the view and not in the registry", id)
		}
	}
	for id := range inGo {
		if !inView[id] {
			t.Errorf("rule %s is due for review in the registry and not in the view", id)
		}
	}
}

// The registry has no runtime writer. This is the half the schema's assertion 18
// cannot check for the role a request actually connects as.
func TestARequestCannotWriteARule(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	_, err := p.Exec(ctx, `
		INSERT INTO statutory_rules (rule_type, jurisdiction, rule_key, value_kind, rate_bps,
		                             valid_from, statute_ref, owner, review_due)
		VALUES ('tds_rate', 'IN', 'tds.mine', 'rate', 0, DATE '2020-01-01', 'none', 'me', DATE '2030-01-01')`)
	if err == nil {
		t.Fatal("a request wrote a statutory rule — an organisation that can write a TDS rate can " +
			"decide its own tax, with no citation and no review")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "permission denied") {
		t.Errorf("the write failed for the wrong reason: %v", err)
	}
}
