// Package service is the property module's public interface — the extraction
// seam other modules reach it through, per property/doc.go.
package service

import (
	"context"

	"github.com/tesserix/dwellm8/services/api/internal/property/domain"
	"github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// Property is what a caller outside this module sees.
type Property = domain.Property

// ErrNoProperty is re-exported so a caller can compare against it without an
// import of store, which is exactly the kind of reach-through property/doc.go
// forbids.
var ErrNoProperty = store.ErrNoProperty

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

// TenantOf returns the organisation id that holds this property.
func (p *Properties) TenantOf(ctx context.Context, propertyID string) (string, error) {
	return p.store.TenantOf(ctx, propertyID)
}

// Register creates a property (issue #32) — the start of everything: a
// listing, a lease and a ledger all point at what this writes.
func (p *Properties) Register(ctx context.Context, d domain.PropertyDraft) (string, error) {
	return p.store.Create(ctx, d)
}

// AddUnit adds a lettable unit or its parking to a property.
func (p *Properties) AddUnit(ctx context.Context, propertyID string, d domain.UnitDraft) (string, error) {
	return p.store.CreateUnit(ctx, propertyID, d)
}
