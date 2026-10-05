// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The access log must not hide what the wrapped writer can do. On 2026-10-05
// it wrapped every response in a writer with no Hijack, so /ws/mux refused
// every upgrade and the cockpit loaded but never connected.
func TestAccessLogKeepsHijackAndFlush(t *testing.T) {
	var logged []int
	log := func(_, _ string, status int, _ time.Duration, _ string) { logged = append(logged, status) }
	h := logAPIAccess(log, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			if _, ok := w.(http.Flusher); !ok {
				t.Error("access log hid http.Flusher")
			}
			_, _ = w.Write([]byte("ok"))
			return
		}
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack through the access log: %v", err)
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\nhello")
		_ = rw.Flush()
	}))
	srv := httptest.NewServer(h)
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("GET /ws HTTP/1.1\r\nHost: x\r\n\r\n"))
	status, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || status != "HTTP/1.1 101 Switching Protocols\r\n" {
		t.Fatalf("upgrade status line = %q, %v", status, err)
	}
	if _, err := http.Get(srv.URL + "/stream"); err != nil {
		t.Fatal(err)
	}
	if len(logged) < 1 || logged[0] != http.StatusSwitchingProtocols {
		t.Fatalf("logged statuses %v, want the upgrade recorded as 101", logged)
	}
}
