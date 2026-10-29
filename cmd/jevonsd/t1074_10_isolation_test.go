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
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/seatpair"
	"github.com/marcelocantos/jevons/internal/server"
)

// TestT107410CostGuardIsolatesSeats commits one ping and one non-transactional
// tool while the daemon is absent. Each child is a new process whose
// startCostGuard executes the pair. The second child is the restart. The
// ping runs once. The other tool stays uncertain and is not replayed. Neither
// seat file contains the other seat's operation id.
func TestT107410CostGuardIsolatesSeats(t *testing.T) {
	if os.Getenv("T1074_10_ISO") == "1" {
		t107410IsoChild(t)
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
	pingID, err := a.Commit(context.Background(), &seatpair.CommitArgs{Generation: 1, Tool: seatpair.ToolPing, ArgsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	otherID, err := b.Commit(context.Background(), &seatpair.CommitArgs{Generation: 1, Tool: "external", ArgsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "seat-a", seatpair.JevonsFile)); !os.IsNotExist(err) {
		t.Fatal("commit created the jevons file while the daemon was absent")
	}
	if _, err := os.Stat(filepath.Join(root, "seat-b", seatpair.JevonsFile)); !os.IsNotExist(err) {
		t.Fatal("commit created the other jevons file while the daemon was absent")
	}
	if err := os.WriteFile(filepath.Join(root, "seat-a", seatpair.GenerationFile), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seat-b", seatpair.GenerationFile), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestT107410CostGuardIsolatesSeats$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			"T1074_10_ISO=1",
			"T1074_10_DIR="+dir,
			"T1074_10_PING="+pingID,
			"T1074_10_OTHER="+otherID,
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
	if len(lines) != 1 || lines[0] != pingID || strings.Contains(string(body), otherID) {
		t.Fatalf("effect log = %q", body)
	}
	if pairHasID(t, filepath.Join(root, "seat-a", seatpair.MCPFile), otherID) ||
		pairHasID(t, filepath.Join(root, "seat-a", seatpair.JevonsFile), otherID) ||
		pairHasID(t, filepath.Join(root, "seat-b", seatpair.MCPFile), pingID) ||
		pairHasID(t, filepath.Join(root, "seat-b", seatpair.JevonsFile), pingID) {
		t.Fatal("seat files share operation ids")
	}
}

func t107410IsoChild(t *testing.T) {
	dir := os.Getenv("T1074_10_DIR")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
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
	ping, err := a.Wait(wait, os.Getenv("T1074_10_PING"))
	if err != nil || ping.Outcome != seatpair.OutcomeAccepted || ping.Reason != "pong" || ping.Body != "1" {
		t.Fatalf("ping = %+v %v", ping, err)
	}
	other, err := b.Wait(wait, os.Getenv("T1074_10_OTHER"))
	if err != nil || other.Outcome != seatpair.OutcomeUncertain || other.Reason != "unknown tool" || other.Body != "" {
		t.Fatalf("other = %+v %v", other, err)
	}
}

func pairHasID(t *testing.T, path, id string) bool {
	t.Helper()
	db, err := sqlpipe.OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{seatpair.IntentTable, seatpair.OutcomeTable} {
		qr, err := db.Query("SELECT operation_id FROM "+table+" WHERE operation_id = ?", id)
		if err != nil {
			t.Fatal(err)
		}
		if len(qr.Rows) > 0 {
			return true
		}
	}
	return false
}
