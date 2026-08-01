package automations_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/automation"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/surface/automations"
)

// The settings surface composes the catalogue, the resolved overrides and the
// recent activity into one screen. Everything here runs against an in-memory
// runner store and a fake Reader — the decisions worth testing are in the
// handlers' shaping of the response and in the routes' refusals, not in SQL.

const org = tenancy.ID("11111111-1111-1111-1111-111111111111")

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type orgs []tenancy.ID

func (o orgs) Active(context.Context) ([]tenancy.ID, error) { return o, nil }

type runnerStore struct {
	overrides map[automation.Key]automation.Override
	saved     []automation.Override
}

func newRunnerStore() *runnerStore {
	return &runnerStore{overrides: map[automation.Key]automation.Override{}}
}

// Overrides mirrors the real store's tenancy check: a row-level-security-backed
// query cannot answer with no organisation in context, and that refusal is the
// one TestListWithNoOrganisationInContextIsUnauthorized exercises.
func (s *runnerStore) Overrides(ctx context.Context) (map[automation.Key]automation.Override, error) {
	if _, ok := tenancy.From(ctx); !ok {
		return nil, tenancy.ErrNoTenant
	}
	return s.overrides, nil
}
func (s *runnerStore) Save(_ context.Context, k automation.Key, o automation.Override, _ string) error {
	s.overrides[k] = o
	s.saved = append(s.saved, o)
	return nil
}
func (s *runnerStore) Recorded(context.Context, automation.Key, string) (bool, error) { return false, nil }
func (s *runnerStore) Record(context.Context, automation.Record) (bool, error)        { return true, nil }
func (s *runnerStore) Requested(context.Context, automation.Key, string) (bool, error) {
	return false, nil
}
func (s *runnerStore) RequestApproval(context.Context, automation.ApprovalRequest) (bool, error) {
	return true, nil
}

// reader fakes the surface's own Reader seam — activity, history and the
// approval queue — independently of the runner's store.
type reader struct {
	activity map[automation.Key]automation.Activity
	history  []automation.RunRecord
	pending  []automation.Approval
	decided  []string
	decideOn error
}

func (r *reader) Activity(context.Context) (map[automation.Key]automation.Activity, error) {
	return r.activity, nil
}
func (r *reader) History(context.Context, automation.Subject, int) ([]automation.RunRecord, error) {
	return r.history, nil
}
func (r *reader) Pending(context.Context, int) ([]automation.Approval, error) { return r.pending, nil }
func (r *reader) Decide(_ context.Context, id, state, reason, by string) error {
	if r.decideOn != nil {
		return r.decideOn
	}
	r.decided = append(r.decided, id+":"+state)
	return nil
}

func def(key automation.Key) automation.Definition {
	return automation.Definition{
		Key: key, Name: "Arrears ladder", Purpose: "Chases unpaid rent.",
		Trigger: automation.TriggerSchedule, EnabledByDefault: true, CeilingMinor: 500_00,
		Params: []automation.Param{
			{Name: "after_days", Purpose: "days late before the first nudge", Unit: "days",
				Default: 3, Min: 1, Max: 30},
		},
		Act: func(context.Context, *automation.Run) error { return nil },
	}
}

func harness(t *testing.T, rs *runnerStore, rd *reader, defs ...automation.Definition) (*http.ServeMux, string) {
	t.Helper()
	if len(defs) == 0 {
		defs = []automation.Definition{def("arrears_ladder")}
	}
	runner, err := automation.NewRunner(defs, orgs{org}, rs, quiet())
	if err != nil {
		t.Fatalf("wiring the runner: %v", err)
	}
	mux := http.NewServeMux()
	automations.New(runner, rd, quiet()).Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	return mux, "/v1/automations"
}

func scoped(req *http.Request) *http.Request {
	return req.WithContext(tenancy.With(req.Context(), org))
}

func TestListReturnsEveryShippedAutomationEvenWithNoOverrides(t *testing.T) {
	mux, path := harness(t, newRunnerStore(), &reader{})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, scoped(httptest.NewRequest(http.MethodGet, path, nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var body struct {
		Automations []struct {
			Key     string `json:"key"`
			Enabled bool   `json:"enabled"`
			Params  []struct {
				Name  string `json:"name"`
				Value int64  `json:"value"`
			} `json:"params"`
		} `json:"automations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(body.Automations) != 1 {
		t.Fatalf("got %d automations, want 1", len(body.Automations))
	}
	a := body.Automations[0]
	if a.Key != "arrears_ladder" || !a.Enabled {
		t.Fatalf("automation = %+v, want arrears_ladder enabled by default", a)
	}
	if len(a.Params) != 1 || a.Params[0].Value != 3 {
		t.Fatalf("params = %+v, want after_days resolved to its default of 3", a.Params)
	}
}

func TestListFoldsActivityIntoEachRow(t *testing.T) {
	rd := &reader{activity: map[automation.Key]automation.Activity{
		"arrears_ladder": {Automation: "arrears_ladder", Runs: 12, Acted: 4, Awaiting: 1, Failed: 0},
	}}
	mux, path := harness(t, newRunnerStore(), rd)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, scoped(httptest.NewRequest(http.MethodGet, path, nil)))

	var body struct {
		Automations []struct {
			Runs     int `json:"runs"`
			Acted    int `json:"acted"`
			Awaiting int `json:"awaiting_approval"`
		} `json:"automations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if got := body.Automations[0]; got.Runs != 12 || got.Acted != 4 || got.Awaiting != 1 {
		t.Fatalf("row = %+v, want the activity folded in", got)
	}
}

// Setting an override persists it and the response reflects the new value —
// a client re-renders from one response rather than issuing a second GET.
func TestSetPersistsAnOverrideAndReturnsTheResolvedList(t *testing.T) {
	rs := newRunnerStore()
	mux, path := harness(t, rs, &reader{})

	body := strings.NewReader(`{"enabled":false,"params":{"after_days":7}}`)
	req := scoped(httptest.NewRequest(http.MethodPut, path+"/arrears_ladder", body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if len(rs.saved) != 1 || rs.saved[0].Enabled == nil || *rs.saved[0].Enabled {
		t.Fatalf("saved overrides = %+v, want one override disabling the automation", rs.saved)
	}

	var resp struct {
		Automations []struct {
			Enabled bool `json:"enabled"`
		} `json:"automations"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if resp.Automations[0].Enabled {
		t.Fatal("the response still reports the automation as enabled after disabling it")
	}
}

func TestSetOnAnUnknownAutomationIsUnprocessable(t *testing.T) {
	mux, path := harness(t, newRunnerStore(), &reader{})
	req := scoped(httptest.NewRequest(http.MethodPut, path+"/not_a_real_key",
		strings.NewReader(`{"enabled":false}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 for an unknown automation key", rec.Code)
	}
}

func TestSetWithUnparseableBodyIsBadRequest(t *testing.T) {
	mux, path := harness(t, newRunnerStore(), &reader{})
	req := scoped(httptest.NewRequest(http.MethodPut, path+"/arrears_ladder", strings.NewReader("not json")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a body that does not parse", rec.Code)
	}
}

func TestListWithNoOrganisationInContextIsUnauthorized(t *testing.T) {
	mux, path := harness(t, newRunnerStore(), &reader{})
	rec := httptest.NewRecorder()
	// Deliberately not scoped: no tenancy.With, matching a request that
	// reached the surface with no resolved organisation.
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 with no organisation in context", rec.Code)
	}
}

func TestApprovalsListsWhatIsWaiting(t *testing.T) {
	rd := &reader{pending: []automation.Approval{
		{ID: "req-1", Automation: "arrears_ladder", Action: "waive_late_fee", Amount: 25_00, Ceiling: 10_00},
	}}
	mux, path := harness(t, newRunnerStore(), rd)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, scoped(httptest.NewRequest(http.MethodGet, path+"/approvals", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var body struct {
		Approvals []struct{ ID string } `json:"approvals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(body.Approvals) != 1 || body.Approvals[0].ID != "req-1" {
		t.Fatalf("approvals = %+v, want the one pending request", body.Approvals)
	}
}

func TestDecideRecordsTheDecisionAgainstTheNamedRequest(t *testing.T) {
	rd := &reader{}
	mux, path := harness(t, newRunnerStore(), rd)
	req := scoped(httptest.NewRequest(http.MethodPost, path+"/approvals/req-1",
		strings.NewReader(`{"decision":"approved","reason":"looks right"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if len(rd.decided) != 1 || rd.decided[0] != "req-1:approved" {
		t.Fatalf("decided = %v, want one decision recorded against req-1", rd.decided)
	}
}

func TestDecidingAnApprovalThatDoesNotExistIsNotFound(t *testing.T) {
	rd := &reader{decideOn: automation.ErrNoApproval}
	mux, path := harness(t, newRunnerStore(), rd)
	req := scoped(httptest.NewRequest(http.MethodPost, path+"/approvals/missing",
		strings.NewReader(`{"decision":"approved"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a decision on a request that does not exist", rec.Code)
	}
}

func TestHistoryReturnsWhatActedOnTheNamedSubject(t *testing.T) {
	rd := &reader{history: []automation.RunRecord{
		{ID: "run-1", Automation: "arrears_ladder", Action: "sent_reminder"},
	}}
	mux, path := harness(t, newRunnerStore(), rd)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, scoped(httptest.NewRequest(http.MethodGet, path+"/history/lease/lease-1", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var body struct {
		History []struct{ ID string } `json:"history"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(body.History) != 1 || body.History[0].ID != "run-1" {
		t.Fatalf("history = %+v, want the one run for this subject", body.History)
	}
}
