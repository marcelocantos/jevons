// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/claudiapin"
)

// TestT707PublishedPinResolvesWithoutSiblingCheckout is the load-bearing
// 🎯T707 ratchet: a tree that does not contain ../claudia must still
// resolve github.com/marcelocantos/claudia to the published pin in
// go.mod. A committed `replace => ../claudia` is the T693 false-green
// that made every bin/gate -clean RED with "replacement directory
// ../claudia does not exist". Development may still consume the sibling
// via ../go.work (🎯T448); that workspace is not the measured graph.
func TestT707PublishedPinResolvesWithoutSiblingCheckout(t *testing.T) {
	mod := readRepo(t, "go.mod")
	if hasLocalClaudiaReplace(mod) {
		t.Fatal("go.mod has a filesystem replace for github.com/marcelocantos/claudia; " +
			"bin/gate -clean checks out HEAD into a temp tree with no ../claudia sibling " +
			"(🎯T707). Development uses ../go.work, not a committed replace (🎯T448).")
	}
	pin := claudiapin.RequireVersion(mod)
	if pin == "" {
		t.Fatal("go.mod has no github.com/marcelocantos/claudia require")
	}

	dir := writeModCopy(t, mod, readRepo(t, "go.sum"))
	if _, err := os.Stat(filepath.Join(dir, "..", "claudia")); err == nil {
		t.Fatal("temp module parent unexpectedly contains claudia; the no-sibling claim is void")
	}

	out, err := goListClaudia(t, dir)
	if err != nil {
		t.Fatalf("GOWORK=off go list against a tree without ../claudia: %v\n%s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		t.Fatalf("GOWORK=off go list: %q", out)
	}
	got, resolved := fields[0], fields[1]
	if got != pin {
		t.Fatalf("resolved claudia %s, go.mod pin is %s", got, pin)
	}
	if !strings.Contains(resolved, "@"+pin) {
		t.Fatalf("Dir = %s, want module cache @%s (published pin, not a sibling checkout)", resolved, pin)
	}
}

// TestT707RequiredSiblingReplaceGoesRed is the mutation: restoring the
// T693 replace into an otherwise identical module graph makes go list
// fail with the exact -clean residual those T627 slices keep hitting.
func TestT707RequiredSiblingReplaceGoesRed(t *testing.T) {
	mod := readRepo(t, "go.mod")
	if hasLocalClaudiaReplace(mod) {
		t.Fatal("committed go.mod already requires the sibling; GREEN path is already RED")
	}
	mutated := strings.TrimRight(mod, "\n") + "\n\nreplace github.com/marcelocantos/claudia => ../claudia\n"
	dir := writeModCopy(t, mutated, readRepo(t, "go.sum"))

	out, err := goListClaudia(t, dir)
	if err == nil {
		t.Fatalf("mutation restored replace => ../claudia but go list succeeded:\n%s", out)
	}
	text := string(out)
	if !strings.Contains(text, "replacement directory") || !strings.Contains(text, "../claudia") {
		t.Fatalf("want replacement-directory ../claudia error, got (%v):\n%s", err, text)
	}
}

func writeModCopy(t *testing.T, mod, sum string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func goListClaudia(t *testing.T, dir string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", "{{.Module.Version}} {{.Dir}}", "github.com/marcelocantos/claudia")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	return cmd.CombinedOutput()
}

func hasLocalClaudiaReplace(mod string) bool {
	inBlock := false
	for _, line := range strings.Split(mod, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inBlock && trimmed == ")":
			inBlock = false
			continue
		case !inBlock && strings.HasPrefix(trimmed, "replace") && strings.HasSuffix(trimmed, "("):
			inBlock = true
			continue
		case !inBlock && !strings.HasPrefix(trimmed, "replace "):
			continue
		}
		if !strings.Contains(trimmed, claudiapin.ModulePath) {
			continue
		}
		arrow := strings.Index(trimmed, "=>")
		if arrow < 0 {
			continue
		}
		rest := strings.Fields(trimmed[arrow+2:])
		if len(rest) == 0 {
			continue
		}
		target := rest[0]
		if target == "." || target == ".." ||
			strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") ||
			strings.HasPrefix(target, "/") {
			return true
		}
	}
	return false
}
