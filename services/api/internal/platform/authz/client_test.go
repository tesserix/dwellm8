package authz

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeFGA is enough of OpenFGA's HTTP surface for Bootstrap and Check.
type fakeFGA struct {
	stores      map[string]string // name -> id
	models      []json.RawMessage
	modelWrites int
	allowed     bool
}

func (f *fakeFGA) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stores", func(w http.ResponseWriter, r *http.Request) {
		type store struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		var out struct {
			Stores []store `json:"stores"`
		}
		for name, id := range f.stores {
			out.Stores = append(out.Stores, store{ID: id, Name: name})
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("POST /stores", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		f.stores[in.Name] = "store-" + in.Name
		json.NewEncoder(w).Encode(map[string]string{"id": "store-" + in.Name})
	})
	mux.HandleFunc("GET /stores/{id}/authorization-models", func(w http.ResponseWriter, r *http.Request) {
		var out struct {
			Models []json.RawMessage `json:"authorization_models"`
		}
		if n := len(f.models); n > 0 {
			out.Models = f.models[n-1:]
		}
		json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("POST /stores/{id}/authorization-models", func(w http.ResponseWriter, r *http.Request) {
		f.modelWrites++
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		m["id"] = "model-1"
		b, _ := json.Marshal(m)
		f.models = append(f.models, b)
		json.NewEncoder(w).Encode(map[string]string{"authorization_model_id": "model-1"})
	})
	mux.HandleFunc("POST /stores/{id}/check", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"allowed": f.allowed})
	})
	return mux
}

func TestBootstrapCreatesOnceThenReuses(t *testing.T) {
	f := &fakeFGA{stores: map[string]string{}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.Bootstrap(context.Background(), "dwellm8"); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if f.modelWrites != 1 {
		t.Fatalf("first bootstrap should write the model once, wrote %d", f.modelWrites)
	}

	// A second boot against the same store: same store id, no new model.
	c2 := NewClient(srv.URL)
	if err := c2.Bootstrap(context.Background(), "dwellm8"); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if f.modelWrites != 1 {
		t.Fatalf("an unchanged model must not be re-written, writes=%d", f.modelWrites)
	}
	if c2.storeID != c.storeID || c2.modelID != c.modelID {
		t.Fatalf("second boot resolved (%s,%s), first (%s,%s)", c2.storeID, c2.modelID, c.storeID, c.modelID)
	}
}

func TestCheckBeforeBootstrapIsAnError(t *testing.T) {
	c := NewClient("http://unreachable.invalid")
	if _, err := c.Check(context.Background(), "user:u", "can_view", "agreement:a"); err == nil {
		t.Fatal("a check before bootstrap must error, not silently deny-or-allow")
	}
}

func TestCheckAsksWithTheModelPinned(t *testing.T) {
	f := &fakeFGA{stores: map[string]string{}, allowed: true}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.Bootstrap(context.Background(), "dwellm8"); err != nil {
		t.Fatal(err)
	}
	allowed, err := c.Check(context.Background(), "user:u", "can_view", "agreement:a")
	if err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	f.allowed = false
	allowed, err = c.Check(context.Background(), "user:u", "can_view", "agreement:a")
	if err != nil || allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
}

// The embedded model must be the transform of model.fga; CI regenerates and
// diffs, this only asserts it parses and carries the schema it claims.
func TestEmbeddedModelIsWellFormed(t *testing.T) {
	var m struct {
		SchemaVersion string         `json:"schema_version"`
		Types         []any          `json:"type_definitions"`
		Conditions    map[string]any `json:"conditions"`
	}
	if err := json.Unmarshal(modelJSON, &m); err != nil {
		t.Fatalf("model.json does not parse: %v", err)
	}
	if m.SchemaVersion != "1.1" || len(m.Types) == 0 || len(m.Conditions) == 0 {
		t.Fatalf("model.json is not the shipped model: schema=%q types=%d conditions=%d",
			m.SchemaVersion, len(m.Types), len(m.Conditions))
	}
}
