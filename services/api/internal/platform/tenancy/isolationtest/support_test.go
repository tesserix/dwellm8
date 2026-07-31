package isolationtest_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Issue #226: a support path that is unlogged is the exact shape of the incident
// that cannot be investigated afterwards.

func act(reason string) tenancy.Act {
	return tenancy.Act{
		ActorID: "00000000-0000-0000-0000-0000000000aa", ActorKind: tenancy.ActorSupport,
		TenantID: isolationtest.OrgA, Module: "money", Action: "unlocked",
		SubjectKind: "payment", SubjectID: "pay-1", Reason: reason,
	}
}

func auditRows(t *testing.T, plat tenancy.PlatformPool, reason string) int {
	t.Helper()
	var n int
	err := tenancy.Platform(context.Background(), plat, "counting audit rows",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM audit_events WHERE reason = $1`, reason).Scan(&n)
		})
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	return n
}

// A support action writes its own audit row.
func TestASupportActionAuditsItself(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	reason := "tenant reported a double debit — " + randomToken(t)
	if err := tenancy.Support(ctx, plat, act(reason), func(ctx context.Context, tx pgx.Tx) error {
		return nil
	}); err != nil {
		t.Fatalf("the support action failed: %v", err)
	}

	if n := auditRows(t, plat, reason); n != 1 {
		t.Fatalf("%d audit rows for one support action, want 1", n)
	}

	var actor, kind, subject string
	if err := tenancy.Platform(ctx, plat, "reading the audit row",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT actor_id::text, actor_kind, subject_id FROM audit_events WHERE reason = $1`,
				reason).Scan(&actor, &kind, &subject)
		}); err != nil {
		t.Fatalf("reading: %v", err)
	}
	if kind != "support" || subject != "pay-1" || actor == "" {
		t.Errorf("the audit row is %s/%s/%s", actor, kind, subject)
	}
}

// The part that is actually subtle: the audit is written in the same transaction
// as the work. An audit written afterwards records actions that were rolled
// back, and one written before records actions that never happened.
func TestAnActionThatFailsLeavesNoAuditRow(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	reason := "a support action that fails — " + randomToken(t)
	boom := errors.New("the work failed")
	err := tenancy.Support(ctx, plat, act(reason), func(ctx context.Context, tx pgx.Tx) error {
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("the failure was swallowed: %v", err)
	}

	if n := auditRows(t, plat, reason); n != 0 {
		t.Errorf("%d audit rows for an action that was rolled back — the trail claims something "+
			"the database does not show", n)
	}
}

// An action that cannot be audited cannot happen. The three questions afterwards
// are always who, to what and why, and a row answering two of them answers none.
func TestAnUnauditableActionIsRefused(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	ran := false
	for _, c := range []struct {
		name string
		a    tenancy.Act
	}{
		{"no actor", func() tenancy.Act { a := act("r"); a.ActorID = ""; return a }()},
		{"no organisation", func() tenancy.Act { a := act("r"); a.TenantID = ""; return a }()},
		{"not a module", func() tenancy.Act { a := act("r"); a.Module = "billing"; return a }()},
		{"no action", func() tenancy.Act { a := act("r"); a.Action = " "; return a }()},
		{"nothing acted on", func() tenancy.Act { a := act("r"); a.SubjectID = ""; return a }()},
		{"no reason", func() tenancy.Act { a := act(""); return a }()},
		{"a provider is not a support actor", func() tenancy.Act {
			a := act("r")
			a.ActorKind = "provider"
			return a
		}()},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := tenancy.Support(ctx, plat, c.a, func(ctx context.Context, tx pgx.Tx) error {
				ran = true
				return nil
			})
			if !errors.Is(err, tenancy.ErrAct) {
				t.Errorf("the action was allowed: %v", err)
			}
		})
	}
	if ran {
		t.Error("an unauditable action's work ran anyway")
	}
}

// The Go vocabulary against the CHECK that would refuse it — the same contract
// every other vocabulary in this schema has.
func TestTheGoAuditVocabulariesAreAcceptedByTheSchema(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)

	var def string
	if err := tenancy.Platform(ctx, plat, "reading the module CHECK",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT pg_get_constraintdef(oid) FROM pg_constraint
				 WHERE conname = 'audit_events_module_check'`).Scan(&def)
		}); err != nil {
		t.Fatalf("reading the CHECK: %v", err)
	}
	for _, m := range tenancy.Modules() {
		if !strings.Contains(def, "'"+m+"'") {
			t.Errorf("module %q is producible in Go and refused by the schema", m)
		}
	}

	var kinds string
	if err := tenancy.Platform(ctx, plat, "reading the actor CHECK",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT pg_get_constraintdef(oid) FROM pg_constraint
				 WHERE conname = 'audit_events_actor_kind_check'`).Scan(&kinds)
		}); err != nil {
		t.Fatalf("reading the CHECK: %v", err)
	}
	for _, k := range []tenancy.ActorKind{tenancy.ActorSupport, tenancy.ActorUser} {
		if !strings.Contains(kinds, "'"+string(k)+"'") {
			t.Errorf("actor kind %q is producible in Go and refused by the schema", k)
		}
	}
}
