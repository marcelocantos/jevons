// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !jevons_claudia_next

package main

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/server"
)

// startCostGuard is the production caller of OpenReplica. On the published
// pin that call fails closed: history stays off, the usage socket stays
// off, catch-up ingests nothing, and neither receipt read is installed.
func TestT1074PublishedPinCostGuardFailsClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New("test", dir)
	mcp := mcpserver.New(dir, nil, nil)

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	guard := startCostGuard(ctx, config.Config{
		StateDir: dir, SessionsDir: dir, OverseerName: "jevons",
	}, config.NewWatcher(nil), reg, nil, srv, mcp)
	slog.SetDefault(prev)

	if guard == nil || guard.collector == nil {
		t.Fatal("cost guard did not start")
	}
	if !strings.Contains(logs.String(), "control plane unavailable: published claudia v0.53.0") {
		t.Fatalf("startCostGuard did not record OpenReplica's refusal: %s", logs.String())
	}
	if guard.collector.UsesSocketHistory() {
		t.Fatal("published pin polls the usage socket")
	}
	if guard.collector.Health().HistoryEnabled {
		t.Fatal("published pin enabled control-plane history")
	}
	if err := guard.collector.CatchUpReplicas(ctx); err != nil {
		t.Fatalf("nil replica catch-up: %v", err)
	}
	usage, err := guard.store.StructuredUsage(ctx, time.Unix(0, 0), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if usage.Calls != 0 {
		t.Fatalf("published pin ingested %d calls", usage.Calls)
	}
	_, err = mcp.LookupHeldReceipt(ctx, claudiawire.ReceiptQuery{
		Seat: "seat", SessionID: "generation", RequestID: "logical-A",
	})
	if err == nil || !strings.Contains(err.Error(), "receipt query unavailable: published claudia v0.53.0 has no receipt API") {
		t.Fatalf("held receipt = %v", err)
	}
	_, err = srv.LookupSendReceipt(ctx, claudiawire.ReceiptQuery{
		Seat: "seat", SessionID: "generation", RequestID: "logical-A",
	})
	if err == nil || !strings.Contains(err.Error(), "send receipt query is not configured") {
		t.Fatalf("server receipt = %v", err)
	}
}
