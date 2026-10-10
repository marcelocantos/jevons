package mcpserver

import (
	"os"
	"strings"
	"testing"
)

func TestFleetBriefQualifiedForeignTargets(t *testing.T) {
	out, injected := EnsureFleetBrief(map[string]bool{}, "worker", "work on a target")
	if !injected {
		t.Fatal("brief not injected")
	}
	for _, phrase := range []string{"claudia/🎯T177", "bare '🎯T177'", "workdir", "other repository MUST"} {
		if !strings.Contains(out, phrase) {
			t.Fatalf("injected brief lacks %q", phrase)
		}
	}
}

func TestOverseerPersonaQualifiedForeignTargets(t *testing.T) {
	data, err := os.ReadFile("../config/persona.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{"claudia/T177", "own ledger", "workdir"} {
		if !strings.Contains(string(data), phrase) {
			t.Fatalf("persona lacks %q", phrase)
		}
	}
}
