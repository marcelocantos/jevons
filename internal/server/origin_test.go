// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// T38 / Fable F3: foreign Origin must not upgrade; same-host and
// missing Origin still succeed.
func TestWebSocketOriginGuard(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, wsAcceptOptions())
		if err != nil {
			return
		}
		conn.CloseNow()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Same-origin (server's own host).
	_, _, err := websocket.Dial(t.Context(), strings.Replace(srv.URL, "http", "ws", 1)+"/ws", nil)
	if err != nil {
		t.Fatalf("same-origin dial failed: %v", err)
	}

	// Foreign Origin must be rejected.
	_, _, err = websocket.Dial(t.Context(), strings.Replace(srv.URL, "http", "ws", 1)+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}},
	})
	if err == nil {
		t.Fatal("expected foreign Origin dial to fail")
	}
}

func TestIsCrossSite(t *testing.T) {
	mk := func(host, origin, site string) *http.Request {
		r := httptest.NewRequest("POST", "http://"+host+"/api/x", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		return r
	}
	cases := []struct {
		name string
		r    *http.Request
		want bool
	}{
		{"no origin", mk("localhost:13705", "", ""), false},
		{"same origin", mk("localhost:13705", "http://localhost:13705", "same-origin"), false},
		{"cross origin header", mk("localhost:13705", "https://evil.example", ""), true},
		{"sec-fetch cross-site", mk("localhost:13705", "", "cross-site"), true},
		{"sec-fetch same-site", mk("localhost:13705", "http://localhost:13705", "same-site"), false},
	}
	for _, tc := range cases {
		if got := isCrossSite(tc.r); got != tc.want {
			t.Errorf("%s: isCrossSite = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRejectCrossSiteOnMutatingHandlers(t *testing.T) {
	s := New("test")
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// The legacy /api/sessions kill route is gone (🎯T41) — a cross-site
	// POST must not find anything to hit.
	req, _ := http.NewRequest("POST", srv.URL+"/api/sessions/abc/kill", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("removed kill route status = %d, want 404", resp.StatusCode)
	}

	// Cross-site token mint must be 403.
	req, _ = http.NewRequest("POST", srv.URL+"/api/realtime/token", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site token status = %d, want 403", resp.StatusCode)
	}
}

func TestTokenRateLimiter(t *testing.T) {
	l := newTokenRateLimiter(2, time.Minute)
	now := time.Now()
	if !l.allow(now) || !l.allow(now) {
		t.Fatal("first two allows should pass")
	}
	if l.allow(now) {
		t.Fatal("third allow in window should fail")
	}
	if !l.allow(now.Add(time.Minute + time.Second)) {
		t.Fatal("allow after window should pass")
	}
}
