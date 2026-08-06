package places

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func ask(t *testing.T, p Provider, q string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	NewHandler(p, quiet()).Autocomplete(w, httptest.NewRequest(http.MethodGet, "/v1/places/autocomplete?q="+q, nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response was not JSON: %q", w.Body.String())
	}
	return w, body
}

func TestAMatchIsReturnedWithItsFieldsSplitOut(t *testing.T) {
	w, body := ask(t, &stub{out: []Suggestion{{
		Description: "Chandra Arcade, Kochi", Line1: "12 Kadavanthra Road",
		City: "Kochi", StateCode: "KL", Pin: "682020",
	}}}, "chandra+arcade")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	got, _ := body["suggestions"].([]any)
	if len(got) != 1 {
		t.Fatalf("want one suggestion, got %v", body)
	}
	first, _ := got[0].(map[string]any)
	if first["state_code"] != "KL" || first["pin_code"] != "682020" {
		t.Errorf("the form's own fields did not survive the encoding: %v", first)
	}
}

func TestNoMatchIsAnEmptyListNotANullOne(t *testing.T) {
	// A null here makes the client's .map() throw on a term nobody matched.
	_, body := ask(t, &stub{}, "zzzzzzzz")
	if got, ok := body["suggestions"].([]any); !ok || got == nil {
		t.Fatalf("want an empty array, got %v", body["suggestions"])
	}
}

type down struct{}

func (down) Suggest(context.Context, string) ([]Suggestion, error) { return nil, ErrUnavailable }

func TestAnOutageIsSaidToBeOneSoTheFormCanOfferManualEntry(t *testing.T) {
	w, body := ask(t, down{}, "kochi")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
	if body["code"] != "geocoder_unavailable" {
		t.Errorf("want a machine-readable code, got %v", body)
	}
}
