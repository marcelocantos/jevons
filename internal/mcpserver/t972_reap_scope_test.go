// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktreereap"
)

// 🎯T972 (split from 🎯T784): a finished-work report must not reap a seat
// that leaves real scope outstanding — a declared blocking wait on a
// tracked background gate, or uncommitted changes owned in the seat's own
// isolated worktree (🎯T254.2) that the report never mentioned. Unrelated
// dirty state in a shared clone (not this seat's own worktree) must not
// count as owned scope.

const t972PendingGateReport = "Done. The T972 fix is committed as abcdef0123456; go test ./internal/mcpserver -run T972 passed locally. Waiting on bin/gate to finish the clean run before closing out."

const t972PlainFinishReport = "Done. SHA abcdef0123456. go test ./internal/mcpserver -run T972 PASS"

func TestOutstandingScopeReasonsPendingGate(t *testing.T) {
	scope := OutstandingScopeReasons(t972PendingGateReport, nil, false)
	if len(scope) != 1 || scope[0].Kind != "pending_gate" {
		t.Fatalf("scope = %+v, want one pending_gate entry", scope)
	}
}

func TestOutstandingScopeReasonsCleanReportNoTree(t *testing.T) {
	if scope := OutstandingScopeReasons(t972PlainFinishReport, nil, false); len(scope) != 0 {
		t.Fatalf("scope = %+v, want none (no gate wait, no tree)", scope)
	}
}

func TestOutstandingScopeReasonsOwnedUncommitted(t *testing.T) {
	tree := &gate.TreeProvenance{
		Repo: "/seat/own/worktree", Commit: "abc123", Clean: false,
		DirtyFiles: 2, DirtySample: []string{"M internal/foo.go", "?? internal/bar.go"},
	}
	scope := OutstandingScopeReasons(t972PlainFinishReport, tree, true)
	if len(scope) != 1 || scope[0].Kind != "owned_uncommitted" {
		t.Fatalf("scope = %+v, want one owned_uncommitted entry", scope)
	}
	if !strings.Contains(scope[0].Detail, "2 uncommitted") {
		t.Fatalf("detail missing count: %q", scope[0].Detail)
	}
}

func TestOutstandingScopeReasonsNotOwnWorktreeIsNotCountedAsOwnedScope(t *testing.T) {
	// Same dirty tree as above, but ownWorktree=false — the workdir could
	// not be distinguished from a shared clone, so it is not this seat's
	// scope to answer for.
	tree := &gate.TreeProvenance{
		Repo: "/shared/clone", Commit: "abc123", Clean: false,
		DirtyFiles: 40, DirtySample: []string{"M unrelated/neighbour.go"},
	}
	if scope := OutstandingScopeReasons(t972PlainFinishReport, tree, false); len(scope) != 0 {
		t.Fatalf("scope = %+v, want none — unrelated shared-tree edits must not count as owned scope", scope)
	}
}

func TestOutstandingScopeReasonsCleanTreeIsNotScope(t *testing.T) {
	tree := &gate.TreeProvenance{Repo: "/seat/own/worktree", Commit: "abc123", Clean: true}
	if scope := OutstandingScopeReasons(t972PlainFinishReport, tree, true); len(scope) != 0 {
		t.Fatalf("scope = %+v, want none — a clean tree has nothing outstanding", scope)
	}
}

func TestOutstandingScopeReasonsBothKinds(t *testing.T) {
	tree := &gate.TreeProvenance{
		Repo: "/seat/own/worktree", Commit: "abc123", Clean: false,
		DirtyFiles: 1, DirtySample: []string{"M x.go"},
	}
	scope := OutstandingScopeReasons(t972PendingGateReport, tree, true)
	if len(scope) != 2 {
		t.Fatalf("scope = %+v, want both pending_gate and owned_uncommitted", scope)
	}
}

// probeOwnWorktree: a real linked git worktree vs the shared clone it was
// created from.

func t972SeededSharedClone(t *testing.T) string {
	t.Helper()
	shared := t.TempDir()
	gitInit(t, shared)
	gitRun(t, shared, "config", "user.email", "test@example.com")
	gitRun(t, shared, "config", "user.name", "Test")
	seed := filepath.Join(shared, "seed.go")
	if err := os.WriteFile(seed, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, shared, "add", "seed.go")
	gitRun(t, shared, "commit", "-m", "seed")
	return shared
}

func TestProbeOwnWorktreeDistinguishesLinkedWorktreeFromSharedClone(t *testing.T) {
	shared := t972SeededSharedClone(t)
	linked := filepath.Join(t.TempDir(), "linked-worktree")
	gitRun(t, shared, "worktree", "add", "--detach", linked, "HEAD")
	// 🎯T440: a killed test run skips t.Cleanup; the reaper needs the mark.
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: linked, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}

	if probeOwnWorktree(shared) {
		t.Fatal("the shared clone's own working tree must not read as a linked (owned) worktree")
	}
	if !probeOwnWorktree(linked) {
		t.Fatal("a linked worktree must read as the seat's own worktree")
	}
	if probeOwnWorktree(t.TempDir()) {
		t.Fatal("a directory outside any git repo must not read as an own worktree")
	}
}

// End-to-end: ShouldAutoReapDoneWorkAgent keeps a seat whose own linked
// worktree carries uncommitted changes, and still reaps when the same
// dirty state sits only in the shared clone the seat's workdir points at.

func t972RegisterAt(t *testing.T, name, workDir string) *claudia.Registry {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: workDir, SessionID: "s-w",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestShouldAutoReapDoneWorkAgentKeepsSeatWithOwnedUncommittedScope(t *testing.T) {
	shared := t972SeededSharedClone(t)
	linked := filepath.Join(t.TempDir(), "linked-worktree")
	gitRun(t, shared, "worktree", "add", "--detach", linked, "HEAD")
	// 🎯T440: a killed test run skips t.Cleanup; the reaper needs the mark.
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: linked, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}
	// Dirty the linked worktree (own scope), not the shared clone.
	if err := os.WriteFile(filepath.Join(linked, "pending.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := t972RegisterAt(t, "jv-t972-owned-scope", linked)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t972-owned-scope", t972PlainFinishReport, nil)
	if ok {
		t.Fatalf("reaped a seat with uncommitted changes owned in its own worktree (reason %s)", reason)
	}
	if !strings.HasPrefix(reason, outstandingScopeReapReasonPrefix) {
		t.Fatalf("reason = %q, want the outstanding_scope_ prefix", reason)
	}
	if !strings.Contains(reason, "owned_uncommitted") {
		t.Fatalf("reason = %q, want it to name owned_uncommitted", reason)
	}
}

func TestShouldAutoReapDoneWorkAgentIgnoresSharedCloneDirtyState(t *testing.T) {
	shared := t972SeededSharedClone(t)
	// Dirty the shared clone itself — an unrelated neighbour's edit, not
	// this seat's own worktree.
	if err := os.WriteFile(filepath.Join(shared, "neighbour.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := t972RegisterAt(t, "jv-t972-shared-dirty", shared)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t972-shared-dirty", t972PlainFinishReport, nil)
	if !ok {
		t.Fatalf("a seat whose workdir IS the shared clone must not be kept on the clone's own dirty state (reason %s)", reason)
	}
}

func TestShouldAutoReapDoneWorkAgentKeepsSeatWaitingOnGate(t *testing.T) {
	name := "jv-t972-gate-wait"
	reg := t395Registry(t, name)
	ok, reason := ShouldAutoReapDoneWorkAgent(reg, name, t972PendingGateReport, nil)
	if ok {
		t.Fatalf("reaped a seat that declared a blocking wait on a tracked gate (reason %s)", reason)
	}
	if reason != outstandingScopeReapReasonPrefix+"pending_gate" {
		t.Fatalf("reason = %q, want %q", reason, outstandingScopeReapReasonPrefix+"pending_gate")
	}
}

// Control: a genuine finished-work report with no gate wait and no dirty
// worktree still reaps — the T972 scan narrows a false positive, it must
// not widen the veto.
func TestShouldAutoReapDoneWorkAgentStillReapsGenuineFinish(t *testing.T) {
	name := "jv-t972-genuine-finish"
	reg := t395Registry(t, name)
	ok, _ := ShouldAutoReapDoneWorkAgent(reg, name, t972PlainFinishReport, nil)
	if !ok {
		t.Fatal("a genuine finish report with no outstanding scope must still reap")
	}
}

// End-to-end through the real reap path: a seat with a pending-gate report
// stays registered and the parent is notified naming the scope (mirrors
// TestT577MisEnvelopedCheckpointNotifiesPO's fixture shape).
func TestMaybeReapDoneWorkAgentKeepsSeatAndNotifiesParentOfOutstandingScope(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons-po", WorkDir: dir, SessionID: "po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
		{Name: "jv-t972-sink-gate-wait", WorkDir: dir, SessionID: "w", Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T972", Materialized: true, Provider: "grok"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	po := &fakeSender{alive: true}
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		if name != "jevons-po" {
			t.Fatalf("unexpected fleet delivery to %q", name)
		}
		return po, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Observed: true, PayloadSeen: true,
		Detail: "transcript gained a user message carrying this payload",
	}))

	s.maybeReapDoneWorkAgent("jv-t972-sink-gate-wait", t972PendingGateReport)

	if reg.Def("jv-t972-sink-gate-wait") == nil {
		t.Fatal("a seat with a pending background gate must not be reaped")
	}
	if len(po.sent) != 1 {
		t.Fatalf("PO deliveries = %d, want 1: %v", len(po.sent), po.sent)
	}
	got := po.sent[0]
	for _, want := range []string{"reap-scope 🎯T972", "🎯T972", "jv-t972-sink-gate-wait", "blocking wait"} {
		if !strings.Contains(got, want) {
			t.Errorf("PO notice missing %q:\n%s", want, got)
		}
	}
}

func TestMaybeReapDoneWorkAgentGenuineFinishDoesNotNotifyParentOfScope(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t972-clean-finish", WorkDir: dir, SessionID: "w",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", TargetID: "T972",
		Materialized: true, Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		t.Fatalf("a genuine finish must not notify the parent about outstanding scope, got send to %q", name)
		return nil, false, nil
	})
	s.maybeReapDoneWorkAgent("jv-t972-clean-finish", t972PlainFinishReport)
	if reg.Def("jv-t972-clean-finish") != nil {
		t.Fatal("genuine finish with no outstanding scope must reap")
	}
}
