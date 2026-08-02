package domain

import (
	"errors"
	"testing"
)

// The publication guardrails (#143): a prohibited claim blocks with the rule
// cited, and honest copy sails through.

func TestCheckClaims(t *testing.T) {
	cases := []struct {
		text string
		rule string // "" means clean
	}{
		{"2BHK near the park, sunny and quiet", ""},
		{"Spacious 3BHK, Hindus only", "discriminatory-preference"},
		{"No Muslims please, family building", "discriminatory-preference"},
		{"Assured returns of 8% for investors", "assured-returns"},
		{"Guaranteed rental income from day one", "assured-returns"},
		{"RERA approved premium tower", "unverified-rera-claim"},
		{"RERA registered PRM/KA/RERA/1251/446/PR/2024", ""},
		{"Brand new studio, brokers excuse", ""},
	}
	for _, c := range cases {
		got := CheckClaims(c.text)
		switch {
		case c.rule == "" && len(got) != 0:
			t.Errorf("%q flagged as %s, want clean", c.text, got[0].Rule)
		case c.rule != "" && (len(got) == 0 || got[0].Rule != c.rule):
			t.Errorf("%q = %+v, want rule %s", c.text, got, c.rule)
		}
	}
}

func TestProhibitedClaimBlocksPublication(t *testing.T) {
	d := Draft{
		PropertyID: "p", UnitID: "u",
		Headline: "Sunny 2BHK, Brahmins only", Locality: "Indiranagar", City: "Bengaluru",
		StateCode: "KA", Costs: Costs{RentMinor: 30_000_00, Confirmed: true},
	}
	err := d.PublishableNow()
	if !errors.Is(err, ErrProhibitedClaim) {
		t.Fatalf("publication = %v, want ErrProhibitedClaim", err)
	}
	for _, want := range []string{"Art. 15", "Brahmins only"} {
		if !containsStr(err.Error(), want) {
			t.Errorf("the refusal does not cite %q: %v", want, err)
		}
	}
	d.Headline = "Sunny 2BHK near the metro"
	if err := d.PublishableNow(); err != nil {
		t.Fatalf("clean copy blocked: %v", err)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
