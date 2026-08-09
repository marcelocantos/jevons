// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/audit"
)

// auditFixtureReport is a well-formed auditor answer spanning two surfaces.
const auditFixtureReport = `{
  "summary": "One critical defect in the chat path; prompts carry a stale rule.",
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
        "acceptance": ["A colliding send is queued or refused visibly"]
      }
    },
    {
      "scope": "prompts",
      "path": "AGENTS.md",
      "severity": "medium",
      "title": "doctrine names a retired spawn tool",
      "detail": "The brief still points at a tool that no longer exists."
    }
  ]
}`

// auditCleanReport reports the same surfaces with nothing outstanding.
const auditCleanReport = `{"summary": "All audited surfaces are clean.", "findings": []}`

// stubDeliverer captures overseer notices instead of sending them.
type stubDeliverer struct{ notices []string }

func (d *stubDeliverer) DeliverAudit(text string) error {
	d.notices = append(d.notices, text)
	return nil
}

// newAuditTestServer wires an auditor over temp dirs whose workdir carries
// the three audited surfaces, with a canned runner in place of the model.
func newAuditTestServer(t *testing.T, answers ...string) (*Server, *stubDeliverer) {
	t.Helper()
	state := t.TempDir()
	work := t.TempDir()
	home := t.TempDir()

	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(work, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/server/chat.go", "package server\n\nfunc Chat() {}\n")
	write("cmd/jevonsd/main.go", "package main\n\nfunc main() {}\n")
	write("AGENTS.md", "# Doctrine\n\nSpawn with the retired tool.\n")
	write(".claude/skills/release/SKILL.md", "# release\n\nPublish a release.\n")

	deliverer := &stubDeliverer{}
	turn := 0
	auditor, err := audit.New(audit.Args{
		StateDir: state,
		Workdir:  work,
		Home:     home,
		Runner: audit.RunnerFunc(func(_ context.Context, a audit.Assignment) (audit.RunOutput, error) {
			raw := auditFixtureReport
			if turn < len(answers) {
				raw = answers[turn]
			}
			turn++
			return audit.RunOutput{Raw: []byte(raw), Model: a.Model}, nil
		}),
		Deliverer: deliverer,
	})
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	s := New(work, nil, nil)
	s.SetAuditor(auditor)
	return s, deliverer
}

func auditCall(t *testing.T, fn func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) string {
	t.Helper()
	res, err := fn(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	text := ideaToolText(res)
	if res.IsError {
		t.Fatalf("tool error: %s", text)
	}
	return text
}

func auditCallErr(t *testing.T, fn func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) string {
	t.Helper()
	res, err := fn(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected a tool error, got: %s", ideaToolText(res))
	}
	return ideaToolText(res)
}

// 🎯T357 acceptance #1 + #2: one cycle covers code, skills, and prompts,
// runs on the advanced-tier pin, and leaves a durable report artifact.
func TestAuditCycleCoversAllScopesAndLeavesArtifacts(t *testing.T) {
	s, deliverer := newAuditTestServer(t)

	text := auditCall(t, s.handleAuditCycle, map[string]any{"force": true})
	for _, want := range []string{"code", "skills", "prompts"} {
		if !strings.Contains(text, want) {
			t.Fatalf("a full scan must cover %s: %s", want, text)
		}
	}
	if !strings.Contains(text, "full_scan=true") {
		t.Fatalf("all three surfaces were in scope, so the pass is a full scan: %s", text)
	}
	if !strings.Contains(text, audit.DefaultModel) {
		t.Fatalf("audits run on the advanced-tier pin %s: %s", audit.DefaultModel, text)
	}
	if !strings.Contains(text, "2 findings") {
		t.Fatalf("cycle should report the fixture findings: %s", text)
	}

	// Acceptance #3: a critical finding notifies the overseer in the same cycle.
	if len(deliverer.notices) != 1 {
		t.Fatalf("critical finding must notify in-cycle, got %d notices", len(deliverer.notices))
	}
	if !strings.Contains(deliverer.notices[0], "owner turn dropped") {
		t.Fatalf("notice must name the critical finding: %s", deliverer.notices[0])
	}
	// The medium finding is below threshold: it is residue, not an interrupt.
	if strings.Contains(deliverer.notices[0], "retired spawn tool") {
		t.Fatalf("sub-threshold findings must not interrupt: %s", deliverer.notices[0])
	}

	// The report is a durable artifact, readable back by id.
	report := auditCall(t, s.handleAuditReport, map[string]any{})
	if !strings.Contains(report, "owner turn dropped") || !strings.Contains(report, "internal/server/chat.go:412") {
		t.Fatalf("report artifact must carry the finding and its evidence: %s", report)
	}

	status := auditCall(t, s.handleAuditStatus, map[string]any{})
	if !strings.Contains(status, "open=2") || !strings.Contains(status, "critical=1") {
		t.Fatalf("status must summarise outstanding residue: %s", status)
	}
	if !strings.Contains(status, "reports=1") {
		t.Fatalf("status must count durable report artifacts: %s", status)
	}
}

// 🎯T357 acceptance #3: findings update prior residue rather than appending
// unread noise — a repeat pass re-reports nothing, and a clean pass resolves.
func TestAuditResidueConvergesAcrossPasses(t *testing.T) {
	s, deliverer := newAuditTestServer(t, auditFixtureReport, auditFixtureReport, auditCleanReport)

	auditCall(t, s.handleAuditCycle, map[string]any{"force": true})
	second := auditCall(t, s.handleAuditCycle, map[string]any{"force": true})
	if !strings.Contains(second, "new=0") {
		t.Fatalf("a repeat pass mints no new residue: %s", second)
	}
	if !strings.Contains(second, "open_total=2") {
		t.Fatalf("standing findings stay open: %s", second)
	}
	// A standing critical does not re-alert every pass.
	if len(deliverer.notices) != 1 {
		t.Fatalf("standing finding re-alerted: %d notices", len(deliverer.notices))
	}

	third := auditCall(t, s.handleAuditCycle, map[string]any{"force": true})
	if !strings.Contains(third, "resolved=2") || !strings.Contains(third, "open_total=0") {
		t.Fatalf("a covering clean pass resolves prior residue: %s", third)
	}
}

// The overseer disposes: the residue tool records handling, and
// ignore_with_reason refuses to be silent about why.
func TestAuditResidueDisposition(t *testing.T) {
	s, _ := newAuditTestServer(t)
	auditCall(t, s.handleAuditCycle, map[string]any{"force": true})

	listing := auditCall(t, s.handleAuditResidue, map[string]any{})
	if !strings.Contains(listing, "disposition: pending") {
		t.Fatalf("fresh residue is pending until the overseer disposes: %s", listing)
	}
	fingerprint := ""
	for line := range strings.SplitSeq(listing, "\n") {
		if _, fp, ok := strings.Cut(strings.TrimSpace(line), "fingerprint: "); ok {
			fingerprint = fp
			break
		}
	}
	if fingerprint == "" {
		t.Fatalf("listing must expose fingerprints to dispose against: %s", listing)
	}

	filed := auditCall(t, s.handleAuditResidue, map[string]any{
		"fingerprint": fingerprint,
		"disposition": audit.DispositionFiled,
		"target_id":   "T361",
	})
	if !strings.Contains(filed, "disposition: filed") || !strings.Contains(filed, "T361") {
		t.Fatalf("filing must record the target it became: %s", filed)
	}

	// ignore_with_reason without a reason is exactly the silent drop the
	// residue ledger exists to prevent.
	errText := auditCallErr(t, s.handleAuditResidue, map[string]any{
		"fingerprint": fingerprint,
		"disposition": audit.DispositionIgnoreWithReason,
	})
	if !strings.Contains(errText, "requires a reason") {
		t.Fatalf("ignore must demand a reason: %s", errText)
	}
	unknown := auditCallErr(t, s.handleAuditResidue, map[string]any{
		"fingerprint": fingerprint,
		"disposition": "shrug",
	})
	if !strings.Contains(unknown, "unknown disposition") {
		t.Fatalf("unknown dispositions are refused: %s", unknown)
	}
}

// Retuning is durable and bounded: the model pin and the cost guards are
// config, and a bad severity is refused rather than silently ignored.
func TestAuditConfigureRetunesDurably(t *testing.T) {
	s, _ := newAuditTestServer(t)

	out := auditCall(t, s.handleAuditConfigure, map[string]any{
		"interval_sec":       float64(3600),
		"model":              "claude-opus-5",
		"max_cycles_per_day": float64(2),
		"notify_severity":    "high",
		"updated_by":         "jevons-po",
	})
	if !strings.Contains(out, "claude-opus-5") || !strings.Contains(out, "interval_sec=3600") {
		t.Fatalf("retune must be reflected back: %s", out)
	}
	if !strings.Contains(out, "at_or_above=high") || !strings.Contains(out, "max_cycles_per_day=2") {
		t.Fatalf("notify and cost dials must retune: %s", out)
	}

	status := auditCall(t, s.handleAuditStatus, map[string]any{})
	if !strings.Contains(status, "claude-opus-5") || !strings.Contains(status, "jevons-po") {
		t.Fatalf("retune must be durable and attributed: %s", status)
	}

	bad := auditCallErr(t, s.handleAuditConfigure, map[string]any{"notify_severity": "urgent"})
	if !strings.Contains(bad, "unknown notify_severity") {
		t.Fatalf("an unknown severity must be refused: %s", bad)
	}
}

// The cost guard is real: without force, a second cycle inside the min gap
// is refused rather than run.
func TestAuditCycleRespectsCostGuard(t *testing.T) {
	s, _ := newAuditTestServer(t)
	auditCall(t, s.handleAuditCycle, map[string]any{"force": true})

	guarded := auditCall(t, s.handleAuditCycle, map[string]any{})
	if !strings.Contains(guarded, "skipped") || !strings.Contains(guarded, "min cycle gap") {
		t.Fatalf("an unforced repeat inside the gap must be skipped: %s", guarded)
	}
}

// Tools refuse cleanly when no auditor is attached, rather than panicking.
func TestAuditToolsWithoutAuditor(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	for name, fn := range map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){
		"cycle":     s.handleAuditCycle,
		"status":    s.handleAuditStatus,
		"residue":   s.handleAuditResidue,
		"report":    s.handleAuditReport,
		"configure": s.handleAuditConfigure,
	} {
		if got := auditCallErr(t, fn, map[string]any{}); !strings.Contains(got, "auditor not configured") {
			t.Fatalf("%s must refuse cleanly: %s", name, got)
		}
	}
}
