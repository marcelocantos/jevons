// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// T38 / Fable F5: a multi-megabyte JSONL line must not silently truncate
// the history replay — subsequent lines still arrive.
func TestSendHistoryLargeLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	// 2 MiB payload exceeds the old 1 MiB Scanner cap.
	big := strings.Repeat("x", 2<<20)
	content := `{"type":"small","n":1}` + "\n" +
		`{"type":"tool_result","data":"` + big + `"}` + "\n" +
		`{"type":"small","n":2}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, wsAcceptOptions())
		if err != nil {
			return
		}
		defer conn.CloseNow()
		sendHistory(conn, r.Context(), path)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	conn, _, err := websocket.Dial(t.Context(), strings.Replace(srv.URL, "http", "ws", 1)+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()
	// Large tool_result lines are legitimate in overseer transcripts;
	// the client must accept multi-megabyte frames to receive them.
	conn.SetReadLimit(8 << 20)

	var lines []string
	for {
		_, data, err := conn.Read(t.Context())
		if err != nil {
			break
		}
		lines = append(lines, string(data))
	}
	if len(lines) < 3 {
		t.Fatalf("got %d history lines, want 3 (large line truncated the rest?): %v", len(lines), summarize(lines))
	}
	if !strings.Contains(lines[0], `"n":1`) {
		t.Fatalf("first line wrong: %s", summarize(lines[:1]))
	}
	if len(lines[1]) < 2<<20 {
		t.Fatalf("middle line too short (%d); large tool_result not fully replayed", len(lines[1]))
	}
	if !strings.Contains(lines[2], `"n":2`) {
		t.Fatalf("third line missing after large line: %s", summarize(lines))
	}
}

func summarize(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if len(l) > 80 {
			out[i] = l[:40] + "..." + l[len(l)-20:] + " (len=" + itoa(len(l)) + ")"
		} else {
			out[i] = l
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
