package isolationtest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	"github.com/tesserix/dwellm8/services/api/internal/platform/workflow"
)

// ADR-0015. The durable-operation record is ordinary tenant-scoped data, unlike
// ADR-0012's reconciliation tables, so it gets ADR-0003's five-part contract rather
// than the platform-inbox treatment.
//
// That difference is the interesting part and is why both tests exist. A settlement
// batch spans every organisation and belongs to none. A payout belongs to exactly
// one owner, and a platform-wide run carries the platform organisation instead of
// carrying no organisation at all — so tenant_id stays NOT NULL and this table stays
// off assertion 12's list.

func TestWorkflowRunsIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "workflow_runs",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO workflow_runs (tenant_id, operation, task_queue, workflow_id,
				                           subject_kind, subject_id)
				VALUES ($1, 'payout.execute', 'dwellm8-payout', $2, 'payout', $3)`,
				tenant.String(), "dwellm8:payout.execute:"+token+"-"+string(tenant)[:1], token)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM workflow_runs WHERE subject_id = $1`, token).Scan(&n)
			return n, err
		},
	})
}

func TestWorkflowStepsIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "workflow_steps",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			var runID string
			if err := tx.QueryRow(ctx, `
				INSERT INTO workflow_runs (tenant_id, operation, task_queue, workflow_id,
				                           subject_kind, subject_id)
				VALUES ($1, 'payout.execute', 'dwellm8-payout', $2, 'payout', $3)
				RETURNING id`,
				tenant.String(), "dwellm8:payout.execute:step-"+token+"-"+string(tenant)[:1],
				token).Scan(&runID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO workflow_steps (run_id, tenant_id, seq, step, direction, idempotency_key)
				VALUES ($1, $2, 0, 'post_platform_fee', 'do', $3)`,
				runID, tenant.String(), token+"#post_platform_fee")
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM workflow_steps WHERE idempotency_key LIKE $1`, token+"%").Scan(&n)
			return n, err
		},
	})
}

// The constraint that carries ADR-0015 §4's rule into the database, exercised
// against a real row rather than read.
//
// A run that passed the point of no return cannot have been compensated, because
// nothing after that point can be undone. The Go saga refuses to try; this is what
// stops anything else — a data fix, a support script, a workflow written by somebody
// who has not read the standard — recording that the world was put back after money
// had already left.
func TestARunPastThePointOfNoReturnCannotBeRecordedAsCompensated(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	tok := randomToken(t)
	id := "dwellm8:payout.execute:" + tok

	var runID string
	if err := tenancy.Platform(ctx, plat, "starting a run", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO workflow_runs (tenant_id, operation, task_queue, workflow_id,
			                           subject_kind, subject_id, past_no_return)
			VALUES ($1, 'payout.execute', 'dwellm8-payout', $2, 'payout', $3, true)
			RETURNING id`,
			isolationtest.OrgA.String(), id, tok).Scan(&runID)
	}); err != nil {
		t.Fatalf("starting a run past the point of no return: %v", err)
	}

	// Compensated is refused.
	err := tenancy.Platform(ctx, plat, "claiming a compensation", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE workflow_runs SET state = 'compensating' WHERE id = $1`, runID)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE workflow_runs SET state = 'compensated', completed_at = now() WHERE id = $1`, runID)
		return err
	})
	if err == nil {
		t.Fatal("a run past the point of no return was recorded as compensated — that claims the world " +
			"was put back after the money had already left, and every report downstream would believe it")
	}
	if !strings.Contains(err.Error(), "workflow_runs_compensated_means_reversible") {
		t.Errorf("refused, but not by the constraint that means it: %v", err)
	}
	t.Logf("refused: %v", err)

	// Escalated is the honest ending for that run, and it needs a reason.
	err = tenancy.Platform(ctx, plat, "escalating with no reason", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE workflow_runs SET state = 'escalated', completed_at = now() WHERE id = $1`, runID)
		return err
	})
	if err == nil {
		t.Error("a run escalated with no reason and no timestamp — a row somebody closed is not a row " +
			"somebody explained")
	}

	if err := tenancy.Platform(ctx, plat, "escalating properly", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE workflow_runs
			   SET state = 'escalated', escalated_at = now(), completed_at = now(),
			       escalation_reason = 'the bank transfer timed out and may have landed'
			 WHERE id = $1`, runID)
		return err
	}); err != nil {
		t.Fatalf("the honest ending was refused: %v", err)
	}
}

// past_no_return is monotonic. Without that, the constraint above could be satisfied
// by editing the evidence rather than by the world being reversible — clear the flag,
// then record the compensation.
func TestThePointOfNoReturnCannotBeUnpassed(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	tok := randomToken(t)
	var runID string
	if err := tenancy.Platform(ctx, plat, "starting a run", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO workflow_runs (tenant_id, operation, task_queue, workflow_id,
			                           subject_kind, subject_id, past_no_return)
			VALUES ($1, 'payout.execute', 'dwellm8-payout', $2, 'payout', $3, true)
			RETURNING id`,
			isolationtest.OrgA.String(), "dwellm8:payout.execute:mono-"+tok, tok).Scan(&runID)
	}); err != nil {
		t.Fatalf("starting: %v", err)
	}

	err := tenancy.Platform(ctx, plat, "un-passing the boundary", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE workflow_runs SET past_no_return = false WHERE id = $1`, runID)
		return err
	})
	if err == nil {
		t.Fatal("the point of no return was cleared, which lets the compensation constraint be " +
			"satisfied by editing the evidence")
	}
	t.Logf("refused: %v", err)
}

// A run walks forward only, and the same state twice is a no-op — the redelivery
// case, exactly as for a payment in ADR-0011 §3.
func TestARunWalksForwardOnly(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	tok := randomToken(t)
	var runID string
	if err := tenancy.Platform(ctx, plat, "starting a run", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO workflow_runs (tenant_id, operation, task_queue, workflow_id,
			                           subject_kind, subject_id)
			VALUES ($1, 'refund.issue', 'dwellm8-refund', $2, 'payment', $3)
			RETURNING id`,
			isolationtest.OrgA.String(), "dwellm8:refund.issue:"+tok, tok).Scan(&runID)
	}); err != nil {
		t.Fatalf("starting: %v", err)
	}

	set := func(state string, extra string) error {
		return tenancy.Platform(ctx, plat, "moving a run", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`UPDATE workflow_runs SET state = $2`+extra+` WHERE id = $1`, runID, state)
			return err
		})
	}

	// running -> running is permitted and changes nothing: a step recorded twice
	// asks for the state the run is already in.
	if err := set("running", ""); err != nil {
		t.Errorf("running -> running was refused, so a step recorded twice would fail: %v", err)
	}
	if err := set("compensating", ""); err != nil {
		t.Fatalf("running -> compensating: %v", err)
	}
	// And backwards is refused.
	if err := set("running", ""); err == nil {
		t.Error("compensating -> running was accepted, so a run can re-open after it began unwinding")
	}
	if err := set("compensated", ", completed_at = now()"); err != nil {
		t.Fatalf("compensating -> compensated: %v", err)
	}
	// Terminal absorbs.
	if err := set("compensating", ", completed_at = NULL"); err == nil {
		t.Error("a compensated run was re-opened")
	}
}

// The deterministic workflow id is what makes starting an operation twice a no-op
// rather than two workflows. It is a unique index, so the guarantee holds when two
// requests race — which is exactly when a double-tap happens.
func TestStartingTheSameOperationTwiceCollides(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	subject := randomToken(t)
	id, err := workflow.ID(workflow.OpPayoutExecute, subject)
	if err != nil {
		t.Fatalf("ID: %v", err)
	}

	start := func() error {
		return tenancy.Platform(ctx, plat, "starting a run", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO workflow_runs (tenant_id, operation, task_queue, workflow_id,
				                           subject_kind, subject_id)
				VALUES ($1, $2, $3, $4, 'payout', $5)`,
				isolationtest.OrgA.String(), string(workflow.OpPayoutExecute),
				"dwellm8-payout", id, subject)
			return err
		})
	}
	if err := start(); err != nil {
		t.Fatalf("the first start was refused: %v", err)
	}
	err = start()
	if err == nil {
		t.Fatal("the same payout started twice — a double-tapped button would produce two payouts")
	}
	if !strings.Contains(err.Error(), "workflow_runs_workflow_idx") {
		t.Errorf("refused, but not by the deterministic-id index: %v", err)
	}
	t.Logf("refused: %v", err)
}

// A retried activity updates its step row rather than adding one. Forty retries over
// a day must read as one step that took forty attempts, not as forty steps — the
// trail is for a person, and an unreadable trail is the same as none.
func TestARetriedStepIsOneRow(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	tok := randomToken(t)
	var runID string
	if err := tenancy.Platform(ctx, plat, "starting a run", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO workflow_runs (tenant_id, operation, task_queue, workflow_id,
			                           subject_kind, subject_id)
			VALUES ($1, 'payout.execute', 'dwellm8-payout', $2, 'payout', $3)
			RETURNING id`,
			isolationtest.OrgA.String(), "dwellm8:payout.execute:retry-"+tok, tok).Scan(&runID)
	}); err != nil {
		t.Fatalf("starting: %v", err)
	}

	key := "dwellm8:payout.execute:retry-" + tok + "#bank_transfer"
	if err := tenancy.Platform(ctx, plat, "recording attempts", func(ctx context.Context, tx pgx.Tx) error {
		for range 40 {
			if _, err := tx.Exec(ctx, `
				INSERT INTO workflow_steps (run_id, tenant_id, seq, step, direction, idempotency_key)
				VALUES ($1, $2, 3, 'bank_transfer', 'do', $3)
				ON CONFLICT (run_id, step, direction)
				DO UPDATE SET attempts = workflow_steps.attempts + 1`,
				runID, isolationtest.OrgA.String(), key); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("recording attempts: %v", err)
	}

	var rows, attempts int
	if err := tenancy.Platform(ctx, plat, "reading the trail", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT count(*), coalesce(max(attempts), 0) FROM workflow_steps
			 WHERE run_id = $1 AND step = 'bank_transfer'`, runID).Scan(&rows, &attempts)
	}); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if rows != 1 {
		t.Errorf("forty attempts produced %d step rows, want 1 — the trail is for a person", rows)
	}
	if attempts != 40 {
		t.Errorf("the row records %d attempts, want 40 — \"it eventually worked\" and \"it worked first "+
			"time\" are different facts about a provider", attempts)
	}
	// And the key that was actually presented is recorded, because the answer to
	// "was the tenant charged twice" has to be what was sent, not what today's code
	// would send.
	var recorded string
	if err := tenancy.Platform(ctx, plat, "reading the key", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT idempotency_key FROM workflow_steps WHERE run_id = $1 AND step = 'bank_transfer'`,
			runID).Scan(&recorded)
	}); err != nil {
		t.Fatalf("reading the key: %v", err)
	}
	if recorded != key {
		t.Errorf("the trail records key %q and the step presented %q", recorded, key)
	}
}
