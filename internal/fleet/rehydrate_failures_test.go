// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestRehydrateFailureSurvivesRestart(t *testing.T) {
	const name = "durable-seat"
	path := filepath.Join(t.TempDir(), "rehydrate-failures.json")
	t.Cleanup(func() {
		_ = UseRehydrateFailureFile("")
		rehydrateFailures.Delete(name)
	})
	if err := UseRehydrateFailureFile(path); err != nil {
		t.Fatal(err)
	}
	noteRehydrate(name, errors.New("cursor acp: connection closed waiting for authenticate"))

	// The next process starts with an empty map and reads the file.
	rehydrateFailures.Delete(name)
	if err := UseRehydrateFailureFile(path); err != nil {
		t.Fatal(err)
	}
	got := RehydrateHealth(claudia.AgentDef{Name: name})
	const want = "broken: cursor acp: connection closed waiting for authenticate"
	if got != want {
		t.Fatalf("after reload RehydrateHealth=%q, want %q", got, want)
	}

	noteRehydrate(name, nil)
	rehydrateFailures.Delete(name)
	if err := UseRehydrateFailureFile(path); err != nil {
		t.Fatal(err)
	}
	if got := RehydrateHealth(claudia.AgentDef{Name: name}); got != "resumable" {
		t.Fatalf("after a successful relaunch RehydrateHealth=%q, want resumable", got)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]string
	if err := json.Unmarshal(b, &all); err != nil {
		t.Fatal(err)
	}
	if _, ok := all[name]; ok {
		t.Fatalf("cleared seat still in file: %s", b)
	}
}
