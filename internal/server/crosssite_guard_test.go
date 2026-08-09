// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 🎯T385: the daily daemon has no TLS and no authentication, so the cross-site
// guard is the whole defence against a web page in the owner's browser driving
// the API. These tests read the mutating route set out of the source at run
// time, so a route added later is covered without anyone updating a list here.

// routePattern matches a mux registration with an explicit method, e.g.
//
//	mux.HandleFunc("POST /api/ideas", s.handleCaptureIdea)
var routePattern = regexp.MustCompile(`mux\.(?:HandleFunc|Handle)\("([A-Z]+) ([^"]+)"`)

// mutatingRoutesFromSource returns every state-changing route registered in
// the daemon's Go sources. The denominator comes from the source, not from a
// list this test owns — a hand-maintained list is exactly what let nine of the
// fourteen mutating routes go unguarded in the first place.
func mutatingRoutesFromSource(t *testing.T) map[string]string {
	t.Helper()
	routes := map[string]string{} // path -> method
	for _, dir := range []string{".", filepath.Join("..", "mcpserver")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			for _, m := range routePattern.FindAllStringSubmatch(string(src), -1) {
				method, path := m[1], m[2]
				if safeMethod(method) {
					continue
				}
				routes[path] = method
			}
		}
	}
	if len(routes) < 12 {
		t.Fatalf("route scan found only %d mutating routes — scanner is broken, "+
			"a vacuous denominator would make this whole test meaningless", len(routes))
	}
	if _, ok := routes["/api/security/confined-exec"]; !ok {
		t.Fatal("route scan missed /api/security/confined-exec — the route this target exists for")
	}
	return routes
}

// concretePath substitutes a literal segment for each wildcard so the request
// actually reaches the registered handler.
func concretePath(pattern string) string {
	out := pattern
	for _, wc := range []string{"{name}", "{id}"} {
		out = strings.ReplaceAll(out, wc, "probe")
	}
	return out
}

// dailyHandler builds the handler the daemon actually serves: the server's own
// routes plus the outer guard that cmd/jevonsd wraps around the whole mux. The
// canary is registered straight onto the raw mux, bypassing the guarding
// router, and so stands in for the route groups mounted outside this package —
// the MCP endpoint and the dev server's static routes.
func dailyHandler(t *testing.T) http.Handler {
	t.Helper()
	s := New("test", t.TempDir())
	s.SetWritExecutor(nil, "", false)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	mux.HandleFunc("POST /api/t385-canary", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	guarded := GuardCrossSite(mux)
	// A handler that panics on a stub Server still proves the guard let the
	// request through, which is all the same-origin leg asserts.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = recover() }()
		guarded.ServeHTTP(w, r)
	})
}

const (
	daemonHost    = "localhost:13705"
	foreignOrigin = "https://evil.example"
)

// TestEveryMutatingRouteRejectsCrossSite is the structural claim: no
// state-changing route on the daemon accepts a foreign-origin request, and
// none of them rejects a same-origin one.
func TestEveryMutatingRouteRejectsCrossSite(t *testing.T) {
	h := dailyHandler(t)
	routes := mutatingRoutesFromSource(t)
	routes["/api/t385-canary"] = http.MethodPost // route group mounted outside this package

	for pattern, method := range routes {
		path := concretePath(pattern)

		// Foreign Origin must be refused before the handler sees the body.
		req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		req.Host = daemonHost
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", foreignOrigin)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s %s: cross-site Origin status = %d, want 403", method, path, rr.Code)
		}

		// Foreign Referer with no Origin must be refused too.
		req = httptest.NewRequest(method, path, strings.NewReader(`{}`))
		req.Host = daemonHost
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Referer", foreignOrigin+"/page")
		rr = httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s %s: cross-site Referer status = %d, want 403", method, path, rr.Code)
		}

		// Same-origin must not be refused by the guard. The handler is free
		// to fail the request on its merits — only 403 means the guard bit.
		req = httptest.NewRequest(method, path, strings.NewReader(`{}`))
		req.Host = daemonHost
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://"+daemonHost)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		rr = httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code == http.StatusForbidden {
			t.Errorf("%s %s: same-origin request was rejected as cross-site", method, path)
		}

		// A non-browser client sends neither header and must still work.
		req = httptest.NewRequest(method, path, strings.NewReader(`{}`))
		req.Host = daemonHost
		req.Header.Set("Content-Type", "application/json")
		rr = httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code == http.StatusForbidden {
			t.Errorf("%s %s: header-less client was rejected as cross-site", method, path)
		}
	}
}

// TestConfinedExecRejectsSimpleRequestPost covers the specific attack in the
// audit finding: text/plain is a CORS simple request, so the browser sends it
// with no preflight. Without a guard this is command execution on the owner's
// machine from any page he has open.
func TestConfinedExecRejectsSimpleRequestPost(t *testing.T) {
	h := dailyHandler(t)
	const path = "/api/security/confined-exec"
	body := `{"argv":["echo","t385"],"pure":true}`

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Host = daemonHost
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8") // CORS simple request
	req.Header.Set("Origin", foreignOrigin)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("text/plain cross-site exec status = %d, want 403 (body=%s)", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "manifest") {
		t.Fatalf("cross-site exec reached the handler: %s", rr.Body.String())
	}

	// Sec-Fetch-Site alone is enough — some browsers omit Origin.
	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Host = daemonHost
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("Sec-Fetch-Site cross-site exec status = %d, want 403", rr.Code)
	}

	// The same request from the cockpit still executes.
	req = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Host = daemonHost
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+daemonHost)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("same-origin exec status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "manifest") {
		t.Fatalf("same-origin exec did not reach the handler: %s", rr.Body.String())
	}
}

// TestCrossSiteExemptionSetIsEnumerated keeps the exemption set honest: an
// entry may exist, but it must carry a written reason.
func TestCrossSiteExemptionSetIsEnumerated(t *testing.T) {
	for path, reason := range crossSiteExemptPaths {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("exempt path %q has no stated reason", path)
		}
	}
}

// TestSafeMethodsAreNotGuarded documents why the guard keys on method: reads
// and WebSocket upgrades (which are GET, and carry their own origin check in
// wsAcceptOptions) must pass through untouched.
func TestSafeMethodsAreNotGuarded(t *testing.T) {
	var reached bool
	h := GuardCrossSite(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
	}))
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		reached = false
		req := httptest.NewRequest(method, "/ws/chat", nil)
		req.Host = daemonHost
		req.Header.Set("Origin", foreignOrigin)
		h.ServeHTTP(httptest.NewRecorder(), req)
		if !reached {
			t.Errorf("%s was blocked by the cross-site guard", method)
		}
	}
}
