package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
)

// What the landlord has furnished, read back as of a date. The rate engine asks
// two questions of it — is there a PAN, and is this non-resident outside section
// 206AA — and the second must be answered by the particulars, not by a caller
// who believes it (#318).

func aTaxProfile() store.TaxProfile {
	return store.TaxProfile{
		Residency:        "non_resident",
		ResidenceCountry: "AE",
		PANFurnished:     true,
		ForeignTINMasked: "XXXXX6789",
		TRCNumber:        "TRC-AE-2026-0041",
		TRCValidFrom:     "2026-04-01",
		TRCValidTo:       "2027-03-31",
		ForeignAddress:   "Villa 12, Al Barsha, Dubai",
		ForeignEmail:     "anjali@example.ae",
		ForeignPhone:     "+971500000001",
		Source:           "owner_declaration",
		ValidFrom:        "2026-04-01",
	}
}

func TestANonResidentWhoFurnishedEveryParticularIsOutsideSection206AA(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "menon-nri")
	party := uuid.NewString()

	if err := s.SaveTaxProfile(ctx, firm, party, aTaxProfile()); err != nil {
		t.Fatalf("saving the profile: %v", err)
	}

	got, err := s.TaxProfile(ctx, firm, party, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if !got.Rule37BCFurnished {
		t.Error("every particular was furnished and the payee is still inside section 206AA")
	}
	if !got.PANFurnished || got.Residency != "non_resident" || got.ResidenceCountry != "AE" {
		t.Errorf("the profile came back as %+v", got)
	}
}

func TestAMissingParticularKeepsTheNonResidentInsideSection206AA(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "menon-nri-partial")
	party := uuid.NewString()

	p := aTaxProfile()
	p.TRCNumber, p.TRCValidFrom, p.TRCValidTo = "", "", ""
	if err := s.SaveTaxProfile(ctx, firm, party, p); err != nil {
		t.Fatalf("saving the profile: %v", err)
	}

	got, err := s.TaxProfile(ctx, firm, party, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if got.Rule37BCFurnished {
		t.Error("a payee with no tax residency certificate escaped section 206AA")
	}
}

// The reason this table is effective dated: April was deducted under 194-I and
// must stay that way after the landlord moves abroad in October.
func TestMovingAbroadLeavesTheEarlierMonthsAsTheyWere(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "menon-moved")
	party := uuid.NewString()

	was := store.TaxProfile{
		Residency: "resident", ResidenceCountry: "IN", PANFurnished: true,
		Source: "owner_declaration", ValidFrom: "2026-04-01",
	}
	if err := s.SaveTaxProfile(ctx, firm, party, was); err != nil {
		t.Fatalf("saving the first profile: %v", err)
	}
	now := aTaxProfile()
	now.ValidFrom = "2026-10-01"
	if err := s.SaveTaxProfile(ctx, firm, party, now); err != nil {
		t.Fatalf("saving the second profile: %v", err)
	}

	april, err := s.TaxProfile(ctx, firm, party, time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("reading April: %v", err)
	}
	if april.Residency != "resident" {
		t.Errorf("April reads as %q — the deduction made then was a resident's", april.Residency)
	}

	november, err := s.TaxProfile(ctx, firm, party, time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("reading November: %v", err)
	}
	if november.Residency != "non_resident" {
		t.Errorf("November reads as %q", november.Residency)
	}
}

func TestAPartyWithNoProfileIsSaidSoRatherThanGuessed(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "menon-unknown")

	_, err := s.TaxProfile(ctx, firm, uuid.NewString(), time.Now())
	if err == nil {
		t.Fatal("a party with no profile returned one — a blank profile deducts at a rate " +
			"nobody chose")
	}
	if !isNoTaxProfile(err) {
		t.Errorf("the absence reads as %v, which a caller cannot tell from a database fault", err)
	}
}

func isNoTaxProfile(err error) bool {
	return err != nil && err.Error() == store.ErrNoTaxProfile.Error()
}
