// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"

	"github.com/marcelocantos/jevons/internal/brokerfixture"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/server"
)

// TestCostGuardConsumesReplicasWithoutSocketPoll runs startCostGuard, the
// production caller of OpenReplica, against a real broker process. The
// guard resolves the broker's control socket the way jevonsd does
// (CLAUDIA_BROKER_SOCKET), keeps its own control-replica.db, and receives
// the broker's usage fact and receipt over the link. It never polls the
// usage socket, and a second catch-up adds no usage.
func TestCostGuardConsumesReplicasWithoutSocketPoll(t *testing.T) {
	broker := brokerfixture.Start(t)
	t.Setenv("CLAUDIA_NO_BROKER", "0")
	t.Setenv("CLAUDIA_BROKER_SOCKET", broker.Socket)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	guard, srv, mcp := startTestCostGuard(t, ctx, dir)
	if guard.collector.UsesSocketHistory() {
		t.Fatal("daemon still polls the usage socket")
	}
	awaitLinkEstablished(t, guard.replica, 0)

	broker.PublishUsage(completedFact("seat", "call-1", 1))
	broker.PublishReceipt(claudia.RequestReceipt{
		RequestID: "logical-A", SessionID: "generation", Seat: "seat",
		State: claudia.ReceiptAccepted, AttemptID: "attempt-A", TurnIDs: []string{},
		AcceptedAt: "2026-10-16T01:00:00.000000000Z",
		UpdatedAt:  "2026-10-16T01:00:00.000000000Z",
	})
	q := claudia.ReceiptQuery{Seat: "seat", SessionID: "generation", RequestID: "logical-A"}
	var found *claudia.RequestReceipt
	waitUntil(t, "the broker's receipt through the held-receipt lookup", func() bool {
		var err error
		found, err = mcp.LookupHeldReceipt(ctx, q)
		return err == nil
	})
	if found.RequestID != "logical-A" || found.State != claudia.ReceiptAccepted || found.AttemptID != "attempt-A" {
		t.Fatalf("held receipt lookup = %+v", found)
	}
	foundSrv, err := srv.LookupSendReceipt(ctx, q)
	if err != nil || foundSrv.RequestID != "logical-A" || foundSrv.State != claudia.ReceiptAccepted || foundSrv.AttemptID != "attempt-A" {
		t.Fatalf("server receipt lookup = %+v %v", foundSrv, err)
	}
	requireCalls(t, ctx, guard, 1)
	requireCalls(t, ctx, guard, 1)
	requireHeldBy(t, filepath.Join(dir, controlplane.JevonsDBFile), os.Getpid())
	requireHeldBy(t, broker.ControlFile(), broker.Pid())
}

func startTestCostGuard(t *testing.T, ctx context.Context, dir string) (*costGuard, *server.Server, *mcpserver.Server) {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New("test", dir)
	mcp := mcpserver.New(dir, nil, nil)
	guard := startCostGuard(ctx, config.Config{
		StateDir: dir, SessionsDir: dir, OverseerName: "jevons",
	}, config.NewWatcher(nil), reg, nil, srv, mcp)
	if guard == nil || guard.collector == nil || guard.replica == nil {
		t.Fatal("cost guard did not start with a replica")
	}
	return guard, srv, mcp
}

func completedFact(seat, call string, seq uint64) claudia.UsageFact {
	return claudia.UsageFact{
		Seq: seq, CallID: call, Seat: seat, SessionID: "generation",
		Kind: "model", State: "completed", Provider: "openai", Model: "gpt-5.5",
		Requests: []claudia.UsageRequestRef{}, StartedAt: "2026-10-16T01:00:00Z", FinishedAt: "2026-10-16T01:00:01Z",
		CounterSource: "adapter_normalized_positive", Coverage: "engine_invocation",
	}
}

func requireCalls(t *testing.T, ctx context.Context, guard *costGuard, want int64) {
	t.Helper()
	if err := guard.collector.CatchUpReplicas(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := guard.store.StructuredUsage(ctx, time.Unix(0, 0), time.Now().Add(time.Hour))
	if err != nil || got.Calls != want {
		t.Fatalf("replica usage = %+v %v, want %d calls", got, err, want)
	}
}

func requireHeldBy(t *testing.T, path string, pid int) {
	t.Helper()
	pids, err := brokerfixture.Holders(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pids, []int{pid}) {
		t.Fatalf("%s is open in pids %v, want %d alone", path, pids, pid)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// replicaHas reports whether Jevons' own file holds a usage row for call.
func replicaHas(t *testing.T, guard *costGuard, call string) bool {
	t.Helper()
	n := int64(-1)
	if err := guard.replica.Read(func(db *sqlpipe.Database) error {
		qr, err := db.Query("SELECT COUNT(*) FROM "+controlplane.UsageTable+" WHERE call_id = ?", call)
		if err != nil {
			return err
		}
		n = qr.Rows[0][0].(int64)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return n == 1
}
