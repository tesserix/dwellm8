package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotReadyUntilReady(t *testing.T) {
	h := NewHealth("test", nil)
	w := httptest.NewRecorder()
	h.Readyz(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz before Ready() = %d, want 503", w.Code)
	}

	h.Ready()
	w = httptest.NewRecorder()
	h.Readyz(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("readyz after Ready() = %d, want 200", w.Code)
	}
}

func TestLiveDoesNotDependOnDependencies(t *testing.T) {
	// A pod with a broken database is alive; restarting it would not help.
	h := NewHealth("test", func(context.Context) error { return errors.New("database down") })
	w := httptest.NewRecorder()
	h.Live(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("healthz with a failing dependency = %d, want 200", w.Code)
	}
}

func TestNotReadyWhenADependencyFails(t *testing.T) {
	h := NewHealth("test", func(context.Context) error { return errors.New("database down") })
	h.Ready()
	w := httptest.NewRecorder()
	h.Readyz(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with a failing dependency = %d, want 503", w.Code)
	}
}

func TestDrainingStopsTraffic(t *testing.T) {
	h := NewHealth("test", nil)
	h.Ready()
	h.Draining()
	w := httptest.NewRecorder()
	h.Readyz(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz while draining = %d, want 503", w.Code)
	}
}
