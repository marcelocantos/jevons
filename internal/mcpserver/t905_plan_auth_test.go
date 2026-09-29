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
	refuse := func(name string) {
		s.idleActivity.NoteTerminalTurn(name, revoked, 0)
		s.idleActivity.NoteTurnErrored(name, true)
	}
	refuse("po")
	refuse("other-po")
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
	refuse("po")
	if !s.PlanAuthFailed("po") {
		t.Fatal("a refusal after the repair did not latch again")
	}
}

// 🎯T945: a seat whose reply discusses a 401 answered on a working login.
// Reading that prose as a refusal offered the owner a sign-in for a plan the
// broker called healthy.
func TestT945ReplyAboutAuthIsNotARefusal(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	s.idleActivity = &IdleActivityTracker{}
	reply := "Continuing to monitor. jevons-po's last message to me was interrupted by a 401 " +
		"before it could report back, so I re-read the report and verified the commits " +
		"against the ledger. Fleet is now fully healthy: 7 running seats."
	s.idleActivity.NoteTerminalTurn("po", reply, 2)
	s.idleActivity.NoteTurnErrored("po", false)
	if s.PlanAuthFailed("po") {
		t.Fatal("a substantive reply mentioning a 401 reads as a login refusal")
	}
}
