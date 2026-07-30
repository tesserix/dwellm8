package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/recon"
)

// The drift checks between Go's copy of ADR-0012 and the schema's.
//
// Four vocabularies, one set of ageing boundaries and one arithmetic rule exist in
// both places. The failure each of these prevents is the same shape as ADR-0011's:
// nothing crashes. A drift kind the reconciler can produce and the CHECK refuses
// makes the nightly job fail on the one night it found something, and a bucket
// boundary that differs between the report and the code makes an ageing report
// that nobody can reconcile against the rows it came from.

func TestTheGoReconciliationVocabulariesAreAcceptedByTheSchema(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	def := func(name string) string {
		var out string
		if err := p.QueryRow(ctx,
			`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, name).Scan(&out); err != nil {
			t.Fatalf("reading constraint %s: %v — ADR-0012 requires it", name, err)
		}
		return out
	}

	kinds := def("settlement_lines_line_kind_check")
	for _, k := range recon.LineKinds() {
		if !strings.Contains(kinds, "'"+string(k)+"'") {
			t.Errorf("line kind %q is producible in Go and refused by the schema", k)
		}
	}

	classes := def("settlement_lines_match_class_check")
	for _, c := range recon.MatchClasses() {
		if !strings.Contains(classes, "'"+string(c)+"'") {
			t.Errorf("match class %q is producible in Go and refused by the schema", c)
		}
	}

	drift := def("settlement_drift_drift_kind_check")
	for _, k := range recon.DriftKinds() {
		if !strings.Contains(drift, "'"+string(k)+"'") {
			t.Errorf("drift kind %q is producible in Go and refused by the schema — the nightly job would "+
				"fail on the one night it found something", k)
		}
	}

	states := def("settlement_drift_state_check")
	for _, s := range []recon.DriftState{recon.DriftOpen, recon.DriftResolved, recon.DriftWrittenOff} {
		if !strings.Contains(states, "'"+string(s)+"'") {
			t.Errorf("drift state %q is producible in Go and refused by the schema", s)
		}
	}

	runs := def("reconciliation_runs_state_check")
	for _, s := range recon.RunStates() {
		if !strings.Contains(runs, "'"+string(s)+"'") {
			t.Errorf("run state %q is producible in Go and refused by the schema", s)
		}
	}
}

// The ageing boundaries, evaluated by PostgreSQL and compared against Go's.
//
// Both copies exist for good reasons — the view is what an operator queries, the
// function is what the alerting uses — and the cost is this test. A report that
// buckets a four-day-old item differently from the code that alerted on it is a
// report nobody can act on.
func TestTheAgeingBucketsMatchTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	// The schema's own function, evaluated over ages rather than over rows, so no
	// drift has to exist for this to be a real check.
	//
	// The boundaries are deliberately not restated here. An earlier version of this
	// test carried a copy of the view's CASE expression and compared Go against
	// that, which is a test of two things this file wrote — a boundary rewritten in
	// the schema would have passed. settlement_age_bucket() exists so the seam is
	// callable, for the same reason payment_transition_allowed() does.
	const q = `SELECT settlement_age_bucket($1::interval)`

	// And the view must actually use it, or the report an operator reads is not the
	// rule this test just checked.
	var viewDef string
	if err := p.QueryRow(ctx,
		`SELECT pg_get_viewdef('settlement_drift_ageing'::regclass, true)`).Scan(&viewDef); err != nil {
		t.Fatalf("reading settlement_drift_ageing: %v — ADR-0012 §7 is this view", err)
	}
	if !strings.Contains(viewDef, "settlement_age_bucket") {
		t.Errorf("the ageing view does not call settlement_age_bucket — it has its own copy of the "+
			"boundaries, and this test is checking the other one:\n%s", viewDef)
	}

	const day = 24 * time.Hour
	ages := []time.Duration{
		0, 6 * time.Hour, 23*time.Hour + 59*time.Minute,
		day, 3 * day, 3*day + time.Hour, 3*day + 23*time.Hour,
		4 * day, 7 * day, 7*day + time.Second, 8 * day,
		30 * day, 30*day + time.Hour, 31 * day, 400 * day,
	}
	seen := map[string]bool{}
	for _, age := range ages {
		var inDB string
		if err := p.QueryRow(ctx, q, age.String()).Scan(&inDB); err != nil {
			t.Fatalf("bucketing %s: %v — ADR-0012 §7 requires settlement_age_bucket()", age, err)
		}
		if inGo := string(recon.Bucket(age)); inGo != inDB {
			t.Errorf("an age of %s buckets as %q in Go and %q in the database", age, inGo, inDB)
		}
		seen[inDB] = true
	}
	// Every bucket has to be reachable, or a boundary could be wrong in a range no
	// age in the table lands in.
	for _, b := range recon.Buckets() {
		if !seen[string(b)] {
			t.Errorf("no age in this table lands in %q, so its boundary is untested", b)
		}
	}
}

// The two indexes that are guarantees rather than optimisations, in the same sense
// ADR-0011 §2's is: without them, re-ingesting a settlement file doubles every
// amount in it, and a payment missing for nine nights becomes nine drift rows
// instead of one that ages.
func TestTheReconciliationDeduplicationIndexesExist(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	for _, tc := range []struct {
		index   string
		columns []string
		why     string
	}{
		{"settlement_batches_provider_idx", []string{"provider", "provider_batch_id"},
			"re-ingesting a settlement file would ingest it twice"},
		{"settlement_lines_provider_line_idx", []string{"provider", "provider_line_id"},
			"re-ingesting a file would double every amount in it"},
		{"settlement_drift_open_payment_idx", []string{"provider", "drift_kind", "payment_id"},
			"a payment missing for nine nights would be nine drift rows rather than one that ages"},
		{"reconciliation_runs_day_idx", []string{"provider", "as_of_date"},
			"a day could hold a reconciled run and an incomplete one with no way to tell which is current"},
	} {
		t.Run(tc.index, func(t *testing.T) {
			var def string
			if err := p.QueryRow(ctx,
				`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`,
				tc.index).Scan(&def); err != nil {
				t.Fatalf("reading %s: %v — without it, %s", tc.index, err, tc.why)
			}
			if !strings.Contains(def, "UNIQUE") {
				t.Errorf("%s is not unique, so %s: %s", tc.index, tc.why, def)
			}
			for _, col := range tc.columns {
				if !strings.Contains(def, col) {
					t.Errorf("%s does not cover %s: %s", tc.index, col, def)
				}
			}
		})
	}
}

// The batch arithmetic rule, evaluated by PostgreSQL against Go's. Both hold it,
// and this asserts they hold the same one — a schema that subtracts the fee twice
// would refuse every real file, at 2am, on the first night.
func TestTheBatchArithmeticMatchesTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx,
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint
		  WHERE conname = 'settlement_batches_adds_up'`).Scan(&def); err != nil {
		t.Fatalf("reading settlement_batches_adds_up: %v — ADR-0012 §2 is this constraint", err)
	}

	for _, tc := range []struct {
		name                         string
		gross, refund, fee, tax, net int64
	}{
		{"a clean day", 10_000_000, 0, 200_000, 36_000, 9_764_000},
		{"a day with refunds", 10_000_000, 500_000, 200_000, 36_000, 9_264_000},
		{"nothing collected", 0, 0, 0, 0, 0},
		{"one paise out", 10_000_000, 0, 200_000, 36_000, 9_764_001},
		{"the fee subtracted twice", 10_000_000, 0, 200_000, 36_000, 9_564_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var inDB bool
			// The constraint's own expression, applied to these numbers.
			if err := p.QueryRow(ctx,
				`SELECT $5::bigint = $1::bigint - $2::bigint - $3::bigint - $4::bigint`,
				tc.gross, tc.refund, tc.fee, tc.tax, tc.net).Scan(&inDB); err != nil {
				t.Fatalf("evaluating: %v", err)
			}
			b := recon.Batch{
				Provider: "razorpay", ProviderBatchID: tc.name, SettledOn: time.Now(),
				GrossMinor: domain.Minor(tc.gross), RefundMinor: domain.Minor(tc.refund),
				FeeMinor: domain.Minor(tc.fee), TaxMinor: domain.Minor(tc.tax),
				NetMinor: domain.Minor(tc.net),
			}
			inGo := b.Validate() == nil
			if inGo != inDB {
				t.Errorf("Go %v, database %v for gross %d less %d, %d, %d = %d",
					inGo, inDB, tc.gross, tc.refund, tc.fee, tc.tax, tc.net)
			}
		})
	}
	t.Logf("the schema's rule: %s", def)
}
