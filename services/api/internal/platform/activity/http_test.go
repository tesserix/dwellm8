package activity

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

type fakeFeeder struct {
	gotQuery Query
	gotNote  Note
	entries  []Entry
}

func (f *fakeFeeder) Feed(_ context.Context, q Query) ([]Entry, error) {
	f.gotQuery = q
	return f.entries, nil
}

func (f *fakeFeeder) AddNote(_ context.Context, n Note) (string, error) {
	f.gotNote = n
	return "note-1", nil
}

func mount(f Feeder) *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(f, slog.New(slog.DiscardHandler)).Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	return mux
}

func TestTheLeaseFeedScopesItsSubjectFromThePath(t *testing.T) {
	f := &fakeFeeder{}
	rec := httptest.NewRecorder()
	mount(f).ServeHTTP(rec, httptest.NewRequest("GET",
		"/v1/activity/lease/L1?types=lease.tenancy.started,note&limit=10", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	q := f.gotQuery
	if q.SubjectKind != "lease" || q.SubjectID != "L1" || q.Audience != AudienceOrg {
		t.Fatalf("query %+v", q)
	}
	if len(q.Types) != 2 || q.Limit != 10 {
		t.Fatalf("filters not carried: %+v", q)
	}
}

func TestPagingneedsBothHalvesOfTheCursor(t *testing.T) {
	rec := httptest.NewRecorder()
	mount(&fakeFeeder{}).ServeHTTP(rec, httptest.NewRequest("GET",
		"/v1/activity/lease/L1?before_at=2026-08-01T00:00:00Z", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("half a cursor answered %d", rec.Code)
	}
}

func TestAFullPageCarriesTheNextCursor(t *testing.T) {
	at := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	f := &fakeFeeder{entries: []Entry{
		{ID: "b", OccurredAt: at},
		{ID: "a", OccurredAt: at.Add(-time.Hour)},
	}}
	rec := httptest.NewRecorder()
	mount(f).ServeHTTP(rec, httptest.NewRequest("GET", "/v1/activity/lease/L1?limit=2", nil))

	var out struct {
		NextBeforeAt string `json:"next_before_at"`
		NextBeforeID string `json:"next_before_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.NextBeforeID != "a" || out.NextBeforeAt == "" {
		t.Fatalf("cursor %+v", out)
	}
}

func TestANoteCarriesItsAuthorFromTheSession(t *testing.T) {
	f := &fakeFeeder{}
	req := httptest.NewRequest("POST", "/v1/activity/lease/L1/notes",
		strings.NewReader(`{"body":"tenant called about the tap","visibility":"shared"}`))
	req = req.WithContext(auth.With(req.Context(), auth.Principal{UID: "uid-7"}))
	rec := httptest.NewRecorder()
	mount(f).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	n := f.gotNote
	if n.Author != "uid-7" || n.SubjectID != "L1" || n.Visibility != "shared" {
		t.Fatalf("note %+v", n)
	}
}

func TestAnUnknownFieldIsRefused(t *testing.T) {
	rec := httptest.NewRecorder()
	mount(&fakeFeeder{}).ServeHTTP(rec, httptest.NewRequest("POST",
		"/v1/activity/lease/L1/notes", strings.NewReader(`{"text":"wrong field"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown field answered %d", rec.Code)
	}
}
