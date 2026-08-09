// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"strings"
)

// Structural cross-site enforcement (🎯T385).
//
// The daily daemon listens on localhost with no TLS and no authentication, so
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
type guardedRouter struct{ mux *http.ServeMux }

func (g guardedRouter) Handle(pattern string, h http.Handler) {
	g.mux.Handle(pattern, guardCrossSite(h))
}

func (g guardedRouter) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	g.mux.Handle(pattern, guardCrossSite(http.HandlerFunc(h)))
}
