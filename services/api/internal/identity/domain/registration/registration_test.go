package registration

import (
	"testing"
	"time"
)

// What a managing firm must produce before it may take somebody else's
// property on. The matrix is the domain rule: a proprietor and a private
// limited company are asked for different paperwork, and asking a sole manager
// for a board resolution is the fastest way to lose them at step two.

func kinds(reqs []Requirement) map[Kind]bool {
	out := map[Kind]bool{}
	for _, r := range reqs {
		out[r.Kind] = true
	}
	return out
}

func TestRequiredDocuments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		of          Constitution
		wants       []Kind
		wantsNot    []Kind
		wantMinimum int
	}{
		{
			name:        "a sole manager produces their own identity and a licence",
			of:          Proprietorship,
			wants:       []Kind{PANCard, AddressProof, BankProof, Photograph, RERACertificate},
			wantsNot:    []Kind{BoardResolution, MOAAOA, PartnershipDeed, LLPAgreement},
			wantMinimum: 5,
		},
		{
			name:     "a partnership produces the firm's PAN and the deed behind it",
			of:       Partnership,
			wants:    []Kind{PANCard, PartnershipDeed, AuthorityLetter, RERACertificate, BankProof},
			wantsNot: []Kind{MOAAOA, LLPAgreement, BoardResolution},
		},
		{
			name:     "an LLP produces its incorporation and its agreement",
			of:       LLP,
			wants:    []Kind{PANCard, IncorporationCertificate, LLPAgreement, RERACertificate},
			wantsNot: []Kind{PartnershipDeed, MOAAOA},
		},
		{
			name:     "a company produces incorporation, constitution and the resolution naming its signatory",
			of:       Company,
			wants:    []Kind{PANCard, IncorporationCertificate, MOAAOA, BoardResolution, SignatoryID, RERACertificate},
			wantsNot: []Kind{PartnershipDeed, LLPAgreement},
		},
		{
			name:     "a trust produces its deed and its registration",
			of:       Trust,
			wants:    []Kind{PANCard, TrustDeed, RegistrationCertificate, RERACertificate},
			wantsNot: []Kind{MOAAOA, PartnershipDeed},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := kinds(RequiredDocuments(c.of))
			for _, k := range c.wants {
				if !got[k] {
					t.Errorf("%s must be asked for %s", c.of, k)
				}
			}
			for _, k := range c.wantsNot {
				if got[k] {
					t.Errorf("%s must not be asked for %s", c.of, k)
				}
			}
			if len(got) < c.wantMinimum {
				t.Errorf("got %d requirements, want at least %d", len(got), c.wantMinimum)
			}
		})
	}
}

func TestEveryRequirementExplainsItself(t *testing.T) {
	t.Parallel()
	// A checklist that says "PAN card" and nothing else gets the wrong PAN card
	// uploaded: the director's, not the company's.
	for _, c := range Constitutions() {
		for _, r := range RequiredDocuments(c) {
			if r.Label == "" || r.Why == "" {
				t.Errorf("%s/%s has no label or reason", c, r.Kind)
			}
		}
	}
}

func TestCompanyPANIsTheCompanys(t *testing.T) {
	t.Parallel()
	// The distinction the domain exists to carry: a body corporate is asked for
	// the entity's PAN, a sole manager for their own.
	for _, r := range RequiredDocuments(Company) {
		if r.Kind == PANCard && r.Subject != OfTheFirm {
			t.Fatalf("a company's PAN is the firm's, got %v", r.Subject)
		}
	}
	for _, r := range RequiredDocuments(Proprietorship) {
		if r.Kind == PANCard && r.Subject != OfThePerson {
			t.Fatalf("a proprietor's PAN is their own, got %v", r.Subject)
		}
	}
}

func TestRequiredFields(t *testing.T) {
	t.Parallel()

	// TAN is not universal: it is required of anybody who will deduct TDS on an
	// owner's behalf, which is what a managing firm collecting rent does.
	body := RequiredFields(Company)
	for _, f := range []Field{FieldLegalName, FieldPAN, FieldTAN, FieldRegistrarID, FieldRegisteredOffice} {
		if !hasField(body, f) {
			t.Errorf("a company must supply %s", f)
		}
	}
	if hasField(RequiredFields(Proprietorship), FieldRegistrarID) {
		t.Error("a proprietorship has no registrar identifier to supply")
	}
}

func hasField(fs []Field, want Field) bool {
	for _, f := range fs {
		if f == want {
			return true
		}
	}
	return false
}

func TestReadinessRefusesUntilTheChecklistIsMet(t *testing.T) {
	t.Parallel()

	on := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	held := map[Kind]Held{}
	for _, r := range RequiredDocuments(Proprietorship) {
		held[r.Kind] = Held{Accepted: true}
	}

	missing := Outstanding(Proprietorship, held, on)
	if len(missing) != 0 {
		t.Fatalf("a complete checklist has nothing outstanding, got %v", missing)
	}

	// An expired RERA certificate is not a held certificate. A firm whose
	// registration lapsed is unregistered under s.9, whatever it uploaded in 2023.
	held[RERACertificate] = Held{Accepted: true, ExpiresOn: on.AddDate(0, 0, -1)}
	missing = Outstanding(Proprietorship, held, on)
	if len(missing) != 1 || missing[0].Kind != RERACertificate {
		t.Fatalf("an expired certificate is outstanding, got %v", missing)
	}

	// Uploaded is not accepted: a document nobody has looked at cannot make a
	// firm active.
	held[RERACertificate] = Held{Accepted: false}
	if len(Outstanding(Proprietorship, held, on)) != 1 {
		t.Fatal("an unreviewed document is still outstanding")
	}
}

func TestOutstandingReportsEverythingMissingAtOnce(t *testing.T) {
	t.Parallel()
	// One round trip per missing document is how an onboarding takes a week.
	on := time.Now()
	missing := Outstanding(Company, map[Kind]Held{}, on)
	if len(missing) != len(RequiredDocuments(Company)) {
		t.Fatalf("got %d outstanding, want all %d", len(missing), len(RequiredDocuments(Company)))
	}
}
