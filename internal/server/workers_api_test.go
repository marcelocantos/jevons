// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/workers"
)

func TestWorkersAPIListAndSSE(t *testing.T) {
	tr, err := workers.NewTracker(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()

	s := New("test", t.TempDir())
	s.SetWorkersTracker(tr)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Empty list.
	resp, err := http.Get(ts.URL + "/api/workers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var list []workers.Worker
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("want empty, got %d", len(list))
	}

	// SSE: subscribe then publish.
	done := make(chan string, 1)
	go func() {
		r, err := http.Get(ts.URL + "/api/workers/events")
		if err != nil {
			done <- "get err: " + err.Error()
			return
		}
		defer r.Body.Close()
		sc := bufio.NewScanner(r.Body)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, "worker_started") {
				done <- line
				return
			}
		}
		done <- "eof"
	}()

	// Give the SSE handler time to subscribe.
	time.Sleep(50 * time.Millisecond)
	if err := tr.Start(workers.StartArgs{ID: "w9", Task: "t", Model: "m", Cwd: "/tmp"}); err != nil {
		t.Fatal(err)
	}

	select {
	case line := <-done:
		if !strings.Contains(line, "w9") {
			t.Fatalf("SSE payload: %s", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SSE worker_started")
	}

	resp2, err := http.Get(ts.URL + "/api/workers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if err := json.NewDecoder(resp2.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "w9" || list[0].Status != workers.StatusRunning {
		t.Fatalf("list after start: %+v", list)
	}

	_ = tr.Progress("w9", "hello")
	resp3, err := http.Get(ts.URL + "/api/workers/w9/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	var evs []workers.Event
	if err := json.NewDecoder(resp3.Body).Decode(&evs); err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Line != "hello" {
		t.Fatalf("events: %+v", evs)
	}
}
