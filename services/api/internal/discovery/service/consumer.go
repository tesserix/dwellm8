package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
)

// Consumer closes adverts when their unit is let. ADR-0019, #135: a listing's
// status follows occupancy automatically, and this durable consumer on the
// lease stream is the mechanism — the moment a tenancy starts, every live or
// paused listing for that unit dies, with no manual step and no way to forget.
type Consumer struct {
	Marker *store.LetMarker
	Log    *slog.Logger
}

// Handle decodes one event and reacts to tenancies starting. Everything else
// is acknowledged untouched — most facts move no listings.
func (c Consumer) Handle(ctx context.Context, body []byte) error {
	var env struct {
		Type     string `json:"type"`
		TenantID string `json:"tenant_id"`
		Data     struct {
			UnitID string `json:"unit_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("discovery: undecodable event: %w", err)
	}
	if env.Type != "lease.tenancy.started" || env.TenantID == "" || env.Data.UnitID == "" {
		return nil
	}
	closed, err := c.Marker.MarkLetByUnit(ctx, env.TenantID, env.Data.UnitID)
	if err != nil {
		return fmt.Errorf("discovery: closing listings for unit %s: %w", env.Data.UnitID, err)
	}
	if closed > 0 {
		c.Log.Info("listings closed by tenancy start", "unit", env.Data.UnitID, "closed", closed)
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
