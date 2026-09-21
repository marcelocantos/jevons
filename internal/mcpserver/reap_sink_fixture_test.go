// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// reapBareDoneNextTurn is a bare done whose "next turn" names no remaining
// work: it must still read as a finished-work report.
const reapBareDoneNextTurn = "Done. Ready for the next turn."

// reapSinkServer is the shared fixture for tests that drive a terminal
// report through the real agentEventSink reap path: one registered work
// agent under jevons-po, and no overseer seam.
func reapSinkServer(t *testing.T, name string) (*Server, *claudia.Registry) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-reap-sink",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok", TargetID: "T165",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		registry:    reg,
		notifyJevon: func(string) {}, // hermetic: no overseer seam
	}
	return s, reg
}
