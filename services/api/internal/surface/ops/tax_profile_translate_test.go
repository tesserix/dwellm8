package ops

import (
	"strings"
	"testing"

	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
)

// The owner's tax profile arrives with the identifiers whole, because the
// manager has just read them off a card, and leaves this function with nothing
// whole in it (#318, ADR-0013). Everything below is about that boundary.

func aResidentRequest() taxProfileRequest {
	return taxProfileRequest{
		Residency: "resident", ResidenceCountry: "IN",
		PAN:    "ABCPD1234E",
		Source: "owner_declaration", ValidFrom: "2026-04-01",
	}
}

func anNRIRequest() taxProfileRequest {
	return taxProfileRequest{
		Residency: "non_resident", ResidenceCountry: "AE",
		PAN:            "ABCPD1234E",
		ForeignTIN:     "784197412345671",
		TRCNumber:      "TRC-AE-2026-0041",
		TRCValidFrom:   "2026-04-01",
		TRCValidTo:     "2027-03-31",
		ForeignAddress: "Villa 12, Al Barsha, Dubai",
		ForeignEmail:   "anjali@example.ae",
		ForeignPhone:   "+971500000001",
		Source:         "owner_declaration",
		ValidFrom:      "2026-04-01",
	}
}

func TestFurnishingAPANIsRecordedWithoutTheNumber(t *testing.T) {
	got, err := taxProfileFrom(aResidentRequest())
	if err != nil {
		t.Fatalf("translating: %v", err)
	}
	if !got.PANFurnished {
		t.Error("a PAN was furnished and the profile does not say so — section 206AA turns on this")
	}
	if strings.Contains(strings.Join(everyField(got), "|"), "ABCPD1234E") {
		t.Error("the PAN itself reached the profile — nothing stored may hold the whole number")
	}
}

func TestNoPANIsNotTheSameAsAPANNobodyChecked(t *testing.T) {
	req := aResidentRequest()
	req.PAN = ""

	got, err := taxProfileFrom(req)
	if err != nil {
		t.Fatalf("translating: %v", err)
	}
	if got.PANFurnished {
		t.Error("no PAN was given and the profile says one was furnished")
	}
}

// A PAN that is not a PAN is the manager mistyping what the card says. It has
// to be refused here: the column holds a mask, so nothing downstream can tell.
func TestAPANThatIsNotAPANIsRefused(t *testing.T) {
	for _, bad := range []string{"ABCD1234E", "ABCPD1234", "123456789012", "NOT A PAN"} {
		req := aResidentRequest()
		req.PAN = bad
		if _, err := taxProfileFrom(req); err == nil {
			t.Errorf("%q was accepted as a PAN", bad)
		}
	}
}

// The error is a log line, so it must not carry the number that failed.
func TestARefusalDoesNotQuoteTheIdentifierThatFailed(t *testing.T) {
	req := aResidentRequest()
	req.PAN = "123456789012"

	_, err := taxProfileFrom(req)
	if err == nil {
		t.Fatal("an Aadhaar-shaped number was accepted as a PAN")
	}
	if strings.Contains(err.Error(), "123456789012") {
		t.Errorf("the refusal quotes the number: %v", err)
	}
}

func TestTheForeignTINIsMaskedBeforeItIsRecorded(t *testing.T) {
	got, err := taxProfileFrom(anNRIRequest())
	if err != nil {
		t.Fatalf("translating: %v", err)
	}
	if got.ForeignTINMasked == "" {
		t.Fatal("the TIN was dropped — rule 37BC(2) requires it, so the payee stays inside 206AA")
	}
	if strings.Contains(got.ForeignTINMasked, "78419741234") {
		t.Errorf("the TIN is recorded whole as %q", got.ForeignTINMasked)
	}
	if !strings.HasSuffix(got.ForeignTINMasked, "5671") {
		t.Errorf("the mask keeps nothing identifying: %q", got.ForeignTINMasked)
	}
}

// The schema has a CHECK relating these two, but a constraint violation reaches
// the manager as "the profile was refused" with no field on it.
func TestResidencyAndCountryMustAgreeBeforeTheDatabaseIsAsked(t *testing.T) {
	for _, c := range []struct{ residency, country string }{
		{"non_resident", "IN"},
		{"resident", "AE"},
	} {
		req := aResidentRequest()
		req.Residency, req.ResidenceCountry = c.residency, c.country
		if _, err := taxProfileFrom(req); err == nil {
			t.Errorf("%s living in %s was accepted", c.residency, c.country)
		}
	}
}

func TestAResidencyNobodyDefinedIsRefused(t *testing.T) {
	req := aResidentRequest()
	req.Residency = "sometimes"
	if _, err := taxProfileFrom(req); err == nil {
		t.Error("an undefined residency was accepted, and it selects the section")
	}
}

// "The system had it" is not an answer to an assessing officer.
func TestAProfileWithNoSourceIsRefused(t *testing.T) {
	req := aResidentRequest()
	req.Source = ""
	if _, err := taxProfileFrom(req); err == nil {
		t.Error("a profile with nobody behind it was accepted")
	}
}

func TestAProfileMustSayWhatDateItIsTrueFrom(t *testing.T) {
	for _, bad := range []string{"", "01-04-2026", "yesterday"} {
		req := aResidentRequest()
		req.ValidFrom = bad
		if _, err := taxProfileFrom(req); err == nil {
			t.Errorf("valid_from %q was accepted", bad)
		}
	}
}

// The country is what selects section 195 over 194-I, so it is not free text.
func TestTheResidenceCountryIsATwoLetterCode(t *testing.T) {
	for _, bad := range []string{"UAE", "u", "United Arab Emirates", "12"} {
		req := anNRIRequest()
		req.ResidenceCountry = bad
		if _, err := taxProfileFrom(req); err == nil {
			t.Errorf("residence_country %q was accepted", bad)
		}
	}
}

func TestANonResidentWithEveryParticularCarriesThemAllThrough(t *testing.T) {
	got, err := taxProfileFrom(anNRIRequest())
	if err != nil {
		t.Fatalf("translating: %v", err)
	}
	for _, c := range []struct{ what, got string }{
		{"TRC number", got.TRCNumber},
		{"TRC valid from", got.TRCValidFrom},
		{"foreign address", got.ForeignAddress},
		{"foreign email", got.ForeignEmail},
		{"foreign phone", got.ForeignPhone},
	} {
		if c.got == "" {
			t.Errorf("%s was dropped — rule 37BC(2) requires it", c.what)
		}
	}
}

func everyField(p identitystore.TaxProfile) []string {
	return []string{p.Residency, p.ResidenceCountry, p.ForeignTINMasked, p.TRCNumber,
		p.TRCValidFrom, p.TRCValidTo, p.ForeignAddress, p.ForeignEmail, p.ForeignPhone,
		p.Source, p.ValidFrom}
}
