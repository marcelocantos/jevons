// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "testing"

// 🎯T905: a login refusal latches per seat and is forgotten once the owner
// repaired the plan; another seat's refusal is left alone.
func TestT905PlanAuthFailureLatchesAndClears(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.idleActivity = &IdleActivityTracker{}
	revoked := `provider refused the turn: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth access token has been revoked."}}`
	s.idleActivity.NoteTerminalTurn("po", revoked, 0)
	s.idleActivity.NoteTerminalTurn("other-po", revoked, 0)
	s.idleActivity.NoteTerminalTurn("worker", "done: all green", 3)
	if !s.PlanAuthFailed("po") || s.PlanAuthFailed("worker") {
		t.Fatalf("po=%v worker=%v", s.PlanAuthFailed("po"), s.PlanAuthFailed("worker"))
	}
	s.ClearPlanAuthFailures([]string{"po", "worker"})
	if s.PlanAuthFailed("po") {
		t.Fatal("the repaired seat still reads refused")
	}
	if !s.PlanAuthFailed("other-po") {
		t.Fatal("a seat not named was cleared")
	}
	s.idleActivity.NoteTerminalTurn("po", revoked, 0)
	if !s.PlanAuthFailed("po") {
		t.Fatal("a refusal after the repair did not latch again")
	}
}
