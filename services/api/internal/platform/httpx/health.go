// Package httpx holds the HTTP concerns every module shares: health, readiness
// and the shape of an error the clients can rely on.
package httpx

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// Health reports whether the process is alive and whether it is ready to take
// traffic. The two are deliberately different: a pod that has lost the database
// is alive — restarting it will not help — but it is not ready.
type Health struct {
	ready     atomic.Bool
	started   time.Time
	version   string
	checkDeps func(context.Context) error
}

// NewHealth returns a Health that reports not-ready until Ready is called.
// checkDeps may be nil while the API has no dependencies to check.
func NewHealth(version string, checkDeps func(context.Context) error) *Health {
	return &Health{started: time.Now(), version: version, checkDeps: checkDeps}
}

// Ready marks the process as able to serve traffic.
func (h *Health) Ready() { h.ready.Store(true) }

// Draining marks the process as no longer able to serve traffic, so that the
// load balancer stops sending requests before the server stops accepting them.
func (h *Health) Draining() { h.ready.Store(false) }

// Live answers the liveness probe. It says nothing about dependencies.
func (h *Health) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "alive",
		"version": h.version,
		"uptime":  time.Since(h.started).Round(time.Second).String(),
	})
}

// Readyz answers the readiness probe, checking dependencies if it has any.
func (h *Health) Readyz(w http.ResponseWriter, r *http.Request) {
	if !h.ready.Load() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not ready",
			"reason": "starting or draining",
		})
		return
	}
	if h.checkDeps != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := h.checkDeps(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "not ready",
				"reason": err.Error(),
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
