// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
)

func TestClassifyDoneEnvelopeOnly(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want DoneClass
	}{
		{"empty", "", DoneUnknown},
		{"bare done prose", "Done. Mission complete.", DoneUnknown},
		{"oracle prose without fence", "Done. SHA abcdef0123456. GATE x GREEN", DoneUnknown},
		{
			"valid finish-report",
			Format(&Message{
				Kind: KindFinishReport, Target: "T766.4", SHA: "abcdef0123456",
				SilentLedger: SilentLedgerEmpty, Payload: "Landed.",
			}),
			DoneYes,
		},
		{
			"finish-report accepted-risk",
			Format(&Message{
				Kind: KindFinishReport, Target: "T766.4", Risk: RiskClass3,
				SilentLedger: SilentLedgerEmpty, Payload: "Owner smoke residual.",
			}),
			DoneYes,
		},
		{
			"finish-report in-progress",
			Format(&Message{
				Kind: KindFinishReport, Target: "T766.4", SHA: "abcdef0123456",
				Status: ProgressInProgress, SilentLedger: SilentLedgerEmpty,
				Payload: "Slice oracle green; remaining work named.",
			}),
			DoneNo,
		},
		{
			"finish-report blocked",
			Format(&Message{
				Kind: KindFinishReport, Target: "T766.4", Status: ProgressBlocked,
				Blocker: "owner-go-ahead", SilentLedger: SilentLedgerEmpty,
			}),
			DoneNo,
		},
		{
			"in-progress even when another slot is missing",
			"```jevons\njevons: kind finish-report\njevons: target T766.4\njevons: status in-progress\n```\n\nDone.",
			DoneNo,
		},
		{
			"scout-report",
			Format(&Message{
				Kind: KindScoutReport, Target: "T766.4",
				SilentLedger: SilentLedgerEmpty,
				Payload:      "Scout done. Fog mapped.",
			}),
			DoneNo,
		},
		{
			"status-ping with done words",
			Format(&Message{
				Kind: KindStatusPing, Target: "T766.4", Status: ProgressInProgress,
				Payload: "Reading done; implementing next.",
			}),
			DoneNo,
		},
		{
			"malformed finish-report",
			"```jevons\njevons: kind finish-report\njevons: target T766.4\n```\n\nDone.",
			DoneUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyDone(tc.in); got != tc.want {
				t.Fatalf("ClassifyDone = %s, want %s", got, tc.want)
			}
			if got := IsDoneReport(tc.in); got != (tc.want == DoneYes) {
				t.Fatalf("IsDoneReport = %v, want %v", got, tc.want == DoneYes)
			}
		})
	}
}

// t7664DoneEnglishAllowlist is the English classifiers ClassifyDone may use
// to decide reap/done. Pinned at zero (🎯T766.4). Adding a phrase scan here
// is the failure the ratchet exists to catch.
var t7664DoneEnglishAllowlist = []string{}

func TestT7664ClassifyDoneEnglishAllowlistZero(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	src := filepath.Join(filepath.Dir(file), "done.go")
	hits := englishCallsInFuncs(t, src, "ClassifyDone", "IsDoneReport")
	if len(hits) != len(t7664DoneEnglishAllowlist) {
		t.Fatalf("English classifiers in the done answer = %v, allowlist %v", hits, t7664DoneEnglishAllowlist)
	}
	for i, hit := range hits {
		if hit != t7664DoneEnglishAllowlist[i] {
			t.Fatalf("allowlist[%d] = %q, hit %q", i, t7664DoneEnglishAllowlist[i], hit)
		}
	}
}

func englishCallsInFuncs(t *testing.T, path string, names ...string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var hits []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !want[fn.Name.Name] {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			call := pkg.Name + "." + sel.Sel.Name
			switch call {
			case "strings.Contains", "strings.HasPrefix", "strings.HasSuffix", "strings.EqualFold":
				hits = append(hits, fn.Name.Name+": "+call)
			}
			return true
		})
	}
	return hits
}
