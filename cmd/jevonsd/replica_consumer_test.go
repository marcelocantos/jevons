// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/server"
)

func TestCostGuardConsumesReplicasWithoutSocketPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
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
	if guard.collector.UsesSocketHistory() {
		t.Fatal("daemon still polls the usage socket")
	}

	fact := claudia.UsageFact{
		Seq: 1, CallID: "call-1", Seat: "seat", SessionID: "generation",
		Kind: "model", State: "started", Provider: "openai", Model: "gpt-5.5",
		Requests: []claudia.UsageRequestRef{}, StartedAt: "2026-10-16T01:00:00Z",
		CounterSource: "adapter_normalized_positive", Coverage: "engine_invocation",
	}
	claudia.RecordObservedUsage(&fact)
	if err := guard.collector.CatchUpReplicas(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := guard.store.StructuredUsage(ctx, time.Unix(0, 0), time.Now().Add(time.Hour))
	if err != nil || got.Calls != 1 || got.IncompleteCalls != 1 {
		t.Fatalf("replica usage = %+v %v", got, err)
	}
	if err := guard.collector.CatchUpReplicas(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := guard.store.StructuredUsage(ctx, time.Unix(0, 0), time.Now().Add(time.Hour))
	if err != nil || again.Calls != 1 || again.IncompleteCalls != 1 {
		t.Fatalf("second replica ingest added usage: %+v %v", again, err)
	}

	receipt := &claudia.RequestReceipt{
		RequestID: "logical-A", SessionID: "generation", Seat: "seat",
		State: claudia.ReceiptAccepted, AttemptID: "attempt-A", TurnIDs: []string{},
		AcceptedAt: "2026-10-16T01:00:00.000000000Z",
		UpdatedAt:  "2026-10-16T01:00:00.000000000Z",
	}
	claudia.RecordObservedReceipt(receipt)
	found, err := mcp.LookupHeldReceipt(ctx, claudia.ReceiptQuery{
		Seat: "seat", SessionID: "generation", RequestID: "logical-A",
	})
	if err != nil || found.RequestID != "logical-A" || found.State != claudia.ReceiptAccepted || found.AttemptID != "attempt-A" {
		t.Fatalf("held receipt lookup = %+v %v", found, err)
	}
}
