// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// wsAcceptOptions is the single AcceptOptions used by every WebSocket
// upgrade in this package (T38 / Fable F3). Origin is validated:
// missing Origin is allowed (CLI / native clients); same-host Origin
// is allowed; foreign Origin is rejected. Never set InsecureSkipVerify.
func wsAcceptOptions() *websocket.AcceptOptions {
	return &websocket.AcceptOptions{
		// Extra local browser patterns; request Host is always authorized
		// by coder/websocket when Origin host matches.
		OriginPatterns: []string{
			"localhost",
			"localhost:*",
			"127.0.0.1",
			"127.0.0.1:*",
			"[::1]",
			"[::1]:*",
		},
	}
}

// isCrossSite reports whether r looks like a browser cross-site request
// (CSRF / drive-by POST). Same-origin browser fetches and non-browser
// clients (no Origin, no Referer, Sec-Fetch-Site absent or "none") are
// allowed.
func isCrossSite(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site == "cross-site" {
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		return !sameHost(origin, r.Host)
	}
	// Origin is omitted by some browsers on navigational form POSTs, but
	// Referer still names the initiating page (🎯T385).
	if ref := r.Header.Get("Referer"); ref != "" {
		return !sameHost(ref, r.Host)
	}
	return false
}

// sameHost reports whether the absolute URL rawURL points at host. A URL
// that will not parse, or that carries no host, is never treated as same-host.
func sameHost(rawURL, host string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

// rejectCrossSite writes 403 and returns true when the request is cross-site.
func rejectCrossSite(w http.ResponseWriter, r *http.Request) bool {
	if !isCrossSite(r) {
		return false
	}
	http.Error(w, `{"error":"cross-site request rejected"}`, http.StatusForbidden)
	return true
}

// tokenRateLimiter is a trivial fixed-window counter for the OpenAI
// token-mint proxy (T38 / Fable F4).
type tokenRateLimiter struct {
	mu     sync.Mutex
	window time.Time
	count  int
	limit  int
	every  time.Duration
}

func newTokenRateLimiter(limit int, every time.Duration) *tokenRateLimiter {
	return &tokenRateLimiter{limit: limit, every: every}
}

// allow reports whether one more mint is permitted and consumes a slot.
func (l *tokenRateLimiter) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.window.IsZero() || now.Sub(l.window) >= l.every {
		l.window = now
		l.count = 0
	}
	if l.count >= l.limit {
		return false
	}
	l.count++
	return true
}

// defaultTokenMintLimit caps OpenAI ephemeral session mints per minute.
const defaultTokenMintLimit = 10
