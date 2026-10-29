// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package claudetrust_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/claudetrust"
)

// ~/.claude.json as Claude Code actually keeps it: far more than jevons
// models, including per-project history this package must not lose
// (🎯T376 — the file is hot shared state, and a typed round-trip drops
// whatever this build does not know about).
const incidentConfig = `{
  "numStartups": 412,
  "mcpServers": {"blurter": {"type": "stdio", "command": "blurter-mcp"}},
  "projects": {
    "/Users/marcelo/work/github.com/marcelocantos/jevons": {
      "hasTrustDialogAccepted": true,
      "history": [{"display": "make test-go"}]
    },
    "/Users/marcelo/work/github.com/marcelocantos/squz": {
      "history": [{"display": "ls"}],
      "lastCost": 0.42
    }
  }
}`

const geWorkdir = "/Users/marcelo/work/github.com/marcelocantos/squz"

var ownerRoots = []string{"/Users/marcelo/work"}

// 🎯T709: the workdir ge-po was minted on had a projects entry but no
// answer to the trust question — which is exactly the state that let the
// modal appear. A missing key is not a yes.
func TestAcceptedReadsTheAnswerNotThePresenceOfAnEntry(t *testing.T) {
	t.Parallel()
	if !claudetrust.Accepted([]byte(incidentConfig), "/Users/marcelo/work/github.com/marcelocantos/jevons") {
		t.Fatal("an entry with hasTrustDialogAccepted=true must read as trusted")
	}
	if claudetrust.Accepted([]byte(incidentConfig), geWorkdir) {
		t.Fatal("an entry WITHOUT the key must not read as trusted")
	}
	if claudetrust.Accepted([]byte(incidentConfig), "/Users/marcelo/work/nowhere") {
		t.Fatal("an absent entry must not read as trusted")
	}
	if claudetrust.Accepted([]byte("{ not json"), geWorkdir) {
		t.Fatal("an unreadable config is not an answer")
	}
}

// The write authors one boolean and leaves everything else byte-intact:
// the other project's cost and history, this project's history, and the
// top-level members including mcpServers, which 🎯T464 says jevons must
// not be editing.
func TestSetAcceptedPreservesEverythingElse(t *testing.T) {
	t.Parallel()
	out, err := claudetrust.SetAccepted([]byte(incidentConfig), geWorkdir)
	if err != nil {
		t.Fatalf("SetAccepted: %v", err)
	}
	if !claudetrust.Accepted(out, geWorkdir) {
		t.Fatal("trust must be recorded for the workdir")
	}

	var got, want map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(incidentConfig), &want); err != nil {
		t.Fatal(err)
	}
	if got["numStartups"] != want["numStartups"] {
		t.Errorf("numStartups changed: %v", got["numStartups"])
	}
	if !jsonEqual(t, got["mcpServers"], want["mcpServers"]) {
		t.Errorf("mcpServers must be untouched (🎯T464): %v", got["mcpServers"])
	}

	projects := got["projects"].(map[string]any)
	ge := projects[geWorkdir].(map[string]any)
	if ge["lastCost"] != 0.42 {
		t.Errorf("sibling members of the edited project were dropped: %v", ge)
	}
	if !jsonEqual(t, ge["history"], want["projects"].(map[string]any)[geWorkdir].(map[string]any)["history"]) {
		t.Errorf("project history was dropped: %v", ge["history"])
	}
	other := projects["/Users/marcelo/work/github.com/marcelocantos/jevons"]
	if !jsonEqual(t, other, want["projects"].(map[string]any)["/Users/marcelo/work/github.com/marcelocantos/jevons"]) {
		t.Errorf("an unrelated project was rewritten: %v", other)
	}
}

// A machine with no config yet gets a well-formed one, not a parse error
// that skips the write and leaves the modal in place.
func TestSetAcceptedOnEmptyDocument(t *testing.T) {
	t.Parallel()
	for _, doc := range []string{"", "   \n", "{}"} {
		out, err := claudetrust.SetAccepted([]byte(doc), geWorkdir)
		if err != nil {
			t.Fatalf("SetAccepted(%q): %v", doc, err)
		}
		if !claudetrust.Accepted(out, geWorkdir) {
			t.Errorf("SetAccepted(%q) did not record trust: %s", doc, out)
		}
	}
	if _, err := claudetrust.SetAccepted([]byte("{}"), "relative/path"); err == nil {
		t.Fatal("a relative workdir must be refused, not written under a bare key")
	}
}

// Trust is a safety boundary: separator-aware so ~/workshop is not read
// as being under ~/work, and a question it cannot resolve answers no.
func TestWithinOwnerRoots(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"/Users/marcelo/work":                  true,
		"/Users/marcelo/work/github.com/x/y":   true,
		"/Users/marcelo/work/":                 true,
		"/Users/marcelo/workshop/github.com/x": false,
		"/Users/marcelo/Downloads/cloned-repo": false,
		"relative/path":                        false,
		"":                                     false,
	}
	for dir, want := range cases {
		if got := claudetrust.WithinOwnerRoots(dir, ownerRoots); got != want {
			t.Errorf("WithinOwnerRoots(%q)=%v want %v", dir, got, want)
		}
	}
	if claudetrust.WithinOwnerRoots("/Users/marcelo/work/x", nil) {
		t.Error("no roots means no automatic yes")
	}
}

// The product path end to end, against a real file: grant once, no-op on
// the second launch, and the file left readable by Claude Code.
func TestEnsureAcceptedGrantsThenIsIdempotent(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte(incidentConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := claudetrust.EnsureAccepted(path, geWorkdir, ownerRoots)
	if err != nil || out != claudetrust.OutcomeGranted {
		t.Fatalf("first call: outcome=%q err=%v want granted", out, err)
	}
	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !claudetrust.Accepted(doc, geWorkdir) {
		t.Fatalf("the written file does not answer the question: %s", doc)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode not preserved across the atomic rename: %v %v", info, err)
	}

	out, err = claudetrust.EnsureAccepted(path, geWorkdir, ownerRoots)
	if err != nil || out != claudetrust.OutcomeAlready {
		t.Fatalf("second call: outcome=%q err=%v want already_trusted", out, err)
	}
}

// Outside the owner roots jevons does not say yes on the owner's behalf.
// It refuses without error — the launch still proceeds, and if the modal
// appears, agenterr.ClassWorkspaceTrust names the owner action.
func TestEnsureAcceptedRefusesOutsideOwnerRoots(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte(incidentConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	stranger := "/Users/marcelo/Downloads/someone-elses-repo"
	out, err := claudetrust.EnsureAccepted(path, stranger, ownerRoots)
	if err != nil || out != claudetrust.OutcomeRefused {
		t.Fatalf("outcome=%q err=%v want refused_outside_owner_roots", out, err)
	}
	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(doc) != incidentConfig {
		t.Fatalf("a refusal must not touch the file:\n%s", doc)
	}
}

// No config file yet: the write creates one rather than reporting an
// error and leaving the seat to meet the modal.
func TestEnsureAcceptedCreatesMissingConfig(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".claude.json")
	out, err := claudetrust.EnsureAccepted(path, geWorkdir, ownerRoots)
	if err != nil || out != claudetrust.OutcomeGranted {
		t.Fatalf("outcome=%q err=%v want granted", out, err)
	}
	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !claudetrust.Accepted(doc, geWorkdir) {
		t.Fatalf("created config does not record trust: %s", doc)
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	x, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	y, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(x) == string(y)
}
