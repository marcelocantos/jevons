// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseReportFixtureShape(t *testing.T) {
	raw := []byte(`{
	  "summary": "Two defects in the chat path; skills and prompts clean.",
	  "findings": [
	    {
	      "scope": "code",
	      "path": "internal/server/chat.go",
	      "line": 412,
	      "severity": "critical",
	      "title": "owner turn dropped when the send collides with a busy seat",
	      "detail": "The busy branch returns without queueing, so the turn is lost.",
	      "evidence": "internal/server/chat.go:412 early return with no enqueue",
	      "suggested_target": {
	        "name": "Owner turns are never silently dropped on a busy seat",
	        "acceptance": ["A colliding send is queued or refused visibly", "Hermetic covers the busy path"]
	      }
	    },
	    {
	      "scope": "prompts",
	      "path": "internal/config/persona.md",
	      "severity": "medium",
	      "title": "persona still names a retired spawn tool",
	      "detail": "Doctrine references a tool that no longer exists."
	    }
	  ]
	}`)
	rep, err := ParseReport(raw, ParseArgs{Workdir: "/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(rep.Findings))
	}
	if rep.Summary == "" {
		t.Fatal("summary lost")
	}
	// Severe-first ordering.
	if rep.Findings[0].Severity != SeverityCritical {
		t.Fatalf("first finding severity = %q, want critical", rep.Findings[0].Severity)
	}
	f := rep.Findings[0]
	if f.Scope != ScopeCode || f.Line != 412 {
		t.Fatalf("finding fields lost: %+v", f)
	}
	if f.Fingerprint == "" {
		t.Fatal("fingerprint not computed")
	}
	if f.SuggestedTarget == nil || len(f.SuggestedTarget.Acceptance) != 2 {
		t.Fatalf("suggested target lost: %+v", f.SuggestedTarget)
	}
	if rep.Findings[1].Scope != ScopePrompts {
		t.Fatalf("second finding scope = %q, want prompts", rep.Findings[1].Scope)
	}
}

func TestParseReportTolerantOfModelWrapping(t *testing.T) {
	// Model answers arrive wrapped in prose and fences; the report must
	// survive that rather than voiding a whole (expensive) pass.
	cases := map[string]string{
		"fenced": "Here is the audit:\n```json\n{\"findings\":[{\"scope\":\"code\",\"severity\":\"high\",\"title\":\"t\",\"path\":\"a.go\"}]}\n```\nDone.",
		"prose":  "I reviewed everything.\n{\"findings\":[{\"scope\":\"code\",\"severity\":\"high\",\"title\":\"t\",\"path\":\"a.go\"}]}\nHope that helps!",
		"braces in prose": "Consider the block {like this} first.\n" +
			"{\"findings\":[{\"scope\":\"code\",\"severity\":\"high\",\"title\":\"t\",\"path\":\"a.go\"}]}",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			rep, err := ParseReport([]byte(raw), ParseArgs{})
			if err != nil {
				t.Fatal(err)
			}
			if len(rep.Findings) != 1 {
				t.Fatalf("findings = %d, want 1", len(rep.Findings))
			}
		})
	}

	if _, err := ParseReport([]byte("I could not complete the audit."), ParseArgs{}); err == nil {
		t.Fatal("expected error when output carries no JSON object")
	}
}

func TestParseReportNormalizesAndBounds(t *testing.T) {
	raw := []byte(`{"findings":[
	  {"scope":"code","severity":"Blocker","title":"a","path":"x.go"},
	  {"scope":"nonsense","severity":"","title":"b","path":"y.go"},
	  {"scope":"code","severity":"high","title":"","path":"z.go"},
	  {"scope":"code","severity":"high","title":"a","path":"x.go"}
	]}`)
	rep, err := ParseReport(raw, ParseArgs{DefaultScope: ScopeCode})
	if err != nil {
		t.Fatal(err)
	}
	// "Blocker" → critical; unrated → info; titleless dropped; duplicate dropped.
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %d, want 2: %+v", len(rep.Findings), rep.Findings)
	}
	if rep.Dropped != 2 {
		t.Fatalf("dropped = %d, want 2", rep.Dropped)
	}
	if rep.Findings[0].Severity != SeverityCritical {
		t.Fatalf("severity normalization failed: %q", rep.Findings[0].Severity)
	}
	var unrated Finding
	for _, f := range rep.Findings {
		if f.Title == "b" {
			unrated = f
		}
	}
	if unrated.Severity != SeverityInfo {
		t.Fatalf("unrated finding severity = %q, want info", unrated.Severity)
	}
	if unrated.Scope != ScopeCode {
		t.Fatalf("unknown scope not defaulted: %q", unrated.Scope)
	}
}

func TestParseReportCapsFindings(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"findings":[`)
	for i := range 50 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"scope":"code","severity":"low","title":"finding %d","path":"f%d.go"}`, i, i)
	}
	b.WriteString(`]}`)
	rep, err := ParseReport([]byte(b.String()), ParseArgs{MaxFindings: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Findings) != 10 {
		t.Fatalf("findings = %d, want 10 (cap)", len(rep.Findings))
	}
	if rep.Dropped != 40 {
		t.Fatalf("dropped = %d, want 40", rep.Dropped)
	}
}

func TestFingerprintStableAcrossLineMoves(t *testing.T) {
	// The same defect must keep its identity when code shifts lines or the
	// auditor rephrases the title cosmetically — otherwise every pass mints
	// duplicate residue instead of updating it.
	a := Fingerprint(ScopeCode, "/repo/internal/server/chat.go", "Owner turn dropped on busy seat", "/repo")
	b := Fingerprint(ScopeCode, "internal/server/chat.go", "owner turn dropped on busy seat.", "/repo")
	if a != b {
		t.Fatalf("fingerprint drifted: %s vs %s", a, b)
	}
	c := Fingerprint(ScopeCode, "internal/server/chat.go", "a different defect entirely", "/repo")
	if a == c {
		t.Fatal("distinct findings collided on one fingerprint")
	}
	d := Fingerprint(ScopePrompts, "internal/server/chat.go", "Owner turn dropped on busy seat", "/repo")
	if a == d {
		t.Fatal("scope not part of finding identity")
	}
}

func TestSeverityLadder(t *testing.T) {
	if !SeverityCritical.AtLeast(SeverityHigh) {
		t.Fatal("critical should meet a high threshold")
	}
	if SeverityMedium.AtLeast(SeverityHigh) {
		t.Fatal("medium should not meet a high threshold")
	}
	if Severity("bogus").AtLeast(SeverityInfo) {
		t.Fatal("unknown severity should never cross a threshold")
	}
}
