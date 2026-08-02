package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
)

// Consumer reacts to the facts that move discovery's rows. Two today: a
// tenancy starting kills every advert for its unit (ADR-0019, #135), and a
// payment arriving confirms the stay booking it pays for (#233) — money that
// actually moved beats a lapsed hold timer.
type Consumer struct {
	Marker *store.LetMarker
	// Stay is optional: nil means payment facts confirm nothing here, which is
	// how a test that only cares about listings wires it.
	Stay *store.Stay
	Log  *slog.Logger
}

// Handle decodes one event and reacts to the two it knows. Everything else is
// acknowledged untouched — most facts move nothing of discovery's.
func (c Consumer) Handle(ctx context.Context, body []byte) error {
	var env struct {
		Type     string `json:"type"`
		TenantID string `json:"tenant_id"`
		Subject  struct {
			ID string `json:"id"`
		} `json:"subject"`
		Data struct {
			UnitID string `json:"unit_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("discovery: undecodable event: %w", err)
	}

	switch env.Type {
	case "lease.tenancy.started":
		if env.TenantID == "" || env.Data.UnitID == "" {
			return nil
		}
		closed, err := c.Marker.MarkLetByUnit(ctx, env.TenantID, env.Data.UnitID)
		if err != nil {
			return fmt.Errorf("discovery: closing listings for unit %s: %w", env.Data.UnitID, err)
		}
		if closed > 0 {
			c.Log.Info("listings closed by tenancy start", "unit", env.Data.UnitID, "closed", closed)
		}
	case "money.payment.received":
		if c.Stay == nil || env.Subject.ID == "" {
			return nil
		}
		booking, err := c.Stay.ConfirmByPayment(ctx, env.Subject.ID)
		if err != nil {
			return fmt.Errorf("discovery: confirming a stay for payment %s: %w", env.Subject.ID, err)
		}
		if booking != "" {
			c.Log.Info("stay confirmed by payment", "booking", booking, "payment", env.Subject.ID)
		}
	}
	return nil
}

// FromLeases adapts the lease module's service to TenancyDrafter — the whole
// of the conversion coupling, visible in one type (ADR-0001 §3).
type FromLeases struct{ Leases *leaseservice.Leases }

// DraftTenancy carries the application across the module boundary.
func (f FromLeases) DraftTenancy(ctx context.Context, a TenancyApplication) (string, error) {
	out, err := f.Leases.DraftFromApplication(ctx, leaseservice.Application{
		PropertyID: a.PropertyID, UnitID: a.UnitID,
		Start: a.Start, End: a.End,
		RentMinor: a.RentMinor, DepositMinor: a.DepositMinor,
		TenantName: a.TenantName, TenantPhone: a.TenantPhone, TenantEmail: a.TenantEmail,
	})
	if err != nil {
		return "", err
	}
	return out.ID, nil
}
