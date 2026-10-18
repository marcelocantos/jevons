// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package main

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/server"
)

// TestT10749LostFrameRecoversAndDuplicateIngestsOnce publishes two seats
// before the replica file exists, drops the handshake, and lets each child
// process run startCostGuard. The second child is the restart, after a
// second handshake has replayed the same rows.
func TestT10749LostFrameRecoversAndDuplicateIngestsOnce(t *testing.T) {
	if os.Getenv("T1074_9_EXEC") == "1" {
		t10749ExecChild(t)
		return
	}
	dir := t.TempDir()
	src, err := cost.OpenReplica(dir)
	if err != nil {
		t.Fatal(err)
	}
	seatA := startedFact("seat-a", "call-a")
	seatB := startedFact("seat-b", "call-b")
	claudia.RecordObservedUsage(&seatA)
	claudia.RecordObservedUsage(&seatB)
	replicaPath := filepath.Join(dir, controlplane.JevonsDBFile)
	if _, err := os.Stat(replicaPath); !os.IsNotExist(err) {
		t.Fatal("usage publish created the jevons replica while the consumer was absent")
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	discarded, err := cost.FaultReplicaFiles(dir, true, false)
	if err != nil || discarded == 0 {
		t.Fatalf("drop = %d %v", discarded, err)
	}
	if n := countUsageFacts(t, replicaPath); n != 0 {
		t.Fatalf("dropped handshake left %d replica facts", n)
	}
	if n := countUsageFacts(t, filepath.Join(dir, controlplane.ClaudiaDBFile)); n != 2 {
		t.Fatalf("claudia facts = %d", n)
	}

	runChild(t, dir)
	extras, err := cost.FaultReplicaFiles(dir, false, true)
	if err != nil || extras != 1 {
		t.Fatalf("duplicate = %d %v", extras, err)
	}
	if n := countUsageFacts(t, replicaPath); n != 2 {
		t.Fatalf("replayed handshake rows = %d", n)
	}
	runChild(t, dir)
}

func runChild(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestT10749LostFrameRecoversAndDuplicateIngestsOnce$", "-test.count=1")
	cmd.Env = append(os.Environ(), "T1074_9_EXEC=1", "T1074_9_DIR="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
}

func t10749ExecChild(t *testing.T) {
	dir := os.Getenv("T1074_9_DIR")
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
	if guard == nil || guard.collector == nil {
		t.Fatal("cost guard did not start")
	}
	if err := guard.collector.CatchUpReplicas(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := guard.store.StructuredUsage(ctx, time.Unix(0, 0), time.Now().Add(time.Hour))
	if err != nil || got.Calls != 2 || got.IncompleteCalls != 2 {
		t.Fatalf("replica usage = %+v %v", got, err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT seat, call_id FROM usage_calls ORDER BY seat, call_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var gotRows []string
	for rows.Next() {
		var seat, call string
		if err := rows.Scan(&seat, &call); err != nil {
			t.Fatal(err)
		}
		gotRows = append(gotRows, seat+" "+call)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"seat-a call-a", "seat-b call-b"}
	if len(gotRows) != len(want) || gotRows[0] != want[0] || gotRows[1] != want[1] {
		t.Fatalf("usage rows = %q", gotRows)
	}
}

func startedFact(seat, call string) claudia.UsageFact {
	return claudia.UsageFact{
		Seq: 1, CallID: call, Seat: seat, SessionID: "generation",
		Kind: "model", State: "started", Provider: "openai", Model: "gpt-5.5",
		Requests: []claudia.UsageRequestRef{}, StartedAt: "2026-10-16T01:00:00Z",
		CounterSource: "adapter_normalized_positive", Coverage: "engine_invocation",
	}
}

func countUsageFacts(t *testing.T, path string) int {
	t.Helper()
	db, err := sqlpipe.OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	qr, err := db.Query("SELECT COUNT(*) FROM " + controlplane.UsageTable)
	if err != nil {
		t.Fatal(err)
	}
	if len(qr.Rows) != 1 || len(qr.Rows[0]) != 1 {
		t.Fatalf("count result = %+v", qr.Rows)
	}
	n, ok := qr.Rows[0][0].(int64)
	if !ok {
		t.Fatalf("count type %T", qr.Rows[0][0])
	}
	return int(n)
}
