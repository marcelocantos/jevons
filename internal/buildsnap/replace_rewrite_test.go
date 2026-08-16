// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package buildsnap

// 🎯T473 — the rewrite itself, over the go.mod forms the end-to-end fixture
// does not reach. An over-broad rewrite is the real risk here: this code edits
// the file that decides what the daemon is built from, so everything it is NOT
// supposed to touch is pinned as tightly as the one thing it is.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteLocalReplaces(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"claudia", "pigeon"} {
		if err := os.MkdirAll(filepath.Join(filepath.Dir(root), filepath.Base(root)+"-sib", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sibs := filepath.Base(root) + "-sib"
	claudia := filepath.Join(filepath.Dir(root), sibs, "claudia")
	pigeon := filepath.Join(filepath.Dir(root), sibs, "pigeon")

	body := "module example.com/root\n" +
		"\n" +
		"require example.com/other v1.2.3\n" +
		"\n" +
		"// a comment mentioning replace ../nothing\n" +
		"replace example.com/claudia => ../" + sibs + "/claudia\n" +
		"\n" +
		"replace (\n" +
		"\texample.com/pigeon => ../" + sibs + "/pigeon // keep this comment\n" +
		"\texample.com/pinned v1.0.0 => example.com/fork v1.1.0\n" +
		"\texample.com/already => " + claudia + "\n" +
		")\n"

	got, changes, err := rewriteLocalReplaces(body, root)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("rewrote %d directives, want the 2 relative ones: %+v", len(changes), changes)
	}

	// The two relative paths now say what the clone's root meant.
	for _, want := range []string{
		"replace example.com/claudia => " + claudia + "\n",
		"\texample.com/pigeon => " + pigeon + " // keep this comment\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewritten go.mod is missing %q:\n%s", want, got)
		}
	}
	// Everything else is byte-identical: a version replacement, an already
	// absolute path, a require, and a comment that merely says "replace".
	for _, want := range []string{
		"require example.com/other v1.2.3\n",
		"// a comment mentioning replace ../nothing\n",
		"\texample.com/pinned v1.0.0 => example.com/fork v1.1.0\n",
		"\texample.com/already => " + claudia + "\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rewrite disturbed a line it must leave alone, wanted %q:\n%s", want, got)
		}
	}
}

// TestRewriteLeavesAPlainModuleAlone: no local replacement, no change and no
// write — the ordinary repo must not even have its go.mod opened for writing.
func TestRewriteLeavesAPlainModuleAlone(t *testing.T) {
	body := "module example.com/root\n\ngo 1.26\n\nrequire example.com/other v1.2.3\n"
	got, changes, err := rewriteLocalReplaces(body, t.TempDir())
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if len(changes) != 0 || got != body {
		t.Errorf("a module with no local replace was rewritten: changes=%+v\n%s", changes, got)
	}
}

// TestRewriteNamesAMissingReplacement is the audible half at the unit level:
// the message must carry the module, the path as written, the absolute path it
// resolved to, and the instruction not to symlink it next to the snapshot —
// which is precisely the host state 🎯T473 exists to stop depending on.
func TestRewriteNamesAMissingReplacement(t *testing.T) {
	root := t.TempDir()
	_, _, err := rewriteLocalReplaces("replace example.com/claudia => ../claudia\n", root)
	if err == nil {
		t.Fatal("a replacement that is not on disk rewrote cleanly")
	}
	abs := filepath.Join(filepath.Dir(root), "claudia")
	for _, want := range []string{"example.com/claudia", "../claudia", abs, "symlink"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}
