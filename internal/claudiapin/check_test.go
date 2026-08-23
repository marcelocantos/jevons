// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package claudiapin_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/claudiapin"
)

func TestRequireVersion(t *testing.T) {
	got := claudiapin.RequireVersion("module x\n\nrequire (\n\tgithub.com/marcelocantos/claudia v0.24.0\n)\n")
	if got != "v0.24.0" {
		t.Fatalf("got %q", got)
	}
	if claudiapin.RequireVersion("module x\n") != "" {
		t.Fatal("expected empty")
	}
}

func TestCheckNamesPinAndMissingAgainstSibling(t *testing.T) {
	root := t.TempDir()
	jevons := filepath.Join(root, "jevons")
	sib := filepath.Join(root, "claudia")
	if err := os.MkdirAll(jevons, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sib, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module github.com/marcelocantos/jevons\n\ngo 1.26\n\nrequire github.com/marcelocantos/claudia v0.24.0\n"
	if err := os.WriteFile(filepath.Join(jevons, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sib, "go.mod"), []byte("module github.com/marcelocantos/claudia\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run(sib, "git", "init")
	run(sib, "git", "config", "user.email", "t448@test")
	run(sib, "git", "config", "user.name", "t448")
	if err := os.WriteFile(filepath.Join(sib, "a.txt"), []byte("pin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(sib, "git", "add", "a.txt")
	run(sib, "git", "commit", "-m", "pin base")
	run(sib, "git", "tag", "v0.24.0")
	if err := os.WriteFile(filepath.Join(sib, "b.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(sib, "git", "add", "b.txt")
	run(sib, "git", "commit", "-m", "fix after pin")

	r, err := claudiapin.Check(jevons)
	if err != nil {
		t.Fatal(err)
	}
	if r.PinVersion != "v0.24.0" {
		t.Fatalf("PinVersion=%q", r.PinVersion)
	}
	if r.PinSHA == "" {
		t.Fatal("expected resolved PinSHA")
	}
	if r.SiblingHEAD == "" || r.SiblingHEAD == r.PinSHA {
		t.Fatalf("expected sibling ahead of pin: pin=%s head=%s", r.PinSHA, r.SiblingHEAD)
	}
	if len(r.Missing) == 0 {
		t.Fatal("expected missing sibling commits named")
	}
	if !strings.Contains(r.Missing[0], "fix after pin") {
		t.Fatalf("missing[0]=%q", r.Missing[0])
	}
	if r.Loud == "" {
		t.Fatal("expected loud staleness")
	}
	if !strings.Contains(r.Decision, "go.work") {
		t.Fatalf("decision missing go.work seam: %s", r.Decision)
	}
	if !claudiapin.HardFail(r) {
		t.Fatal("fixture without T28 squash must HardFail")
	}
	text := claudiapin.FormatHuman(r)
	if !strings.Contains(text, "claudia pin: version=v0.24.0") {
		t.Fatalf("format:\n%s", text)
	}
}

func TestCheckNoSiblingIsLoudNotFatal(t *testing.T) {
	jevons := t.TempDir()
	mod := "module github.com/marcelocantos/jevons\n\nrequire github.com/marcelocantos/claudia v0.24.0\n"
	if err := os.WriteFile(filepath.Join(jevons, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := claudiapin.Check(jevons)
	if err != nil {
		t.Fatal(err)
	}
	if r.Loud == "" {
		t.Fatal("expected loud note without sibling")
	}
	if claudiapin.HardFail(r) {
		t.Fatal("no-sibling must not HardFail on required ancestry")
	}
}
