package isolationtest_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0012. The reconciliation tables are not tenant-scoped in the ordinary sense,
// so they do not get ADR-0003's five-part contract — they get the other contract,
// which is the one the five-part harness cannot express: that a row belonging to
// no organisation is reachable by none of them, and that writes are the platform's
// alone.
//
// This is the same shape ADR-0011 §5 gave the webhook inbox. Three more tables of
// it is why assertion 12 now exists, and why these tests check the pattern from
// both sides rather than trusting the policy to read correctly.

// A settlement batch is one payout from one aggregator account, and its totals are
// every organisation's collections added together. There is no version of it that
// belongs to a tenant — not even to the one whose money is in it.
func TestSettlementBatchesBelongToNoOrganisation(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	provider := "harness-" + randomToken(t)
	batchID := mustIngestBatch(t, plat, provider, 2_500_000, 0, 0, 0, 2_500_000)

	for _, org := range []tenancy.ID{isolationtest.OrgA, isolationtest.OrgB} {
		if err := tenancy.Scoped(tenancy.With(ctx, org), p, func(ctx context.Context, tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM settlement_batches WHERE id = $1`, batchID).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				t.Errorf("organisation %s sees a settlement batch — a batch spans every organisation "+
					"that collected that day, so showing it to one of them shows the others' totals", org)
			}
			return nil
		}); err != nil {
			t.Fatalf("reading as %s: %v", org, err)
		}

		err := tenancy.Scoped(tenancy.With(ctx, org), p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO settlement_batches (provider, provider_batch_id, settled_on,
				                                gross_minor, net_minor)
				VALUES ($1, $2, current_date, 100, 100)`, provider, "forged-"+string(org)[:1])
			return err
		})
		if err == nil {
			t.Errorf("organisation %s ingested a settlement batch — ingestion is the platform's, "+
				"because a batch is not any organisation's record", org)
		}
	}

	// And it is immutable once ingested: its totals are the provider's statement,
	// and a statement that can be edited is not one.
	err := tenancy.Platform(ctx, plat, "editing a batch", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE settlement_batches SET net_minor = 1 WHERE id = $1`, batchID)
		return err
	})
	if err == nil {
		t.Error("a settlement batch was edited after ingestion — the totals are the provider's statement")
	}
}

// The unmatched line is the ADR-0011 §5 case arriving by the other channel: the
// provider settled money against something this system cannot find, and the row
// has no organisation to attribute it to. Kept where only the platform sees it,
// and attributed to nobody on a guess.
func TestAnUnmatchedSettlementLineIsInvisibleToEveryTenantAndAMatchedOneIsNot(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	provider := "harness-" + randomToken(t)
	batchID := mustIngestBatch(t, plat, provider, 5_000_000, 0, 0, 0, 5_000_000)
	paymentID := mustCapturePayment(t, plat, isolationtest.OrgA, provider)

	orphan := "line-orphan-" + randomToken(t)
	matched := "line-matched-" + randomToken(t)

	if err := tenancy.Platform(ctx, plat, "matching a batch", func(ctx context.Context, tx pgx.Tx) error {
		// A line the provider sent for a payment this system never issued. No
		// tenant, no payment, and a class saying so.
		if _, err := tx.Exec(ctx, `
			INSERT INTO settlement_lines (batch_id, provider, provider_line_id, provider_payment_id,
			                              line_kind, direction, amount_minor, settled_on, match_class)
			VALUES ($1, $2, $3, 'rz_pay_nobody', 'payment', 'inward', 2500000, current_date, 'unknown_payment')`,
			batchID, provider, orphan); err != nil {
			return err
		}
		// And one that matched organisation A's payment.
		_, err := tx.Exec(ctx, `
			INSERT INTO settlement_lines (batch_id, tenant_id, provider, provider_line_id,
			                              provider_payment_id, line_kind, direction, amount_minor,
			                              settled_on, payment_id, match_class, matched_at)
			VALUES ($1, $2, $3, $4, $5, 'payment', 'inward', 2750000, current_date, $6, 'exact', now())`,
			batchID, isolationtest.OrgA.String(), provider, matched,
			"rz_pay_"+provider, paymentID)
		return err
	}); err != nil {
		t.Fatalf("writing settlement lines: %v", err)
	}

	visible := func(org tenancy.ID, lineID string) int {
		t.Helper()
		var n int
		if err := tenancy.Scoped(tenancy.With(ctx, org), p, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM settlement_lines WHERE provider_line_id = $1`, lineID).Scan(&n)
		}); err != nil {
			t.Fatalf("reading as %s: %v", org, err)
		}
		return n
	}

	if got := visible(isolationtest.OrgA, orphan); got != 0 {
		t.Errorf("organisation A sees %d unmatched line(s) — a line attributed to nobody must not be "+
			"readable by somebody", got)
	}
	if got := visible(isolationtest.OrgB, orphan); got != 0 {
		t.Errorf("organisation B sees %d unmatched line(s)", got)
	}
	if got := visible(isolationtest.OrgA, matched); got != 1 {
		t.Errorf("organisation A sees %d of its own settled lines, want 1 — an owner asking whether their "+
			"rent has settled has a right to the answer", got)
	}
	if got := visible(isolationtest.OrgB, matched); got != 0 {
		t.Errorf("organisation B sees %d of organisation A's settled lines", got)
	}

	// Attribution is the platform's, made after the payment is found.
	err := tenancy.Scoped(tenancy.With(ctx, isolationtest.OrgB), p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE settlement_lines SET tenant_id = $1 WHERE provider_line_id = $2`,
			isolationtest.OrgB.String(), orphan)
		return err
	})
	if err == nil {
		var claimed int
		_ = tenancy.Platform(ctx, plat, "checking the claim", func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM settlement_lines
				 WHERE provider_line_id = $1 AND tenant_id = $2`,
				orphan, isolationtest.OrgB.String()).Scan(&claimed)
		})
		if claimed > 0 {
			t.Error("organisation B claimed an unmatched settlement line — attributing a line by anything " +
				"other than the provider's payment id is a cross-tenant write")
		}
	}

	// The platform sees both, because the sweep has to.
	if err := tenancy.Platform(ctx, plat, "the reconciliation sweep", func(ctx context.Context, tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM settlement_lines WHERE provider = $1`, provider).Scan(&n); err != nil {
			return err
		}
		if n != 2 {
			t.Errorf("the sweep sees %d lines, want 2 — unmatched means kept, not dropped", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("reading as platform: %v", err)
	}
}

// Drift is read by the organisation whose money it is and resolved only by the
// platform. The split matters: an owner asking where their rent is has a right to
// the answer, and a decision to write money off spans organisations because the
// clearing account does.
func TestDriftIsReadableByItsOwnerAndResolvableOnlyByThePlatform(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	provider := "harness-" + randomToken(t)
	paymentID := mustCapturePayment(t, plat, isolationtest.OrgA, provider)

	var driftID string
	if err := tenancy.Platform(ctx, plat, "recording drift", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO settlement_drift (tenant_id, provider, as_of_date, drift_kind,
			                              payment_id, amount_minor, since)
			VALUES ($1, $2, current_date, 'missing_settlement', $3, 2750000, now() - interval '4 days')
			RETURNING id`,
			isolationtest.OrgA.String(), provider, paymentID).Scan(&driftID)
	}); err != nil {
		t.Fatalf("recording drift: %v", err)
	}

	count := func(org tenancy.ID) int {
		t.Helper()
		var n int
		if err := tenancy.Scoped(tenancy.With(ctx, org), p, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM settlement_drift WHERE id = $1`, driftID).Scan(&n)
		}); err != nil {
			t.Fatalf("reading as %s: %v", org, err)
		}
		return n
	}
	if got := count(isolationtest.OrgA); got != 1 {
		t.Errorf("organisation A sees %d of its own drift rows, want 1", got)
	}
	if got := count(isolationtest.OrgB); got != 0 {
		t.Errorf("organisation B sees %d of organisation A's drift rows", got)
	}

	// The organisation whose money it is still cannot close it.
	err := tenancy.Scoped(tenancy.With(ctx, isolationtest.OrgA), p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE settlement_drift
			   SET state = 'resolved', resolution_note = 'nothing to see here',
			       resolved_by = gen_random_uuid(), resolved_at = now()
			 WHERE id = $1`, driftID)
		return err
	})
	if err == nil {
		var state string
		_ = tenancy.Platform(ctx, plat, "checking the state", func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT state FROM settlement_drift WHERE id = $1`, driftID).Scan(&state)
		})
		if state != "open" {
			t.Error("an organisation resolved its own drift row — the clearing account spans organisations, " +
				"so a resolution decided inside one is a decision about somebody else's money")
		}
	}

	// A resolution with no note and no actor is a row somebody closed rather than
	// a row somebody explained.
	err = tenancy.Platform(ctx, plat, "closing without a note", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE settlement_drift SET state = 'resolved' WHERE id = $1`, driftID)
		return err
	})
	if err == nil {
		t.Error("a drift row was resolved with no note, no actor and no timestamp")
	}

	// And a write-off has to post: money abandoned that the ledger does not know
	// about is the same defect payments_captured_has_entry exists to prevent.
	err = tenancy.Platform(ctx, plat, "writing off without an entry", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE settlement_drift
			   SET state = 'written_off', resolution_note = 'unrecoverable',
			       resolved_by = gen_random_uuid(), resolved_at = now()
			 WHERE id = $1`, driftID)
		return err
	})
	if err == nil {
		t.Error("a clearing balance was written off with no ledger entry behind it")
	} else if !strings.Contains(err.Error(), "settlement_drift_write_off_posts") {
		t.Logf("refused, though not by the expected constraint: %v", err)
	}

	// Nothing here may be deleted, whoever asks.
	err = tenancy.Platform(ctx, plat, "deleting drift", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM settlement_drift WHERE id = $1`, driftID)
		return err
	})
	if err == nil {
		var n int
		_ = tenancy.Platform(ctx, plat, "counting", func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM settlement_drift WHERE id = $1`, driftID).Scan(&n)
		})
		if n == 0 {
			t.Error("a drift row was deleted — a deleted disagreement is indistinguishable from one " +
				"that was never found")
		}
	}
}

// The acceptance criterion, made structural. ADR-0012 §8.
//
// The job that reconciles a day is the same job that would report it clean, so its
// own report is not evidence. What is evidence is the drift table, and the database
// is what reads it: `reconciled` is refused while anything is open, and the run's
// counters are recomputed rather than accepted.
func TestADayCannotBeCalledReconciledWhileDriftIsOpen(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	provider := "harness-" + randomToken(t)
	mustCapturePayment(t, plat, isolationtest.OrgA, provider)
	today := time.Now().UTC().Format("2006-01-02")

	// A clean day reconciles.
	if err := tenancy.Platform(ctx, plat, "a clean run", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO reconciliation_runs (provider, as_of_date, state, file_present,
			                                 lines_read, lines_matched, completed_at)
			VALUES ($1, $2::date, 'reconciled', true, 497, 497, now())`, provider, today)
		return err
	}); err != nil {
		t.Fatalf("a clean day was refused: %v", err)
	}

	// Three payments go missing. The same day can no longer be called reconciled.
	if err := tenancy.Platform(ctx, plat, "recording drift", func(ctx context.Context, tx pgx.Tx) error {
		for range 3 {
			if _, err := tx.Exec(ctx, `
				INSERT INTO settlement_drift (tenant_id, provider, as_of_date, drift_kind,
				                              provider_payment_id, amount_minor, since)
				VALUES ($1, $2, $3::date, 'missing_settlement', $4, 2750000,
				        now() - interval '3 days')`,
				isolationtest.OrgA.String(), provider, today, "rz-"+randomToken(t)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("recording drift: %v", err)
	}

	err := tenancy.Platform(ctx, plat, "claiming a clean day", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE reconciliation_runs SET state = 'reconciled', completed_at = now()
			 WHERE provider = $1 AND as_of_date = $2::date`, provider, today)
		return err
	})
	if err == nil {
		t.Fatal("a day with three open drift rows was called reconciled — a day that quietly becomes " +
			"reconciled because nobody looked is how money goes missing")
	}
	t.Logf("refused: %v", err)

	// `drift` is the honest ending, and the counters come from the drift table
	// rather than from whatever the job passed in.
	if err := tenancy.Platform(ctx, plat, "an honest run", func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE reconciliation_runs
			   SET state = 'drift', completed_at = now(),
			       unresolved_count = 0, unresolved_minor = 0
			 WHERE provider = $1 AND as_of_date = $2::date`, provider, today); err != nil {
			return err
		}
		var count int
		var minor int64
		if err := tx.QueryRow(ctx, `
			SELECT unresolved_count, unresolved_minor FROM reconciliation_runs
			 WHERE provider = $1 AND as_of_date = $2::date`, provider, today).Scan(&count, &minor); err != nil {
			return err
		}
		if count != 3 || minor != 3*2_750_000 {
			t.Errorf("the run reports %d item(s) worth %d after being told zero — the counters must be "+
				"computed from the drift table, or a job can report whatever it likes", count, minor)
		}
		return nil
	}); err != nil {
		t.Fatalf("the honest ending was refused: %v", err)
	}

	// And the failure scenario: a day whose file never arrived. A comparison over
	// no lines looks perfectly clean, and calling it reconciled is the defect.
	err = tenancy.Platform(ctx, plat, "a day with no file", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO reconciliation_runs (provider, as_of_date, state, file_present, completed_at)
			VALUES ($1, ($2::date - 1), 'reconciled', false, now())`, provider, today)
		return err
	})
	if err == nil {
		t.Error("a day whose settlement file never arrived was called reconciled")
	}
}

// The batch's own arithmetic, in the database as well as in Go. The failure this
// prevents is one where our numbers are wrong rather than the provider's, and a
// guard that lives only in the code that got it wrong is not a guard.
func TestABatchThatDoesNotAddUpIsRefusedByTheDatabase(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)

	provider := "harness-" + randomToken(t)
	// gross 100000, refunds 5000, fee 2000, tax 360 → net must be 92640.
	err := tenancy.Platform(ctx, plat, "an inconsistent batch", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO settlement_batches (provider, provider_batch_id, settled_on,
			                                gross_minor, refund_minor, fee_minor, tax_minor, net_minor)
			VALUES ($1, 'off-by-one', current_date, 100000, 5000, 2000, 360, 92641)`, provider)
		return err
	})
	if err == nil {
		t.Fatal("a settlement batch one paise out was ingested — a file we parsed wrong is now " +
			"indistinguishable from a file we disagree with")
	}
	if !strings.Contains(err.Error(), "settlement_batches_adds_up") {
		t.Errorf("refused, but not by the arithmetic constraint: %v", err)
	}
	t.Logf("refused: %v", err)
}

// Two classes may post and four may not, and the database holds half of that rule:
// a line that did not reconcile cannot have caused an entry, whatever the code
// believed at the time.
func TestALineThatDidNotReconcileCannotHaveCausedAnEntry(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	provider := "harness-" + randomToken(t)
	batchID := mustIngestBatch(t, plat, provider, 2_750_000, 0, 0, 0, 2_750_000)
	paymentID := mustCapturePayment(t, plat, isolationtest.OrgA, provider)

	var entryID string
	if err := tenancy.Platform(ctx, plat, "reading the payment's entry", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT entry_id FROM payments WHERE id = $1`, paymentID).Scan(&entryID)
	}); err != nil {
		t.Fatalf("reading the entry: %v", err)
	}

	for _, class := range []string{"partial", "amount_drift", "duplicate", "unknown_payment"} {
		err := tenancy.Platform(ctx, plat, "posting an unreconciled line", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO settlement_lines (batch_id, tenant_id, provider, provider_line_id,
				                              provider_payment_id, line_kind, direction, amount_minor,
				                              settled_on, payment_id, match_class, entry_id)
				VALUES ($1, $2, $3, $4, $5, 'payment', 'inward', 1000000, current_date, $6, $7, $8)`,
				batchID, isolationtest.OrgA.String(), provider, "line-"+class+"-"+randomToken(t),
				"rz_pay_"+provider, paymentID, class, entryID)
			return err
		})
		if err == nil {
			t.Errorf("a %s line posted a settlement entry — only a line that reconciles in full may, "+
				"because anything else leaves a clearing residue no later line can clear", class)
		}
	}
}

// mustIngestBatch writes one settlement batch as the platform and returns its id.
func mustIngestBatch(t *testing.T, plat tenancy.PlatformPool, provider string,
	gross, refund, fee, tax, net int64) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := tenancy.Platform(ctx, plat, "ingesting a settlement batch", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO settlement_batches (provider, provider_batch_id, utr, settled_on,
			                                gross_minor, refund_minor, fee_minor, tax_minor, net_minor)
			VALUES ($1, $2, $3, current_date, $4, $5, $6, $7, $8) RETURNING id`,
			provider, "batch-"+provider, "UTR"+provider, gross, refund, fee, tax, net).Scan(&id)
	}); err != nil {
		t.Fatalf("ingesting a settlement batch: %v", err)
	}
	return id
}

// mustCapturePayment writes a captured payment with the ledger entry the schema
// requires behind it, and returns its id.
func mustCapturePayment(t *testing.T, plat tenancy.PlatformPool, org tenancy.ID, provider string) string {
	t.Helper()
	ctx := context.Background()
	var paymentID string
	err := tenancy.Platform(ctx, plat, "capturing a payment for the harness", func(ctx context.Context, tx pgx.Tx) error {
		var entryID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO journal_entries (tenant_id, entry_kind, occurred_on, source_kind,
			                             source_id, idempotency_key, memo)
			VALUES ($1, 'payment', current_date, 'recon-harness', $2, $2, 'recon harness')
			RETURNING id`, org.String(), provider).Scan(&entryID); err != nil {
			return err
		}
		for _, p := range []struct {
			account, side, party, partyID string
		}{
			{"gateway_clearing", "debit", "platform", "00000000-0000-0000-0000-0000000000d8"},
			{"tenant_receivable", "credit", "tenant", "aaaaaaaa-0000-0000-0000-00000000000c"},
		} {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ledger_postings (entry_id, tenant_id, property_id, account_code, side,
				                             amount_minor, party_kind, party_id)
				VALUES ($1, $2, $3, $4, $5, 2750000, $6, $7)`,
				entryID, org.String(), collectionProperty(org),
				p.account, p.side, p.party, p.partyID); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `
			INSERT INTO payments (tenant_id, property_id, payer_kind, payer_id, amount_minor,
			                      method, provider, provider_payment_id, status, idempotency_key,
			                      entry_id, captured_at)
			VALUES ($1, $2, 'tenant', gen_random_uuid(), 2750000, 'upi_collect', $3, $4,
			        'captured', $5, $6, now() - interval '3 days')
			RETURNING id`,
			org.String(), collectionProperty(org), provider, "rz_pay_"+provider,
			"recon-"+provider, entryID).Scan(&paymentID)
	})
	if err != nil {
		t.Fatalf("capturing a payment: %v", err)
	}
	return paymentID
}
