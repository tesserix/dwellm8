package recon_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/recon"
)

// ADR-0012, without a database, because reconciliation is a comparison and a
// comparison is arithmetic.
//
// The scenario the story is built on is the first test: five hundred collections,
// three of them missing from the file. It is the one that only passes if the
// implementation looks at both sides, and a reconciliation that reads only the
// settlement file will produce a perfectly clean run over it.

const day = 24 * time.Hour

var (
	asOf = time.Date(2026, 7, 30, 6, 0, 0, 0, time.UTC)
	sla  = 2 * day
)

// captured builds one captured payment, captured `ago` before the run.
func captured(n int, amount domain.Minor, ago time.Duration) recon.Captured {
	return recon.Captured{
		PaymentID:         fmt.Sprintf("pay-%04d", n),
		TenantID:          "11111111-1111-1111-1111-111111111111",
		ProviderPaymentID: fmt.Sprintf("rz-%04d", n),
		AmountMinor:       amount,
		CapturedAt:        asOf.Add(-ago),
		Method:            collect.MethodUPICollect,
	}
}

// settles builds the settlement line for a captured payment.
func settles(n int, amount domain.Minor, on time.Time) recon.Line {
	return recon.Line{
		ProviderLineID:    fmt.Sprintf("stl-%04d", n),
		Kind:              recon.LinePayment,
		Direction:         recon.Inward,
		ProviderPaymentID: fmt.Sprintf("rz-%04d", n),
		AmountMinor:       amount,
		SettledOn:         on,
	}
}

// The acceptance criterion, as a test. 500 collections, 3 missing, and the run
// must name the three and nothing else.
func TestThreeMissingFromFiveHundredAreFoundAndAlerted(t *testing.T) {
	const total, missing = 500, 3
	rent := domain.Minor(2_500_000) // ₹25,000

	in := recon.Input{Provider: "razorpay", AsOf: asOf, SLA: sla}
	for i := 1; i <= total; i++ {
		in.Captured = append(in.Captured, captured(i, rent, 3*day))
		if i > missing {
			in.Lines = append(in.Lines, settles(i, rent, asOf.Add(-1*day)))
		}
	}

	res, err := recon.Reconcile(in)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if res.LinesMatched != total-missing {
		t.Errorf("matched %d lines, want %d", res.LinesMatched, total-missing)
	}
	if want := rent * (total - missing); res.SettledMinor != want {
		t.Errorf("settled %s, want %s", res.SettledMinor, want)
	}
	if res.Unresolved() != missing {
		t.Fatalf("found %d unresolved item(s), want %d — a run that reads only the settlement file "+
			"finds none of them", res.Unresolved(), missing)
	}
	if want := rent * missing; res.UnresolvedMinor() != want {
		t.Errorf("unresolved money is %s, want %s", res.UnresolvedMinor(), want)
	}

	// Named individually, with an ageing bucket, per the story.
	for _, d := range res.Drift {
		if d.Kind != recon.DriftMissing {
			t.Errorf("drift of kind %s in a run whose only defect is three absences", d.Kind)
		}
		if d.PaymentID == "" {
			t.Error("a missing settlement that does not name its payment cannot be chased")
		}
		if d.Bucket() != recon.Bucket1To3 {
			t.Errorf("a payment captured three days ago is in bucket %s, want %s", d.Bucket(), recon.Bucket1To3)
		}
	}

	// And the alert says the count and the amount, in one line.
	alerts := recon.Alerts("razorpay", res, recon.Thresholds{Count: 0, AmountMinor: 0, StaleAfter: day})
	if len(alerts) != 1 {
		t.Fatalf("raised %d alerts, want 1 — three missing payments is one incident with three items in it, "+
			"and three alerts is how a pager gets muted", len(alerts))
	}
	a := alerts[0]
	if a.Count != missing || a.Minor != rent*missing {
		t.Errorf("alert says %d item(s) worth %s, want %d worth %s", a.Count, a.Minor, missing, rent*missing)
	}
	if a.Kind != recon.DriftMissing {
		t.Errorf("alert kind is %s", a.Kind)
	}
	t.Logf("alert: %s", a.Message)
}

// The step that separates this from a report. A payment captured yesterday with
// an SLA of two days is not missing yet, and alerting on it would produce an
// alert for every payment taken today, every night.
func TestSilenceInsideTheSLAIsNotDrift(t *testing.T) {
	in := recon.Input{
		Provider: "razorpay", AsOf: asOf, SLA: sla,
		Captured: []recon.Captured{
			captured(1, 100_000, 1*day),   // inside the SLA
			captured(2, 100_000, 3*day),   // outside it
			captured(3, 100_000, 2*day-1), // the boundary, just inside
		},
	}
	res, err := recon.Reconcile(in)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Unresolved() != 1 {
		t.Fatalf("found %d unresolved, want 1 — only the payment past the SLA is missing", res.Unresolved())
	}
	if got := res.Drift[0].PaymentID; got != "pay-0002" {
		t.Errorf("flagged %s, want pay-0002", got)
	}
}

// Offline money is never in a gateway's settlement file, and its absence is not
// drift. Without this the product produces one alert per cash payment, every
// night, until somebody switches the alerting off — which is the outcome that
// matters, not the noise.
func TestOfflinePaymentsAreNotExpectedInASettlementFile(t *testing.T) {
	cash := captured(1, 500_000, 10*day)
	cash.Method = collect.MethodOfflineCash
	cash.ProviderPaymentID = ""

	online := captured(2, 500_000, 10*day)

	res, err := recon.Reconcile(recon.Input{
		Provider: "razorpay", AsOf: asOf, SLA: sla,
		Captured: []recon.Captured{cash, online},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.SkippedOffline != 1 {
		t.Errorf("skipped %d offline payment(s), want 1", res.SkippedOffline)
	}
	if res.Unresolved() != 1 {
		t.Fatalf("found %d unresolved, want 1 — the cash payment is not missing, it is offline", res.Unresolved())
	}
	if got := res.Drift[0].PaymentID; got != "pay-0002" {
		t.Errorf("flagged %s, want the online payment pay-0002", got)
	}
}

// A captured online payment with no provider id can never be matched by anybody.
// It is surfaced rather than skipped: it is money we cannot chase, and skipping it
// is how it stops existing.
func TestACapturedPaymentWithNoProviderIDIsFlagged(t *testing.T) {
	c := captured(1, 700_000, 10*day)
	c.ProviderPaymentID = ""

	res, err := recon.Reconcile(recon.Input{
		Provider: "razorpay", AsOf: asOf, SLA: sla, Captured: []recon.Captured{c},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.Unresolved() != 1 || res.Drift[0].Kind != recon.DriftMissing {
		t.Fatalf("got %d unresolved %v, want one missing_settlement", res.Unresolved(), res.Drift)
	}
}

// The classification table, stated explicitly rather than derived, so that a
// change to the rule shows up as a change to this table.
func TestTheMatchClasses(t *testing.T) {
	rent := domain.Minor(1_000_000) // ₹10,000
	onTime := asOf.Add(-1 * day)

	cases := []struct {
		name  string
		line  recon.Line
		want  recon.MatchClass
		drift domain.Minor
		posts bool
	}{
		{
			name: "settled to the paisa",
			line: settles(1, rent, onTime),
			want: recon.MatchExact, posts: true,
		},
		{
			name: "the provider kept its charge",
			line: func() recon.Line {
				l := settles(1, rent, onTime)
				l.FeeMinor, l.TaxMinor = 20_000, 3_600 // 2% + 18% GST on it
				return l
			}(),
			want: recon.MatchFeeAdjusted, posts: true,
		},
		{
			name: "a split settlement, not yet whole",
			line: settles(1, rent-400_000, onTime),
			want: recon.MatchPartial, drift: -400_000, posts: false,
		},
		{
			name: "the provider settled more than was collected",
			line: settles(1, rent+100, onTime),
			want: recon.MatchAmountDrift, drift: 100, posts: false,
		},
		{
			name: "a line naming a payment this system never issued",
			line: func() recon.Line {
				l := settles(1, rent, onTime)
				l.ProviderPaymentID = "rz-nobody"
				return l
			}(),
			want: recon.MatchUnknown, posts: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := recon.Reconcile(recon.Input{
				Provider: "razorpay", AsOf: asOf, SLA: sla,
				Captured: []recon.Captured{captured(1, rent, 1*day)},
				Lines:    []recon.Line{tc.line},
			})
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if len(res.Matches) != 1 {
				t.Fatalf("got %d matches, want 1", len(res.Matches))
			}
			m := res.Matches[0]
			if m.Class != tc.want {
				t.Errorf("classified as %s, want %s", m.Class, tc.want)
			}
			if m.DriftMinor != tc.drift {
				t.Errorf("drift is %s, want %s", m.DriftMinor, tc.drift)
			}
			if m.Class.Posts() != tc.posts {
				t.Errorf("Posts() is %v, want %v", m.Class.Posts(), tc.posts)
			}
		})
	}
}

// A duplicate is only detectable across runs: a re-sent file is deduplicated by
// line id, and a provider genuinely settling the same payment twice sends two
// different line ids. So the previous runs' settled ids are an input.
func TestAPaymentSettledInAnEarlierBatchIsADuplicate(t *testing.T) {
	rent := domain.Minor(1_000_000)
	res, err := recon.Reconcile(recon.Input{
		Provider: "razorpay", AsOf: asOf, SLA: sla,
		Captured:       []recon.Captured{captured(1, rent, 1*day)},
		Lines:          []recon.Line{settles(1, rent, asOf.Add(-1*day))},
		SettledEarlier: map[string]bool{"rz-0001": true},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := res.Matches[0].Class; got != recon.MatchDuplicate {
		t.Fatalf("classified as %s, want duplicate", got)
	}
	if res.LinesMatched != 0 || res.SettledMinor != 0 {
		t.Error("a duplicate contributed to the settled total — that credits a bank balance the bank does not have")
	}
	// And it alerts regardless of thresholds.
	if alerts := recon.Alerts("razorpay", res, recon.Thresholds{Count: 25, AmountMinor: recon.MaxAlertAmountMinor}); len(alerts) != 1 {
		t.Errorf("a duplicate settlement raised %d alerts under the loosest legal thresholds, want 1", len(alerts))
	}
}

// Lateness is not a class. A line can be both late and fee-adjusted, and the
// version of this design where "timing" is a class has to choose between them.
func TestALateSettlementStillPostsAndIsStillReported(t *testing.T) {
	rent := domain.Minor(1_000_000)
	line := settles(1, rent, asOf.Add(-1*day)) // captured 9 days ago, settled yesterday
	line.FeeMinor = 20_000

	res, err := recon.Reconcile(recon.Input{
		Provider: "razorpay", AsOf: asOf, SLA: sla,
		Captured: []recon.Captured{captured(1, rent, 9*day)},
		Lines:    []recon.Line{line},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	m := res.Matches[0]
	if m.Class != recon.MatchFeeAdjusted {
		t.Errorf("a late fee-adjusted line classified as %s — lateness took the class", m.Class)
	}
	if !m.Late {
		t.Error("the line settled eight days after the SLA and is not marked late")
	}
	if res.SettledMinor != rent {
		t.Error("a late settlement did not post: the money arrived, and it belongs in the bank")
	}
	// Recorded, and not something a person has to close.
	if res.Unresolved() != 0 {
		t.Errorf("%d unresolved from a run whose only fault was slowness", res.Unresolved())
	}
	if len(res.Drift) != 1 || res.Drift[0].Kind != recon.DriftLate {
		t.Fatalf("drift is %v, want one late_settlement row", res.Drift)
	}
	if got := res.Drift[0].Bucket(); got != recon.Bucket8To30 {
		t.Errorf("a settlement eight days after capture is bucketed %s", got)
	}
}

// Two lines finishing one payment. The first posts nothing; together they are
// exact. Getting this wrong is how a clearing account keeps a residue forever.
func TestASplitSettlementCompletes(t *testing.T) {
	rent := domain.Minor(1_000_000)
	first := settles(1, 600_000, asOf.Add(-1*day))
	first.FeeMinor = 12_000
	second := settles(1, 400_000, asOf.Add(-1*day))
	second.ProviderLineID = "stl-0001-b"
	second.FeeMinor = 8_000

	res, err := recon.Reconcile(recon.Input{
		Provider: "razorpay", AsOf: asOf, SLA: sla,
		Captured: []recon.Captured{captured(1, rent, 1*day)},
		Lines:    []recon.Line{first, second},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := res.Matches[0].Class; got != recon.MatchPartial {
		t.Errorf("the first half classified as %s, want partial", got)
	}
	completing := res.Matches[1]
	if completing.Class != recon.MatchFeeAdjusted {
		t.Errorf("the completing half classified as %s, want fee_adjusted", completing.Class)
	}
	// The posting is built from the running totals, not from the line: clearing
	// was debited once with the gross, so it must be credited once with the gross.
	if completing.GrossMinor != rent {
		t.Errorf("the completing line posts a gross of %s, want %s — anything less leaves a clearing "+
			"residue that no later line can clear", completing.GrossMinor, rent)
	}
	if completing.FeeMinor != 20_000 {
		t.Errorf("the completing line carries %s of fee, want ₹200 — the fee taken out of the first half "+
			"would otherwise be lost", completing.FeeMinor)
	}
	if res.SettledMinor != rent {
		t.Errorf("settled %s, want %s — the completing line carries the whole gross", res.SettledMinor, rent)
	}
	if res.Unresolved() != 0 {
		t.Errorf("%d unresolved from a payment that was settled in full", res.Unresolved())
	}
}

// A partial settlement that never completes must age like anything else. The
// outstanding part is the drift, not the whole payment.
func TestAPartialSettlementThatNeverCompletesAges(t *testing.T) {
	rent := domain.Minor(1_000_000)
	res, err := recon.Reconcile(recon.Input{
		Provider: "razorpay", AsOf: asOf, SLA: sla,
		Captured: []recon.Captured{captured(1, rent, 9*day)},
		Lines:    []recon.Line{settles(1, 600_000, asOf.Add(-8*day))},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var missing *recon.Drift
	for i := range res.Drift {
		if res.Drift[i].Kind == recon.DriftMissing {
			missing = &res.Drift[i]
		}
	}
	if missing == nil {
		t.Fatal("a payment 40% settled nine days ago produced no missing_settlement row")
	}
	if missing.AmountMinor != 400_000 {
		t.Errorf("the drift is %s, want ₹4,000 — the outstanding part, not the whole payment", missing.AmountMinor)
	}
}

// The batch's own arithmetic, checked before any line of it is believed. A file
// we parsed wrong must not be able to look like a file we disagree with.
func TestABatchMustAddUpBeforeAnyLineOfItIsBelieved(t *testing.T) {
	good := recon.Batch{
		Provider: "razorpay", ProviderBatchID: "setl_001", UTR: "UTR12345",
		SettledOn:  asOf,
		GrossMinor: 10_000_000, RefundMinor: 500_000, FeeMinor: 200_000, TaxMinor: 36_000,
		NetMinor: 9_264_000,
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("a batch that adds up was refused: %v", err)
	}

	bad := good
	bad.NetMinor = 9_264_001 // one paise out
	err := bad.Validate()
	if err == nil {
		t.Fatal("a batch one paise out was accepted — the ingestion of a misparsed file is now indistinguishable " +
			"from a disagreement with the provider")
	}
	if !errors.Is(err, recon.ErrBatchArithmetic) {
		t.Errorf("the error is not distinguishable as arithmetic: %v", err)
	}
	t.Logf("refused: %v", err)
}

// The line shape rules, each of which is a way a misparsed file would otherwise
// inflate or deflate a settlement.
func TestTheLineShapeRules(t *testing.T) {
	base := recon.Line{
		ProviderLineID: "stl-1", Kind: recon.LinePayment, Direction: recon.Inward,
		ProviderPaymentID: "rz-1", AmountMinor: 1_000_000, SettledOn: asOf,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed line was refused: %v", err)
	}

	cases := []struct {
		name string
		mut  func(l *recon.Line)
	}{
		{"no line id, so a re-ingested file doubles every amount", func(l *recon.Line) { l.ProviderLineID = "" }},
		{"a refund claiming to be inward", func(l *recon.Line) { l.Kind = recon.LineRefund; l.ProviderPaymentID = "" }},
		{"a payment line that does not say which payment", func(l *recon.Line) { l.ProviderPaymentID = "" }},
		{"an adjustment against a payment", func(l *recon.Line) { l.Kind = recon.LineAdjustment }},
		{"an unknown kind", func(l *recon.Line) { l.Kind = "settled_maybe" }},
		{"a fee larger than the collection", func(l *recon.Line) { l.FeeMinor = l.AmountMinor }},
		{"an amount of nothing", func(l *recon.Line) { l.AmountMinor = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := base
			tc.mut(&l)
			if err := l.Validate(); err == nil {
				t.Errorf("accepted: %s", tc.name)
			}
		})
	}
}

// The third comparison, and the only one that finds our own bugs.
func TestTheClearingBalanceIsTheThirdAccountOfTheMoney(t *testing.T) {
	if d := recon.ClearingCheck(2_500_000, 2_500_000); d != nil {
		t.Errorf("two agreeing balances produced drift: %v", d)
	}
	// A settlement entry posted for a payment still marked captured: the ledger
	// has cleared money the payments table still thinks is outstanding.
	d := recon.ClearingCheck(2_000_000, 2_500_000)
	if d == nil {
		t.Fatal("the ledger and the payments table disagree by ₹5,000 and no drift was raised — " +
			"no settlement file will ever reveal this")
	}
	if d.Kind != recon.DriftClearingBalance || d.AmountMinor != 500_000 {
		t.Errorf("got %s of %s, want clearing_balance of ₹5,000", d.Kind, d.AmountMinor)
	}
	if d.TenantID != "" {
		t.Error("the clearing check is platform-wide and must not claim an organisation")
	}
}

// Thresholds are configuration, and configuration does not get to switch alerting
// off by choosing a number no incident will reach.
func TestAlertThresholdsHaveACeiling(t *testing.T) {
	ok := recon.Thresholds{Count: 5, AmountMinor: 1_000_000, StaleAfter: 26 * time.Hour}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a reasonable configuration was refused: %v", err)
	}
	// Zero is the strictest setting, not the loosest: everything alerts.
	if err := (recon.Thresholds{}).Validate(); err != nil {
		t.Errorf("zero thresholds were refused, and zero means alert on anything: %v", err)
	}

	for _, tc := range []struct {
		name string
		th   recon.Thresholds
	}{
		{"a count no incident reaches", recon.Thresholds{Count: recon.MaxAlertCount + 1}},
		{"an amount no incident reaches", recon.Thresholds{AmountMinor: recon.MaxAlertAmountMinor + 1}},
		{"a week before anybody is told", recon.Thresholds{StaleAfter: 7 * day}},
		{"a negative threshold", recon.Thresholds{Count: -1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.th.Validate(); err == nil {
				t.Errorf("accepted: %s", tc.name)
			} else {
				t.Logf("refused: %v", err)
			}
		})
	}
}

// The reconciler cannot be the thing that alerts on the reconciler being down. A
// day with no run at all, and a run that died half way, must both alert — and
// both are found by reading the run table, which needs no run to happen.
func TestTheWatchdogFiresWhenTheReconcilerIsDead(t *testing.T) {
	th := recon.Thresholds{StaleAfter: 26 * time.Hour}
	runs := []recon.RunSummary{
		{Provider: "razorpay", AsOfDate: asOf.Add(-4 * day), State: recon.StateRunning},
		{Provider: "razorpay", AsOfDate: asOf.Add(-3 * day), State: recon.StateIncomplete},
		{Provider: "razorpay", AsOfDate: asOf.Add(-2 * day), State: recon.StateDrift},
		{Provider: "razorpay", AsOfDate: asOf.Add(-2 * day), State: recon.StateReconciled},
		{Provider: "razorpay", AsOfDate: asOf.Add(-1 * time.Hour), State: recon.StateRunning},
	}
	alerts := recon.StaleRuns(runs, asOf, th)
	if len(alerts) != 2 {
		t.Fatalf("raised %d alerts, want 2: the run stuck in `running` for four days and the day whose file "+
			"never arrived. Got %v", len(alerts), alerts)
	}
	for _, a := range alerts {
		t.Logf("watchdog: %s", a.Message)
	}

	// A run in progress right now is not an incident.
	if got := recon.StaleRuns(runs[4:], asOf, th); len(got) != 0 {
		t.Errorf("a run that started an hour ago alerted: %v", got)
	}
	// `drift` is a finished state. The job did its job; the money is the incident,
	// and Alerts is what raises it.
	if got := recon.StaleRuns(runs[2:3], asOf, th); len(got) != 0 {
		t.Errorf("a completed run with drift was reported as a dead reconciler: %v", got)
	}
}

// A day is never marked reconciled by a job that decides for itself. StateFor
// computes it from what the run found, and a missing file is `incomplete` however
// clean the comparison looked.
func TestTheStateOfARunIsComputedFromWhatItFound(t *testing.T) {
	clean, err := recon.Reconcile(recon.Input{
		Provider: "razorpay", AsOf: asOf, SLA: sla,
		Captured: []recon.Captured{captured(1, 100_000, 1*day)},
		Lines:    []recon.Line{settles(1, 100_000, asOf.Add(-1*time.Hour))},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := recon.StateFor(clean, true); got != recon.StateReconciled {
		t.Errorf("a clean run is %s, want reconciled", got)
	}
	// The failure scenario from the story: the file was not available. The
	// comparison over nothing looks perfectly clean, and the day is not reconciled.
	empty, err := recon.Reconcile(recon.Input{Provider: "razorpay", AsOf: asOf, SLA: sla})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if empty.Unresolved() != 0 {
		t.Fatal("the empty run found drift, which this test needs it not to")
	}
	if got := recon.StateFor(empty, false); got != recon.StateIncomplete {
		t.Fatalf("a day whose settlement file never arrived is %s — a comparison over no lines looks clean, "+
			"and calling that reconciled is how money goes missing quietly", got)
	}
	if recon.StateIncomplete.Settled() {
		t.Error("`incomplete` reports itself as settled, so the watchdog would stop watching it")
	}
}

// The ageing boundaries, which the schema's view repeats and the store contract
// test compares.
func TestTheAgeingBuckets(t *testing.T) {
	for _, tc := range []struct {
		age  time.Duration
		want recon.AgeBucket
	}{
		{0, recon.BucketSameDay},
		{23 * time.Hour, recon.BucketSameDay},
		{day, recon.Bucket1To3},
		{3 * day, recon.Bucket1To3},
		{4 * day, recon.Bucket4To7},
		{7 * day, recon.Bucket4To7},
		{8 * day, recon.Bucket8To30},
		{30 * day, recon.Bucket8To30},
		{31 * day, recon.BucketOver30},
		{400 * day, recon.BucketOver30},
	} {
		if got := recon.Bucket(tc.age); got != tc.want {
			t.Errorf("an age of %s buckets as %s, want %s", tc.age, got, tc.want)
		}
	}
}

// A run over identical inputs must produce an identical answer, or nothing about
// it can be diffed, tested or trusted. Map iteration in the missing-payment pass
// is where this was not true.
func TestARunIsDeterministic(t *testing.T) {
	in := recon.Input{Provider: "razorpay", AsOf: asOf, SLA: sla}
	for i := 1; i <= 40; i++ {
		in.Captured = append(in.Captured, captured(i, domain.Minor(100_000+i), 5*day))
	}

	first, err := recon.Reconcile(in)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	for range 20 {
		again, err := recon.Reconcile(in)
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		for i := range first.Drift {
			if first.Drift[i] != again.Drift[i] {
				t.Fatalf("row %d differs between identical runs: %v vs %v", i, first.Drift[i], again.Drift[i])
			}
		}
	}
}

// Reconcile is a comparison, and a comparison writes nothing. The same property
// ADR-0011 §4 gives the webhook path, for the same reason: what may be posted is
// two classes out of six, and it is a method on the class rather than a decision
// somebody makes per call site.
func TestOnlyTwoClassesMayPostWithoutAPerson(t *testing.T) {
	var posts []recon.MatchClass
	for _, c := range recon.MatchClasses() {
		if c.Posts() {
			posts = append(posts, c)
		}
	}
	if len(posts) != 2 {
		t.Fatalf("%d classes may post unattended: %v — ADR-0012 §5 permits exact and fee_adjusted", len(posts), posts)
	}
	for _, c := range posts {
		if c != recon.MatchExact && c != recon.MatchFeeAdjusted {
			t.Errorf("%s may post unattended", c)
		}
	}
}

// A run needs a clock and an SLA. Both defaults are dangerous in opposite
// directions, so neither has one.
func TestARunRefusesToGuessItsClockOrItsSLA(t *testing.T) {
	if _, err := recon.Reconcile(recon.Input{SLA: sla}); err == nil {
		t.Error("a run with no as-of instant was accepted, and its ageing report ages against nothing")
	}
	if _, err := recon.Reconcile(recon.Input{AsOf: asOf}); err == nil {
		t.Error("a run with an SLA of zero was accepted, and every payment captured today is missing")
	}
}
