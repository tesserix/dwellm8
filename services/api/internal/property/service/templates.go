package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// Template is one instrument a firm issues (#341).
type Template = store.Template

// ErrNoTemplate is re-exported so a caller need not import store.
var ErrNoTemplate = store.ErrNoTemplate

// The jurisdiction every firm on this platform is in today. Named rather than
// scattered, so the day a second one exists there is one place to widen.
const DefaultJurisdiction = "IN"

// A merge field is what the generator substitutes, so it must be a name and
// not a sentence: unknown shapes fail at upload rather than printing into a
// signed document.
var mergeField = regexp.MustCompile(`^[a-z][a-z0-9_]{1,48}$`)

// Templates is the module's template service.
type Templates struct{ store *store.Templates }

// NewTemplates wires the service over the store.
func NewTemplates(s *store.Templates) *Templates { return &Templates{store: s} }

// Live is what the firm issues today, its own revisions resolved over the
// platform library.
func (t *Templates) Live(ctx context.Context, kind string) ([]Template, error) {
	return t.store.Live(ctx, kind, DefaultJurisdiction)
}

// History is every version, superseded ones included.
func (t *Templates) History(ctx context.Context, kind string) ([]Template, error) {
	return t.store.History(ctx, kind, DefaultJurisdiction)
}

// Get reads one template.
func (t *Templates) Get(ctx context.Context, id string) (Template, error) {
	return t.store.Get(ctx, id)
}

// Revise publishes the firm's own version. A revision may add fields; it may
// not drop one the live template declares, because the clause the generator
// fills from it would come out blank on a document somebody then signs.
func (t *Templates) Revise(ctx context.Context, tenantID string, in Template) (Template, error) {
	in.Jurisdiction = DefaultJurisdiction
	for _, f := range in.MergeFields {
		if !mergeField.MatchString(f) {
			return Template{}, fmt.Errorf("%q is not a merge field name", f)
		}
	}
	live, err := t.store.Live(ctx, in.Kind, DefaultJurisdiction)
	if err != nil {
		return Template{}, err
	}
	if len(live) == 0 {
		return Template{}, fmt.Errorf("%q is not an instrument this issues", in.Kind)
	}
	if missing := missingFrom(live[0].MergeFields, in.MergeFields); len(missing) > 0 {
		return Template{}, fmt.Errorf(
			"a revision must still declare %s", strings.Join(missing, ", "))
	}
	return t.store.Revise(ctx, tenantID, in)
}

func missingFrom(required, declared []string) []string {
	has := make(map[string]bool, len(declared))
	for _, f := range declared {
		has[f] = true
	}
	var missing []string
	for _, f := range required {
		if !has[f] {
			missing = append(missing, f)
		}
	}
	sort.Strings(missing)
	return missing
}
