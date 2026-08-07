package docscan_test

import (
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/identity/docscan"
)

// The ICAO 9303 specimen. A passport's machine-readable zone is not OCR
// guesswork once it is read: every field has a position and four of them carry
// check digits, so a misread is detected rather than prefilled.
const (
	mrzLine1 = "P<UTOERIKSSON<<ANNA<MARIA<<<<<<<<<<<<<<<<<<<"
	mrzLine2 = "L898902C36UTO7408122F1204159ZE184226B<<<<<10"
)

func TestTheSpecimenPassportReadsFieldByField(t *testing.T) {
	got, err := docscan.ParseMRZ(mrzLine1 + "\n" + mrzLine2)
	if err != nil {
		t.Fatalf("parsing the specimen: %v", err)
	}

	for _, c := range []struct{ what, got, want string }{
		{"surname", got.Surname, "ERIKSSON"},
		{"given names", got.GivenNames, "ANNA MARIA"},
		{"number", got.Number, "L898902C3"},
		{"nationality", got.Nationality, "UTO"},
		{"date of birth", got.DateOfBirth, "1974-08-12"},
		{"sex", got.Sex, "F"},
		{"expiry", got.ExpiresOn, "2012-04-15"},
	} {
		if c.got != c.want {
			t.Errorf("%s is %q, want %q", c.what, c.got, c.want)
		}
	}
}

// The point of the check digits. A single transposed character is the ordinary
// OCR failure, and prefilling a passport number that is wrong by one is worse
// than prefilling nothing.
func TestASingleMisreadCharacterIsRefusedRatherThanPrefilled(t *testing.T) {
	broken := strings.Replace(mrzLine2, "L898902C3", "L898902C8", 1)

	_, err := docscan.ParseMRZ(mrzLine1 + "\n" + broken)
	if err == nil {
		t.Fatal("a passport number failing its own check digit was accepted")
	}
	if !strings.Contains(err.Error(), "number") {
		t.Errorf("the refusal does not say which field failed: %v", err)
	}
}

func TestTheCompositeCheckCatchesAConsistentButWrongLine(t *testing.T) {
	// A different expiry carrying its own correct check digit: every field agrees
	// with itself, and only the composite digit over the whole line disagrees.
	broken := strings.Replace(mrzLine2, "1204159", "1204160", 1)

	_, err := docscan.ParseMRZ(mrzLine1 + "\n" + broken)
	if err == nil {
		t.Fatal("a line whose fields agree with themselves but not with the composite was accepted")
	}
	if !strings.Contains(err.Error(), "as a whole") {
		t.Errorf("this was caught by a field check, not the composite: %v", err)
	}
}

func TestTextThatIsNotAMachineReadableZoneIsSaidSo(t *testing.T) {
	for _, in := range []string{
		"", "hello", mrzLine1, mrzLine2 + "\n" + mrzLine2,
		strings.Repeat("A", 44) + "\n" + strings.Repeat("B", 44),
	} {
		if _, err := docscan.ParseMRZ(in); err == nil {
			t.Errorf("%.20q was read as a passport", in)
		}
	}
}

// The scan sits in a page of OCR output, not alone: the two lines are found
// among whatever else the camera caught.
func TestTheZoneIsFoundInsideAFullPageOfOCROutput(t *testing.T) {
	page := "REPUBLIC OF UTOPIA\nPASSPORT\nType P  Code UTO\n" +
		mrzLine1 + "\n" + mrzLine2 + "\nSigned\n"

	got, err := docscan.ParseMRZ(page)
	if err != nil {
		t.Fatalf("finding the zone in a page: %v", err)
	}
	if got.Number != "L898902C3" {
		t.Errorf("number is %q", got.Number)
	}
}

// Two-digit years: a passport expiring in 12 expires in 2012, and somebody born
// in 74 was born in 1974. The window is the one ICAO 9303 uses.
func TestATwoDigitYearIsPlacedInTheRightCentury(t *testing.T) {
	got, err := docscan.ParseMRZ(mrzLine1 + "\n" + mrzLine2)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if !strings.HasPrefix(got.DateOfBirth, "19") {
		t.Errorf("date of birth %q — a birth year of 74 is 1974", got.DateOfBirth)
	}
	if !strings.HasPrefix(got.ExpiresOn, "20") {
		t.Errorf("expiry %q — an expiry year of 12 is 2012", got.ExpiresOn)
	}
}
