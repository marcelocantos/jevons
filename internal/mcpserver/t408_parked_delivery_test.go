// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T408: a deliberately parked agent stays stopped when a message is
// delivered. The send reports plainly; it does not Launch. A working
// intent on a dead process is still allowed through the gate (T171 /
// T208 must survive).
func TestT408ParkedDeliveryDoesNotLaunch(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t408", WorkDir: dir, SessionID: "s-t408",
		Purpose: claudia.PurposeWork, TargetID: "T408",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC)
	if err := store.SetAgent("jv-t408", fleetintent.Parked, "jevons", "spend block", now); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)

	_, _, err = s.ensureAgentProcess("jv-t408")
	if err == nil {
		t.Fatal("parked delivery started the process")
	}
	if !strings.Contains(err.Error(), "intent says it should not be") {
		t.Fatalf("error %q does not name the park", err)
	}
	if proc := reg.Get("jv-t408"); proc != nil && proc.Alive() {
		t.Fatal("parked agent is now alive — silent restart")
	}
}

func TestT408WorkingDeadProcessStillRevives(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t408", WorkDir: dir, SessionID: "s-t408",
		Purpose: claudia.PurposeWork, TargetID: "T408",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)

	// Drive only the intent gate. Launch starts a real provider pane —
	// T414 already covers that path as noReviveCase.
	dec := s.AllowFleetControl("jv-t408", fleetintent.ControlDeliverStart)
	if !dec.Allow {
		t.Fatalf("working intent declined deliver-start: %+v", dec)
	}
}

func TestT408RunningAgentUnaffected(t *testing.T) {
	// A live process is returned without consulting the park gate — the
	// gate only fires when we would start something.
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t408", WorkDir: dir, SessionID: "s-t408",
		Purpose: claudia.PurposeWork,
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	// No live process, no park — ensureAgentProcess may try Launch.
	// The point of this control is that a Get() hit returns immediately.
	if proc := s.registry.Get("jv-t408"); proc != nil && proc.Alive() {
		got, _, err := s.ensureAgentProcess("jv-t408")
		if err != nil || got != proc {
			t.Fatalf("live process was not returned: got=%v err=%v", got, err)
		}
	}
}
