// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpattach

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1239: a stdio Playwright MCP must not resolve its workspace root
// (and therefore screenshot output) against the seat's own working
// directory, which for every Build seat is a git checkout.
func TestT1239EnsurePlaywrightIsolatedMovesCwdOutOfWorkDir(t *testing.T) {
	workDir := t.TempDir() // stands in for a seat's git checkout
	list := []claudia.MCPServer{{
		Name:    "playwright",
		Type:    "stdio",
		Command: "npx",
		Args:    []string{"@playwright/mcp@latest"},
	}}

	out := ensurePlaywrightIsolated(list, workDir)
	if len(out) != 1 {
		t.Fatalf("want 1 server, got %+v", out)
	}
	pw := out[0]
	if pw.Command != "sh" {
		t.Fatalf("want wrapped command sh, got %q (args %v)", pw.Command, pw.Args)
	}
	scratch := playwrightScratchDir(workDir)
	if strings.HasPrefix(scratch, workDir) {
		t.Fatalf("scratch dir %q must not be inside the seat's workDir %q", scratch, workDir)
	}
	joined := strings.Join(pw.Args, "\x00")
	if !strings.Contains(joined, scratch) {
		t.Fatalf("wrapped args do not carry the scratch dir: %v (want %q)", pw.Args, scratch)
	}
	if !strings.Contains(joined, "npx") {
		t.Fatalf("wrapped args lost the original command: %v", pw.Args)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("scratch dir was not created: %v", err)
	}
}

// 🎯T1239: two seats with different workDirs land on two different
// scratch dirs, so one seat's screenshots cannot collide with another's
// and neither lands in a checkout.
func TestT1239ScratchDirDistinctPerWorkDir(t *testing.T) {
	a := playwrightScratchDir("/Users/marcelo/work/a")
	b := playwrightScratchDir("/Users/marcelo/work/b")
	if a == b {
		t.Fatalf("distinct workDirs must mint distinct scratch dirs: %q", a)
	}
	if playwrightScratchDir("/Users/marcelo/work/a") != a {
		t.Fatalf("scratch dir must be deterministic for the same workDir")
	}
	for _, dir := range []string{a, b} {
		if strings.HasPrefix(dir, "/Users/marcelo/work/") {
			t.Fatalf("scratch dir %q must not sit inside a workDir-rooted path", dir)
		}
	}
}

// 🎯T1239: re-running ensurePlaywrightIsolated on an already-moved server
// must not nest another "sh -c" layer around the previous one.
func TestT1239EnsurePlaywrightIsolatedIdempotentAcrossRewrap(t *testing.T) {
	workDir := t.TempDir()
	list := []claudia.MCPServer{{
		Name: "playwright", Type: "stdio", Command: "npx",
		Args: []string{"@playwright/mcp@latest"},
	}}
	once := ensurePlaywrightIsolated(list, workDir)
	twice := ensurePlaywrightIsolated(once, workDir)
	if !ServersEqual(once, twice) {
		t.Fatalf("rewrap must be idempotent: once=%+v twice=%+v", once, twice)
	}
	if strings.Count(strings.Join(twice[0].Args, " "), "sh -c") > 0 {
		// The inner argv has no literal "sh -c" substring (it's split
		// across argv elements), but a nested wrap would duplicate the
		// scratch dir path twice in the joined args; guard that too.
	}
	scratch := playwrightScratchDir(workDir)
	if n := strings.Count(strings.Join(twice[0].Args, "\x00"), scratch); n != 1 {
		t.Fatalf("nested rewrap duplicated the scratch dir %d times: %v", n, twice[0].Args)
	}
}

// 🎯T1239 acceptance (hermetic): a seat's screenshot call with a relative
// filename resolves under the seat scratch dir, not the process's own
// launch cwd. This drives the wrapped command/args through a real shell
// (the same mechanism claudia uses to exec a stdio MCP) with a stand-in
// "playwright" that just resolves and reports a relative path the way
// Playwright MCP's workspace-root rule does: against its own cwd.
func TestT1239WrappedCommandResolvesRelativePathUnderScratch(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	workDir := t.TempDir()
	scratchDir := playwrightScratchDir(workDir)
	if err := os.MkdirAll(scratchDir, 0o755); err != nil {
		t.Fatalf("mkdir scratch: %v", err)
	}
	// A stand-in "screenshot" tool: print the absolute path a relative
	// filename resolves to from its own cwd, mirroring Playwright MCP's
	// "explicit names are resolved against the workspace root" rule.
	command, args := wrapCommandWithCwd("pwd", nil, scratchDir)
	out, err := exec.Command(command, args...).Output()
	if err != nil {
		t.Fatalf("wrapped pwd failed: %v", err)
	}
	got := strings.TrimSpace(string(out))
	want := scratchDir
	// Resolve symlinks (e.g. /tmp -> /private/tmp on macOS) before
	// comparing, since pwd reports the resolved path.
	wantResolved, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("eval symlinks on scratch dir: %v", err)
	}
	if got != wantResolved && got != want {
		t.Fatalf("relative-path resolution landed at %q, want %q (or resolved %q)", got, want, wantResolved)
	}
	if strings.HasPrefix(got, workDir) {
		t.Fatalf("resolution landed inside the seat workDir: %q", got)
	}
}
