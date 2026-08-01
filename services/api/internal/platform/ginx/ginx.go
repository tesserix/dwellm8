// Package ginx is Gin behind the same governance as everything else.
//
// The platform standardises new HTTP surfaces on Gin (owner's call,
// 2026-08-02) while keeping ADR-0020's contract: a route exists only by
// declaring its authorisation or the reason it has none. This registrar is
// the only sanctioned way to put a handler on a Gin engine — it has no method
// that takes one without a Check or a reasoned Open, exactly like
// authz.Registrar — and the guard consulted is the process's one Guard, never
// a copy.
//
// A ginx engine is an http.Handler, so it mounts under the existing
// authentication, tenancy-resolution and rate-limit middleware unchanged; Gin
// replaces only the routing and handler ergonomics inside its subtree.
package ginx

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

// Engine is a quiet production Gin engine.
func Engine() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()
	e.Use(gin.Recovery())
	return e
}

// Registrar is how a module publishes Gin routes. Same contract as
// authz.Registrar: declaration is the price of registration.
type Registrar struct {
	engine *gin.Engine
	guard  *authz.Guard
	// Declared mirrors authz.Registrar's, for the wiring test.
	Declared []authz.Declaration
}

// New builds the registrar over an engine and the process guard.
func New(engine *gin.Engine, guard *authz.Guard) *Registrar {
	return &Registrar{engine: engine, guard: guard}
}

// Handle registers a guarded route. Gin's :param values are copied onto the
// request's PathValue before the check runs, so authz.PathObject reads them
// the same way it reads a ServeMux pattern's.
func (r *Registrar) Handle(method, path string, c authz.Check, h gin.HandlerFunc) {
	r.Declared = append(r.Declared, authz.Declaration{Pattern: method + " " + path, Relation: c.Relation})
	guard := r.guard
	r.engine.Handle(method, path, func(gc *gin.Context) {
		for _, p := range gc.Params {
			gc.Request.SetPathValue(p.Key, p.Value)
		}
		guard.Wrap(c, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			gc.Request = req
			h(gc)
		})).ServeHTTP(gc.Writer, gc.Request)
	})
}

// Open registers a deliberately unguarded route, and makes the registrant say
// why — an empty reason panics at wiring time, before any traffic.
func (r *Registrar) Open(method, path, reason string, h gin.HandlerFunc) {
	if reason == "" {
		panic("ginx: an Open route must state its reason: " + method + " " + path)
	}
	r.Declared = append(r.Declared, authz.Declaration{Pattern: method + " " + path, Reason: reason})
	r.engine.Handle(method, path, func(gc *gin.Context) {
		for _, p := range gc.Params {
			gc.Request.SetPathValue(p.Key, p.Value)
		}
		h(gc)
	})
}
