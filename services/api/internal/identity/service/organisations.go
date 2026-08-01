package service

import (
	"context"

	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Organisations answers the one question another module may ask about an
// organisation it can name: what to call it. The seam exists because surfaces
// compose services, never stores (ADR-0001 §3) — the arch guard enforces it.
type Organisations struct{ store *store.Organisations }

// NewOrganisations wires the service over the store.
func NewOrganisations(s *store.Organisations) *Organisations {
	return &Organisations{store: s}
}

// Name returns an organisation's display name and kind.
func (o *Organisations) Name(ctx context.Context, id tenancy.ID) (name, kind string, err error) {
	return o.store.Name(ctx, id)
}
