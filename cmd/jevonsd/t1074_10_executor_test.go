// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/seatpair"
	"github.com/marcelocantos/jevons/internal/server"
)

// TestT107410ExecutorIsolationRestartAndFrames drives the MCP caller and the
// daemon cost guard. The parent commits while no guard is running, drops one
// seat's handshake, and duplicates the other's. Each child is a new process
// whose startCostGuard is the executor. The second child is the restart.
func TestT107410ExecutorIsolationRestartAndFrames(t *testing.T) {
	if os.Getenv("T1074_10_EXEC") == "1" {
		t107410ExecChild(t)
		return
	}
	dir := t.TempDir()
	root := filepath.Join(dir, seatpair.DirName)
	a, err := seatpair.OpenCaller(root, "seat-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := seatpair.OpenCaller(root, "seat-b")
	if err != nil {
		t.Fatal(err)
	}
	lostID, err := a.Commit(context.Background(), &seatpair.CommitArgs{Generation: 1, Tool: seatpair.ToolPing, ArgsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	staleID, err := a.Commit(context.Background(), &seatpair.CommitArgs{Generation: 9, Tool: seatpair.ToolPing, ArgsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	dupID, err := b.Commit(context.Background(), &seatpair.CommitArgs{Generation: 1, Tool: seatpair.ToolPing, ArgsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(aJevons(root)); !os.IsNotExist(err) {
		t.Fatal("commit created the jevons file while the daemon was absent")
	}
	if err := os.WriteFile(filepath.Join(root, "seat-a", seatpair.GenerationFile), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seat-b", seatpair.GenerationFile), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	discarded, err := seatpair.FaultSync(root, "seat-a", true, false)
	if err != nil || discarded == 0 {
		t.Fatalf("drop = %d %v", discarded, err)
	}
	extras, err := seatpair.FaultSync(root, "seat-b", false, true)
	if err != nil || extras != 1 {
		t.Fatalf("duplicate = %d %v", extras, err)
	}

	run := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestT107410ExecutorIsolationRestartAndFrames$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			"T1074_10_EXEC=1",
			"T1074_10_DIR="+dir,
			"T1074_10_LOST="+lostID,
			"T1074_10_DUP="+dupID,
			"T1074_10_STALE="+staleID,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %v\n%s", err, out)
		}
	}
	run()
	run()

	body, err := os.ReadFile(filepath.Join(dir, seatpair.EffectLog))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	got := map[string]int{}
	for _, line := range lines {
		got[line]++
	}
	if got[lostID] != 1 || got[dupID] != 1 || got[staleID] != 0 || len(lines) != 2 {
		t.Fatalf("effect log = %q", body)
	}
}

func t107410ExecChild(t *testing.T) {
	dir := os.Getenv("T1074_10_DIR")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New("test", dir)
	mcp := mcpserver.New(dir, nil, nil)
	guard := startCostGuard(ctx, config.Config{
		StateDir: dir, SessionsDir: dir, OverseerName: "jevons",
	}, config.NewWatcher(nil), reg, nil, srv, mcp)
	if guard == nil {
		t.Fatal("cost guard did not start")
	}
	root := filepath.Join(dir, seatpair.DirName)
	a, err := seatpair.OpenCaller(root, "seat-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := seatpair.OpenCaller(root, "seat-b")
	if err != nil {
		t.Fatal(err)
	}
	wait, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	lost, err := a.Wait(wait, os.Getenv("T1074_10_LOST"))
	if err != nil || lost.Outcome != seatpair.OutcomeAccepted || lost.Reason != "pong" || lost.Body != "1" {
		t.Fatalf("lost = %+v %v", lost, err)
	}
	dup, err := b.Wait(wait, os.Getenv("T1074_10_DUP"))
	if err != nil || dup.Outcome != seatpair.OutcomeAccepted || dup.Reason != "pong" || dup.Body != "1" {
		t.Fatalf("dup = %+v %v", dup, err)
	}
	stale, err := a.Wait(wait, os.Getenv("T1074_10_STALE"))
	if err != nil || stale.Outcome != seatpair.OutcomeStale || stale.Body != "" {
		t.Fatalf("stale = %+v %v", stale, err)
	}
}

func aJevons(root string) string {
	return filepath.Join(root, "seat-a", seatpair.JevonsFile)
}
