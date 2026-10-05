// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

// Structural cross-site enforcement (🎯T385).
//
// The development daemon listens on localhost with no TLS and no authentication, so
// the check in isCrossSite is the only thing standing between an ordinary web
// page open in the owner's browser and a state-changing API call. Relying on
// each handler to call rejectCrossSite had already failed: nine of the
// fourteen mutating routes omitted it, including
// POST /api/security/confined-exec, which runs arbitrary argv. A text/plain
// body makes that a CORS *simple* request — no preflight fires, so the browser
// sends it and the missing guard is decisive.
//
// The guard therefore lives at registration (guardedRouter) and at the outer
// handler (GuardCrossSite), never in handler bodies. A newly added handler is
// covered because of where it is mounted, not because its author remembered.

// safeMethod reports whether m cannot change server state and so needs no
// cross-site guard. WebSocket upgrades are GET; their origin check is
// wsAcceptOptions, which every websocket.Accept in this package passes.
func safeMethod(m string) bool {
	switch strings.ToUpper(m) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// crossSiteExemptPaths is the complete, explicit exemption set: request paths
// whose state-changing methods are deliberately left unguarded.
//
// It is empty, and that is the intended steady state. Non-browser clients —
// the CLI, fleet agents over MCP, the iOS thin client over the pigeon relay —
// send neither Origin nor Referer, so isCrossSite already returns false for
// them and no exemption is needed to keep them working. The browser cockpit is
// same-origin with the daemon, so it is not cross-site either. Any entry added
// here must carry its reason inline, so the exemption set stays visible rather
// than becoming implicit again.
var crossSiteExemptPaths = map[string]string{}

// apiAccessLog is the hook guardedRouter uses to record every API call to
// the jevons eventlog (🎯 API-layer audit trail). It is set once at server
// construction via guardedRouter{mux, journal}; nil means no-op (tests that
// build a bare guardedRouter{mux: m} keep working without a journal).
type apiAccessLog func(method, path string, status int, dur time.Duration, remote string)

// statusRecorder captures the status code a handler writes, defaulting to
// 200 the way net/http does when WriteHeader is never called explicitly.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Hijack, Flush and Unwrap pass through to the wrapped writer. Without them
// the access log broke every WebSocket upgrade (/ws/mux answered "does not
// implement http.Hijacker") and every streamed response, so the cockpit
// loaded but never connected (2026-10-05).
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("server: %T does not support hijacking", r.ResponseWriter)
	}
	// A hijacked connection never writes a status; 101 is what it became.
	r.status = http.StatusSwitchingProtocols
	return h.Hijack()
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logAPIAccess wraps a handler so every request, regardless of outcome, is
// recorded — independent of whatever the downstream service (e.g. Claudia)
// logs on its own side. This is deliberate duplication (🎯 cross-service
// reconciliation): when Jevons and Claudia disagree about what happened,
// both sides need their own record of the API traffic between them, not
// just whichever side remembered to log first.
func logAPIAccess(log apiAccessLog, next http.Handler) http.Handler {
	if log == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log(r.Method, r.URL.Path, rec.status, time.Since(start), r.RemoteAddr)
	})
}

// guardCrossSite applies the cross-site check to every state-changing request
// before next sees the body.
func guardCrossSite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !safeMethod(r.Method) {
			if _, exempt := crossSiteExemptPaths[r.URL.Path]; !exempt {
				if rejectCrossSite(w, r) {
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// GuardCrossSite wraps the daemon's whole handler chain so route groups
// mounted outside this package — the MCP endpoint, the dev server's static
// routes — are guarded on the same terms as the server's own routes.
func GuardCrossSite(next http.Handler) http.Handler { return guardCrossSite(next) }

// router is the route-registration surface used by every route group in this
// package. RegisterRoutes hands the groups a guarding implementation, so a
// handler added to any group is cross-site guarded by construction.
// *http.ServeMux also satisfies it, which keeps the groups testable in
// isolation.
type router interface {
	Handle(pattern string, h http.Handler)
	HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request))
}

// guardedRouter registers every handler behind guardCrossSite.
type guardedRouter struct {
	mux *http.ServeMux
	// journal is the eventlog destination for API-layer access records.
	// Nil keeps guardedRouter usable in tests that only care about the
	// cross-site guard.
	journal *eventlog.Journal
}

// newGuardedRouter wires the API access logger for every route the
// returned router mounts.
func newGuardedRouter(mux *http.ServeMux, journal *eventlog.Journal) guardedRouter {
	return guardedRouter{mux: mux, journal: journal}
}

func (g guardedRouter) accessLog() apiAccessLog {
	if g.journal == nil {
		return nil
	}
	journal := g.journal
	return func(method, path string, status int, dur time.Duration, remote string) {
		eventlog.LogEvent(journal, "info", "api_access", "handled", method+" "+path, map[string]any{
			"method":      method,
			"path":        path,
			"status":      status,
			"duration_ms": dur.Milliseconds(),
			"remote":      remote,
		})
	}
}

func (g guardedRouter) Handle(pattern string, h http.Handler) {
	g.mux.Handle(pattern, logAPIAccess(g.accessLog(), guardCrossSite(h)))
}

func (g guardedRouter) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	g.mux.Handle(pattern, logAPIAccess(g.accessLog(), guardCrossSite(http.HandlerFunc(h))))
}
