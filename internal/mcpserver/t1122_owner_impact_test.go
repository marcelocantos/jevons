// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/gate"
)

// t1122GitRun runs a git command in dir with a fixed committer identity so
// tests are hermetic.
func t1122GitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// t1122UITouchRepo makes a repo whose tip commit touches ui/src (owner-
// visible surface, 🎯T1122 clause 3). Returns the repo dir and the tip SHA.
func t1122UITouchRepo(t *testing.T) (dir, tip string) {
	t.Helper()
	dir = t.TempDir()
	t1122GitRun(t, dir, "init", "-q")
	t1122GitRun(t, dir, "checkout", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(dir, "ui", "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1122GitRun(t, dir, "add", ".")
	t1122GitRun(t, dir, "commit", "-q", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "ui", "src", "App.tsx"), []byte("export const App = () => null;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1122GitRun(t, dir, "add", ".")
	t1122GitRun(t, dir, "commit", "-q", "-m", "ui change")
	tip = t1122GitRun(t, dir, "rev-parse", "HEAD")
	return dir, tip
}

// t1122BackendOnlyRepo makes a repo whose tip commit touches only backend
// (non-ui, non-persona) files. Returns the repo dir and the tip SHA.
func t1122BackendOnlyRepo(t *testing.T) (dir, tip string) {
	t.Helper()
	dir = t.TempDir()
	t1122GitRun(t, dir, "init", "-q")
	t1122GitRun(t, dir, "checkout", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1122GitRun(t, dir, "add", ".")
	t1122GitRun(t, dir, "commit", "-q", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1122GitRun(t, dir, "add", ".")
	t1122GitRun(t, dir, "commit", "-q", "-m", "backend change")
	tip = t1122GitRun(t, dir, "rev-parse", "HEAD")
	return dir, tip
}

// t1122Registry registers name with workDir, mirroring t439Registry but
// taking an explicit workDir rather than minting a fresh temp dir — the
// owner-impact check needs a real git repo to diff.
func t1122Registry(t *testing.T, name, workDir string) *claudia.Registry {
	t.Helper()
	regDir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(regDir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: workDir, SessionID: "s-w",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func t1122FinishReport(sha string, ownerImpact envelope.OwnerImpact) string {
	return envelope.Format(&envelope.Message{
		Kind:         envelope.KindFinishReport,
		Target:       "T1122",
		SHA:          sha,
		OwnerImpact:  ownerImpact,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "Landed.",
	})
}

// (a) a ui-touching report without the slot is kept and flagged.
func TestT1122UITouchingReportWithoutSlotIsKeptAndFlagged(t *testing.T) {
	dir, tip := t1122UITouchRepo(t)
	report := t1122FinishReport(tip, envelope.OwnerImpactNone)
	reg := t1122Registry(t, "jv-t1122-no-slot", dir)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t1122-no-slot", report, nil)
	if ok {
		t.Fatalf("ui-touching report without owner-impact reaped: reason=%s", reason)
	}
	if reason != "false_green_owner_impact_missing" {
		t.Fatalf("reason = %q, want false_green_owner_impact_missing", reason)
	}

	// Explicit "none" is the same as absent for the flag (🎯T1122 clause 3).
	reportNone := t1122FinishReport(tip, envelope.OwnerImpactNoneSlot)
	reg2 := t1122Registry(t, "jv-t1122-none-slot", dir)
	ok2, reason2 := ShouldAutoReapDoneWorkAgent(reg2, "jv-t1122-none-slot", reportNone, nil)
	if ok2 {
		t.Fatalf("ui-touching report with owner-impact none reaped: reason=%s", reason2)
	}
	if reason2 != "false_green_owner_impact_missing" {
		t.Fatalf("reason = %q, want false_green_owner_impact_missing", reason2)
	}
}

// (b) a ui-touching report with owner-impact decision reaps normally.
func TestT1122UITouchingReportWithDecisionSlotReaps(t *testing.T) {
	dir, tip := t1122UITouchRepo(t)
	report := t1122FinishReport(tip, envelope.OwnerImpactDecision)
	reg := t1122Registry(t, "jv-t1122-decision", dir)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t1122-decision", report, nil)
	if !ok {
		t.Fatalf("ui-touching report with owner-impact decision kept: reason=%s", reason)
	}
}

// (c) a backend-only report (no ui/overseer-speech diff) needs no slot and
// reaps normally regardless.
func TestT1122BackendOnlyReportNeedsNoSlot(t *testing.T) {
	dir, tip := t1122BackendOnlyRepo(t)
	report := t1122FinishReport(tip, envelope.OwnerImpactNone)
	reg := t1122Registry(t, "jv-t1122-backend", dir)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t1122-backend", report, nil)
	if !ok {
		t.Fatalf("backend-only report without owner-impact kept: reason=%s", reason)
	}
}

// The notice text names the missing slot and the accepted values so the
// parent can ask for exactly what is missing (🎯T1122 clause 3, item 2's
// "one typed notice to the parent"). The reap path (reap_done.go) fires
// this exact formatter when the owner-impact flag is the veto's first
// flag; TestT1122UITouchingReportWithoutSlotIsKeptAndFlagged pins that the
// veto fires with reason false_green_owner_impact_missing in the first
// place.
func TestT1122OwnerImpactMissingNoticeNamesSlot(t *testing.T) {
	dir, tip := t1122UITouchRepo(t)
	report := t1122FinishReport(tip, envelope.OwnerImpactNone)
	m, err := envelope.Parse(report)
	if m == nil || err != nil {
		t.Fatalf("parse: %v %v", m, err)
	}
	flags := gate.CheckOwnerImpactMissing(m, dir)
	if len(flags) != 1 {
		t.Fatalf("flags = %v, want one", flags)
	}
	notice := FormatOwnerImpactMissingNotice("T1122", "jv-t1122-notice", flags[0])
	for _, want := range []string{"🎯T1122", "jv-t1122-notice", "owner-impact-missing", "decision|fault|plan-change|none"} {
		if !strings.Contains(notice, want) {
			t.Fatalf("notice %q missing %q", notice, want)
		}
	}
}
