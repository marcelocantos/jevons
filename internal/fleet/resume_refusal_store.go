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

var (
	resumeRefusalMu     sync.Mutex
	resumeRefusalCounts = map[string]int{}
	resumeRefusalFile   string
	resumeRefusalMax    int
)

func resumeRefusalEscalateAfter() int {
	if resumeRefusalMax > 0 {
		return resumeRefusalMax
	}
	return DefaultResumeRefusalEscalateAfter
}

// UseResumeRefusalFile loads and persists per-seat refusal counts.
func UseResumeRefusalFile(path string) error {
	resumeRefusalMu.Lock()
	resumeRefusalFile = path
	resumeRefusalMu.Unlock()
	if path == "" {
		return nil
	}
	return loadResumeRefusalCounts(path)
}

// SetResumeRefusalEscalateAfter overrides the default bound (tests).
func SetResumeRefusalEscalateAfter(n int) {
	resumeRefusalMax = n
}

func noteResumeRefusalFailure(name string) int {
	resumeRefusalMu.Lock()
	resumeRefusalCounts[name]++
	n := resumeRefusalCounts[name]
	resumeRefusalMu.Unlock()
	saveResumeRefusalCounts()
	return n
}

func clearResumeRefusal(name string) {
	resumeRefusalMu.Lock()
	delete(resumeRefusalCounts, name)
	resumeRefusalMu.Unlock()
	saveResumeRefusalCounts()
}

func loadResumeRefusalCounts(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("resume refusal counts: %w", err)
	}
	var raw map[string]int
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("resume refusal counts: %s: %w", path, err)
	}
	resumeRefusalMu.Lock()
	resumeRefusalCounts = raw
	resumeRefusalMu.Unlock()
	return nil
}

func saveResumeRefusalCounts() {
	resumeRefusalMu.Lock()
	path := resumeRefusalFile
	snap := map[string]int{}
	for k, v := range resumeRefusalCounts {
		if v > 0 {
			snap[k] = v
		}
	}
	resumeRefusalMu.Unlock()
	if path == "" {
		return
	}
	if err := writeResumeRefusalCounts(path, snap); err != nil {
		slog.Error("resume refusal count store write failed", "err", err, "path", path)
	}
}

func writeResumeRefusalCounts(path string, all map[string]int) error {
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
