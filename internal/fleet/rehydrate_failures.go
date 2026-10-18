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
// "unknown". The file keeps that sentence across the bounce, and the
// typed cause when the error carried one. A sentence is not a cause.
// An older file is a map of sentences. Those rows load with an empty
// cause, so they still show on the fleet row and do not revive a plan.

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
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("rehydrate failures: %s: %w", path, err)
	}
	for name, msg := range raw {
		if name == "" {
			continue
		}
		rec, err := decodeRehydrateValue(msg)
		if err != nil {
			return fmt.Errorf("rehydrate failures: %s: %s: %w", path, name, err)
		}
		if rec.Text == "" {
			continue
		}
		rehydrateFailures.Store(name, rec)
	}
	return nil
}

// decodeRehydrateValue reads one file value. A JSON string is the older
// sentence-only row. An object carries the sentence and, when present,
// the typed cause. Anything else is malformed.
func decodeRehydrateValue(msg json.RawMessage) (rehydrateRecord, error) {
	var text string
	if err := json.Unmarshal(msg, &text); err == nil {
		return rehydrateRecord{Text: text}, nil
	}
	var obj struct {
		Text  string `json:"text"`
		Cause string `json:"cause"`
	}
	if err := json.Unmarshal(msg, &obj); err != nil {
		return rehydrateRecord{}, err
	}
	return rehydrateRecord{Text: obj.Text, Cause: obj.Cause}, nil
}

func saveRehydrateFailures() {
	rehydrateFailureMu.Lock()
	path := rehydrateFailureFile
	rehydrateFailureMu.Unlock()
	if path == "" {
		return
	}
	all := map[string]rehydrateFileRecord{}
	rehydrateFailures.Range(func(k, v any) bool {
		name, _ := k.(string)
		rec, ok := v.(rehydrateRecord)
		if ok && name != "" && rec.Text != "" {
			all[name] = rehydrateFileRecord{Text: rec.Text, Cause: rec.Cause}
		}
		return true
	})
	if err := writeRehydrateFailures(path, all); err != nil {
		slog.Error("rehydrate failure store write failed", "err", err, "path", path)
	}
}

// rehydrateFileRecord is one on-disk refusal. Cause is omitted when the
// error carried none, which is also how an older sentence-only file reads.
type rehydrateFileRecord struct {
	Text  string `json:"text"`
	Cause string `json:"cause,omitempty"`
}

func writeRehydrateFailures(path string, all map[string]rehydrateFileRecord) error {
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
