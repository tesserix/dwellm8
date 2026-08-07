package domain

import (
	"strings"
	"testing"
)

// The five instruments a firm issues (#341) print as a blank form a manager can
// read before issuing it (#350).

func TestEveryInstrumentThisFirmIssuesCanBePrinted(t *testing.T) {
	kinds := []string{
		"management_agreement", "onboarding_checklist",
		"rent_agreement", "lease_deed", "power_of_attorney",
	}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			a, err := PreviewInstrument(kind)
			if err != nil {
				t.Fatalf("previewing %s: %v", kind, err)
			}
			if strings.TrimSpace(a.Title) == "" {
				t.Error("an instrument with no title is not a document")
			}
			if len(a.Clauses) == 0 {
				t.Error("an instrument with no clause is a blank page")
			}
			if len(a.Signatures) == 0 {
				t.Error("an instrument nobody signs binds nobody")
			}
		})
	}
}

func TestAnUnknownInstrumentIsRefused(t *testing.T) {
	if _, err := PreviewInstrument("bill_of_sale"); err == nil {
		t.Fatal("an instrument this firm does not issue was printed anyway")
	}
}

// A preview is the blank form. A placeholder left in it would read as though
// the clause said "{{owner_name}}", which is what somebody signs.
func TestAPreviewRulesBlanksRatherThanShowingPlaceholders(t *testing.T) {
	a, err := PreviewInstrument("rent_agreement")
	if err != nil {
		t.Fatalf("previewing: %v", err)
	}
	body := text(a)
	if strings.Contains(body, "{{") {
		t.Errorf("a placeholder survived into the preview: %s", body)
	}
	if !strings.Contains(body, "____") {
		t.Error("nothing was left to fill in by hand — the blank form has no blanks")
	}
}

// The rent agreement is eleven months for the reason the whole vertical exists:
// twelve or more attracts compulsory registration and stamp duty.
func TestTheRentAgreementRunsElevenMonths(t *testing.T) {
	a, err := PreviewInstrument("rent_agreement")
	if err != nil {
		t.Fatalf("previewing: %v", err)
	}
	if !strings.Contains(strings.ToLower(text(a)), "eleven") {
		t.Error("the rent agreement does not state its eleven-month term")
	}
}

// A power of attorney that does not say what it excludes is read as general.
func TestThePowerOfAttorneyIsLimited(t *testing.T) {
	a, err := PreviewInstrument("power_of_attorney")
	if err != nil {
		t.Fatalf("previewing: %v", err)
	}
	body := strings.ToLower(text(a))
	for _, want := range []string{"no power to sell", "revoke"} {
		if !strings.Contains(body, want) {
			t.Errorf("the power of attorney does not say %q", want)
		}
	}
}

func TestTheManagementAgreementIsTheOneAlreadyIssued(t *testing.T) {
	a, err := PreviewInstrument("management_agreement")
	if err != nil {
		t.Fatalf("previewing: %v", err)
	}
	if !strings.Contains(strings.ToLower(text(a)), "no authority to sell") {
		t.Error("the previewed management agreement is not the instrument #340 prints")
	}
}
