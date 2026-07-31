package dpdp_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/dpdp"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

func outcome(t *testing.T, out []dpdp.Outcome, c dpdp.Class) dpdp.Outcome {
	t.Helper()
	for _, o := range out {
		if o.Class == c {
			return o
		}
	}
	t.Fatalf("no outcome for %s in %+v", c, out)
	return dpdp.Outcome{}
}

// The story's primary scenario: a tenant with an executed agreement and three
// years of rent receipts asks to be erased. What can go, goes. What cannot is
// named with its statute and its date, and the requester is told.
func TestAnErasureNamesWhatIsKeptAndWhy(t *testing.T) {
	out, err := dpdp.Assess(dpdp.Subject{
		PartyID: "party-1", TenantID: "org-1",
		RelationshipEndedOn: effective.Day(2026, 6, 30),
		Present: []dpdp.Class{
			dpdp.ClassContact, dpdp.ClassSupport,
			dpdp.ClassFinancial, dpdp.ClassTax, dpdp.ClassAgreement, dpdp.ClassKYC,
		},
	}, effective.Day(2026, 8, 1))
	if err != nil {
		t.Fatalf("assessing: %v", err)
	}

	// Held on consent alone, so it goes.
	for _, c := range []dpdp.Class{dpdp.ClassContact, dpdp.ClassSupport} {
		if got := outcome(t, out, c); got.Action != dpdp.Erase {
			t.Errorf("%s is %s, want erased — nothing requires it to be kept", c, got.Action)
		}
	}

	// Retained, and each says under what and until when.
	for _, c := range []struct {
		class dpdp.Class
		years int
	}{
		{dpdp.ClassFinancial, 8},
		{dpdp.ClassTax, 8},
		{dpdp.ClassAgreement, 12},
		{dpdp.ClassKYC, 5},
	} {
		got := outcome(t, out, c.class)
		if got.Action != dpdp.Retain {
			t.Errorf("%s is %s, want retained", c.class, got.Action)
			continue
		}
		want := effective.Day(2026+c.years, 6, 30)
		if !got.Until.Equal(want) {
			t.Errorf("%s is retained until %s, want %s", c.class, got.Until, want)
		}
		if got.Because == "" || !strings.Contains(got.Because, "until") {
			t.Errorf("%s does not tell the requester why: %q", c.class, got.Because)
		}
		r, ok := dpdp.RetentionFor(c.class)
		if !ok || r.Statute == "" {
			t.Errorf("%s is retained under nothing", c.class)
		}
		if !strings.Contains(got.Because, r.Statute) {
			t.Errorf("%s does not name its statute: %q", c.class, got.Because)
		}
	}

	if dpdp.Erasable(out) {
		t.Error("the request reports as fully completable while the ledger is still retained")
	}
}

// Once the period has run, the same records are erased — the retention is a
// period, not a permanent exemption.
func TestRetentionExpiresAndThenItIsErased(t *testing.T) {
	s := dpdp.Subject{
		PartyID: "party-1", TenantID: "org-1",
		RelationshipEndedOn: effective.Day(2026, 6, 30),
		Present:             []dpdp.Class{dpdp.ClassKYC},
	}

	if got := outcome(t, mustAssess(t, s, effective.Day(2031, 6, 29)), dpdp.ClassKYC); got.Action != dpdp.Retain {
		t.Errorf("a day before the five years are up, KYC is %s", got.Action)
	}
	got := outcome(t, mustAssess(t, s, effective.Day(2031, 6, 30)), dpdp.ClassKYC)
	if got.Action != dpdp.Erase {
		t.Errorf("five years after the relationship ended, KYC is %s, want erased", got.Action)
	}
	if !strings.Contains(got.Because, "has passed") {
		t.Errorf("the erasure does not explain itself: %q", got.Because)
	}
}

// The story's failure scenario: an open dispute defers rather than being
// silently ignored or blindly executed, and the deferral names the dispute.
func TestAnOpenDisputeDefersAndNamesItself(t *testing.T) {
	out := mustAssess(t, dpdp.Subject{
		PartyID: "party-1", TenantID: "org-1",
		RelationshipEndedOn: effective.Day(2026, 6, 30),
		OpenDisputes: []dpdp.Entanglement{
			{What: "a disputed deposit deduction", Reference: "D-1042"}},
		Present: []dpdp.Class{dpdp.ClassFinancial, dpdp.ClassContact},
	}, effective.Day(2026, 8, 1))

	got := outcome(t, out, dpdp.ClassFinancial)
	if got.Action != dpdp.Defer {
		t.Fatalf("an open dispute did not defer the ledger: %s", got.Action)
	}
	if !strings.Contains(got.Because, "D-1042") {
		t.Errorf("the deferral does not name the dispute: %q", got.Because)
	}
	if len(got.Blocking) != 1 {
		t.Errorf("the deferral is blocked by %+v", got.Blocking)
	}
	if len(dpdp.Deferred(out)) != 1 {
		t.Errorf("%d classes deferred, want 1", len(dpdp.Deferred(out)))
	}

	// And a dispute about rent is not a reason to keep somebody's marketing
	// preferences.
	if got := outcome(t, out, dpdp.ClassContact); got.Action != dpdp.Erase {
		t.Errorf("contact data is %s because of a financial dispute — nothing requires it", got.Action)
	}
}

// Money still in flight and a certificate the landlord is still owed defer for
// the same reason a dispute does.
func TestUnsettledMoneyAndOutstandingObligationsDefer(t *testing.T) {
	out := mustAssess(t, dpdp.Subject{
		PartyID: "party-1", TenantID: "org-1",
		RelationshipEndedOn: effective.Day(2026, 6, 30),
		UnsettledMoney: []dpdp.Entanglement{
			{What: "a deposit not yet returned", Reference: "lease-9"}},
		OutstandingObligations: []dpdp.Entanglement{
			{What: "Form 16C not yet issued to the landlord", Reference: "obl-3"}},
		Present: []dpdp.Class{dpdp.ClassTax},
	}, effective.Day(2026, 8, 1))

	got := outcome(t, out, dpdp.ClassTax)
	if got.Action != dpdp.Defer {
		t.Fatalf("tax data is %s while a certificate is still owed", got.Action)
	}
	if len(got.Blocking) != 2 {
		t.Errorf("the deferral names %d things, want both", len(got.Blocking))
	}
	for _, want := range []string{"lease-9", "obl-3"} {
		if !strings.Contains(got.Because, want) {
			t.Errorf("the deferral omits %s: %q", want, got.Because)
		}
	}
}

// A live relationship defers everything with a period, because the clock has
// not started.
func TestALiveRelationshipDefersTheClock(t *testing.T) {
	out := mustAssess(t, dpdp.Subject{
		PartyID: "party-1", TenantID: "org-1",
		Present: []dpdp.Class{dpdp.ClassAgreement, dpdp.ClassContact},
	}, effective.Day(2026, 8, 1))

	if got := outcome(t, out, dpdp.ClassAgreement); got.Action != dpdp.Defer {
		t.Errorf("the agreement is %s while the tenancy is still running", got.Action)
	}
	// Marketing still goes: an active tenancy is not consent to be marketed to.
	if got := outcome(t, out, dpdp.ClassContact); got.Action != dpdp.Erase {
		t.Errorf("contact data is %s during a live tenancy", got.Action)
	}
}

// The story's first edge case: a person who is a tenant of one organisation and
// an owner in another. The request is scoped, and a request that names no
// organisation cannot be answered at all.
func TestAnErasureIsScopedToOneOrganisation(t *testing.T) {
	if _, err := dpdp.Assess(dpdp.Subject{PartyID: "party-1"}, effective.Day(2026, 8, 1)); !errors.Is(err, dpdp.ErrConsent) {
		t.Errorf("an unscoped erasure request was assessed: %v", err)
	}
	if _, err := dpdp.Assess(dpdp.Subject{TenantID: "org-1"}, effective.Day(2026, 8, 1)); !errors.Is(err, dpdp.ErrConsent) {
		t.Errorf("an erasure request naming nobody was assessed: %v", err)
	}

	// The same person, two organisations, two different answers — one has ended
	// and one has not.
	asTenant := mustAssess(t, dpdp.Subject{
		PartyID: "party-1", TenantID: "org-1",
		RelationshipEndedOn: effective.Day(2020, 1, 1),
		Present:             []dpdp.Class{dpdp.ClassKYC},
	}, effective.Day(2026, 8, 1))
	asOwner := mustAssess(t, dpdp.Subject{
		PartyID: "party-1", TenantID: "org-2",
		Present: []dpdp.Class{dpdp.ClassKYC},
	}, effective.Day(2026, 8, 1))

	if outcome(t, asTenant, dpdp.ClassKYC).Action != dpdp.Erase {
		t.Error("the ended relationship's KYC was not erased")
	}
	if outcome(t, asOwner, dpdp.ClassKYC).Action != dpdp.Defer {
		t.Error("the live relationship's KYC was erased by a request made to another organisation")
	}
}

// The story's second edge case: withdrawing consent that would break an active
// lease obligation. It does not break it, and the person is told why rather than
// having the withdrawal accepted in silence.
func TestWithdrawingConsentToATenancySaysWhatItDoesNotDo(t *testing.T) {
	w := dpdp.Withdraw(dpdp.PurposeTenancy)
	if w.Stopped {
		t.Error("withdrawing consent stopped a tenancy that is performed under a contract")
	}
	for _, want := range []string{"rather than consent", "tenancy"} {
		if !strings.Contains(w.Because, want) {
			t.Errorf("the explanation omits %q: %q", want, w.Because)
		}
	}

	if m := dpdp.Withdraw(dpdp.PurposeMarketing); !m.Stopped {
		t.Error("withdrawing consent to marketing did not stop it — that one really is consent")
	}
	if s := dpdp.Withdraw(dpdp.PurposeStatutory); s.Stopped {
		t.Error("withdrawing consent stopped a statutory obligation")
	}
}

// A consent artefact that could not be produced as evidence is not one.
func TestAConsentArtefactMustBeEvidence(t *testing.T) {
	good := dpdp.Consent{
		PartyID: "party-1", Purpose: dpdp.PurposeKYC,
		NoticeVersion: "2026-07-01", Language: "en",
		GivenOn: effective.Day(2026, 7, 5),
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("a complete artefact was refused: %v", err)
	}
	if !good.Live(effective.Day(2026, 8, 1)) {
		t.Error("a consent given in July does not stand in August")
	}

	withdrawn := good
	withdrawn.WithdrawnOn = effective.Day(2026, 7, 20)
	if withdrawn.Live(effective.Day(2026, 8, 1)) {
		t.Error("a withdrawn consent still stands")
	}

	for _, c := range []struct {
		name   string
		mutate func(dpdp.Consent) dpdp.Consent
	}{
		{"nobody gave it", func(c dpdp.Consent) dpdp.Consent { c.PartyID = ""; return c }},
		{"no purpose", func(c dpdp.Consent) dpdp.Consent { c.Purpose = ""; return c }},
		{"no notice version", func(c dpdp.Consent) dpdp.Consent { c.NoticeVersion = " "; return c }},
		{"no language", func(c dpdp.Consent) dpdp.Consent { c.Language = ""; return c }},
		{"no date", func(c dpdp.Consent) dpdp.Consent { c.GivenOn = effective.Date{}; return c }},
		{"withdrawn before it was given", func(c dpdp.Consent) dpdp.Consent {
			c.WithdrawnOn = effective.Day(2026, 1, 1)
			return c
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := c.mutate(good).Validate(); !errors.Is(err, dpdp.ErrConsent) {
				t.Errorf("accepted anyway: %v", err)
			}
		})
	}
}

// Every class the code knows has a retention rule, so an erasure cannot meet a
// class nobody decided about.
func TestEveryClassHasARule(t *testing.T) {
	for _, c := range dpdp.Classes() {
		r, ok := dpdp.RetentionFor(c)
		if !ok {
			t.Errorf("%s has no retention rule", c)
			continue
		}
		if r.Years > 0 && (r.Statute == "" || r.Anchor == "") {
			t.Errorf("%s is retained for %d years under %q from %q — a period with no statute is "+
				"a period nobody can defend", c, r.Years, r.Statute, r.Anchor)
		}
	}
}

func mustAssess(t *testing.T, s dpdp.Subject, on effective.Date) []dpdp.Outcome {
	t.Helper()
	out, err := dpdp.Assess(s, on)
	if err != nil {
		t.Fatalf("assessing: %v", err)
	}
	return out
}
