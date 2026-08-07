package domain

import (
	"strings"
	"testing"
)

// The owner–manager agreement (#340). Three things in it are the reason it
// exists: the manager may enter and manage, may not sell, and the owner who
// intends to sell must give four months' notice.

func filled() map[string]string {
	return map[string]string{
		"owner_name": "Anjali Menon", "owner_address": "12 Marine Drive, Kochi",
		"owner_pan":    "ABCDE1234F",
		"manager_name": "Nest Property Managers LLP", "manager_address": "4 MG Road, Bengaluru",
		"manager_rera":         "PRM/KA/RERA/1251/446",
		"property_address":     "Flat 101, Kadavanthra Heights, Kochi",
		"property_description": "Two-bedroom flat, 1,150 sq ft",
		"term_months":          "24", "commencement_date": "2026-09-01",
		"management_fee_pct": "8", "repair_ceiling": "10000",
		"execution_place": "Kochi", "execution_date": "2026-08-08",
	}
}

func text(a Agreement) string {
	var b strings.Builder
	b.WriteString(a.Title)
	for _, r := range a.Recitals {
		b.WriteString("\n" + r)
	}
	for _, c := range a.Clauses {
		b.WriteString("\n" + c.Number + " " + c.Heading + "\n" + c.Text)
	}
	return b.String()
}

func TestTheAgreementBarsSellingAndDealing(t *testing.T) {
	a, err := BuildAgreement(filled())
	if err != nil {
		t.Fatalf("building the agreement: %v", err)
	}
	body := strings.ToLower(text(a))
	for _, want := range []string{"no authority to sell", "mortgage", "transaction"} {
		if !strings.Contains(body, want) {
			t.Errorf("the agreement must say %q — a manager with an unstated bar has an implied one", want)
		}
	}
}

// Four months, not "immediately": the manager has tenancies to place and a
// tenant to rehouse, and that is what the notice period is for.
func TestTheOwnerMustGiveFourMonthsNoticeOfASale(t *testing.T) {
	a, err := BuildAgreement(filled())
	if err != nil {
		t.Fatalf("building the agreement: %v", err)
	}
	body := text(a)
	if !strings.Contains(body, "four (4) months") {
		t.Errorf("the sale-notice clause must state four months, got:\n%s", body)
	}
}

func TestTheAgreementSetsOutBothSidesResponsibilities(t *testing.T) {
	a, err := BuildAgreement(filled())
	if err != nil {
		t.Fatalf("building the agreement: %v", err)
	}
	body := strings.ToLower(text(a))
	for _, want := range []string{"manager shall", "owner shall", "structural", "safe"} {
		if !strings.Contains(body, want) {
			t.Errorf("the agreement must distinguish the roles: missing %q", want)
		}
	}
}

// Nothing is signed online yet, so the printed copy must have somewhere to
// sign: both parties and two witnesses, which is what an Indian instrument is
// attested with.
func TestTheAgreementLeavesPlacesToSign(t *testing.T) {
	a, err := BuildAgreement(filled())
	if err != nil {
		t.Fatalf("building the agreement: %v", err)
	}
	if len(a.Signatures) != 4 {
		t.Fatalf("want the owner, the manager and two witnesses, got %d blocks", len(a.Signatures))
	}
	var roles []string
	for _, s := range a.Signatures {
		roles = append(roles, s.Role)
	}
	want := []string{"Owner", "Property Manager", "Witness 1", "Witness 2"}
	for i, w := range want {
		if roles[i] != w {
			t.Errorf("signature block %d is %q, want %q", i, roles[i], w)
		}
	}
	if a.Signatures[0].Name != "Anjali Menon" {
		t.Errorf("the owner's block carries their name, got %q", a.Signatures[0].Name)
	}
}

// A clause printed with "{{management_fee_pct}}" still in it is a document
// somebody signs believing a figure is in there.
func TestAnAgreementWillNotPrintWithAFieldUnfilled(t *testing.T) {
	fields := filled()
	delete(fields, "management_fee_pct")

	_, err := BuildAgreement(fields)
	if err == nil {
		t.Fatal("an agreement missing a merge field must be refused")
	}
	if !strings.Contains(err.Error(), "management_fee_pct") {
		t.Errorf("the refusal names the field, got %v", err)
	}
}

func TestEveryFieldTheAgreementNeedsIsDeclared(t *testing.T) {
	a, err := BuildAgreement(filled())
	if err != nil {
		t.Fatalf("building the agreement: %v", err)
	}
	body := text(a)
	if strings.Contains(body, "{{") {
		t.Errorf("a placeholder survived into the agreement:\n%s", body)
	}
	// What the platform template declares is what this renders, so a firm's
	// revision cannot quietly drop a field this needs (#341).
	for _, f := range AgreementFields() {
		if _, ok := filled()[f]; !ok {
			t.Errorf("AgreementFields names %q, which the agreement cannot fill", f)
		}
	}
}
