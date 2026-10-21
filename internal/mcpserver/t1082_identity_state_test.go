// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
)

// 🎯T1082: an isolate worker reads its identity header on every brief. On a
// daemon whose state is not ~/.jevons the header names that state and
// forbids the development one; on the development daemon it says nothing.
func TestT1082IdentityHeaderNamesAnIsolateStateDir(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "jv-t1082-w", WorkDir: dir, SessionID: "s-t1082", Purpose: "work", Provider: claudia.ProviderGrok}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}

	s.SetDaemonStateDir(dir)
	head := s.identityHeaderFor("jv-t1082-w")
	want := "- STATE DIR: " + dir + " — this daemon's own state."
	if !strings.Contains(head, want) || !strings.Contains(head, "never read, list, open, query or write anything under ~/.jevons") {
		t.Fatalf("isolate header lacks its state dir:\n%s", head)
	}

	s.SetDaemonStateDir(config.Default().StateDir)
	if head := s.identityHeaderFor("jv-t1082-w"); strings.Contains(head, "STATE DIR") {
		t.Fatalf("development header names a state dir:\n%s", head)
	}
	s.SetDaemonStateDir("")
	if head := s.identityHeaderFor("jv-t1082-w"); strings.Contains(head, "STATE DIR") || !strings.Contains(head, "- NAME: jv-t1082-w") {
		t.Fatalf("unset state dir header:\n%s", head)
	}
}
