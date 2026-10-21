// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

func TestHeldReceiptsReapedSeatUsesPublicQueryWithoutLoading(t *testing.T) {
	dir := t.TempDir()
	before, _, _ := t418Daemon(t, dir)
	e := receiptHeldEntry(t, before.sendQueue(), "reaped-seat", "", false)
	s, sender, up := t418Daemon(t, dir)
	receiptJournal(t, s, dir)
	intent, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := intent.SetAgent("reaped-seat", fleetintent.Reaped, "product", "old seat removed", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(intent)
	// Only the read-only local receipt wire exists. No loaded seat, broker,
	// credential provider or model is supplied by this fixture.
	sockDir, err := os.MkdirTemp("", "jv-receipt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	socket := filepath.Join(sockDir, "r.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	t.Setenv("CLAUDIA_OMP_SOCKET", socket)
	wire := make(chan map[string]any, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			return
		}
		var msg map[string]any
		if json.Unmarshal(line, &msg) != nil {
			return
		}
		wire <- msg
		q := claudiawire.ReceiptQuery{Seat: "reaped-seat", SessionID: e.SessionID, RequestID: e.TransportRequestID}
		_ = json.NewEncoder(conn).Encode(map[string]any{"type": "receipt", "rpc_id": msg["rpc_id"], "request_id": e.TransportRequestID, "receipt": heldReceipt(q, claudiawire.ReceiptCancelled)})
	}()
	s.SweepSendBacklogs()
	select {
	case msg := <-wire:
		if msg["op"] != "receipt" || msg["session_id"] != e.SessionID || msg["request_id"] != e.TransportRequestID || msg["seat"] != "reaped-seat" || msg["token"] != nil || msg["provider"] != nil || msg["model"] != nil {
			t.Fatalf("recovery attempted model/credential operation: %+v", msg)
		}
	default:
		t.Fatal("production Claudia query did not reach fixture")
	}
	rows, err := s.sendQueue().Snapshot("reaped-seat")
	if err != nil || len(rows) != 0 || len(sender.delivered()) != 0 || len(up.all()) != 0 || intent.AgentState("reaped-seat") != fleetintent.Reaped {
		t.Fatalf("reaped recovery altered lifecycle: %+v %v", rows, err)
	}
}
