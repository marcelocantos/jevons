// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// t1122Repo makes a small git repo with a base commit, then a UI-touching
// commit on top, and returns the two SHAs. Callers diff from base to ui.
func t1122Repo(t *testing.T) (dir, base, ui string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) string {
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
	run("init", "-q")
	run("checkout", "-q", "-b", "main")
	if err := os.MkdirAll(dir+"/ui/src", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/README.md", []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "base")
	base = run("rev-parse", "HEAD")

	if err := os.WriteFile(dir+"/ui/src/App.tsx", []byte("export const App = () => null;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "ui change")
	ui = run("rev-parse", "HEAD")
	return dir, base, ui
}

func TestCheckOwnerImpactMissing_UITouchNoSlot(t *testing.T) {
	dir, _, ui := t1122Repo(t)
	report := "```jevons\njevons: kind finish-report\njevons: target T1\njevons: oracle sha=" + ui + "\njevons: silent-ledger none\n```"
	m, err := envelope.Parse(report)
	if m == nil || err != nil {
		t.Fatalf("parse: %v %v", m, err)
	}
	flags := CheckOwnerImpactMissing(m, dir)
	if len(flags) != 1 || flags[0].Kind != FlagOwnerImpactMissingKind {
		t.Fatalf("flags = %v, want one owner_impact_missing", flags)
	}
}

func TestCheckOwnerImpactMissing_ExplicitNoneStillFlags(t *testing.T) {
	dir, _, ui := t1122Repo(t)
	report := "```jevons\njevons: kind finish-report\njevons: target T1\njevons: oracle sha=" + ui + "\njevons: owner-impact none\njevons: silent-ledger none\n```"
	m, err := envelope.Parse(report)
	if m == nil || err != nil {
		t.Fatalf("parse: %v %v", m, err)
	}
	flags := CheckOwnerImpactMissing(m, dir)
	if len(flags) != 1 || flags[0].Kind != FlagOwnerImpactMissingKind {
		t.Fatalf("flags = %v, want one owner_impact_missing", flags)
	}
}

func TestCheckOwnerImpactMissing_DeclaredSkips(t *testing.T) {
	dir, _, ui := t1122Repo(t)
	for _, v := range []string{"decision", "fault", "plan-change"} {
		report := "```jevons\njevons: kind finish-report\njevons: target T1\njevons: oracle sha=" + ui + "\njevons: owner-impact " + v + "\njevons: silent-ledger none\n```"
		m, err := envelope.Parse(report)
		if m == nil || err != nil {
			t.Fatalf("parse(%s): %v %v", v, m, err)
		}
		if flags := CheckOwnerImpactMissing(m, dir); len(flags) != 0 {
			t.Fatalf("owner-impact %s: flags = %v, want none", v, flags)
		}
	}
}

func TestCheckOwnerImpactMissing_BackendOnlySkips(t *testing.T) {
	dir, base, _ := t1122Repo(t)
	report := "```jevons\njevons: kind finish-report\njevons: target T1\njevons: oracle sha=" + base + "\njevons: silent-ledger none\n```"
	m, err := envelope.Parse(report)
	if m == nil || err != nil {
		t.Fatalf("parse: %v %v", m, err)
	}
	if flags := CheckOwnerImpactMissing(m, dir); len(flags) != 0 {
		t.Fatalf("backend-only commit: flags = %v, want none", flags)
	}
}

func TestCheckOwnerImpactMissing_TestOnlyUIPathSkips(t *testing.T) {
	dir, _, _ := t1122Repo(t)
	run := func(args ...string) string {
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
	if err := os.WriteFile(dir+"/ui/src/App.test.tsx", []byte("test('x', () => {});\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "ui test only")
	testOnly := run("rev-parse", "HEAD")

	report := "```jevons\njevons: kind finish-report\njevons: target T1\njevons: oracle sha=" + testOnly + "\njevons: silent-ledger none\n```"
	m, err := envelope.Parse(report)
	if m == nil || err != nil {
		t.Fatalf("parse: %v %v", m, err)
	}
	if flags := CheckOwnerImpactMissing(m, dir); len(flags) != 0 {
		t.Fatalf("test-only ui path: flags = %v, want none", flags)
	}
}
