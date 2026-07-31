package statutory_test

import (
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory"
)

// A rule builder with the governance columns filled in, so a test that is about
// dates does not repeat what a test about verification is for.
func rule(typ statutory.Type, j statutory.Jurisdiction, key string, iv effective.Interval) statutory.Rule {
	return statutory.Rule{
		ID: key + "@" + iv.String() + "/" + string(j), Type: typ, Jurisdiction: j, Key: key,
		Kind: statutory.KindRate, RateBps: 1800, Validity: iv,
		StatuteRef: "Notification 5/2022-CT(R)", Verification: statutory.NeedsBareActCheck,
		Owner: "compliance", ReviewDue: effective.Day(2027, 1, 1), Enforcement: statutory.Warn,
	}
}

func since(t *testing.T, y int, m time.Month, d int) effective.Interval {
	t.Helper()
	iv, err := effective.Since(effective.Day(y, m, d))
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	return iv
}

func between(t *testing.T, fromY int, fromM time.Month, fromD, toY int, toM time.Month, toD int) effective.Interval {
	t.Helper()
	iv, err := effective.Between(effective.Day(fromY, fromM, fromD), effective.Day(toY, toM, toD))
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	return iv
}

// The story's primary scenario, and the only one that matters on the day a rate
// changes: an invoice dated before the change resolves the old rate and one dated
// after resolves the new one, from the same table.
func TestARateChangeDoesNotRewriteTheInvoicesBeforeIt(t *testing.T) {
	old := rule(statutory.GSTRate, statutory.National, "gst.let", between(t, 2026, 4, 1, 2026, 10, 1))
	old.RateBps = 1800
	next := rule(statutory.GSTRate, statutory.National, "gst.let", since(t, 2026, 10, 1))
	next.RateBps = 500

	table, err := statutory.NewTable([]statutory.Rule{old, next})
	if err != nil {
		t.Fatalf("building the table: %v", err)
	}

	for _, c := range []struct {
		name string
		on   effective.Date
		want int
	}{
		{"the invoice before the change", effective.Day(2026, 9, 15), 1800},
		{"the day before the change", effective.Day(2026, 9, 30), 1800},
		{"the day the change takes effect", effective.Day(2026, 10, 1), 500},
		{"the invoice after the change", effective.Day(2026, 10, 15), 500},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := table.Resolve(statutory.GSTRate, statutory.National, "gst.let", c.on)
			if err != nil {
				t.Fatalf("resolving on %s: %v", c.on, err)
			}
			rate, err := got.Rule.Rate()
			if err != nil {
				t.Fatalf("reading the rate: %v", err)
			}
			if rate != c.want {
				t.Errorf("on %s the rate resolved to %d bps, want %d — a recomputation of an old "+
					"invoice must use the rule that was in force when it was raised", c.on, rate, c.want)
			}
		})
	}
}

// The failure scenario. No default, no nearest date, no most-recent-anywhere: a
// gap names itself, because a calculation that proceeds with an unauthorised
// number is worse than one that stops.
func TestAMissingRuleFailsLoudlyRatherThanDefaulting(t *testing.T) {
	r := rule(statutory.TDSRate, statutory.National, "tds.194i", since(t, 2025, 4, 1))
	table, err := statutory.NewTable([]statutory.Rule{r})
	if err != nil {
		t.Fatalf("building the table: %v", err)
	}

	_, err = table.Resolve(statutory.TDSRate, statutory.National, "tds.194i", effective.Day(2024, 1, 1))
	if !errors.Is(err, statutory.ErrNoRule) {
		t.Fatalf("a date before the first rule resolved to %v, want a named gap", err)
	}
	var gap *statutory.Gap
	if !errors.As(err, &gap) {
		t.Fatalf("the error is not a *Gap: %v", err)
	}
	if gap.Type != statutory.TDSRate || gap.Key != "tds.194i" || !gap.On.Equal(effective.Day(2024, 1, 1)) {
		t.Errorf("the gap does not name what was asked: %+v", gap)
	}
}

// A national rule is stored once. Twenty-eight copies of the same number is
// twenty-eight rows to update on the day it changes, and one of them will be
// missed.
func TestANationalRuleIsNotReplicatedPerState(t *testing.T) {
	national := rule(statutory.GSTRegistrationThreshold, statutory.National, "gst.threshold", since(t, 2017, 7, 1))
	national.Kind = statutory.KindAmount
	national.RateBps = 0
	national.AmountMinor = 200000000
	special := national
	special.ID = "special"
	special.Jurisdiction = statutory.State("MZ")
	special.AmountMinor = 100000000

	table, err := statutory.NewTable([]statutory.Rule{national, special})
	if err != nil {
		t.Fatalf("building the table: %v", err)
	}

	on := effective.Day(2026, 7, 1)

	got, err := table.Resolve(statutory.GSTRegistrationThreshold, statutory.State("KA"), "gst.threshold", on)
	if err != nil {
		t.Fatalf("resolving for a state with no row: %v", err)
	}
	if got.Scope != statutory.ScopeNational {
		t.Errorf("a state with no row resolved with scope %q, want %q", got.Scope, statutory.ScopeNational)
	}
	if amount, _ := got.Rule.Amount(); amount != 200000000 {
		t.Errorf("the national threshold resolved to %d, want 200000000", amount)
	}

	got, err = table.Resolve(statutory.GSTRegistrationThreshold, statutory.State("MZ"), "gst.threshold", on)
	if err != nil {
		t.Fatalf("resolving for the state that overrides: %v", err)
	}
	if got.Scope != statutory.ScopeState {
		t.Errorf("a state with its own row resolved with scope %q, want %q", got.Scope, statutory.ScopeState)
	}
	if amount, _ := got.Rule.Amount(); amount != 100000000 {
		t.Errorf("the state threshold resolved to %d, want 100000000", amount)
	}
}

// The deposit cap is the case the fallback must not cover: the Model Tenancy Act
// is adopted state by state, so an unlisted state has no cap rather than the
// model's two months.
func TestNoNationalRowMeansAnUnlistedStateIsAGapAndNotADefault(t *testing.T) {
	mh := rule(statutory.DepositCapMonths, statutory.State("MH"), "deposit.residential", since(t, 2020, 4, 1))
	mh.Kind = statutory.KindCount
	mh.RateBps = 0
	mh.CountValue = 2

	table, err := statutory.NewTable([]statutory.Rule{mh})
	if err != nil {
		t.Fatalf("building the table: %v", err)
	}

	_, err = table.Resolve(statutory.DepositCapMonths, statutory.State("GA"), "deposit.residential",
		effective.Day(2026, 7, 1))
	if !errors.Is(err, statutory.ErrNoRule) {
		t.Fatalf("an unlisted state resolved to %v, want a gap", err)
	}
	var gap *statutory.Gap
	if errors.As(err, &gap) && !gap.Fallback {
		t.Error("the gap does not record that the national rule was tried, so a reader cannot tell " +
			"'this state has no row' from 'nobody has a row'")
	}
}

// Two rules true on the same day means the answer depends on which sorted first.
// The schema refuses this with an exclusion constraint; a table built in Go never
// went through the schema.
func TestTwoRulesTrueOnTheSameDayAreRefused(t *testing.T) {
	a := rule(statutory.GSTRate, statutory.National, "gst.let", since(t, 2026, 4, 1))
	b := rule(statutory.GSTRate, statutory.National, "gst.let", since(t, 2026, 10, 1))
	b.ID = "b"

	if _, err := statutory.NewTable([]statutory.Rule{a, b}); !errors.Is(err, effective.ErrOverlap) {
		t.Fatalf("two overlapping rules were accepted: %v", err)
	}
}

// An entry past its review date raises an operational warning — the story's
// second edge case. It still resolves: an overdue rule is a reminder with an
// owner on it, not a service that stops computing rent.
func TestARuleApproachingItsReviewDateIsReported(t *testing.T) {
	soon := rule(statutory.GSTRate, statutory.National, "gst.soon", since(t, 2020, 1, 1))
	soon.ReviewDue = effective.Day(2026, 8, 10)
	overdue := rule(statutory.TDSRate, statutory.National, "tds.overdue", since(t, 2020, 1, 1))
	overdue.ReviewDue = effective.Day(2026, 1, 1)
	later := rule(statutory.TDSThreshold, statutory.National, "tds.later", since(t, 2020, 1, 1))
	later.ReviewDue = effective.Day(2027, 1, 1)
	superseded := rule(statutory.GSTRate, statutory.National, "gst.old", between(t, 2020, 1, 1, 2021, 1, 1))
	superseded.ReviewDue = effective.Day(2020, 6, 1)

	table, err := statutory.NewTable([]statutory.Rule{soon, overdue, later, superseded})
	if err != nil {
		t.Fatalf("building the table: %v", err)
	}

	today := effective.Day(2026, 7, 31)
	due := table.DueForReview(today, 30)
	var keys []string
	for _, r := range due {
		keys = append(keys, r.Key)
	}
	if len(keys) != 2 || keys[0] != "tds.overdue" || keys[1] != "gst.soon" {
		t.Fatalf("the review report is %v, want the overdue rule first and the one due within 30 days "+
			"second — and neither the rule due next year nor the superseded one", keys)
	}

	// And it still resolves, overdue or not.
	if _, err := table.Resolve(statutory.TDSRate, statutory.National, "tds.overdue", today); err != nil {
		t.Errorf("an overdue rule stopped resolving: %v — a missed review must warn, not break rent", err)
	}
}

// india-property-compliance.md §1.1: an unverified row may never block. The
// schema has the same CHECK, and this is the half that catches a rule assembled
// in code.
func TestAnUnverifiedRuleCannotBlock(t *testing.T) {
	r := rule(statutory.DepositCapMonths, statutory.State("KA"), "deposit.residential", since(t, 2026, 1, 1))
	r.Kind = statutory.KindCount
	r.RateBps = 0
	r.CountValue = 2
	r.Verification = statutory.Unverified
	r.Enforcement = statutory.Block

	if err := r.Validate(); err == nil {
		t.Fatal("an unverified rule was accepted as blocking — a cap enforced from a blog post is " +
			"worse than no cap, because it is wrong with authority")
	}

	r.Enforcement = statutory.Warn
	if err := r.Validate(); err != nil {
		t.Fatalf("the same rule set to warn was refused: %v", err)
	}
	if r.Blocks() {
		t.Error("Blocks() is true for an unverified rule")
	}
}

func TestARuleWithNoOwnerOrCitationIsRefused(t *testing.T) {
	for _, c := range []struct {
		name   string
		break_ func(*statutory.Rule)
	}{
		{"no citation", func(r *statutory.Rule) { r.StatuteRef = "  " }},
		{"no owner", func(r *statutory.Rule) { r.Owner = "" }},
		{"no review date", func(r *statutory.Rule) { r.ReviewDue = effective.Date{} }},
		{"a review date before it takes effect", func(r *statutory.Rule) { r.ReviewDue = effective.Day(2019, 1, 1) }},
		{"verified by nobody", func(r *statutory.Rule) { r.Verification = statutory.Verified }},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := rule(statutory.GSTRate, statutory.National, "gst.let", since(t, 2020, 1, 1))
			c.break_(&r)
			if err := r.Validate(); err == nil {
				t.Error("accepted")
			}
		})
	}
}

// A scale with a hole resolves to nothing for an amount that falls in it, which
// is a failure a customer finds. The schema's trigger says the same thing; this
// is the copy that runs before a row is ever written.
func TestASlabScaleMustCoverEveryAmount(t *testing.T) {
	base := rule(statutory.StampDutyRate, statutory.State("KA"), "stamp.leave_and_licence", since(t, 2020, 1, 1))
	base.Kind = statutory.KindSlabs
	base.RateBps = 0

	holed := base
	holed.Slabs = []statutory.Slab{
		{Seq: 0, LowerMinor: 0, UpperMinor: 1000000, RateBps: 50},
		{Seq: 1, LowerMinor: 2000000, Top: true, RateBps: 100},
	}
	if err := holed.Validate(); err == nil {
		t.Error("a scale with a gap between ₹10,000 and ₹20,000 was accepted")
	}

	capped := base
	capped.Slabs = []statutory.Slab{
		{Seq: 0, LowerMinor: 0, UpperMinor: 1000000, RateBps: 50},
		{Seq: 1, LowerMinor: 1000000, UpperMinor: 2000000, RateBps: 100},
	}
	if err := capped.Validate(); err == nil {
		t.Error("a scale with no top band was accepted — an amount above ₹20,000 resolves to nothing")
	}

	good := base
	good.Slabs = []statutory.Slab{
		{Seq: 0, LowerMinor: 0, UpperMinor: 1000000, RateBps: 50},
		{Seq: 1, LowerMinor: 1000000, Top: true, RateBps: 100},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("a covering scale was refused: %v", err)
	}
	band, err := good.Band(1000000)
	if err != nil {
		t.Fatalf("resolving the band at the boundary: %v", err)
	}
	// Half-open, like every other interval here: the bound belongs to the band
	// above it.
	if band.Seq != 1 {
		t.Errorf("₹10,000 resolved to band %d, want 1 — [lower, upper) means the bound is the next "+
			"band's floor", band.Seq)
	}
}

// Asking a rate rule for an amount is a bug, not a zero.
func TestAValueIsOnlyReadableAsWhatItIs(t *testing.T) {
	r := rule(statutory.GSTRate, statutory.National, "gst.let", since(t, 2020, 1, 1))
	if _, err := r.Amount(); !errors.Is(err, statutory.ErrWrongKind) {
		t.Errorf("a rate rule answered an amount query with %v", err)
	}
	if _, err := r.Count(); !errors.Is(err, statutory.ErrWrongKind) {
		t.Errorf("a rate rule answered a count query with %v", err)
	}
}
