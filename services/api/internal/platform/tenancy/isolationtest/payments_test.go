package isolationtest_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0011. The payments table is a new data path, so it gets ADR-0003's
// five-part contract like every other tenant-scoped table.
//
// It is worth having separately from the ledger's contract because the policy is
// a different shape: payments carries a NOT NULL property_id, so its delegated
// branch is unconditional where ledger_postings' has to guard against a posting
// that belongs to no building. A policy that is correct for one is not
// automatically correct for the other.
func TestPaymentsIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "payments",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO payments (tenant_id, property_id, payer_kind, payer_id,
				                      amount_minor, method, provider, idempotency_key)
				VALUES ($1, $2, 'tenant', gen_random_uuid(), 2750000,
				        'upi_collect', 'razorpay', $3)`,
				tenant.String(), collectionProperty(tenant), token)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM payments WHERE idempotency_key = $1`, token).Scan(&n)
			return n, err
		},
	})
}

// The webhook inbox is the one table in the schema whose rows may belong to no
// organisation at all — a delivery naming a payment this system has never seen.
// That is what "parked, not dropped" means structurally, and it has to be
// checked from the other side: a tenant session must not see a parked event, and
// must not be able to write one.
func TestParkedWebhooksAreInvisibleToEveryTenant(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	eventID := "evt-" + t.Name() + "-" + randomToken(t)

	// The handler runs as the platform role, because at the moment a delivery
	// arrives there is no organisation to attribute it to.
	if err := tenancy.Platform(ctx, plat, "parking a delivery for the harness", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO payment_events (provider, provider_event_id, event_type,
			                            signature_verified, payload, park_reason)
			VALUES ('razorpay', $1, 'payment.captured', true, '{}'::jsonb, 'unknown_payment')`,
			eventID)
		return err
	}); err != nil {
		t.Fatalf("parking a delivery: %v", err)
	}

	for _, org := range []tenancy.ID{isolationtest.OrgA, isolationtest.OrgB} {
		if err := tenancy.Scoped(tenancy.With(ctx, org), p, func(ctx context.Context, tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(ctx,
				`SELECT count(*) FROM payment_events WHERE provider_event_id = $1`,
				eventID).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				t.Errorf("organisation %s sees %d parked event(s) — a delivery attributed to "+
					"nobody must not be readable by somebody", org, n)
			}
			return nil
		}); err != nil {
			t.Fatalf("reading as %s: %v", org, err)
		}

		// And cannot write one either: attributing a delivery is the platform's
		// call, made after the payment is found. In its own transaction, because
		// a refused write aborts the one it was attempted in.
		err := tenancy.Scoped(tenancy.With(ctx, org), p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO payment_events (tenant_id, provider, provider_event_id, event_type,
				                            signature_verified, payload)
				VALUES ($1, 'razorpay', $2, 'payment.captured', true, '{}'::jsonb)`,
				org.String(), eventID+"-forged-"+string(org)[:1])
			return err
		})
		if err == nil {
			t.Errorf("organisation %s wrote a webhook delivery — the inbox is the platform's", org)
		}
	}

	// The platform session, which is the reconciliation sweep, does see it.
	if err := tenancy.Platform(ctx, plat, "the reconciliation sweep", func(ctx context.Context, tx pgx.Tx) error {
		var n int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM payment_events WHERE provider_event_id = $1 AND park_reason = 'unknown_payment'`,
			eventID).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			t.Errorf("the platform sees %d parked events, want 1 — parked means kept, not dropped", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("reading as platform: %v", err)
	}
}

// Organisations A and B need somewhere to collect against: payments carries a
// NOT NULL property_id, unlike every other table the harness exercises.
var collectionProperties = map[tenancy.ID]string{
	isolationtest.OrgA: "aaaaaaaa-0000-4000-8000-00000000000a",
	isolationtest.OrgB: "bbbbbbbb-0000-4000-8000-00000000000b",
}

func collectionProperty(t tenancy.ID) string { return collectionProperties[t] }

func seedCollectionProperties(t *testing.T, p tenancy.PlatformPool) {
	t.Helper()
	ctx := context.Background()
	err := tenancy.Platform(ctx, p, "seeding collection properties", func(ctx context.Context, tx pgx.Tx) error {
		for org, id := range collectionProperties {
			if _, err := tx.Exec(ctx, `
				INSERT INTO properties (id, tenant_id, code, name, kind, address_line1,
				                        locality, city, state_code, pin, state)
				VALUES ($1, $2, $3, 'Harness Collection', 'building', '1 Harness Road',
				        'Indiranagar', 'Bengaluru', 'KA', '560038', 'active')
				ON CONFLICT (id) DO NOTHING`,
				id, org.String(), "HARNESS-COLLECT-"+string(org)[:1]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding collection properties: %v", err)
	}
}

func randomToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("token: %v", err)
	}
	return hex.EncodeToString(b)
}
