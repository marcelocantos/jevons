// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// 🎯T742. T737 made ScanOutput shape-aware so a name catalog of panic:/FAIL
// is not a panic. Minutes later jv-t738-grok-goal-close was banner-flagged
// under a different rule, shell_array_trap, for quoting the standing brief's
// PIPESTATUS warning — the seat had not used PIPESTATUS; it was documenting
// the hazard at the PO's instruction. Any rule that string-matches a hazard
// will fire on prose that names it. The discrimination belongs at the layer
// that decides what text a rule may read, not repeated inside each matcher.
//
// Hermetic, one per string-matching rule: a report explaining the hazard in
// prose is clean; a report exhibiting it in quoted output is flagged; a
// mutation restoring a bare substring match on the whole report goes RED.
// The enumeration is ratcheted so a later rule cannot ship scanning
// unbounded text.

type t742Case struct {
	kind    FlagKind
	needles []string
	prose   string
	quoted  string
}

func t742Cases() []t742Case {
	return []t742Case{
		{
			kind:    FlagShellArrayTrap,
			needles: []string{"${PIPESTATUS[0]}", "${pipestatus[0]}"},
			prose: strings.Join([]string{
				"🎯T738 done, make test-go is green.",
				"",
				"PIPESTATUS is bash-only and this harness runs zsh, where the expansion is empty — the status was never read.",
				"I did not use ${PIPESTATUS[0]} or ${pipestatus[0]}. Recorded the hazard as instructed.",
				"",
				"Landed as abc1234.",
			}, "\n"),
			quoted: reportZshPipestatus,
		},
		{
			kind:    FlagPipelineMasked,
			needles: []string{"go test ./... | tail -20"},
			prose: strings.Join([]string{
				"🎯T414 done, make test-go is green.",
				"",
				"I did not run go test ./... | tail -20; that masks the status. Ran bin/gate instead.",
				"",
				"Landed as abc1234.",
			}, "\n"),
			quoted: reportPipedExitCode,
		},
		{
			kind:    FlagEmptyStatus,
			needles: []string{"EXIT="},
			prose: strings.Join([]string{
				"🎯T386 done, make test-go is green.",
				"",
				"A status variable expanded to nothing; an empty status is not zero. The trap prints a bare EXIT=",
				"I did not cite that. Ran bin/gate.",
				"",
				"Landed as abc1234.",
			}, "\n"),
			quoted: strings.Join([]string{
				"🎯T386 done, make test-go is green.",
				"",
				"    make test-web 2>&1 | tail -25; echo EXIT=",
				"    EXIT=",
				"",
				"Calling that a pass.",
			}, "\n"),
		},
		{
			kind:    FlagOutputContradicts,
			needles: []string{"panic:", "DATA RACE", "--- FAIL"},
			prose:   t737MarkerProseReport(),
			quoted:  reportGreenOverPanic,
		},
	}
}

func TestT742ProseNamingAHazardIsClean(t *testing.T) {
	for _, tc := range t742Cases() {
		t.Run(string(tc.kind), func(t *testing.T) {
			for _, n := range tc.needles {
				if !strings.Contains(tc.prose, n) {
					t.Fatalf("prose fixture no longer contains %q", n)
				}
			}
			flags := FlagFalseGreen(tc.prose, nil)
			if hasKind(flags, tc.kind) {
				t.Fatalf("prose naming %s flagged %v:\n%s", tc.kind, kinds(flags), Banner(flags))
			}
			if len(flags) != 0 {
				t.Fatalf("prose report flagged %v:\n%s", kinds(flags), Banner(flags))
			}
		})
	}
}

func TestT742QuotedExhibitionIsFlagged(t *testing.T) {
	for _, tc := range t742Cases() {
		t.Run(string(tc.kind), func(t *testing.T) {
			flags := FlagFalseGreen(tc.quoted, nil)
			if !hasKind(flags, tc.kind) {
				t.Fatalf("quoted exhibition flags = %v, want %s", kinds(flags), tc.kind)
			}
		})
	}
}

func TestT742FencedExhibitionIsFlagged(t *testing.T) {
	report := strings.Join([]string{
		"🎯T738 done, make test-go is green.",
		"",
		"```",
		`make test-web 2>&1 | tail -25; echo "EXIT=${PIPESTATUS[0]}"`,
		"EXIT=",
		"```",
		"",
		"Calling that a pass.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagShellArrayTrap) {
		t.Fatalf("fenced PIPESTATUS flags = %v, want %s", kinds(flags), FlagShellArrayTrap)
	}
}

func TestT742AllHazardsNamedInOneProseReportAreClean(t *testing.T) {
	// The T738 specimen class: a careful seat documenting every verification
	// hazard the brief names, then citing a real green. Tonight that report
	// was the one the checker punished.
	report := strings.Join([]string{
		"🎯T742 done, make test-go is green.",
		"",
		"PIPESTATUS is bash-only and this harness runs zsh, where ${PIPESTATUS[0]} prints a bare EXIT=",
		"zsh ${pipestatus[0]} is empty too. I did not run go test ./... | tail -20.",
		t737FogKnownSpecimen,
		t737AcceptanceSpecimen,
		"",
		"Landed as abc1234.",
	}, "\n")
	for _, n := range []string{"${PIPESTATUS[0]}", "${pipestatus[0]}", "go test ./... | tail -20", "EXIT=", "panic:", "DATA RACE", "--- FAIL"} {
		if !strings.Contains(report, n) {
			t.Fatalf("combined prose no longer contains %q", n)
		}
	}
	flags := FlagFalseGreen(report, nil)
	if len(flags) != 0 {
		t.Fatalf("combined hazard-prose report flagged %v:\n%s", kinds(flags), Banner(flags))
	}
}

func TestT742QuotedDoesNotHideARealFailOnTheSameReport(t *testing.T) {
	report := strings.Join([]string{
		"🎯T742 done, make test-go is green.",
		"",
		"PIPESTATUS is bash-only; I did not use ${PIPESTATUS[0]}.",
		"",
		"    --- FAIL: TestT742 (0.02s)",
		"",
		"Calling it a pass.",
	}, "\n")
	flags := FlagFalseGreen(report, nil)
	if !hasKind(flags, FlagOutputContradicts) {
		t.Fatalf("flags = %v, want %s — prose must not blanket-skip a quoted fail", kinds(flags), FlagOutputContradicts)
	}
}

func TestT742MutationBareSubstringOnProseGoesRed(t *testing.T) {
	// Control: the same matchers, fed the whole report, still see the
	// needles. If a future edit points them at unbounded text,
	// TestT742ProseNamingAHazardIsClean fails. If a future edit removes
	// the needles from the fixtures, this fails instead of silently
	// weakening the prose-is-clean half.
	for _, tc := range t742Cases() {
		t.Run(string(tc.kind), func(t *testing.T) {
			var unbounded []Flag
			switch tc.kind {
			case FlagPipelineMasked:
				unbounded = scanPipelineMasked(tc.prose)
			case FlagShellArrayTrap:
				unbounded = scanShellArrayTrap(tc.prose)
			case FlagEmptyStatus:
				unbounded = scanEmptyStatus(tc.prose)
			case FlagOutputContradicts:
				// Shape-aware on purpose (🎯T737): a catalog is not a
				// panic even on the whole report. The mutation this
				// row catches is "flag any occurrence", which T737
				// already holds; here we only require the needles.
				if ScanOutput(tc.prose) != nil {
					t.Fatalf("T737 catalog scanned as output: %v", ScanOutput(tc.prose))
				}
				return
			}
			if !hasKind(unbounded, tc.kind) {
				t.Fatalf("unbounded scan of prose did not see %s — the fixture no longer exhibits the hazard as a substring", tc.kind)
			}
		})
	}
}

func TestT742QuotedRegionExtractsIndentedAndFencedNotProse(t *testing.T) {
	text := strings.Join([]string{
		`Prose names ${PIPESTATUS[0]} and go test ./... | tail -20.`,
		"",
		"    indented ${PIPESTATUS[0]}",
		"  two-space nested list ${pipestatus[0]} stays prose",
		"",
		"```",
		"fenced EXIT=",
		"```",
		"",
		"trailing prose EXIT=",
	}, "\n")
	got := quotedRegion(text)
	if !strings.Contains(got, "indented ${PIPESTATUS[0]}") {
		t.Fatalf("quoted region dropped indented code: %q", got)
	}
	if !strings.Contains(got, "fenced EXIT=") {
		t.Fatalf("quoted region dropped fenced code: %q", got)
	}
	if strings.Contains(got, "Prose names") {
		t.Fatalf("quoted region included unindented prose: %q", got)
	}
	if strings.Contains(got, "two-space nested list") {
		t.Fatalf("quoted region treated a 2-space list as code: %q", got)
	}
	if strings.Contains(got, "trailing prose") {
		t.Fatalf("quoted region included trailing prose: %q", got)
	}
}

func TestT742EveryFlagKindIsScopedOrStructured(t *testing.T) {
	kinds := flagKindConstants(t)
	if len(kinds) < 10 {
		t.Fatalf("found only %d FlagKind constants (%v) — the enumerator has rotted, not the flags", len(kinds), kinds)
	}
	classified := map[FlagKind]string{}
	for _, k := range structuredFlagKinds {
		classified[k] = "structured"
	}
	for _, r := range hazardRules {
		if prev, ok := classified[r.kind]; ok {
			t.Errorf("%s classified as both %s and hazard", r.kind, prev)
		}
		classified[r.kind] = string(r.region)
		if r.region != RegionQuoted && r.region != RegionShaped {
			t.Errorf("%s region %q is not quoted or shaped — unbounded scan is not a legal region (🎯T742)", r.kind, r.region)
		}
		if r.scan == nil {
			t.Errorf("%s has no scanner", r.kind)
		}
	}
	for _, k := range kinds {
		if _, ok := classified[k]; !ok {
			t.Errorf("FlagKind %s is not in hazardRules and not in structuredFlagKinds — a new string-matching rule must declare a ScanRegion (🎯T742)", k)
		}
	}
	for k := range classified {
		if !slices.Contains(kinds, k) {
			t.Errorf("classified %s but it is not a FlagKind constant", k)
		}
	}
}

func TestT742FlagFalseGreenDoesNotScanUnbounded(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "claim.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.FuncDecl
	for _, decl := range f.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if ok && d.Name.Name == "FlagFalseGreen" {
			fn = d
			break
		}
	}
	if fn == nil || fn.Body == nil {
		t.Fatal("FlagFalseGreen not found in claim.go")
	}
	dispatched := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call.Fun)
		switch name {
		case "scanHazards":
			dispatched = true
		case "FindString", "MatchString", "ScanOutput":
			t.Errorf("FlagFalseGreen calls %s — hazard matchers must run on a declared ScanRegion via hazardRules, not on the whole report (🎯T742)", name)
		}
		return true
	})
	if !dispatched {
		t.Fatal("FlagFalseGreen no longer dispatches through the enumerated hazard table (🎯T742)")
	}
}

func callName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	default:
		return ""
	}
}

func TestT742HazardRulesCoverEveryStringMatchingKind(t *testing.T) {
	want := []FlagKind{FlagPipelineMasked, FlagShellArrayTrap, FlagEmptyStatus, FlagOutputContradicts}
	var got []FlagKind
	for _, r := range hazardRules {
		got = append(got, r.kind)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("hazardRules kinds = %v, want %v — adding a string-matching rule means adding it here with a region (🎯T742)", got, want)
	}
}

func flagKindConstants(t *testing.T) []FlagKind {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []FlagKind
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				typ := ""
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					if ident, ok := vs.Type.(*ast.Ident); ok {
						typ = ident.Name
					}
					if typ != "FlagKind" {
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
						kinds = append(kinds, FlagKind(s))
					}
				}
			}
		}
	}
	return kinds
}
