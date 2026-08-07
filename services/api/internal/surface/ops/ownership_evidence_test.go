package ops_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// A property of its own per test: this suite commits, and what is on file
// against a property is exactly what these assertions turn on.
func seedBareProperty(t *testing.T, plat tenancy.PlatformPool) string {
	t.Helper()
	tok := token(t)
	var property string
	err := tenancy.Platform(context.Background(), plat, "seeding a property with nothing on file",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO properties (tenant_id, code, name, kind, address_line1,
				                        locality, city, state_code, pin)
				VALUES ($1, $2, 'Evidence Fixture', 'standalone', '4 Deed Road',
				        'Indiranagar', 'Bengaluru', 'KA', '560038')
				RETURNING id::text`,
				isolationtest.OrgOwner.String(), "EVD-"+tok[:8]).Scan(&property)
		})
	if err != nil {
		t.Fatalf("seeding a property: %v", err)
	}
	return property
}

// What proves the owner may let the property at all (#339). A firm that
// markets a flat on somebody's say-so has no answer when the real owner
// appears, so onboarding asks for the deed — or the power of attorney where
// the owner authorised somebody else to act.

type evidence struct {
	Documents []struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Filename string `json:"filename"`
	} `json:"documents"`
	Ownership struct {
		Proven   bool     `json:"proven"`
		Held     []string `json:"held"`
		Missing  []string `json:"missing"`
		Advisory []string `json:"advisory"`
	} `json:"ownership"`
}

func ownershipEvidence(t *testing.T, mux *http.ServeMux, property string) evidence {
	t.Helper()
	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/properties/"+property+"/documents")
	if w.Code != http.StatusOK {
		t.Fatalf("GET the property's documents: %d %s", w.Code, w.Body.String())
	}
	var out evidence
	decode(t, w, &out)
	return out
}

func aDocument(kind string) map[string]any {
	return map[string]any{
		"kind":         kind,
		"object_path":  "org/" + isolationtest.OrgOwner.String() + "/documents/" + uuid.NewString() + ".pdf",
		"filename":     kind + ".pdf",
		"content_type": "application/pdf",
	}
}

// A property with nothing on file is not proven, and the answer names what is
// wanted rather than saying only "no".
func TestAPropertyWithNoDeedIsNotProven(t *testing.T) {
	mux, plat := serveWithPool(t)
	property := seedBareProperty(t, plat)

	out := ownershipEvidence(t, mux, property)
	if out.Ownership.Proven {
		t.Fatalf("a property with nothing on file cannot be proven, got %+v", out.Ownership)
	}
	if len(out.Ownership.Missing) == 0 {
		t.Error("the answer names what is still wanted")
	}
}

func TestATitleDeedProvesTheOwnerMayLet(t *testing.T) {
	mux, plat := serveWithPool(t)
	property := seedBareProperty(t, plat)

	w := file(t, mux, isolationtest.OrgOwner,
		"/v1/ops/properties/"+property+"/documents", aDocument("title_deed"))
	if w.Code != http.StatusCreated {
		t.Fatalf("filing a title deed: %d %s", w.Code, w.Body.String())
	}

	out := ownershipEvidence(t, mux, property)
	if !out.Ownership.Proven {
		t.Fatalf("a title deed on file proves the right to let, got %+v", out.Ownership)
	}
	if len(out.Documents) != 1 || out.Documents[0].Kind != "title_deed" {
		t.Errorf("the deed reads back as a deed, got %+v", out.Documents)
	}
	// An encumbrance certificate is worth having and is not what proves the
	// right to let, so it belongs on a different line from the missing one.
	var advisable bool
	for _, k := range out.Ownership.Advisory {
		if k == "encumbrance_certificate" {
			advisable = true
		}
	}
	if !advisable {
		t.Errorf("an EC is advisable once the deed is in, got %+v", out.Ownership)
	}
}

// An owner who authorised somebody else to act has no deed to give the firm;
// the PoA is what the firm holds instead, and it proves the same thing.
func TestAPowerOfAttorneyProvesItToo(t *testing.T) {
	mux, plat := serveWithPool(t)
	property := seedBareProperty(t, plat)

	w := file(t, mux, isolationtest.OrgOwner,
		"/v1/ops/properties/"+property+"/documents", aDocument("power_of_attorney"))
	if w.Code != http.StatusCreated {
		t.Fatalf("filing a power of attorney: %d %s", w.Code, w.Body.String())
	}

	if out := ownershipEvidence(t, mux, property); !out.Ownership.Proven {
		t.Fatalf("a PoA proves the authority to let, got %+v", out.Ownership)
	}
}

func TestADocumentOfAnUnknownKindIsRefused(t *testing.T) {
	mux, plat := serveWithPool(t)
	property := seedBareProperty(t, plat)

	w := file(t, mux, isolationtest.OrgOwner,
		"/v1/ops/properties/"+property+"/documents", aDocument("vibes"))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("filing a document of no known kind: %d %s, want 422", w.Code, w.Body.String())
	}
}

// A path the client chose is a claim on somebody else's object, and the
// download URL minted afterwards would honour it.
func TestADocumentPathOutsideTheFirmsPrefixIsRefused(t *testing.T) {
	mux, plat := serveWithPool(t)
	property := seedBareProperty(t, plat)

	doc := aDocument("title_deed")
	doc["object_path"] = "org/" + isolationtest.OrgOutsider.String() + "/documents/stolen.pdf"
	w := file(t, mux, isolationtest.OrgOwner,
		"/v1/ops/properties/"+property+"/documents", doc)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("filing against another organisation's path: %d %s, want 422", w.Code, w.Body.String())
	}
}

func TestCannotReadAnotherOrganisationsDocuments(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/properties/"+isolationtest.PropertySecond+"/documents")
	if w.Code != http.StatusNotFound {
		t.Fatalf("reading the second landlord's documents: %d %s, want 404", w.Code, w.Body.String())
	}
}
