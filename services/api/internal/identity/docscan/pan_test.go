package docscan_test

import (
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/identity/docscan"
)

// A PAN card has no machine-readable zone, so this is genuine OCR text with
// everything the camera caught. ABCPD1234E has P in the fourth position, which is an
// individual.
const panCard = `आयकर विभाग                भारत सरकार
INCOME TAX DEPARTMENT      GOVT. OF INDIA

Permanent Account Number Card
ABCPD1234E

Name
ANJALI MENON

Father's Name
RAVI MENON

Date of Birth
12/08/1974
`

func TestALabelledPANCardReadsFieldByField(t *testing.T) {
	got, err := docscan.ParsePANCard(panCard)
	if err != nil {
		t.Fatalf("reading the card: %v", err)
	}

	for _, c := range []struct{ what, got, want string }{
		{"number", got.Number, "ABCPD1234E"},
		{"name", got.Name, "ANJALI MENON"},
		{"father's name", got.FatherName, "RAVI MENON"},
		{"date of birth", got.DateOfBirth, "1974-08-12"},
	} {
		if c.got != c.want {
			t.Errorf("%s is %q, want %q", c.what, c.got, c.want)
		}
	}
}

// The father's name sits directly under the holder's on every card, and taking
// the wrong one prefills a tax record against the wrong person.
func TestTheHoldersNameIsNotTheFathersName(t *testing.T) {
	got, err := docscan.ParsePANCard(panCard)
	if err != nil {
		t.Fatalf("reading the card: %v", err)
	}
	if got.Name == got.FatherName {
		t.Fatalf("both names read as %q", got.Name)
	}
}

// The fourth character says what kind of holder this is, which the onboarding
// wizard otherwise asks the manager to assert.
func TestTheFourthCharacterSaysWhetherTheHolderIsAnIndividual(t *testing.T) {
	got, err := docscan.ParsePANCard(panCard)
	if err != nil {
		t.Fatalf("reading the card: %v", err)
	}
	if !got.Individual {
		t.Error("ABCPD1234E has P in the fourth position and did not read as an individual")
	}

	company := strings.Replace(panCard, "ABCPD1234E", "ABCCD1234E", 1)
	firm, err := docscan.ParsePANCard(company)
	if err != nil {
		t.Fatalf("reading a company's card: %v", err)
	}
	if firm.Individual {
		t.Error("ABCCD1234E has C in the fourth position and read as an individual")
	}
}

// The failure that actually happens. A PAN mixes letters and digits in fixed
// positions, so an O read where a 0 belongs is recoverable — and worth
// recovering, because the alternative is the manager typing it by hand.
func TestTheOrdinaryCharacterConfusionsAreResolvedByPosition(t *testing.T) {
	// I for 1 and Z for 2 in the digit block; 8 for B and 0 for O in the letters.
	for _, misread := range []string{"ABCPDI234E", "ABCPD1Z34E", "A8CPD1234E", "ABCPD1234E"} {
		card := strings.Replace(panCard, "ABCPD1234E", misread, 1)
		got, err := docscan.ParsePANCard(card)
		if err != nil {
			t.Errorf("%q was not recovered: %v", misread, err)
			continue
		}
		if got.Number != "ABCPD1234E" {
			t.Errorf("%q read as %q", misread, got.Number)
		}
	}
}

// The other half of recovery: it must not invent a PAN out of text that has
// none, because a confidently wrong prefill is not checked by anybody.
func TestTextWithNoPANIsSaidSoRatherThanGuessedAt(t *testing.T) {
	for _, in := range []string{
		"", "INCOME TAX DEPARTMENT",
		"AADHAAR 1234 5678 9012",
		"ABCDEFGHIJ",  // ten letters
		"1234567890",  // ten digits
		"ABCD1234EF",  // four letters where five belong
		"ABCPD1234EG", // one too long
	} {
		if got, err := docscan.ParsePANCard(in); err == nil {
			t.Errorf("%q was read as PAN %q", in, got.Number)
		}
	}
}

func TestAnUnlabelledCardStillYieldsTheNumberAndDate(t *testing.T) {
	old := "INCOME TAX DEPARTMENT\nABCPD1234E\nANJALI MENON\nRAVI MENON\n12/08/1974\n"

	got, err := docscan.ParsePANCard(old)
	if err != nil {
		t.Fatalf("reading an older card: %v", err)
	}
	if got.Number != "ABCPD1234E" || got.DateOfBirth != "1974-08-12" {
		t.Errorf("number %q, date of birth %q", got.Number, got.DateOfBirth)
	}
}
