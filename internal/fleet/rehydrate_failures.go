// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// rehydrateFailures is in-memory. A daemon restart used to forget why a
// stopped seat's last relaunch failed, so the fleet row fell back to
// "unknown". The file keeps that sentence across the bounce.

var (
	rehydrateFailureMu   sync.Mutex
	rehydrateFailureFile string
)

// UseRehydrateFailureFile remembers path and loads it into
// rehydrateFailures. An empty path keeps the record in memory only.
// A missing file is an empty record. A file that exists and does not
// parse is an error — malformed state is not silently reset.
func UseRehydrateFailureFile(path string) error {
	rehydrateFailureMu.Lock()
	rehydrateFailureFile = path
	rehydrateFailureMu.Unlock()
	if path == "" {
		return nil
	}
	return loadRehydrateFailures(path)
}

func loadRehydrateFailures(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("rehydrate failures: %w", err)
	}
	var all map[string]string
	if err := json.Unmarshal(b, &all); err != nil {
		return fmt.Errorf("rehydrate failures: %s: %w", path, err)
	}
	for name, why := range all {
		if name == "" || why == "" {
			continue
		}
		rehydrateFailures.Store(name, why)
	}
	return nil
}

func saveRehydrateFailures() {
	rehydrateFailureMu.Lock()
	path := rehydrateFailureFile
	rehydrateFailureMu.Unlock()
	if path == "" {
		return
	}
	all := map[string]string{}
	rehydrateFailures.Range(func(k, v any) bool {
		name, _ := k.(string)
		why, _ := v.(string)
		if name != "" && why != "" {
			all[name] = why
		}
		return true
	})
	if err := writeRehydrateFailures(path, all); err != nil {
		slog.Error("rehydrate failure store write failed", "err", err, "path", path)
	}
}

func writeRehydrateFailures(path string, all map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
