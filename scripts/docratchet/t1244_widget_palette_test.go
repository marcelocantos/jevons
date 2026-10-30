// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"regexp"
	"strings"
	"testing"
)

// TestT1244WidgetPaletteMatchesCockpit keeps the Android usage widget's band
// colours on the cockpit's dark palette. The widget transcribed its colours by
// hand and drifted: "under" (spare capacity, blue in the cockpit) was drawn
// amber like "ahead", and "locked" (purple) was drawn green like "ok", so the
// owner saw a healthy Fable window as a warning on the Pixel Fold (2026-10-30).
// Both the constants and the band -> constant mapping are checked.
func TestT1244WidgetPaletteMatchesCockpit(t *testing.T) {
	css := readRepo(t, "ui/src/cockpit.css")
	start := strings.Index(css, `:root, [data-theme="dark"] {`)
	if start < 0 {
		t.Fatal("cockpit.css: no dark :root block")
	}
	end := strings.Index(css[start:], "}")
	dark := css[start : start+end]
	cssVar := func(name string) string {
		m := regexp.MustCompile(`--` + regexp.QuoteMeta(name) + `:\s*#([0-9a-fA-F]{6})\s*;`).FindStringSubmatch(dark)
		if m == nil {
			t.Fatalf("cockpit.css dark palette has no --%s", name)
		}
		return strings.ToLower(m[1])
	}

	const rowsPath = "mobile/android/app/src/main/kotlin/com/canticode/jevons_mobile/widget/UsageRows.kt"
	kt := readRepo(t, rowsPath)
	ktConst := func(name string) string {
		m := regexp.MustCompile(`const val ` + name + ` = 0xff([0-9a-fA-F]{6})\.toInt\(\)`).FindStringSubmatch(kt)
		if m == nil {
			t.Fatalf("%s has no const %s", rowsPath, name)
		}
		return strings.ToLower(m[1])
	}

	for _, c := range []struct{ konst, cssVar string }{
		{"OK", "green"},
		{"ELEVATED", "amber"},
		{"CRITICAL", "red"},
		{"UNDER", "plan-under"},
		{"LOCKED", "plan-locked"},
	} {
		if got, want := ktConst(c.konst), cssVar(c.cssVar); got != want {
			t.Errorf("UsageRows.%s = #%s, cockpit --%s = #%s", c.konst, got, c.cssVar, want)
		}
	}

	// Every server band maps to the constant whose colour the cockpit uses for it.
	for band, konst := range map[string]string{
		"hot": "CRITICAL", "exhausted": "CRITICAL", "ahead": "ELEVATED",
		"under": "UNDER", "locked": "LOCKED", "ok": "OK",
	} {
		re := regexp.MustCompile(`(?m)^\s*(?:"[a-z]+",\s*)*"` + band + `"(?:,\s*"[a-z]+")*\s*->\s*([A-Z]+)\s*$`)
		m := re.FindStringSubmatch(kt)
		if m == nil {
			t.Errorf("%s: no colour mapping for band %q", rowsPath, band)
			continue
		}
		if m[1] != konst {
			t.Errorf("band %q -> %s, want %s (the cockpit's colour for it)", band, m[1], konst)
		}
	}

	const borderPath = "mobile/android/app/src/main/res/drawable/usage_track_exhausted.xml"
	m := regexp.MustCompile(`<stroke[^>]*android:color="#([0-9a-fA-F]{6})"`).FindStringSubmatch(readRepo(t, borderPath))
	if m == nil {
		t.Fatalf("%s has no stroke colour", borderPath)
	}
	if got, want := strings.ToLower(m[1]), cssVar("plan-exhausted"); got != want {
		t.Errorf("exhausted border #%s, cockpit --plan-exhausted #%s", got, want)
	}
}
