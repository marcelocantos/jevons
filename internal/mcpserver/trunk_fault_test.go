// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/worktree"
)

func t1198FaultRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q", "-b", "master", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	f := &worktree.TrunkFault{Kind: worktree.TrunkRed, ID: strings.Repeat("a", 64), Base: strings.Repeat("b", 40), Tests: []string{"TestT742DoctrineNamesEveryStringMatchingRule"}, MergeGate: "merge-red", BaseGate: "base-red"}
	if err := worktree.SaveTrunkFault(dir, f); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestT1198SentinelSurfacesTrunkFaultWhileFleetPaused(t *testing.T) {
	dir := t1198FaultRepo(t)
	n := &recordingNotifier{}
	s := New(dir, nil, nil)
	s.SetOwnerNotifier(n)
	args := SentinelLoopArgs{Server: s, Workdir: dir, AutoSpawnPaused: true}
	s.runSentinelCycle(args)
	if n.count() != 1 || !strings.Contains(n.sent[0], "trunk_red") || !strings.Contains(n.sent[0], "TestT742DoctrineNamesEveryStringMatchingRule") {
		t.Fatalf("owner missed actionable fault: %v", n.sent)
	}
	f, err := worktree.ReadTrunkFault(dir)
	if err != nil || !f.OwnerNotified || f.POWoken {
		t.Fatalf("bad receipts: %+v %v", f, err)
	}
	// A daemon restart must not re-notify or wake a provider for the same
	// persisted incident; this is durable dedup, not an in-memory cooldown.
	s = New(dir, nil, nil)
	s.SetOwnerNotifier(n)
	args.Server = s
	s.runSentinelCycle(args)
	if n.count() != 1 {
		t.Fatalf("duplicate notice after restart: %v", n.sent)
	}
}

func TestT1198DryRunDoesNotNotifyOrAcknowledge(t *testing.T) {
	dir := t1198FaultRepo(t)
	n := &recordingNotifier{}
	s := New(dir, nil, nil)
	s.SetOwnerNotifier(n)
	s.runSentinelCycle(SentinelLoopArgs{Server: s, Workdir: dir, DryRun: true})
	f, err := worktree.ReadTrunkFault(dir)
	if err != nil || f.OwnerNotified || f.POWoken || n.count() != 0 {
		t.Fatalf("dry run acted: %+v %v notices=%d", f, err, n.count())
	}
}

func TestT1198SentinelWakesPOOnceThroughTargetedSend(t *testing.T) {
	s, dir := t1193Fixture(t)
	cmd := exec.Command("git", "init", "-q", "-b", "master", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	f := &worktree.TrunkFault{Kind: worktree.TrunkRed, ID: strings.Repeat("c", 64), Base: strings.Repeat("b", 40), Tests: []string{"TestT742DoctrineNamesEveryStringMatchingRule"}, MergeGate: "merge-red", BaseGate: "base-red"}
	if err := worktree.SaveTrunkFault(dir, f); err != nil {
		t.Fatal(err)
	}
	n := &recordingNotifier{}
	s.SetOwnerNotifier(n)
	sender := &fakeSender{alive: true}
	observeSenderFixture(s, "jevons-po", sender)
	setObservedSenderResolver(s, func(name string) (agentSender, bool, error) {
		if name != "jevons-po" {
			t.Fatalf("misrouted repair to %s", name)
		}
		return sender, false, nil
	})
	s.SetTurnWitness(witnessYielding(TurnEvidence{Observed: true, PayloadSeen: true}))
	args := SentinelLoopArgs{Server: s, Workdir: dir, ProcessRunning: func(string) bool { return true }}
	s.runSentinelCycle(args)
	s.runSentinelCycle(args)
	sent := sender.sentSnapshot()
	if len(sent) != 1 || n.count() != 1 || !strings.Contains(sent[0], "jv-trunk-fix-cccccccccccc") || !strings.Contains(sent[0], "TestT742DoctrineNamesEveryStringMatchingRule") {
		t.Fatalf("not one actionable targeted wake: sent=%v notices=%d", sent, n.count())
	}
	f, err := worktree.ReadTrunkFault(dir)
	if err != nil || !f.POWoken || !f.OwnerNotified {
		t.Fatalf("delivery not persisted: %+v %v", f, err)
	}
	// Reset delivery runtime to model a daemon bounce; persisted receipt prevents
	// another paid turn even though in-memory coalescers are empty.
	s2 := New(dir, nil, nil)
	s2.SetRegistry(s.registry)
	s2.SetOwnerNotifier(n)
	s2.SetSenderResolver(func(string) (agentSender, bool, error) {
		t.Fatal("duplicate delivery after restart")
		return nil, false, nil
	})
	args.Server = s2
	s2.runSentinelCycle(args)
	if n.count() != 1 {
		t.Fatal("owner re-notified")
	}
}
