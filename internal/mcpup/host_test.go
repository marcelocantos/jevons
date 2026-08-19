// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("atlassian", &claudia.MCPToken{
		AccessToken:  "a1",
		RefreshToken: "r1",
		ClientID:     "cid",
		TokenURL:     "http://token",
	}); err != nil {
		t.Fatal(err)
	}
	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	tok := s2.Get("atlassian")
	if tok == nil || tok.AccessToken != "a1" || tok.RefreshToken != "r1" || tok.ClientID != "cid" {
		t.Fatalf("reloaded = %+v", tok)
	}
}

func TestMountAdvertisesLoopbackAndEnsures(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(up.Close)

	var ensured []claudia.EnsureMCPArgs
	var mu sync.Mutex
	mux := http.NewServeMux()
	h, err := Mount(mux, &MountArgs{
		PublicBase: "http://127.0.0.1:13705",
		Servers: []claudia.MCPServer{
			{Name: "atlassian", URL: up.URL},
			{Name: "bullseye", Command: "/bin/true"}, // stdio skipped
			{Name: "jevonsmcp", URL: "http://127.0.0.1:13705/mcp"},
		},
		SkipNames: map[string]bool{"jevonsmcp": true},
		Ensure: func(args *claudia.EnsureMCPArgs) error {
			mu.Lock()
			ensured = append(ensured, *args)
			mu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	adv := h.Proxy.Advertised()
	if len(adv) != 1 || adv[0].Name != "atlassian" {
		t.Fatalf("Advertised = %+v", adv)
	}
	if adv[0].URL != "http://127.0.0.1:13705/upstream/atlassian" {
		t.Fatalf("advertised URL = %q", adv[0].URL)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ensured) != 1 || ensured[0].URL != adv[0].URL {
		t.Fatalf("ensured = %+v", ensured)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/upstream/atlassian", strings.NewReader(`{}`)))
	if rec.Code != 200 {
		t.Fatalf("proxy status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestUpstreamRegistryAvoidsProxyLoop(t *testing.T) {
	dir := t.TempDir()
	reg, err := OpenUpstreamRegistry(filepath.Join(dir, "upstreams.json"))
	if err != nil {
		t.Fatal(err)
	}
	prefix := "http://127.0.0.1:13705/upstream"
	first, err := reg.Resolve([]claudia.MCPServer{
		{Name: "atlassian", URL: "https://mcp.atlassian.com/v1/mcp"},
	}, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].URL != "https://mcp.atlassian.com/v1/mcp" {
		t.Fatalf("first = %+v", first)
	}
	second, err := reg.Resolve([]claudia.MCPServer{
		{Name: "atlassian", URL: prefix + "/atlassian"},
	}, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].URL != "https://mcp.atlassian.com/v1/mcp" {
		t.Fatalf("second (loopback input) = %+v", second)
	}
}

func TestMountReseedsStoredToken(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("atlassian", &claudia.MCPToken{
		AccessToken: "seeded", TokenType: "Bearer",
	}); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer seeded" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(up.Close)

	mux := http.NewServeMux()
	h, err := Mount(mux, &MountArgs{
		PublicBase: "http://127.0.0.1:9",
		Servers:    []claudia.MCPServer{{Name: "atlassian", URL: up.URL}},
		Store:      store,
		Ensure:     func(*claudia.EnsureMCPArgs) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if tok := h.Proxy.Token("atlassian"); tok == nil || tok.AccessToken != "seeded" {
		t.Fatalf("reseed = %+v", tok)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/upstream/atlassian", strings.NewReader(`{}`)))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}

// 🎯T520: fixture 401 with expired access + valid refresh succeeds on the
// second upstream attempt; Authorize (browser) is never called. Mount
// also persists the refreshed token via OnTokenChange → Store.
func TestMountRefreshOnExpiredAccessNoAuthorize(t *testing.T) {
	var upstreamHits atomic.Int32
	var authorizeHits atomic.Int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			http.Error(w, "want refresh_token", 400)
			return
		}
		if r.Form.Get("refresh_token") != "refresh-good" || r.Form.Get("client_id") != "cid-1" {
			http.Error(w, "bad refresh", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-fresh",
			"refresh_token": "refresh-good",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(tokenSrv.Close)

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := upstreamHits.Add(1)
		auth := r.Header.Get("Authorization")
		switch {
		case auth == "Bearer access-fresh":
			_, _ = io.WriteString(w, `{"ok":true}`)
		case auth == "Bearer access-expired":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://example/.well-known/oauth-protected-resource", error="invalid_token"`)
			http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
		default:
			t.Errorf("unexpected Authorization on hit %d: %q", n, auth)
			http.Error(w, "unexpected", http.StatusUnauthorized)
		}
	}))
	t.Cleanup(up.Close)

	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("atlassian", &claudia.MCPToken{
		AccessToken:  "access-expired",
		RefreshToken: "refresh-good",
		TokenType:    "Bearer",
		ClientID:     "cid-1",
		TokenURL:     tokenSrv.URL,
		Resource:     up.URL,
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	h, err := Mount(mux, &MountArgs{
		PublicBase: "http://127.0.0.1:9",
		Servers:    []claudia.MCPServer{{Name: "atlassian", URL: up.URL}},
		Store:      store,
		Ensure:     func(*claudia.EnsureMCPArgs) error { return nil },
		Probe: func(ctx context.Context, rawURL string) (*claudia.MCPProbe, error) {
			return &claudia.MCPProbe{Kind: claudia.MCPAuthOAuth, URL: rawURL, Status: 401, ResourceMetadata: "http://example/.well-known"}, nil
		},
		Authorize: func(ctx context.Context, args *claudia.AuthorizeMCPArgs) (*claudia.MCPToken, error) {
			authorizeHits.Add(1)
			t.Fatal("Authorize must not run when refresh succeeds")
			return nil, context.Canceled
		},
		OpenURL: func(string) error {
			t.Fatal("OpenURL must not run when refresh succeeds")
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/upstream/atlassian", strings.NewReader(`{}`)))
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := upstreamHits.Load(); got != 2 {
		t.Fatalf("upstream hits = %d, want 2 (expired then fresh)", got)
	}
	if authorizeHits.Load() != 0 {
		t.Fatalf("Authorize called %d times", authorizeHits.Load())
	}
	tok := h.Proxy.Token("atlassian")
	if tok == nil || tok.AccessToken != "access-fresh" {
		t.Fatalf("in-memory token = %+v", tok)
	}
	persisted := store.Get("atlassian")
	if persisted == nil || persisted.AccessToken != "access-fresh" || persisted.RefreshToken != "refresh-good" {
		t.Fatalf("persisted token = %+v", persisted)
	}
}

// 🎯T520: failed refresh is the only path that invokes the browser flow.
func TestMountFailedRefreshInvokesAuthorize(t *testing.T) {
	var authorizeHits atomic.Int32

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
	}))
	t.Cleanup(tokenSrv.Close)

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer access-from-browser" {
			_, _ = io.WriteString(w, `{"ok":true}`)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://example/.well-known/oauth-protected-resource"`)
		http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
	}))
	t.Cleanup(up.Close)

	dir := t.TempDir()
	store, err := OpenStore(filepath.Join(dir, "tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put("atlassian", &claudia.MCPToken{
		AccessToken:  "access-expired",
		RefreshToken: "refresh-stale",
		ClientID:     "cid-1",
		TokenURL:     tokenSrv.URL,
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	_, err = Mount(mux, &MountArgs{
		PublicBase: "http://127.0.0.1:9",
		Servers:    []claudia.MCPServer{{Name: "atlassian", URL: up.URL}},
		Store:      store,
		Ensure:     func(*claudia.EnsureMCPArgs) error { return nil },
		Probe: func(ctx context.Context, rawURL string) (*claudia.MCPProbe, error) {
			return &claudia.MCPProbe{Kind: claudia.MCPAuthOAuth, URL: rawURL, Status: 401, ResourceMetadata: "http://example/.well-known"}, nil
		},
		Authorize: func(ctx context.Context, args *claudia.AuthorizeMCPArgs) (*claudia.MCPToken, error) {
			authorizeHits.Add(1)
			return &claudia.MCPToken{AccessToken: "access-from-browser", TokenType: "Bearer"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/upstream/atlassian", strings.NewReader(`{}`)))
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if authorizeHits.Load() != 1 {
		t.Fatalf("Authorize hits = %d, want 1 (failed refresh → browser)", authorizeHits.Load())
	}
}
