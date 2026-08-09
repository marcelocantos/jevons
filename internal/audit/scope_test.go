// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyFile(t *testing.T) {
	const maxBytes = 1000
	cases := []struct {
		name string
		file string
		size int64
		want SkipReason
	}{
		{"go source", "internal/server/chat.go", 500, SkipNone},
		{"markdown prompt", "internal/config/persona.md", 500, SkipNone},
		{"skill definition", "skills/release/SKILL.md", 500, SkipNone},
		{"yaml config", "bullseye.yaml", 500, SkipNone},
		{"dotfile", ".env", 100, SkipHidden},
		{"lockfile", "go.sum", 100, SkipGenerated},
		{"minified", "web/vendor/lib.min.js", 100, SkipGenerated},
		{"generated go", "api/service.pb.go", 100, SkipGenerated},
		{"binary", "bin/jevonsd", 100, SkipNotAudited},
		{"image", "web/logo.png", 100, SkipNotAudited},
		{"empty", "internal/empty.go", 0, SkipEmpty},
		{"oversized", "internal/huge.go", maxBytes + 1, SkipTooLarge},
		{"at limit", "internal/big.go", maxBytes, SkipNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyFile(tc.file, tc.size, maxBytes); got != tc.want {
				t.Fatalf("ClassifyFile(%q, %d) = %q, want %q", tc.file, tc.size, got, tc.want)
			}
		})
	}
}

func TestSkipDirKeepsClaudeTree(t *testing.T) {
	// The skills and prompt surfaces live under .claude: skipping all
	// hidden directories would silently void two thirds of the audit.
	if SkipDir(".claude") {
		t.Fatal("SkipDir(.claude) = true, want false (skills/prompts surface)")
	}
	for _, name := range []string{".git", "node_modules", "vendor", "dist", ".venv", ".hidden"} {
		if !SkipDir(name) {
			t.Fatalf("SkipDir(%q) = false, want true", name)
		}
	}
	if SkipDir("internal") {
		t.Fatal("SkipDir(internal) = true, want false")
	}
}

func TestNormalizeScope(t *testing.T) {
	for in, want := range map[string]ScopeKind{
		"code": ScopeCode, "CODE": ScopeCode, " skills ": ScopeSkills,
		"prompts": ScopePrompts, "nonsense": "", "": "",
	} {
		if got := NormalizeScope(in); got != want {
			t.Fatalf("NormalizeScope(%q) = %q, want %q", in, got, want)
		}
	}
}

// writeFile creates a file with n bytes of content.
func writeFile(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = 'x'
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

// fixtureTree builds a repo-shaped tree covering all three scopes.
func fixtureTree(t *testing.T) (workdir, home string) {
	t.Helper()
	root := t.TempDir()
	workdir = filepath.Join(root, "repo")
	home = filepath.Join(root, "home")

	writeFile(t, filepath.Join(workdir, "internal", "server", "chat.go"), 200)
	writeFile(t, filepath.Join(workdir, "internal", "server", "server.go"), 300)
	writeFile(t, filepath.Join(workdir, "cmd", "jevonsd", "main.go"), 400)
	// Excluded: build output and third-party trees must not burn budget.
	writeFile(t, filepath.Join(workdir, "internal", "node_modules", "dep", "index.js"), 100)
	writeFile(t, filepath.Join(workdir, "internal", "server", "logo.png"), 100)

	writeFile(t, filepath.Join(workdir, "AGENTS.md"), 150)
	writeFile(t, filepath.Join(workdir, "internal", "config", "persona.md"), 250)
	writeFile(t, filepath.Join(home, ".claude", "AGENTS.md"), 180)

	writeFile(t, filepath.Join(home, ".claude", "skills", "release", "SKILL.md"), 120)
	writeFile(t, filepath.Join(home, ".claude", "skills", "push", "SKILL.md"), 130)
	return workdir, home
}

func TestBuildManifestCoversAllScopes(t *testing.T) {
	workdir, home := fixtureTree(t)
	cfg := DefaultConfig(workdir, home)
	man, err := BuildManifest(cfg, time.Unix(1000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}

	// Acceptance #1: a full-scan pass covers code, skills, and prompts.
	if !man.FullScan() {
		t.Fatalf("FullScan() = false, missing scopes: %v", man.MissingScopes())
	}
	for _, kind := range RequiredScopes {
		if !man.Scope(kind).Covered() {
			t.Fatalf("scope %s has no files", kind)
		}
	}

	code := man.Scope(ScopeCode)
	for _, f := range code.Files {
		if filepath.Ext(f.Path) == ".png" {
			t.Fatalf("binary leaked into manifest: %s", f.Path)
		}
		if containsDir(f.Path, "node_modules") {
			t.Fatalf("vendored tree leaked into manifest: %s", f.Path)
		}
	}
	if len(code.Files) != 3 {
		t.Fatalf("code scope files = %d, want 3: %v", len(code.Files), code.Files)
	}
	if got := man.Scope(ScopeSkills); len(got.Files) != 2 {
		t.Fatalf("skills scope files = %d, want 2", len(got.Files))
	}
	// Prompt roots are individual files plus the global brief.
	if got := man.Scope(ScopePrompts); len(got.Files) != 3 {
		t.Fatalf("prompts scope files = %d, want 3: %v", len(got.Files), got.Files)
	}
	if man.Truncated() {
		t.Fatal("manifest truncated unexpectedly")
	}
}

func TestBuildManifestIsDeterministic(t *testing.T) {
	workdir, home := fixtureTree(t)
	cfg := DefaultConfig(workdir, home)
	first, err := BuildManifest(cfg, time.Unix(1000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildManifest(cfg, time.Unix(2000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range first.Scopes {
		other := second.Scopes[i]
		if len(s.Files) != len(other.Files) {
			t.Fatalf("scope %s file count drift: %d vs %d", s.Kind, len(s.Files), len(other.Files))
		}
		for j := range s.Files {
			if s.Files[j].Path != other.Files[j].Path {
				t.Fatalf("scope %s order drift at %d: %s vs %s", s.Kind, j, s.Files[j].Path, other.Files[j].Path)
			}
		}
	}
}

func TestBuildManifestBoundsTruncate(t *testing.T) {
	workdir, home := fixtureTree(t)
	cfg := DefaultConfig(workdir, home)
	cfg.MaxFilesPerScope = 2
	man, err := BuildManifest(cfg, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	code := man.Scope(ScopeCode)
	if len(code.Files) != 2 {
		t.Fatalf("bounded code files = %d, want 2", len(code.Files))
	}
	if !code.Truncated || code.Omitted != 1 {
		t.Fatalf("truncation not recorded: truncated=%v omitted=%d", code.Truncated, code.Omitted)
	}
	if !man.Truncated() {
		t.Fatal("Manifest.Truncated() = false, want true")
	}

	// Byte bound clips too, and always keeps at least one file so a scope
	// with a single large file is never silently empty.
	cfg = DefaultConfig(workdir, home)
	cfg.MaxBytesPerScope = 250
	man, err = BuildManifest(cfg, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	code = man.Scope(ScopeCode)
	// Contract: stay under the budget, except that a scope always keeps at
	// least one file — a surface whose first file is oversized must still
	// be visible to the auditor rather than silently empty.
	if len(code.Files) == 0 {
		t.Fatal("byte bound emptied the code scope")
	}
	if code.TotalBytes > 250 && len(code.Files) != 1 {
		t.Fatalf("byte bound exceeded with %d files (%d bytes)", len(code.Files), code.TotalBytes)
	}
	if !code.Truncated {
		t.Fatal("byte-bounded scope not marked truncated")
	}
}

func TestBuildManifestMissingRootsAreDegradedNotFatal(t *testing.T) {
	workdir, home := fixtureTree(t)
	cfg := DefaultConfig(workdir, home)
	// A machine with no skills tree still gets a code+prompts audit, and
	// the gap is visible rather than silent.
	cfg.SkillsRoots = []string{filepath.Join(home, "nope", "skills")}
	man, err := BuildManifest(cfg, time.Time{})
	if err != nil {
		t.Fatalf("missing root should not be fatal: %v", err)
	}
	if man.FullScan() {
		t.Fatal("FullScan() = true with an absent skills tree")
	}
	missing := man.MissingScopes()
	if len(missing) != 1 || missing[0] != ScopeSkills {
		t.Fatalf("MissingScopes() = %v, want [skills]", missing)
	}
	if got := man.Scope(ScopeSkills).MissingRoots; len(got) != 1 {
		t.Fatalf("MissingRoots = %v, want one entry", got)
	}
	if !man.Scope(ScopeCode).Covered() {
		t.Fatal("code scope lost when skills root was absent")
	}
}

func containsDir(path, dir string) bool {
	return strings.Contains(filepath.ToSlash(path), "/"+dir+"/")
}
