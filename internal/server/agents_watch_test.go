// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 🎯T82: registry file mutation notifies chat listeners (event path).
func TestWatchAgentsFileNotifiesListeners(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")
	if err := os.WriteFile(path, []byte(`[]`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New("test", dir)
	ch := make(chan string, 8)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()

	stop := s.WatchAgentsFile(path)
	t.Cleanup(stop)

	// Allow watcher to start.
	time.Sleep(30 * time.Millisecond)
	if err := os.WriteFile(path, []byte(`[{"name":"x"}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case line := <-ch:
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				continue
			}
			if m["type"] == "agents_changed" {
				return // pass
			}
		case <-deadline:
			t.Fatal("timeout waiting for agents_changed after agents.json write")
		}
	}
}

func TestNotifyAgentsChangedShape(t *testing.T) {
	s := New("test", t.TempDir())
	ch := make(chan string, 1)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, ch)
	s.mu.Unlock()
	s.NotifyAgentsChanged()
	select {
	case line := <-ch:
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatal(err)
		}
		if m["type"] != "agents_changed" {
			t.Fatalf("type=%v", m["type"])
		}
	case <-time.After(time.Second):
		t.Fatal("no notification")
	}
}
