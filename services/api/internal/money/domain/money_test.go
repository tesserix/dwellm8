package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// The money standard, ADR-0007. The properties asserted here are the ones a
// disputed statement is argued over: that a split adds up, that a reversal
// reverses to the paisa, and that no amount changes value on its way through the
// wire.

// The issue's primary scenario: a monthly rent of ₹27,500 prorated across 17 of
// 31 days. The exact share is 1,508,064.516… paise, so the standard's
// half-away-from-zero rule takes it up, and the two slices of the month sum to
// the whole month.
func TestProratedRentAcrossSeventeenOfThirtyOneDays(t *testing.T) {
	const rent Minor = 2_750_000 // ₹27,500

	got, err := Prorate(rent, 17, 31)
	if err != nil {
		t.Fatalf("Prorate: %v", err)
	}
	if want := Minor(1_508_065); got != want {
		t.Errorf("17/31 of %s = %d paise, want %d", rent, got, want)
	}
	if got.Rupees() != "15080.65" {
		t.Errorf("rendered %q, want %q", got.Rupees(), "15080.65")
	}

	// Both sides of the split, posted together: the halves must reconstruct the
	// month exactly, which independently rounded slices do not.
	slices, err := Allocate(rent, []int64{17, 14})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	var total Minor
	for _, s := range slices {
		total += s
	}
	if total != rent {
		t.Errorf("slices %v sum to %s, want %s", slices, total, rent)
	}
	if slices[0] != 1_508_065 || slices[1] != 1_241_935 {
		t.Errorf("split %v, want [1508065 1241935]", slices)
	}
}

// Every day of a month, allocated one slice each, still sums to the month. This
// is the property that matters more than any single expected number: 31 slices
// of a value that divides evenly into none of them.
func TestAllocationLosesNothingAcrossAWholeMonth(t *testing.T) {
	for _, rent := range []Minor{2_750_000, 1, 99, 100_000_01, 7} {
		for _, days := range []int{28, 29, 30, 31, 365} {
			slices, err := AllocateEqually(rent, days)
			if err != nil {
				t.Fatalf("AllocateEqually(%s, %d): %v", rent, days, err)
			}
			var total Minor
			for _, s := range slices {
				total += s
			}
			if total != rent {
				t.Errorf("%s across %d days sums to %s", rent, days, total)
			}
			// No slice may be more than a paisa from any other, or the split is
			// not the fair one it claims to be.
			if slices[0]-slices[len(slices)-1] > 1 {
				t.Errorf("%s across %d days spread from %s to %s",
					rent, days, slices[0], slices[len(slices)-1])
			}
		}
	}
}

func TestAllocationFollowsTheWeightsAndBreaksTiesEarliest(t *testing.T) {
	// 100 paise, three equal shares: 33.33 each and a paisa over. The extra
	// goes to the first slice, deterministically, so re-running the split
	// produces the same rows rather than a different distribution each time.
	got, err := AllocateEqually(100, 3)
	if err != nil {
		t.Fatalf("AllocateEqually: %v", err)
	}
	if got[0] != 34 || got[1] != 33 || got[2] != 33 {
		t.Errorf("100 across 3 = %v, want [34 33 33]", got)
	}

	// Weights need no scale: square feet work the same as days.
	byArea, err := Allocate(1_000_000, []int64{450, 900, 650})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	var total Minor
	for _, s := range byArea {
		total += s
	}
	if total != 1_000_000 {
		t.Errorf("area split %v sums to %s", byArea, total)
	}
	if byArea[1] <= byArea[2] || byArea[2] <= byArea[0] {
		t.Errorf("area split %v does not follow the weights", byArea)
	}

	// A zero weight is a party with no share, not a party with a paisa.
	withZero, err := Allocate(101, []int64{1, 0, 1})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if withZero[1] != 0 {
		t.Errorf("zero weight got %s", withZero[1])
	}
}

func TestAllocationRejectsWhatCannotBeSplit(t *testing.T) {
	for name, weights := range map[string][]int64{
		"no slices":        {},
		"all zero":         {0, 0},
		"negative weight":  {5, -1},
		"negative weights": {-1},
	} {
		if _, err := Allocate(1000, weights); err == nil {
			t.Errorf("%s: split accepted", name)
		}
	}
}

// The rounding rule is symmetric about zero, which is what lets a reversal
// reverse exactly. A half-up rule would round −0.5 to 0 and +0.5 to 1, and the
// pair would leave a stranded paisa.
func TestRoundingIsHalfAwayFromZeroAndSymmetric(t *testing.T) {
	cases := []struct {
		amount Minor
		num    int64
		den    int64
		want   Minor
	}{
		{1, 1, 2, 1},    // 0.5 → 1, away from zero
		{3, 1, 2, 2},    // 1.5 → 2
		{5, 1, 2, 3},    // 2.5 → 3, not 2: this is where banker's rounding differs
		{1, 1, 3, 0},    // 0.333 → 0
		{2, 1, 3, 1},    // 0.667 → 1
		{100, 1, 8, 13}, // 12.5 → 13
	}
	for _, c := range cases {
		got, err := mulDivRound(c.amount, c.num, c.den)
		if err != nil {
			t.Fatalf("mulDivRound(%s, %d, %d): %v", c.amount, c.num, c.den, err)
		}
		if got != c.want {
			t.Errorf("%s × %d ÷ %d = %s, want %s", c.amount, c.num, c.den, got, c.want)
		}
		negated, err := mulDivRound(-c.amount, c.num, c.den)
		if err != nil {
			t.Fatalf("mulDivRound(-%s, %d, %d): %v", c.amount, c.num, c.den, err)
		}
		if negated != -got {
			t.Errorf("round(-%s×%d/%d) = %s, but -round(...) = %s: the rule is not symmetric",
				c.amount, c.num, c.den, negated, -got)
		}
	}
}

func TestRateAppliesOnceAndExactly(t *testing.T) {
	cases := []struct {
		rate   Rate
		amount Minor
		want   Minor
		render string
	}{
		{299, 2_500_000, 74_750, "2.99%"}, // the platform fee on ₹25,000
		{1800, 149_900, 26_982, "18%"},    // GST at 18% on ₹1,499
		{1000, 2_750_000, 275_000, "10%"}, // TDS under 194-I
		{500, 1, 0, "5%"},                 // 5% of one paisa rounds to nothing
		{500, 10, 1, "5%"},                // and of ten paise to one, half away from zero
		{50, 3_333_333, 16_667, "0.5%"},   // 16,666.665 → 16,667
	}
	for _, c := range cases {
		got, err := c.rate.Of(c.amount)
		if err != nil {
			t.Fatalf("%s of %s: %v", c.rate, c.amount, err)
		}
		if got != c.want {
			t.Errorf("%s of %s = %s, want %s", c.rate, c.amount, got, c.want)
		}
		if c.rate.String() != c.render {
			t.Errorf("rate %d bp renders %q, want %q", int64(c.rate), c.rate.String(), c.render)
		}
	}

	if _, err := Rate(-100).Of(1000); err == nil {
		t.Error("a negative rate was accepted")
	}
}

// A fee charged on a split total must equal the sum of the fees on the slices —
// otherwise an owner's statement and the portfolio total disagree, and both are
// arithmetically defensible, which is the worst kind of disagreement.
func TestFeeOnTheWholeMatchesTheSumOfTheSlicesWhenAllocatedFirst(t *testing.T) {
	const total Minor = 2_750_000
	fee, err := Rate(299).Of(total)
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	// The fee is computed once and then allocated, which is the rule: round at
	// the posting boundary, allocate everywhere else.
	slices, err := Allocate(fee, []int64{17, 14})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	var sum Minor
	for _, s := range slices {
		sum += s
	}
	if sum != fee {
		t.Errorf("fee slices %v sum to %s, want %s", slices, sum, fee)
	}
}

func TestRupeesIsTheExportForm(t *testing.T) {
	cases := map[Minor]string{
		0:          "0.00",
		5:          "0.05",
		50:         "0.50",
		100:        "1.00",
		2_750_000:  "27500.00",
		1_508_065:  "15080.65",
		-1:         "-0.01",
		-2_750_000: "-27500.00",
	}
	for amount, want := range cases {
		if got := amount.Rupees(); got != want {
			t.Errorf("%d paise renders %q, want %q", int64(amount), got, want)
		}
		if amount.String() != want {
			t.Errorf("String() and Rupees() disagree for %d", int64(amount))
		}
		// No grouping and no symbol: the CSV rule is that a field never needs
		// quoting and a parser never needs a locale.
		if strings.ContainsAny(want, ",₹ ") {
			t.Errorf("%q carries grouping or a symbol", want)
		}
	}
}

func TestJSONIsAWholeNumberOfPaise(t *testing.T) {
	b, err := json.Marshal(Minor(1_508_065))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(b) != "1508065" {
		t.Errorf("marshalled %s, want 1508065", b)
	}

	var m Minor
	if err := json.Unmarshal([]byte("2750050"), &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if m != 2_750_050 {
		t.Errorf("unmarshalled %s", m)
	}

	// The defect this rejection exists to prevent: a client sending rupees with
	// paise after the point. Truncating it would charge ₹275 instead of ₹27,500.
	for _, bad := range []string{"27500.50", `"2750050"`, "2.75005e6", "1e3", "9007199254740992"} {
		var v Minor
		if err := json.Unmarshal([]byte(bad), &v); err == nil {
			t.Errorf("%s was accepted as %s", bad, v)
		}
	}

	// null leaves the field alone rather than erroring, matching what every
	// other Unmarshaler in the standard library does.
	var untouched Minor = 42
	if err := json.Unmarshal([]byte("null"), &untouched); err != nil || untouched != 42 {
		t.Errorf("null gave (%s, %v)", untouched, err)
	}
}

func TestAmountsBeyondTheSafeRangeAreRefusedEverywhere(t *testing.T) {
	over := MaxSafeMinor + 1

	if err := over.Valid(); err == nil {
		t.Error("Valid accepted an amount beyond the safe range")
	}
	if _, err := json.Marshal(over); err == nil {
		t.Error("Marshal accepted an amount beyond the safe range")
	}
	if _, err := Allocate(over, []int64{1, 1}); err == nil {
		t.Error("Allocate accepted an amount beyond the safe range")
	}
	if _, err := Prorate(over, 1, 2); err == nil {
		t.Error("Prorate accepted an amount beyond the safe range")
	}
	if _, err := Rate(10_000).Of(over); err == nil {
		t.Error("Rate accepted an amount beyond the safe range")
	}
	if err := MaxSafeMinor.Valid(); err != nil {
		t.Errorf("the boundary itself was refused: %v", err)
	}

	// And an entry carrying one never reaches the database.
	e := Entry{
		Kind: KindLateFee, TemplateVersion: 1,
		Postings: []Posting{
			{Account: TenantReceivable, Side: Debit, Amount: over, Party: Party{Tenant, "t"}},
			{Account: LateFeeIncome, Side: Credit, Amount: over, Party: Party{Owner, "o"}},
		},
	}
	if err := e.Validate(); err == nil {
		t.Error("an entry posting an unrepresentable amount validated")
	}
}

func TestProrateRejectsAnImpossiblePeriod(t *testing.T) {
	if _, err := Prorate(1000, 5, 0); err == nil {
		t.Error("a period of zero days was accepted")
	}
	if _, err := Prorate(1000, -1, 30); err == nil {
		t.Error("a negative number of days was accepted")
	}
}
