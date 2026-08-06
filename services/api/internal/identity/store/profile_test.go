package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/identity/domain/registration"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The firm's own registration. What is worth a database for is that the
// checklist is answered from what is actually held — a profile that reports
// itself complete because somebody typed a RERA number is the failure mode.

func aFirm(t *testing.T, s *store.Principals, name string) tenancy.ID {
	t.Helper()
	p := auth.Principal{UID: uid(), Surface: auth.SurfaceOps, Email: name + "@example.test", SignInProvider: "password"}
	person, _, err := s.Onboard(context.Background(), store.Onboarding{
		Principal: p, OrganisationName: name, Slug: name + "-" + uid(), Kind: "agency", Role: "manager",
	})
	if err != nil {
		t.Fatalf("onboarding a firm: %v", err)
	}
	id, ok := person.Organisation()
	if !ok {
		t.Fatal("a fresh onboarding has exactly one organisation")
	}
	return id
}

func TestSaveAndReadTheManagerProfile(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "menon-properties")

	in := store.ManagerProfile{
		LegalName:    "Menon Properties Private Limited",
		TradeName:    "Menon Properties",
		Constitution: string(registration.Company),
		PANMasked:    "XXXXXX234F",
		TAN:          "BLRM12345C",
		GSTIN:        "29AABCU9603R1ZM",
		RegistrarID:  "U70100KA2019PTC123456",
		AddressLine1: "12 Residency Road",
		City:         "Bengaluru",
		StateCode:    "KA",
		PINCode:      "560025",
		ContactEmail: "office@menon.example.test",
		ContactPhone: "+919845012345",
	}
	if err := s.SaveManagerProfile(ctx, firm, in); err != nil {
		t.Fatalf("saving: %v", err)
	}

	got, err := s.ManagerProfile(ctx, firm)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got.LegalName != in.LegalName || got.GSTIN != in.GSTIN || got.StateCode != "KA" {
		t.Fatalf("read back %+v", got)
	}
	// A profile with nothing uploaded and no live registration is not a firm
	// that may take a mandate.
	if got.State != "draft" {
		t.Fatalf("a new profile is a draft, got %q", got.State)
	}

	// Saving again corrects rather than duplicates: one firm, one profile.
	in.TradeName = "Menon Property Services"
	if err := s.SaveManagerProfile(ctx, firm, in); err != nil {
		t.Fatalf("re-saving: %v", err)
	}
	got, err = s.ManagerProfile(ctx, firm)
	if err != nil {
		t.Fatal(err)
	}
	if got.TradeName != "Menon Property Services" {
		t.Fatalf("the correction did not stick: %+v", got)
	}
}

func TestARegistrationIsPerStateAndExpires(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "twostate")

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, st := range []string{"KA", "MH"} {
		if err := s.AddRegistration(ctx, firm, store.Registration{
			Authority: "rera", StateCode: st, Number: "PRM/KA/RERA/" + st + "/AG/001",
			ValidFrom: from, ValidTo: from.AddDate(5, 0, 0),
		}); err != nil {
			t.Fatalf("adding %s: %v", st, err)
		}
	}

	live, err := s.LiveRegistrations(ctx, firm, from.AddDate(1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 2 {
		t.Fatalf("a firm working two states holds two registrations, got %d", len(live))
	}

	// A firm whose registration lapsed is unregistered under s.9, whatever it
	// filed in 2026.
	live, err = s.LiveRegistrations(ctx, firm, from.AddDate(6, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Fatalf("an expired registration is not live, got %d", len(live))
	}
}

func TestTheChecklistIsAnsweredFromWhatIsHeld(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := aFirm(t, s, "checklist")

	if err := s.SaveManagerProfile(ctx, firm, store.ManagerProfile{
		LegalName: "Sole Manager", Constitution: string(registration.Proprietorship),
		AddressLine1: "4 MG Road", City: "Pune", StateCode: "MH", PINCode: "411001",
		ContactEmail: "sole@example.test", ContactPhone: "+919812345678",
	}); err != nil {
		t.Fatal(err)
	}

	held, err := s.HeldDocuments(ctx, firm)
	if err != nil {
		t.Fatal(err)
	}
	if len(registration.Outstanding(registration.Proprietorship, held, time.Now())) == 0 {
		t.Fatal("a firm that has uploaded nothing has everything outstanding")
	}

	if err := s.RecordDocument(ctx, firm, store.ManagerDocument{
		Kind: string(registration.PANCard), ObjectPath: "org/" + string(firm) + "/manager/pan.pdf",
		Filename: "pan.pdf", ContentType: "application/pdf", UploadedBy: "manager@example.test",
	}); err != nil {
		t.Fatalf("recording: %v", err)
	}

	held, err = s.HeldDocuments(ctx, firm)
	if err != nil {
		t.Fatal(err)
	}
	// Uploaded is not accepted. A document nobody has reviewed cannot tick a
	// statutory box.
	if h := held[registration.PANCard]; h.Accepted {
		t.Fatal("an unreviewed upload counts as accepted")
	}

	if err := s.ReviewDocument(ctx, firm, string(registration.PANCard), "accepted", ""); err != nil {
		t.Fatalf("reviewing: %v", err)
	}
	held, err = s.HeldDocuments(ctx, firm)
	if err != nil {
		t.Fatal(err)
	}
	if !held[registration.PANCard].Accepted {
		t.Fatal("an accepted document is held")
	}
}
