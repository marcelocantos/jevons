// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T818: react_paint.js shipped with a duplicate `const hostBox` and every
// react-paint journey died at script load. Nothing parsed the node scripts
// the journeys and UI tests load, so it went unnoticed. This ratchet runs
// `node --check` over every .js/.cjs/.mjs under scripts/.
package docratchet_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// nodeCheckFailures returns "path: first error line" for each script under
// dir that fails `node --check`.
func nodeCheckFailures(t *testing.T, dir string) []string {
	t.Helper()
	var failures []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".js", ".cjs", ".mjs":
		default:
			return nil
		}
		out, err := exec.Command("node", "--check", path).CombinedOutput()
		if err != nil {
			msg := "node --check failed"
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "Error") {
					msg = strings.TrimSpace(line)
					break
				}
			}
			failures = append(failures, path+": "+msg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return failures
}

func TestT818NodeScriptsParse(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}
	root := repoRoot(t)
	for _, f := range nodeCheckFailures(t, filepath.Join(root, "scripts")) {
		t.Error(f)
	}
}

// TestT818NodeCheckControl proves the walker catches the T818 defect class.
func TestT818NodeCheckControl(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}
	dir := t.TempDir()
	broken := "const hostBox = 1;\nconst hostBox = 2;\n"
	if err := os.WriteFile(filepath.Join(dir, "broken.js"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fine.cjs"), []byte("const a = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := nodeCheckFailures(t, dir)
	if len(got) != 1 || !strings.Contains(got[0], "broken.js") || !strings.Contains(got[0], "hostBox") {
		t.Fatalf("control: want exactly broken.js flagged naming hostBox, got %v", got)
	}
}
