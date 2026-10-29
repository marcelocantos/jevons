// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/seatpair"
	"github.com/marcelocantos/jevons/internal/server"
)

func TestT107410CostGuardCatchesSeatPair(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	root := filepath.Join(dir, seatpair.DirName)
	caller, err := seatpair.OpenCaller(root, "seat-a")
	if err != nil {
		t.Fatal(err)
	}
	id, err := caller.Commit(context.Background(), &seatpair.CommitArgs{
		Generation: 1, Tool: seatpair.ToolPing, ArgsJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seat-a", seatpair.GenerationFile), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New("test", dir)
	mcp := mcpserver.New(dir, nil, nil)
	replica, guard := startControlServices(ctx, config.Config{
		StateDir: dir, SessionsDir: dir, OverseerName: "jevons",
	}, config.NewWatcher(nil), reg, nil, srv, mcp)
	defer replica.Close()
	if guard == nil || guard.collector == nil {
		t.Fatal("cost guard did not start")
	}
	if guard.collector.UsesSocketHistory() {
		t.Fatal("daemon still polls the usage socket")
	}
	wait, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	got, err := caller.Wait(wait, id)
	if err != nil || got.Outcome != seatpair.OutcomeAccepted || got.Reason != "pong" {
		t.Fatalf("startup catch-up = %+v %v", got, err)
	}
	unknown, err := caller.Commit(context.Background(), &seatpair.CommitArgs{
		Generation: 1, Tool: "not-a-tool", ArgsJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := seatpair.CatchUpDir(context.Background(), root, mcp.ExecuteSeatPair); err != nil {
		t.Fatal(err)
	}
	if err := seatpair.CatchUpDir(context.Background(), root, mcp.ExecuteSeatPair); err != nil {
		t.Fatal(err)
	}
	second, err := caller.Wait(wait, unknown)
	if err != nil || second.Outcome != seatpair.OutcomeUncertain || second.Reason != "unknown tool" {
		t.Fatalf("unknown tool = %+v %v", second, err)
	}
}
