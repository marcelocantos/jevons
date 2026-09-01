// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// 🎯T610's closing clause: adding a band here must break a test until the
// cockpit can paint it. The runtime half already exists — paceOfWindow
// reports a served band it cannot map instead of swallowing it — but a
// console warning in a browser nobody has open is not an oracle. This test
// is the build-time half: it enumerates every WeeklyBand constant in this
// package and every key of SERVED_BAND in ui/src/plan/pace.ts, and fails
// when a band that can reach the wire has no paint.
//
// backendOnlyBands is the deliberate residue: bands WeeklyBandOf can return
// for a backend but BandOfWindow never serves for a window (WithBands is the
// only writer of Window.Band). Growing this set is a conscious edit with
// this comment staring back, not a silent default.
var backendOnlyBands = map[string]bool{
	string(BandUnpublished): true, // no window ⇒ nothing on the wire to band
}

func TestEveryServableBandHasPaint(t *testing.T) {
	goBands := weeklyBandConstants(t)
	tsBands := servedBandKeys(t)

	// Self-checks: an enumerator that finds nothing proves nothing.
	if len(goBands) < 6 {
		t.Fatalf("found only %d WeeklyBand constants (%v) — the enumerator has rotted, not the bands", len(goBands), goBands)
	}
	if len(tsBands) < 6 {
		t.Fatalf("found only %d SERVED_BAND keys (%v) — the pace.ts parser has rotted, not the map", len(tsBands), tsBands)
	}

	for _, band := range goBands {
		if backendOnlyBands[band] {
			continue
		}
		if !tsBands[band] {
			t.Errorf("band %q exists in internal/planusage but ui/src/plan/pace.ts SERVED_BAND cannot paint it (🎯T610): map it in the cockpit, or — only if BandOfWindow can never return it — add it to backendOnlyBands with the reason", band)
		}
	}
	for band := range tsBands {
		if !slices.Contains(goBands, band) {
			t.Errorf("SERVED_BAND maps %q but no WeeklyBand constant serves it — a paint with no daemon behind it is the second model this target retired", band)
		}
	}
}

// weeklyBandConstants walks every non-test file in this package for const
// declarations of type WeeklyBand and returns their string values. AST, not
// regex, so a band declared in a new file or a reformatted block still
// counts.
func weeklyBandConstants(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var bands []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					ident, ok := vs.Type.(*ast.Ident)
					if !ok || ident.Name != "WeeklyBand" {
						continue
					}
					for _, v := range vs.Values {
						lit, ok := v.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						s, err := strconv.Unquote(lit.Value)
						if err != nil {
							t.Fatalf("unquoting %s: %v", lit.Value, err)
						}
						bands = append(bands, s)
					}
				}
			}
		}
	}
	return bands
}

// servedBandKeys reads the SERVED_BAND map out of the cockpit source. The
// cockpit is TypeScript, so this side is textual: anchored to the const's
// declaration and its closing brace, keys taken one per line.
func servedBandKeys(t *testing.T) map[string]bool {
	t.Helper()
	path := filepath.Join("..", "..", "ui", "src", "plan", "pace.ts")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	block := regexp.MustCompile(`(?s)const SERVED_BAND[^=]*=\s*\{(.*?)\n\};`).FindSubmatch(src)
	if block == nil {
		t.Fatalf("no SERVED_BAND map found in %s — if it moved or was renamed, move this anchor with it", path)
	}
	keys := map[string]bool{}
	keyRe := regexp.MustCompile(`(?m)^\s*'?(\w+)'?\s*:`)
	for _, m := range keyRe.FindAllSubmatch(block[1], -1) {
		keys[string(m[1])] = true
	}
	return keys
}
