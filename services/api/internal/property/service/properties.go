// Package service is the property module's public interface — the extraction
// seam other modules reach it through, per property/doc.go.
package service

import (
	"context"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/property/domain"
	"github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// Property is what a caller outside this module sees.
type Property = domain.Property

// Unit is one lettable space in it.
type Unit = domain.Unit

// ErrNoProperty is re-exported so a caller can compare against it without an
// import of store, which is exactly the kind of reach-through property/doc.go
// forbids.
var ErrNoProperty = store.ErrNoProperty

// ErrNoUnit is re-exported for the same reason.
var ErrNoUnit = store.ErrNoUnit

// Properties is the module's service.
type Properties struct{ store *store.Properties }

// New wires the service over the store.
func New(s *store.Properties) *Properties { return &Properties{store: s} }

// List returns every property the caller's session may see.
func (p *Properties) List(ctx context.Context) ([]Property, error) {
	return p.store.List(ctx)
}

// Get reads one property.
func (p *Properties) Get(ctx context.Context, id string) (Property, error) {
	return p.store.Get(ctx, id)
}

// Units returns the property's lettable units.
func (p *Properties) Units(ctx context.Context, id string) ([]Unit, error) {
	return p.store.Units(ctx, id)
}

// Unit reads one unit of the register.
func (p *Properties) Unit(ctx context.Context, id string) (Unit, error) {
	return p.store.Unit(ctx, id)
}

// Ancillaries returns what is allotted to a unit — parking, storage.
func (p *Properties) Ancillaries(ctx context.Context, unitID string) ([]Unit, error) {
	return p.store.Ancillaries(ctx, unitID)
}

// TenantOf returns the organisation id that holds this property.
func (p *Properties) TenantOf(ctx context.Context, propertyID string) (string, error) {
	return p.store.TenantOf(ctx, propertyID)
}

// Register creates a property (issue #32) — the start of everything: a
// listing, a lease and a ledger all point at what this writes.
func (p *Properties) Register(ctx context.Context, d domain.PropertyDraft) (string, error) {
	return p.store.Create(ctx, d)
}

// RecordOwnership names who the rent is credited to, whole, from a date (#302).
func (p *Properties) RecordOwnership(ctx context.Context, propertyID, ownerPartyID string, from effective.Date) error {
	return p.store.RecordOwnership(ctx, propertyID, ownerPartyID, from)
}

// AddUnit adds a lettable unit or its parking to a property.
func (p *Properties) AddUnit(ctx context.Context, propertyID string, d domain.UnitDraft) (string, error) {
	return p.store.CreateUnit(ctx, propertyID, d)
}

// Bed is what is let in a hostel or a PG.
type Bed = domain.Bed

// ErrNoBed is no such bed, re-exported for the same reason as ErrNoUnit.
var ErrNoBed = store.ErrNoBed

// ErrBedExists is a repeated bed label, re-exported on the same terms.
var ErrBedExists = store.ErrBedExists

// Beds returns a property's bed board.
func (p *Properties) Beds(ctx context.Context, propertyID string) ([]Bed, error) {
	return p.store.Beds(ctx, propertyID)
}

// AddBed puts a bed in a room.
func (p *Properties) AddBed(ctx context.Context, unitID, label string, rentMinor int64) (Bed, error) {
	return p.store.AddBed(ctx, unitID, label, rentMinor)
}

// AllocateBed moves a bed between states, holding it against a lease.
func (p *Properties) AllocateBed(ctx context.Context, bedID, state, leaseID string) error {
	return p.store.AllocateBed(ctx, bedID, state, leaseID)
}
