// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/brokerfixture"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/server"
)

// Boot through the same service composition as main, with the real broker
// initially absent. Its link loop must recover without re-running startup.
func TestT1228BootOfflineRecoversPlanMuxAndReceipts(t *testing.T) {
	broker := brokerfixture.StartStopped(t)
	t.Setenv("CLAUDIA_NO_BROKER", "0")
	t.Setenv("CLAUDIA_BROKER_SOCKET", broker.Socket)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "budget.json"), []byte(`{"disabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, mcp := server.New("test", dir), mcpserver.New(dir, nil, nil)
	replica, guard := startControlServices(ctx, config.Config{StateDir: dir}, config.NewWatcher(nil), nil, nil, s, mcp)
	if replica == nil || guard != nil {
		t.Fatalf("replica=%v guard=%v", replica, guard)
	}
	defer replica.Close()
	ready := make(chan struct{})
	if _, err := startPlanUsage(ctx, mcp, s, dir, ready, replica); err != nil {
		t.Fatal(err)
	}
	close(ready)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()
	get := func() string {
		resp, err := http.Get(httpSrv.URL + "/api/plan-usage")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	if got := get(); !strings.Contains(got, `"disabled":true`) || !strings.Contains(got, "control plane disconnected") {
		t.Fatalf("offline serving: %s", got)
	}
	wsCtx, stop := context.WithTimeout(ctx, 12*time.Second)
	defer stop()
	conn, _, err := websocket.Dial(wsCtx, "ws"+strings.TrimPrefix(httpSrv.URL, "http")+"/ws/mux", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, _, err := conn.Read(wsCtx); err != nil {
		t.Fatal(err)
	} // hello
	if err := conn.Write(wsCtx, websocket.MessageText, []byte(`{"v":1,"ch":"plan-usage","t":"open"}`)); err != nil {
		t.Fatal(err)
	}
	// Opening the channel sends the offline state; subsequent frames must be
	// pushed on that same socket, not obtained by reopening or restarting.
	if _, initial, err := conn.Read(wsCtx); err != nil {
		t.Fatal(err)
	} else if !strings.Contains(string(initial), `"disabled":true`) {
		t.Fatalf("offline mux: %s", initial)
	}
	broker.Restart()
	broker.PublishPlan(47)
	q := claudia.ReceiptQuery{Seat: "seat", SessionID: "session", RequestID: "boot-offline"}
	broker.PublishReceipt(claudia.RequestReceipt{Seat: q.Seat, SessionID: q.SessionID, RequestID: q.RequestID, State: claudia.ReceiptAccepted, AttemptID: "attempt", TurnIDs: []string{}, AcceptedAt: "2026-10-16T01:00:00Z", UpdatedAt: "2026-10-16T01:00:00Z"})
	waitUntil(t, "receipt after broker start", func() bool {
		r, e := s.LookupSendReceipt(ctx, q)
		return e == nil && r != nil && r.RequestID == q.RequestID
	})
	waitUntil(t, "HTTP plan after broker start", func() bool { return strings.Contains(get(), `"remaining_percent":47`) })
	for {
		_, frame, err := conn.Read(wsCtx)
		if err != nil {
			t.Fatalf("no recovered mux frame: %v", err)
		}
		if strings.Contains(string(frame), `"remaining_percent":47`) {
			break
		}
	}
}
