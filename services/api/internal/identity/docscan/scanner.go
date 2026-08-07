package docscan

import (
	"context"
	"fmt"
)

// MaxImageBytes is the ceiling on a document photograph. A phone camera picture
// is well under it; anything above is not a document.
const MaxImageBytes = 8 << 20

// minImageBytes rejects a truncated upload before it is paid for.
const minImageBytes = 1 << 10

// Recogniser turns a photograph into text. It is an interface so the engine is
// swappable — Cloud Vision today, an on-device recogniser or another vendor
// later — without any of the parsing above knowing which one ran.
type Recogniser interface {
	Text(ctx context.Context, image []byte) (string, error)
}

// Scanner reads identity documents. It returns fields to be confirmed by the
// person holding the document; it stores nothing and logs nothing it read.
type Scanner struct{ engine Recogniser }

func New(engine Recogniser) *Scanner { return &Scanner{engine: engine} }

func (s *Scanner) Passport(ctx context.Context, image []byte) (Passport, error) {
	text, err := s.read(ctx, image)
	if err != nil {
		return Passport{}, err
	}
	return ParseMRZ(text)
}

func (s *Scanner) PAN(ctx context.Context, image []byte) (PANCard, error) {
	text, err := s.read(ctx, image)
	if err != nil {
		return PANCard{}, err
	}
	return ParsePANCard(text)
}

func (s *Scanner) read(ctx context.Context, image []byte) (string, error) {
	switch {
	case len(image) < minImageBytes:
		return "", fmt.Errorf("docscan: %d bytes is not a photograph of a document", len(image))
	case len(image) > MaxImageBytes:
		return "", fmt.Errorf("docscan: the image is larger than the %d byte ceiling", MaxImageBytes)
	}
	text, err := s.engine.Text(ctx, image)
	if err != nil {
		return "", fmt.Errorf("docscan: reading the image: %w", err)
	}
	return text, nil
}
