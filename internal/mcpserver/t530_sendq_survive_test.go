// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// 🎯T530 — parent kill must not abandon held sendq on descendants.
//
// Incident shape: reminting jevons-po killed mid-drain seats and stamped them
// reaped again while sendq still held gate feedback, regenerating reaped_held.
// The product path refuses leaf kill of a recovery seat still holding sendq,
// and on parent kill restarts held descendants for drain under the survivor.

func t530Server(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	s.SetFleetIntentStore(store)
	s.SetSendQueueDir(dir)
	s.SetRemovalAccount(fleetlog.New(nil))
	up := &upward{}
	s.SetOverseerDeliver(up.deliver)
	return s, dir
}

func t530RegisterTree(t *testing.T, s *Server, dir string) {
	t.Helper()
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer, Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Provider: "grok"},
		{Name: "jv-t530-drain", WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: "jevons-po", Provider: "grok", TargetID: "T530"},
	} {
		if err := s.registry.Register(d); err != nil {
			t.Fatal(err)
		}
	}
}

func TestT530ClassifyKillHeldSendqRefusesLeaf(t *testing.T) {
	t.Parallel()
	depth := map[string]int{"jv-t530-drain": 2}
	refuse, reason, restart := ClassifyKillHeldSendq("jv-t530-drain", nil, func(n string) int { return depth[n] })
	if !refuse {
		t.Fatal("want refuse kill of recovery seat with held sendq")
	}
	if !strings.Contains(reason, "DRAINED/EMPTY") || !strings.Contains(reason, "T530") {
		t.Fatalf("reason=%q", reason)
	}
	if len(restart) != 0 {
		t.Fatalf("restart=%v want empty on refuse", restart)
	}
}

func TestT530ClassifyKillHeldSendqRestartsDescendants(t *testing.T) {
	t.Parallel()
	depth := map[string]int{"jv-t530-drain": 3, "jv-other": 1}
	refuse, reason, restart := ClassifyKillHeldSendq(
		"jevons-po",
		[]string{"jv-t530-drain", "jv-clean", "jv-other"},
		func(n string) int { return depth[n] },
	)
	if refuse {
		t.Fatalf("parent kill must be allowed, got refuse %q", reason)
	}
	if len(restart) != 2 {
		t.Fatalf("restart=%v want two held descendants", restart)
	}
	joined := strings.Join(restart, ",")
	if !strings.Contains(joined, "jv-t530-drain") || !strings.Contains(joined, "jv-other") {
		t.Fatalf("restart=%v", restart)
	}
	if strings.Contains(joined, "jv-clean") {
		t.Fatalf("empty-sendq descendant must not restart: %v", restart)
	}
}

func TestT530ClassifyKillHeldSendqAllowsParentWithOwnQueue(t *testing.T) {
	t.Parallel()
	depth := map[string]int{"jevons-po": 4, "jv-t530-drain": 2}
	refuse, reason, restart := ClassifyKillHeldSendq(
		"jevons-po",
		[]string{"jv-t530-drain"},
		func(n string) int { return depth[n] },
	)
	if refuse {
		t.Fatalf("unworkable-PO remint must be allowed even when the PO has sendq; got %q", reason)
	}
	if len(restart) != 1 || restart[0] != "jv-t530-drain" {
		t.Fatalf("restart=%v want [jv-t530-drain]", restart)
	}
}

func TestT530ReapedHeldOlderThanGrace(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 19, 21, 0, 0, 0, time.UTC)
	backlogs := []struct {
		Agent  string
		Oldest time.Time
	}{
		{Agent: "stale", Oldest: now.Add(-10 * time.Minute)},
		{Agent: "fresh", Oldest: now.Add(-30 * time.Second)},
		{Agent: "recovering", Oldest: now.Add(-10 * time.Minute)},
	}
	restarted := map[string]time.Time{
		"recovering": now.Add(-30 * time.Second),
	}
	got := ReapedHeldOlderThanGrace(backlogs, restarted, now, RemintGraceWindow)
	if len(got) != 1 || got[0] != "stale" {
		t.Fatalf("got=%v want [stale]", got)
	}
}

// Clause: plant held sendq on a child, kill the parent → child is re-registered
// under the surviving parent, sendq depth survives, and reaped_held is not the
// sweep outcome for that name inside the remint grace window.
func TestT530ParentKillRestartsHeldSendqForDrain(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)
	// Hermetic: refuse provider Launch so the oracle measures registry +
	// sendq survival, not a live grok mint.
	s.drainLaunch = func(string) (*claudia.Agent, error) {
		return nil, fmt.Errorf("hermetic: drain launch deferred")
	}

	const child = "jv-t530-drain"
	if _, err := s.enqueueAgentSend(child, "gate: master red on your SHA — fix"); err != nil {
		t.Fatal(err)
	}
	if depth := s.pendingAgentSends(child); depth != 1 {
		t.Fatalf("planted depth=%d want 1", depth)
	}

	req := mcp.CallToolRequest{}
	// 🎯T560: T530 drain restart applies to seats that leave — the explicit subtree kill.
	req.Params.Arguments = map[string]any{"name": "jevons-po", "actor": "jevons", "subtree": true}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("parent kill refused: %s", toolText(res))
	}
	body := toolText(res)
	if !strings.Contains(body, "T530") || !strings.Contains(body, child) {
		t.Fatalf("kill reply missing drain restart notice:\n%s", body)
	}
	if s.registry.Def("jevons-po") != nil {
		t.Fatal("jevons-po should be gone after kill")
	}
	def := s.registry.Def(child)
	if def == nil {
		t.Fatal("held-sendq child must be re-registered for drain")
	}
	if def.Parent != "jevons" {
		t.Fatalf("child parent=%q want jevons (surviving parent)", def.Parent)
	}
	if depth := s.pendingAgentSends(child); depth != 1 {
		t.Fatalf("sendq depth after parent kill=%d; want 1 held (not abandoned)", depth)
	}
	at := s.drainRestartTimes()
	if _, ok := at[child]; !ok {
		t.Fatal("drainRestartAt missing for restarted child")
	}

	// Live-probe half: aged held backlog mid-recovery must not count as a
	// regenerated reaped_held older than RemintGraceWindow.
	if !s.suppressHeldReapedDuringDrainRestart(child, time.Now()) {
		t.Fatal("mid-recovery must suppress reaped_held regeneration")
	}
	older := ReapedHeldOlderThanGrace([]struct {
		Agent  string
		Oldest time.Time
	}{
		{Agent: child, Oldest: time.Now().Add(-10 * time.Minute)},
	}, s.drainRestartTimes(), time.Now(), RemintGraceWindow)
	if len(older) != 0 {
		t.Fatalf("live probe must see no reaped_held older than grace during recovery; got %v", older)
	}
	if got := s.ProbeReapedHeldOlderThanGrace(time.Now()); len(got) != 0 {
		t.Fatalf("ProbeReapedHeldOlderThanGrace=%v want empty (child is registered for drain)", got)
	}

	// Sweep must not drop or reaped_held-abandon the held queue: seat is
	// registered again (stall notice OK; depth stays until a live drain).
	s.SweepSendBacklogs()
	if depth := s.pendingAgentSends(child); depth != 1 {
		t.Fatalf("after sweep depth=%d; want 1 still held for drain", depth)
	}
}

// Clause: kill of the recovery seat itself before DRAINED/EMPTY is refused.
func TestT530RefuseKillOfHeldRecoverySeat(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)

	const child = "jv-t530-drain"
	if _, err := s.enqueueAgentSend(child, "gate feedback still draining"); err != nil {
		t.Fatal(err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": child, "actor": "jevons-po"}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("want refuse kill of recovery seat with held sendq")
	}
	body := toolText(res)
	if !strings.Contains(body, "DRAINED/EMPTY") && !strings.Contains(body, "T530") {
		t.Fatalf("refuse message=%q", body)
	}
	if s.registry.Def(child) == nil {
		t.Fatal("recovery seat must remain registered after refused kill")
	}
	if depth := s.pendingAgentSends(child); depth != 1 {
		t.Fatalf("depth=%d; refuse must not abandon sendq", depth)
	}
}

// Control: parent kill with empty descendant sendq still deregisters the
// subtree (pre-T530 behaviour preserved when nothing is held).
func TestT530ParentKillWithoutHeldSendqStillDeregisters(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)

	req := mcp.CallToolRequest{}
	// 🎯T560: T530 drain restart applies to seats that leave — the explicit subtree kill.
	req.Params.Arguments = map[string]any{"name": "jevons-po", "actor": "jevons", "subtree": true}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("kill: %s", toolText(res))
	}
	if s.registry.Def("jevons-po") != nil || s.registry.Def("jv-t530-drain") != nil {
		t.Fatal("empty-sendq subtree must stay deregistered")
	}
	if strings.Contains(toolText(res), "restarted") {
		t.Fatalf("no held sendq → no restart notice: %s", toolText(res))
	}
}

// Clause: reminting an unworkable PO that itself still has sendq must not
// refuse the kill (the queue stays keyed by name for the remint start).
func TestT530ParentKillWithOwnSendqStillRestartsHeldChild(t *testing.T) {
	s, dir := t530Server(t)
	t530RegisterTree(t, s, dir)
	s.drainLaunch = func(string) (*claudia.Agent, error) {
		return nil, fmt.Errorf("hermetic: drain launch deferred")
	}

	const child = "jv-t530-drain"
	if _, err := s.enqueueAgentSend("jevons-po", "stalled PO queue"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.enqueueAgentSend(child, "gate feedback still draining"); err != nil {
		t.Fatal(err)
	}

	req := mcp.CallToolRequest{}
	// 🎯T560: T530 drain restart applies to seats that leave — the explicit subtree kill.
	req.Params.Arguments = map[string]any{"name": "jevons-po", "actor": "jevons", "subtree": true}
	res, err := s.handleAgentKill(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("parent remint refused because PO had sendq: %s", toolText(res))
	}
	if s.registry.Def(child) == nil {
		t.Fatal("held-sendq child must restart for drain")
	}
	if s.pendingAgentSends(child) != 1 {
		t.Fatalf("child depth=%d want 1", s.pendingAgentSends(child))
	}
	if s.pendingAgentSends("jevons-po") != 1 {
		t.Fatalf("PO sendq must survive keyed by name; depth=%d", s.pendingAgentSends("jevons-po"))
	}
}
