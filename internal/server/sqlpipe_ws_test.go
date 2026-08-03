// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	jvsync "github.com/marcelocantos/jevons/internal/sync"
)

type stubPeerSession struct {
	hello    []byte
	handled  [][]byte
	response []byte
	closed   bool
}

func (s *stubPeerSession) Hello() ([]byte, error) { return s.hello, nil }
func (s *stubPeerSession) HandlePeerFrame(data []byte) ([]byte, error) {
	s.handled = append(s.handled, append([]byte(nil), data...))
	return s.response, nil
}
func (s *stubPeerSession) Close() error { s.closed = true; return nil }

func TestSqlpipePureTransportRejectsApplicationJSON(t *testing.T) {
	stub := &stubPeerSession{}
	srv := New("test", t.TempDir())
	srv.SetPeerSessionFactory(func() (PeerSession, error) { return stub, nil })

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/sqlpipe"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	// Application JSON text frame must violate pure transport.
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"user_message","text":"nope"}`)); err != nil {
		t.Fatal(err)
	}

	// Server should close after violation; next read fails.
	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Fatal("expected connection close after pure transport violation")
	}
	if len(stub.handled) != 0 {
		t.Fatalf("peer should not handle JSON frames; handled=%d", len(stub.handled))
	}
}

func TestSqlpipePureTransportAcceptsPeerBinary(t *testing.T) {
	frame, err := jvsync.EncodePeerFrame(jvsync.RoleAsReplica, jvsync.TagAck, make([]byte, 8))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := jvsync.EncodePeerFrame(jvsync.RoleAsMaster, jvsync.TagAck, make([]byte, 8))
	if err != nil {
		t.Fatal(err)
	}

	stub := &stubPeerSession{response: resp}
	srv := New("test", t.TempDir())
	srv.SetPeerSessionFactory(func() (PeerSession, error) { return stub, nil })

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/sqlpipe"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
		t.Fatal(err)
	}

	mt, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mt != websocket.MessageBinary {
		t.Fatalf("response type: got %v want binary", mt)
	}
	if string(data) != string(resp) {
		t.Fatalf("response mismatch: got %v want %v", data, resp)
	}
	if len(stub.handled) != 1 {
		t.Fatalf("handled frames: got %d want 1", len(stub.handled))
	}
}

func TestSqlpipeFailsClosedWithoutPeer(t *testing.T) {
	srv := New("test", t.TempDir())
	// No factory → fail closed.

	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/sqlpipe"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		// Some stacks surface close during dial; either way not a live session.
		return
	}
	defer conn.CloseNow()

	_, _, err = conn.Read(ctx)
	if err == nil {
		t.Fatal("expected close when peer not configured")
	}
}
