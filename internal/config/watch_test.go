// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type knob struct {
	Limit int `json:"limit"`
}

func loadKnob(path string) (knob, error) {
	k := knob{Limit: 1}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return k, nil
	}
	if err != nil {
		return k, err
	}
	err = json.Unmarshal(data, &k)
	return k, err
}

// bump makes the next write observably newer even on coarse-mtime
// filesystems.
func bump(t *testing.T, path string) {
	t.Helper()
	if err := os.Chtimes(path, time.Now(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
}

// A policy file written under a running Watcher swaps the governing value;
// garbage keeps the last-good value; a clean write afterwards swaps again
// (🎯T574).
func TestT574WatchSwapsOnWriteAndKeepsLastGoodOnGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knob.json")
	w := NewWatcher(nil)
	var changes []int
	h, err := Watch(w, &WatchArgs[knob]{Path: path, Load: loadKnob,
		OnChange: func(k knob) { changes = append(changes, k.Limit) }})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get().Limit; got != 1 {
		t.Fatalf("missing file should load defaults, got %d", got)
	}

	os.WriteFile(path, []byte(`{"limit": 7}`), 0o644)
	bump(t, path)
	w.Poll()
	if got := h.Get().Limit; got != 7 {
		t.Fatalf("after write: want 7, got %d", got)
	}

	os.WriteFile(path, []byte(`{"limit": `), 0o644)
	bump(t, path)
	w.Poll()
	w.Poll()
	if got := h.Get().Limit; got != 7 {
		t.Fatalf("garbage must keep last-good 7, got %d", got)
	}

	os.WriteFile(path, []byte(`{"limit": 9}`), 0o644)
	bump(t, path)
	w.Poll()
	if got := h.Get().Limit; got != 9 {
		t.Fatalf("after repair: want 9, got %d", got)
	}
	// OnChange fires for the prime and each successful swap, never for the
	// garbage revision.
	if len(changes) != 3 || changes[0] != 1 || changes[1] != 7 || changes[2] != 9 {
		t.Fatalf("OnChange sequence = %v, want [1 7 9]", changes)
	}

	// Deleting the file is a change back to defaults, not a silent hold.
	os.Remove(path)
	w.Poll()
	if got := h.Get().Limit; got != 1 {
		t.Fatalf("after delete: want defaults 1, got %d", got)
	}
}

// A file that is garbage at boot installs the fallback and reports the
// error, then heals on the next good write.
func TestT574WatchFirstLoadFailureInstallsFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knob.json")
	os.WriteFile(path, []byte(`nope`), 0o644)
	w := NewWatcher(nil)
	h, err := Watch(w, &WatchArgs[knob]{Path: path, Load: loadKnob, Fallback: knob{Limit: 42}})
	if err == nil {
		t.Fatal("want first-load error")
	}
	if got := h.Get().Limit; got != 42 {
		t.Fatalf("fallback: want 42, got %d", got)
	}
	os.WriteFile(path, []byte(`{"limit": 3}`), 0o644)
	bump(t, path)
	w.Poll()
	if got := h.Get().Limit; got != 3 {
		t.Fatalf("heal: want 3, got %d", got)
	}
}

// The loader's own error is what the caller sees, unwrapped.
func TestT574WatchReturnsLoaderError(t *testing.T) {
	sentinel := errors.New("boom")
	w := NewWatcher(nil)
	_, err := Watch(w, &WatchArgs[int]{Path: filepath.Join(t.TempDir(), "x"),
		Load: func(string) (int, error) { return 0, sentinel }})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
}
