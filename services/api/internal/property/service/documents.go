package service

import (
	"context"
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/property/domain"
	"github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// Document is what a firm holds against a property.
type Document = store.Document

// Ownership is what those documents prove about the right to let (#339).
type Ownership = domain.Ownership

// Documents lists what is on file for a property, newest first, with what the
// paperwork adds up to.
func (p *Properties) Documents(ctx context.Context, propertyID string) ([]Document, Ownership, error) {
	held, err := p.store.Documents(ctx, propertyID)
	if err != nil {
		return nil, Ownership{}, err
	}
	kinds := make([]string, 0, len(held))
	for _, d := range held {
		kinds = append(kinds, d.Kind)
	}
	return held, domain.OwnershipFrom(kinds), nil
}

// KnownDocumentKind reports whether a kind may be filed against a property, so
// a surface can refuse it as a sentence rather than as a rejected write.
func KnownDocumentKind(kind string) bool { return domain.KnownDocumentKind(kind) }

// RecordDocument files a document that has already reached the bucket. A firm
// that markets a flat on somebody's say-so has no answer when the real owner
// appears, so an unknown kind is refused rather than filed as a blob.
func (p *Properties) RecordDocument(ctx context.Context, tenantID string, d Document) (string, error) {
	if !domain.KnownDocumentKind(d.Kind) {
		return "", fmt.Errorf("%q is not a document this holds against a property", d.Kind)
	}
	return p.store.RecordDocument(ctx, tenantID, d)
}
