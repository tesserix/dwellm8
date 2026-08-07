package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
)

// The owner's own paperwork (#318). An owner abroad cannot produce an original,
// so what is held is a copy and the question worth answering later is what the
// attestation on it was worth.

func aPassportCopy() store.PartyDocument {
	return store.PartyDocument{
		Kind:           "passport",
		ObjectPath:     "org/x/party-documents/" + uuid.NewString() + ".pdf",
		Filename:       "passport.pdf",
		ContentType:    "application/pdf",
		IssuingCountry: "IN",
		NumberMasked:   "XXXX4226",
		IssuedOn:       "2019-05-02",
		ExpiresOn:      "2029-05-01",
		Attestation:    "self",
		AttestedOn:     "2026-08-01",
		UploadedBy:     "manager@example.com",
	}
}

func TestASelfAttestedCopyIsHeldWithWhatTheAttestationWasWorth(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "menon-docs")
	party := uuid.NewString()

	if err := s.SavePartyDocument(ctx, firm, party, aPassportCopy()); err != nil {
		t.Fatalf("recording the copy: %v", err)
	}

	held, err := s.PartyDocuments(ctx, firm, party)
	if err != nil {
		t.Fatalf("reading them back: %v", err)
	}
	if len(held) != 1 {
		t.Fatalf("held %d documents, wanted the one that was recorded", len(held))
	}
	got := held[0]
	if got.Attestation != "self" || got.AttestedOn != "2026-08-01" {
		t.Errorf("the attestation came back as %q on %q", got.Attestation, got.AttestedOn)
	}
	if got.Kind != "passport" || got.NumberMasked != "XXXX4226" || got.ExpiresOn != "2029-05-01" {
		t.Errorf("the document came back as %+v", got)
	}
}

// The mask is the whole of ADR-0013 on this table: a number that reaches the
// column unmasked would be an identifier held whole in the system of record.
func TestAnIdentifierThatIsNotAMaskIsRefused(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "menon-docs-unmasked")

	d := aPassportCopy()
	d.NumberMasked = "L898902C3"
	if err := s.SavePartyDocument(ctx, firm, uuid.NewString(), d); err == nil {
		t.Error("a whole passport number was written to the record")
	}
}

// Only a person attests in their own name. A copy claiming a mission
// endorsement with nobody named on it is a claim no officer can check.
func TestAnEndorsementWithNobodyNamedIsRefused(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "menon-docs-unnamed")

	d := aPassportCopy()
	d.Attestation = "indian_mission"
	if err := s.SavePartyDocument(ctx, firm, uuid.NewString(), d); err == nil {
		t.Error("an endorsement was accepted with no notary or officer named on it")
	}
}

// The checklist reads the newest of a kind, and a re-upload is what happens
// when the first copy was refused.
func TestTheNewestCopyOfAKindIsWhatComesBackFirst(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "menon-docs-again")
	party := uuid.NewString()

	first := aPassportCopy()
	if err := s.SavePartyDocument(ctx, firm, party, first); err != nil {
		t.Fatalf("recording the first copy: %v", err)
	}
	second := aPassportCopy()
	second.Filename = "passport-rescanned.pdf"
	if err := s.SavePartyDocument(ctx, firm, party, second); err != nil {
		t.Fatalf("recording the second copy: %v", err)
	}

	held, err := s.PartyDocuments(ctx, firm, party)
	if err != nil {
		t.Fatalf("reading them back: %v", err)
	}
	if len(held) != 2 || held[0].Filename != "passport-rescanned.pdf" {
		t.Errorf("held %d documents, newest %q", len(held), held[0].Filename)
	}
}

func TestOneFirmsOwnerDocumentsAreNotAnothersToRead(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	mine := aFirm(t, s, "menon-docs-mine")
	theirs := aFirm(t, s, "menon-docs-theirs")
	party := uuid.NewString()

	if err := s.SavePartyDocument(ctx, mine, party, aPassportCopy()); err != nil {
		t.Fatalf("recording the copy: %v", err)
	}

	held, err := s.PartyDocuments(ctx, theirs, party)
	if err != nil {
		t.Fatalf("reading them back: %v", err)
	}
	if len(held) != 0 {
		t.Errorf("another firm's books returned %d of this owner's documents", len(held))
	}
}
