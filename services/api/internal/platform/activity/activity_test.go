package activity

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The SQL the store builds is the security boundary the resident audience
// rides on, so its shape is asserted directly: what the renter's query may
// and may not contain.

func TestAResidentQueryIsAllowlistedAndLeaseScoped(t *testing.T) {
	q := Query{SubjectKind: "lease", SubjectID: "L1", Audience: AudienceResident}

	events, _ := q.eventsSQL()
	if !strings.Contains(events, "LIKE ANY") {
		t.Fatal("a resident's event query must carry the allowlist")
	}
	notes, _ := q.notesSQL(0)
	if !strings.Contains(notes, "n.visibility = 'shared'") {
		t.Fatal("a resident sees shared notes only")
	}

	// The allowlist is an allowlist: lifecycle and notices, and nothing
	// financial-side. #196's failure scenario — the renter must never see the
	// owner's fees or payouts, and a new event type must not reach them by
	// default.
	financial := []string{
		"money.payout_account.changed", "money.platform_fee.charged",
		"money.payment.captured", "identity.organisation.created",
	}
	for _, typ := range financial {
		for _, p := range residentPrefixes {
			if strings.HasPrefix(typ, strings.TrimSuffix(p, "%")) {
				t.Fatalf("the allowlist admits %s via %s", typ, p)
			}
		}
	}
}

func TestAResidentFeedMustNameItsLease(t *testing.T) {
	st := NewStore(nil)
	if _, err := st.Feed(t.Context(), Query{Audience: AudienceResident}); err == nil {
		t.Fatal("a resident feed with no lease would be the landlord's portfolio")
	}
	if _, err := st.Feed(t.Context(), Query{Audience: AudienceResident,
		SubjectKind: "organisation", SubjectID: "o1"}); err == nil {
		t.Fatal("a resident feed scoped to anything but a lease must be refused")
	}
}

func TestASubjectIsAKindAndAnIDTogether(t *testing.T) {
	st := NewStore(nil)
	if _, err := st.Feed(t.Context(), Query{SubjectKind: "lease"}); err == nil {
		t.Fatal("a kind without an id must be refused")
	}
	if _, err := st.Feed(t.Context(), Query{SubjectID: "L1"}); err == nil {
		t.Fatal("an id without a kind must be refused")
	}
}

func TestTypesFilterSplitsAcrossTheUnion(t *testing.T) {
	// Only notes asked for: the event half must select nothing.
	q := Query{Types: []string{"note"}}
	events, _ := q.eventsSQL()
	if !strings.Contains(events, "false") {
		t.Fatal("types=[note] must silence the event half")
	}
	notes, _ := q.notesSQL(0)
	if strings.Contains(notes, "false") {
		t.Fatal("types=[note] must keep the note half")
	}

	// Only an event type asked for: the note half must select nothing.
	q = Query{Types: []string{"lease.tenancy.started"}}
	if events, _ := q.eventsSQL(); strings.Contains(events, "false") {
		t.Fatal("a named event type must keep the event half")
	}
	if notes, _ := q.notesSQL(0); !strings.Contains(notes, "false") {
		t.Fatal("types without \"note\" must silence the note half")
	}
}

func TestNoteValidation(t *testing.T) {
	st := NewStore(nil)
	base := Note{SubjectKind: "lease", SubjectID: "L1", Author: "a", Body: "call the plumber"}

	bad := map[string]Note{
		"no subject":     {Author: "a", Body: "x"},
		"empty body":     {SubjectKind: "lease", SubjectID: "L1", Author: "a", Body: "   "},
		"no author":      {SubjectKind: "lease", SubjectID: "L1", Body: "x"},
		"odd visibility": {SubjectKind: "lease", SubjectID: "L1", Author: "a", Body: "x", Visibility: "public"},
		"a pasted essay": {SubjectKind: "lease", SubjectID: "L1", Author: "a", Body: strings.Repeat("x", maxNoteRunes+1)},
	}
	for name, n := range bad {
		if _, err := st.AddNote(t.Context(), n); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The valid one gets past validation and fails at the tenancy boundary,
	// which is the next gate an unscoped context meets.
	if _, err := st.AddNote(t.Context(), base); !errors.Is(err, tenancy.ErrNoTenant) {
		t.Fatalf("a valid note should have reached the tenancy boundary, got: %v", err)
	}
}

func TestCursorAndWindowApplyToTheWholeFeed(t *testing.T) {
	at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	q := Query{From: at.Add(-time.Hour), To: at, BeforeAt: at, BeforeID: "01ABC"}
	where, args := q.cursorSQL(nil)
	for _, want := range []string{"occurred_at >=", "occurred_at <=", "(occurred_at, id) <"} {
		if !strings.Contains(where, want) {
			t.Errorf("missing %q in %q", want, where)
		}
	}
	if len(args) != 4 {
		t.Fatalf("got %d args, want 4", len(args))
	}
}
