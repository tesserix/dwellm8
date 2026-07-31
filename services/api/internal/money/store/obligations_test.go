package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/tdsfiling"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Tracking a deduction to receipt, against PostgreSQL. The deadlines are
// tdsfiling's; what is asserted here is that a step cannot be closed without its
// evidence and that a retried run does not deduct twice.

func obligationFor(t *testing.T, f fixture, section tds.Section, from effective.Date) tdsfiling.Obligation {
	t.Helper()
	to := from.AddDays(31)
	period, err := effective.Between(from, to)
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	o, err := tdsfiling.New(section, f.lease, period, from, 9_000_00, false, effective.Date{})
	if err != nil {
		t.Fatalf("scheduling: %v", err)
	}
	return o
}

func TestADeductionIsTrackedToReceipt(t *testing.T) {
	f := newFixture(t)
	obs := store.NewObligations(pool(t))
	ob := obligationFor(t, f, tds.Section194I, effective.Day(2026, 8, 5))

	id, err := obs.Record(f.ctx, ob, 1000, "")
	if err != nil {
		t.Fatalf("recording: %v", err)
	}
	if id == "" {
		t.Fatal("recording produced no obligation")
	}

	// Everything is outstanding, and the deposit is due on 7 September.
	outstanding, err := obs.Due(f.ctx, effective.Day(2026, 12, 31))
	if err != nil {
		t.Fatalf("reading what is due: %v", err)
	}
	var deposit *store.Outstanding
	for i := range outstanding {
		if outstanding[i].ObligationID == id && outstanding[i].Step == tdsfiling.Deposit {
			deposit = &outstanding[i]
		}
	}
	if deposit == nil {
		t.Fatalf("the deposit step is not outstanding: %+v", outstanding)
	}
	if !deposit.DueBy.Equal(effective.Day(2026, 9, 7)) {
		t.Errorf("the deposit is due %s, want 2026-09-07", deposit.DueBy)
	}
	if deposit.Artefact != tds.Challan {
		t.Errorf("the deposit's artefact is %s", deposit.Artefact)
	}
	if deposit.DaysLate <= 0 {
		t.Errorf("on 31 December a 7 September deadline is %d days late", deposit.DaysLate)
	}

	// The challan closes it, and nothing else.
	if err := obs.Evidence(f.ctx, id, tdsfiling.Deposit, "CIN-0001234", effective.Day(2026, 9, 6)); err != nil {
		t.Fatalf("recording the challan: %v", err)
	}
	after, err := obs.Due(f.ctx, effective.Day(2026, 12, 31))
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	for _, s := range after {
		if s.ObligationID == id && s.Step == tdsfiling.Deposit {
			t.Error("the deposit is still outstanding after its challan was recorded")
		}
	}
	// The return and the certificate are not closed by a challan.
	var still int
	for _, s := range after {
		if s.ObligationID == id {
			still++
		}
	}
	if still != 3 {
		t.Errorf("%d steps remain outstanding, want 3 — the deduction, the return and the "+
			"certificate are not discharged by a deposit", still)
	}
}

// A retried period does not deduct twice. The second deduction would be visible
// only as a landlord paid short again.
func TestRecordingTheSamePeriodTwiceDeductsOnce(t *testing.T) {
	f := newFixture(t)
	obs := store.NewObligations(pool(t))
	ob := obligationFor(t, f, tds.Section194I, effective.Day(2026, 9, 5))

	first, err := obs.Record(f.ctx, ob, 1000, "")
	if err != nil {
		t.Fatalf("recording: %v", err)
	}
	second, err := obs.Record(f.ctx, ob, 1000, "")
	if err != nil {
		t.Fatalf("recording again: %v", err)
	}
	if first != second {
		t.Errorf("a retry raised a second deduction: %s then %s", first, second)
	}

	var n int
	if err := tenancy.Scoped(f.ctx, pool(t), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM tds_obligations WHERE lease_id = $1`, f.lease).Scan(&n)
	}); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 1 {
		t.Errorf("the lease has %d obligations for one period, want 1", n)
	}
}

// The schema refuses a half-recorded step: a challan number with no date is not
// a deposit, and an unfiled return must not be able to look filed.
func TestAStepCannotBeHalfRecorded(t *testing.T) {
	f := newFixture(t)
	obs := store.NewObligations(pool(t))
	ob := obligationFor(t, f, tds.Section195, effective.Day(2026, 10, 5))

	id, err := obs.Record(f.ctx, ob, 1000, "")
	if err != nil {
		t.Fatalf("recording: %v", err)
	}

	err = tenancy.Scoped(f.ctx, pool(t), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE tds_obligation_steps SET reference = 'CIN-9'
			 WHERE obligation_id = $1 AND step = 'deposit'`, id)
		return err
	})
	if err == nil {
		t.Fatal("a reference was recorded with no date — an unfiled return can look filed")
	}
	if !strings.Contains(err.Error(), "tds_obligation_steps_evidence") {
		t.Errorf("refused, but not by the evidence constraint: %v", err)
	}
}

// A section 195 deduction reports on 27Q, and the artefact reaches the database
// intact rather than being flattened to whatever the resident path uses.
func TestTheArtefactSurvivesTheRoundTrip(t *testing.T) {
	f := newFixture(t)
	obs := store.NewObligations(pool(t))
	ob := obligationFor(t, f, tds.Section195, effective.Day(2026, 11, 5))

	id, err := obs.Record(f.ctx, ob, 1000, "")
	if err != nil {
		t.Fatalf("recording: %v", err)
	}
	due, err := obs.Due(f.ctx, effective.Day(2027, 3, 31))
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	var found bool
	for _, s := range due {
		if s.ObligationID == id && s.Step == tdsfiling.Report {
			found = true
			if s.Artefact != tds.Return27Q {
				t.Errorf("a section 195 deduction reports on %s, want 27Q", s.Artefact)
			}
			if s.Section != tds.Section195 {
				t.Errorf("the section came back as %s", s.Section)
			}
		}
	}
	if !found {
		t.Error("the return step did not come back")
	}
}
