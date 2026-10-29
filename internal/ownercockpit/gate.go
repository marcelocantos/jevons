// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ownercockpit enforces 🎯T1129: only a real owner-opened cockpit
// session may assert owner-origin sends on browser-shaped HTTP/WebSocket paths.
// Fleet automation browsers carry an explicit marker and never receive the
// owner session cookie.
package ownercockpit

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HeaderFleetBrowser marks HTTP requests from fleet Playwright / browser MCP
// sessions. Owner-origin sends carrying it are refused.
const HeaderFleetBrowser = "X-Jevons-Fleet-Browser"

// CookieName holds the per-daemon owner cockpit session secret.
const CookieName = "jevons-owner-cockpit"

// Gate holds the daemon instance secret compared to the owner cookie.
type Gate struct {
	token string
}

// NewGate mints a fresh owner cockpit session secret.
func NewGate() *Gate {
	return &Gate{token: mintToken()}
}

// Token exposes the secret for hermetic tests that assemble cookies by hand.
func (g *Gate) Token() string {
	if g == nil {
		return ""
	}
	return g.token
}

func mintToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("ownercockpit: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// FleetBrowser reports whether r carries the fleet automation marker.
const fleetBrowserUA = "JevonsFleetBrowser/"

func FleetBrowser(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.Contains(r.Header.Get("User-Agent"), fleetBrowserUA) {
		return true
	}
	v := strings.TrimSpace(r.Header.Get(HeaderFleetBrowser))
	return v == "1" || strings.EqualFold(v, "true")
}

// BrowserShaped reports whether r looks like a same-origin browser product
// request (as opposed to header-less CLI / fleet MCP HTTP clients).
func BrowserShaped(r *http.Request) bool {
	if r == nil {
		return false
	}
	switch strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")) {
	case "same-origin", "same-site", "none":
		return true
	}
	if o := strings.TrimSpace(r.Header.Get("Origin")); o != "" {
		return true
	}
	return false
}

// IssueSessionCookie sets the HttpOnly owner cockpit cookie when bootstrap is
// allowed. It refuses fleet browsers and non-browser-shaped callers.
func (g *Gate) IssueSessionCookie(w http.ResponseWriter, r *http.Request) error {
	if g == nil || g.token == "" {
		return fmt.Errorf("owner cockpit gate not configured")
	}
	if FleetBrowser(r) {
		return fmt.Errorf("fleet browser cannot mint an owner cockpit session")
	}
	if !BrowserShaped(r) {
		return nil
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    g.token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   int((7 * 24 * time.Hour).Seconds()),
	})
	return nil
}

// AuthorizeOwnerSend returns an error when an owner-origin browser-shaped send
// must be refused. Non-owner origins and non-browser callers are always OK.
func (g *Gate) AuthorizeOwnerSend(r *http.Request, origin string) error {
	if g == nil || g.token == "" {
		return nil
	}
	if o := strings.TrimSpace(origin); o != "" && o != "owner" {
		return nil
	}
	if !BrowserShaped(r) {
		return nil
	}
	if FleetBrowser(r) {
		return fmt.Errorf("denied: fleet browser may not send as the owner (🎯T1129); use an isolate for interactive checks (make test-ui-live / UI_HOST)")
	}
	c, err := r.Cookie(CookieName)
	if err != nil || strings.TrimSpace(c.Value) != g.token {
		return fmt.Errorf("denied: owner cockpit session required (🎯T1129); reload the cockpit in your browser")
	}
	return nil
}

// TestCookie returns a cookie for hermetic server tests that exercise the
// browser-shaped owner path.
func (g *Gate) TestCookie() *http.Cookie {
	if g == nil {
		return nil
	}
	return &http.Cookie{Name: CookieName, Value: g.token}
}
