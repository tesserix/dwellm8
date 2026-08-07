package ops_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The agreements a firm issues (#341). Every firm starts on the platform's
// India templates; a firm that wants its own clause uploads a revision, which
// supersedes rather than replaces — what was signed last year was signed
// against the version live last year.

type templateList struct {
	Templates []struct {
		ID           string   `json:"id"`
		Kind         string   `json:"kind"`
		Name         string   `json:"name"`
		Version      int      `json:"version"`
		IsDefault    bool     `json:"is_default"`
		Filename     string   `json:"filename"`
		MergeFields  []string `json:"merge_fields"`
		SupersededAt string   `json:"superseded_at"`
	} `json:"templates"`
}

func templates(t *testing.T, mux *http.ServeMux, path string) templateList {
	t.Helper()
	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet, path)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
	}
	var out templateList
	decode(t, w, &out)
	return out
}

func TestOpsListsThePlatformTemplateLibrary(t *testing.T) {
	mux := serve(t)

	out := templates(t, mux, "/v1/ops/document-templates")
	byKind := map[string]int{}
	for i, tpl := range out.Templates {
		byKind[tpl.Kind] = i
	}
	for _, kind := range []string{
		"management_agreement", "onboarding_checklist",
		"rent_agreement", "lease_deed", "power_of_attorney",
	} {
		if _, ok := byKind[kind]; !ok {
			t.Fatalf("the India library is missing %s, got %+v", kind, out.Templates)
		}
	}

	// Nothing in this suite revises the checklist, so it is the one that still
	// proves a firm reads the platform's library rather than its own rows.
	if !out.Templates[byKind["onboarding_checklist"]].IsDefault {
		t.Error("a firm that has not revised anything is on the platform's template")
	}

	pma := out.Templates[byKind["management_agreement"]]
	// #340 generates the agreement from these; a template that does not declare
	// the notice period cannot carry the four-month bar the owner agreed to.
	want := map[string]bool{"owner_name": false, "manager_name": false,
		"property_address": false, "sale_notice_months": false}
	for _, f := range pma.MergeFields {
		if _, ok := want[f]; ok {
			want[f] = true
		}
	}
	for f, found := range want {
		if !found {
			t.Errorf("the management agreement does not declare %s, got %v", f, pma.MergeFields)
		}
	}
}

// A revision is the firm's own paperwork, and from the moment it is published
// it is what the firm issues.
func TestAFirmsRevisionSupersedesThePlatformTemplate(t *testing.T) {
	mux := serve(t)

	// Whatever is live now, default or a revision this suite left behind on an
	// earlier run — the next version counts on from it.
	before := templates(t, mux, "/v1/ops/document-templates?kind=rent_agreement")
	if len(before.Templates) != 1 {
		t.Fatalf("one live rent agreement to revise, got %+v", before.Templates)
	}

	w := file(t, mux, isolationtest.OrgOwner, "/v1/ops/document-templates", map[string]any{
		"kind":         "rent_agreement",
		"name":         "Our own rent agreement",
		"object_path":  "org/" + isolationtest.OrgOwner.String() + "/templates/" + uuid.NewString() + ".docx",
		"filename":     "rent-agreement.docx",
		"content_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"merge_fields": mergeFieldsOf(before.Templates[0].MergeFields, "society_name"),
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("publishing a revision: %d %s", w.Code, w.Body.String())
	}

	after := templates(t, mux, "/v1/ops/document-templates?kind=rent_agreement")
	if len(after.Templates) != 1 {
		t.Fatalf("one live rent agreement, got %+v", after.Templates)
	}
	live := after.Templates[0]
	if live.IsDefault || live.Name != "Our own rent agreement" {
		t.Errorf("the firm's own revision is what it issues now, got %+v", live)
	}
	if live.Version != before.Templates[0].Version+1 {
		t.Errorf("version = %d, want %d — a revision counts on from what it replaced",
			live.Version, before.Templates[0].Version+1)
	}
}

// Dropping a field the generator relies on produces an agreement with a blank
// where a clause was, so it is refused at the point of upload rather than
// discovered on a signed copy.
func TestARevisionMustKeepTheFieldsTheAgreementNeeds(t *testing.T) {
	mux := serve(t)

	w := file(t, mux, isolationtest.OrgOwner, "/v1/ops/document-templates", map[string]any{
		"kind":         "management_agreement",
		"name":         "Trimmed agreement",
		"object_path":  "org/" + isolationtest.OrgOwner.String() + "/templates/" + uuid.NewString() + ".docx",
		"filename":     "trimmed.docx",
		"content_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"merge_fields": []string{"owner_name", "manager_name"},
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a revision dropping declared fields: %d %s, want 422", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "sale_notice_months") {
		t.Errorf("the refusal names what is missing, got %s", w.Body.String())
	}
}

// The superseded version is still readable: an agreement signed against it is
// only meaningful beside the text that was signed.
func TestSupersededTemplatesStayReadable(t *testing.T) {
	mux := serve(t)

	base := templates(t, mux, "/v1/ops/document-templates?kind=lease_deed")
	w := file(t, mux, isolationtest.OrgOwner, "/v1/ops/document-templates", map[string]any{
		"kind":         "lease_deed",
		"name":         "Our lease deed",
		"object_path":  "org/" + isolationtest.OrgOwner.String() + "/templates/" + uuid.NewString() + ".docx",
		"filename":     "lease-deed.docx",
		"content_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"merge_fields": base.Templates[0].MergeFields,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("publishing a revision: %d %s", w.Code, w.Body.String())
	}
	w = file(t, mux, isolationtest.OrgOwner, "/v1/ops/document-templates", map[string]any{
		"kind":         "lease_deed",
		"name":         "Our lease deed, corrected",
		"object_path":  "org/" + isolationtest.OrgOwner.String() + "/templates/" + uuid.NewString() + ".docx",
		"filename":     "lease-deed-2.docx",
		"content_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"merge_fields": base.Templates[0].MergeFields,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("publishing a second revision: %d %s", w.Code, w.Body.String())
	}

	history := templates(t, mux, "/v1/ops/document-templates?kind=lease_deed&history=true")
	var superseded int
	for _, tpl := range history.Templates {
		if tpl.SupersededAt != "" {
			superseded++
		}
	}
	if superseded < 1 {
		t.Fatalf("the replaced revision is kept, got %+v", history.Templates)
	}
}

// One firm's clause is not another firm's, and the library underneath is.
func TestAFirmsRevisionIsNotVisibleToAnotherOrganisation(t *testing.T) {
	mux := serve(t)

	w := file(t, mux, isolationtest.OrgOwner, "/v1/ops/document-templates", map[string]any{
		"kind":         "power_of_attorney",
		"name":         "Our limited PoA",
		"object_path":  "org/" + isolationtest.OrgOwner.String() + "/templates/" + uuid.NewString() + ".docx",
		"filename":     "poa.docx",
		"content_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"merge_fields": templates(t, mux, "/v1/ops/document-templates?kind=power_of_attorney").Templates[0].MergeFields,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("publishing a revision: %d %s", w.Code, w.Body.String())
	}

	out := call(t, mux, isolationtest.OrgOutsider, http.MethodGet,
		"/v1/ops/document-templates?kind=power_of_attorney")
	if out.Code != http.StatusOK {
		t.Fatalf("GET as another organisation: %d %s", out.Code, out.Body.String())
	}
	var theirs templateList
	decode(t, out, &theirs)
	if len(theirs.Templates) != 1 || !theirs.Templates[0].IsDefault {
		t.Fatalf("another organisation reads the platform's PoA, got %+v", theirs.Templates)
	}
}

func mergeFieldsOf(base []string, extra ...string) []string {
	return append(append([]string{}, base...), extra...)
}
