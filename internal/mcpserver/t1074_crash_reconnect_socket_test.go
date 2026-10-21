// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// TestT1074InterruptedReceiptIsNotReplayedAcrossRestart is the crash between
// acceptance and receipt. The first daemon bound host-crash and died before a
// receipt came back. After restart the sidecar reports that dispatch as
// unknown/interrupted. Drain must not send it again, must keep that id, and
// must still send the accepted-but-not-started follow-up once.
func TestT1074InterruptedReceiptIsNotReplayedAcrossRestart(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	dir := t.TempDir()
	const name = "jv-t1074-crash"
	const session = "gen-crash"
	const crashedID = "host-crash"
	const followID = "host-follow"
	const text = "same text"

	before, _, _ := t418Daemon(t, dir)
	crashed, _, claimed, err := before.sendQueue().AppendAndClaimWithRequestID(name, text, crashedID, time.Now())
	if err != nil || !claimed {
		t.Fatalf("admit crash: claimed=%v err=%v", claimed, err)
	}
	crashed, err = before.sendQueue().BindReceipt(name, crashed, session)
	if err != nil || crashed.TransportRequestID != crashedID || crashed.SessionID != session {
		t.Fatalf("bind: %+v %v", crashed, err)
	}
	if _, _, err := before.sendQueue().AppendWithRequestID(name, text, followID, time.Now()); err != nil {
		t.Fatal(err)
	}

	// A test-named TempDir exceeds the macOS unix socket path limit.
	sockDir, err := os.MkdirTemp("", "jv-t1074-")
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
	t.Setenv("CLAUDIA_OMP_SOCKET", socket)
	var mu sync.Mutex
	var queries []claudiawire.ReceiptQuery
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var msg map[string]any
				if json.Unmarshal(line, &msg) != nil {
					return
				}
				q := claudiawire.ReceiptQuery{
					Seat:      strField(msg["seat"]),
					SessionID: strField(msg["session_id"]),
					RequestID: strField(msg["request_id"]),
				}
				mu.Lock()
				queries = append(queries, q)
				mu.Unlock()
				receipt := heldReceipt(q, claudiawire.ReceiptUnknown)
				receipt.Reason = "interrupted"
				receipt.Cause = claudiawire.ReceiptCauseUnknown
				if q.RequestID == followID {
					receipt = heldReceipt(q, claudiawire.ReceiptAccepted)
				}
				_ = json.NewEncoder(conn).Encode(map[string]any{
					"type": "receipt", "rpc_id": msg["rpc_id"], "request_id": q.RequestID, "receipt": receipt,
				})
			}(conn)
		}
	}()

	after, _, _ := t418Daemon(t, dir)
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: session, Materialized: true, Provider: claudia.ProviderGrok,
	}); err != nil {
		t.Fatal(err)
	}
	after.registry = reg
	sender := &requestIDSender{}
	setObservedSenderResolver(after, func(string) (agentSender, bool, error) { return sender, false, nil })
	journal := receiptJournal(t, after, dir)

	after.drainAgentSendQueue(name)
	after.drainAgentSendQueue(name)
	calls, legacy := sender.snapshot()
	if len(calls) != 1 || calls[0] != followID+":"+text || legacy != 0 {
		t.Fatalf("replayed or dropped the follow-up: calls=%v legacy=%d", calls, legacy)
	}
	rows, err := after.sendQueue().Snapshot(name)
	if err != nil || len(rows) != 1 || rows[0].ID != crashed.ID || rows[0].RequestID != crashedID || rows[0].TransportRequestID != crashedID || rows[0].SessionID != session || rows[0].State == sendq.Pending {
		t.Fatalf("interrupted receipt was reminted or released: %+v %v", rows, err)
	}
	mu.Lock()
	seen := append([]claudiawire.ReceiptQuery(nil), queries...)
	mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("drain did not query the durable receipt")
	}
	seenIDs := map[string]int{}
	for _, q := range seen {
		if q.Seat != name || q.SessionID != session || (q.RequestID != crashedID && q.RequestID != followID) {
			t.Fatalf("query was not one of the two original requests: %+v", seen)
		}
		seenIDs[q.RequestID]++
	}
	if seenIDs[crashedID] == 0 || seenIDs[followID] == 0 {
		t.Fatalf("drain did not query both the interrupted request and its follow-up: %+v", seen)
	}
	events, err := eventlog.Tail(journal.Path(), eventlog.TailOptions{Decision: "sendq_receipt_observed"})
	if err != nil || len(events) != 1 {
		t.Fatalf("receipt journal: %+v %v", events, err)
	}
	if events[0].Fields["transport_request_id"] != followID || events[0].Fields["receipt_cause"] != "" {
		t.Fatalf("interrupted id was settled, or prose became a cause: %+v", events[0].Fields)
	}
	if _, _, err = after.sendQueue().AppendWithRequestID(name, text, crashedID, time.Now()); err == nil {
		t.Fatal("reoffering the interrupted id was admitted")
	}
	var duplicate *sendq.DuplicateRequestIDError
	if !errors.As(err, &duplicate) || duplicate.Existing.ID != crashed.ID || duplicate.Conflict {
		t.Fatalf("duplicate=%+v err=%v", duplicate, err)
	}
	after.drainAgentSendQueue(name)
	calls, _ = sender.snapshot()
	if len(calls) != 1 {
		t.Fatalf("reoffer executed: %v", calls)
	}
}

func strField(v any) string {
	s, _ := v.(string)
	return s
}
