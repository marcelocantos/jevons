// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"html"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/marcelocantos/jevons/web"
)

// indexAssetErrorAttr marks the injected banner. Tests assert on the literal.
const indexAssetErrorAttr = "data-jevons-asset-error"

// bodyOpenRE finds the opening <body …> tag so the banner lands as the
// first thing in the document body.
var bodyOpenRE = regexp.MustCompile(`(?i)<body[^>]*>`)

// guardIndexAssets checks every local scripts/… module index.html loads
// against the tree about to serve it, and — when any is absent — returns
// the document with an owner-visible banner naming the missing modules.
//
// 🎯T374. The compile-time ratchet (🎯T292) only guards the embedded binary,
// but the daily daemon serves web/ from disk, so index.html can reference a
// module that is not on the serving path. That used to 404 quietly: the
// undefined global threw at inline-script top level, aborted the rest of
// index.html, and left every later `let` in TDZ, so already-registered
// listeners emitted recurring window.onerror on each fleet poll. One 404
// became a permanently broken cockpit with no owner-visible cause. The
// document is still served — a banner plus a live-but-degraded page beats
// both a silent cascade and a blank error screen.
func guardIndexAssets(raw []byte, exists func(ref string) bool) ([]byte, []string) {
	var missing []string
	for _, ref := range web.IndexScriptRefs(raw) {
		if !exists(ref) {
			missing = append(missing, ref)
		}
	}
	if len(missing) == 0 {
		return raw, nil
	}

	var b strings.Builder
	b.WriteString(`<div ` + indexAssetErrorAttr + `="1" style="position:fixed;top:0;left:0;right:0;z-index:2147483647;` +
		`background:#7f1d1d;color:#fff;font:13px/1.45 -apple-system,system-ui,sans-serif;` +
		`padding:10px 14px;border-bottom:2px solid #ef4444">` +
		`<strong>Cockpit assets incomplete — the UI will not finish initialising.</strong><br>` +
		`jevonsd is serving <code>web/</code> from disk and these modules are not on the serving path: `)
	for i, ref := range missing {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("<code>" + html.EscapeString(ref) + "</code>")
	}
	b.WriteString(`. Restore them (or rebuild), then hard-reload.</div>`)
	banner := []byte(b.String())

	if loc := bodyOpenRE.FindIndex(raw); loc != nil {
		out := make([]byte, 0, len(raw)+len(banner))
		out = append(out, raw[:loc[1]]...)
		out = append(out, banner...)
		out = append(out, raw[loc[1]:]...)
		return out, missing
	}
	return append(banner, raw...), missing
}

// indexContentType is set explicitly because the guard writes the body
// itself rather than going through http.ServeFile's sniffing.
const indexContentType = "text/html; charset=utf-8"

// serveGuardedIndex writes index.html through guardIndexAssets, logging any
// missing module at error level so the fault is visible in the daemon log as
// well as in the cockpit. source names the serving tree (a directory, or
// "embedded") for the log line.
func serveGuardedIndex(w http.ResponseWriter, raw []byte, source string, exists func(ref string) bool) {
	out, missing := guardIndexAssets(raw, exists)
	if len(missing) > 0 {
		slog.Error("cockpit index.html loads modules absent from the serving path",
			"source", source, "missing", missing)
	}
	w.Header().Set("Content-Type", indexContentType)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(out); err != nil {
		slog.Warn("write index.html", "err", err)
	}
}
