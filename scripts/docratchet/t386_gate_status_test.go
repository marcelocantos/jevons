// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestGateStatusDoctrineMarkers ratchets 🎯T386 / 🎯T396: a worker is told how
// to run a gate so the status survives, concretely enough to follow.
//
// The two facts a worker must carry away are the pipeline rule (the status
// belongs to the LAST command) and the runner that removes the pipeline. The
// zsh trap is here because it is the half a worker cannot deduce: this harness
// runs zsh, bash's PIPESTATUS does not exist there, and the careful worker who
// reaches for it prints an empty status while believing they read a real one.
func TestGateStatusDoctrineMarkers(t *testing.T) {
	for _, doc := range []struct {
		name string
		need []string
	}{
		{"internal/config/persona.md", []string{
			"🎯T386", "🎯T396",
			"bin/gate",
			"PIPESTATUS",
			"pipestatus",
			"exit=unknown",
			"SUSPECT",
			"bin/gate last",
			"FALSE-GREEN",
		}},
		{"AGENTS.md", []string{
			"🎯T386", "🎯T396",
			"bin/gate",
			"PIPESTATUS",
			"pipestatus",
			"exit=unknown",
			"SUSPECT",
			"bin/gate last",
			"FALSE-GREEN",
		}},
		{"agents-guide.md", []string{
			"🎯T386", "🎯T396",
			"bin/gate",
			"PIPESTATUS",
			"pipestatus",
			"exit=unknown",
			"SUSPECT",
			"bin/gate last",
			"FALSE-GREEN",
		}},
		{"internal/mcpserver/fleet_brief.go", []string{
			"🎯T386", "🎯T396",
			"bin/gate",
			"PIPESTATUS",
			"pipestatus",
			"exit=unknown",
			"SUSPECT",
			"bin/gate last",
			"FALSE-GREEN",
		}},
	} {
		body := readRepo(t, doc.name)
		for _, m := range doc.need {
			if !strings.Contains(body, m) {
				t.Errorf("%s missing 🎯T386/🎯T396 doctrine marker %q", doc.name, m)
			}
		}
	}
}

// TestBullseyeGateRunsUnderTheGateRunner ratchets the wiring rather than the
// prose. An oracle is a loop, not an artifact: the doctrine tells workers to
// run gates through bin/gate, and the repo's own standing invariant recipe is
// the first place that has to obey it.
//
// The sibling incident is the reason. claudia's sanctioned `make bullseye` ran
//
//	go test -race ./... | tail -n 5 && echo "✓ tests"
//
// and printed the tick over a suite that had failed — the recipe that certifies
// the repo was itself the false green. This test fails if this repo's recipe
// drifts back to a bare `go test` whose status a later pipe could mask.
func TestBullseyeGateRunsUnderTheGateRunner(t *testing.T) {
	mk := readRepo(t, "Makefile")

	i := strings.Index(mk, "\nbullseye:")
	if i < 0 {
		t.Fatal("Makefile has no bullseye target")
	}
	recipe := mk[i:]
	if end := strings.Index(recipe[1:], "\n.PHONY"); end >= 0 {
		recipe = recipe[:end+1]
	}

	if !strings.Contains(recipe, "bin/gate") {
		t.Errorf("make bullseye does not run its test step under bin/gate (🎯T386):\n%s", recipe)
	}
	for _, banned := range []string{"| tail", "| head", "| grep"} {
		if strings.Contains(recipe, banned) {
			t.Errorf("make bullseye pipes a gate through %q — the status becomes that command's (🎯T386):\n%s",
				banned, recipe)
		}
	}
}
