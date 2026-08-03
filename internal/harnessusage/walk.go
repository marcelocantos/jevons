// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// walkMatching walks root and invokes fn for each file matching match.
// MaxFiles caps how many matching files are opened (0 = no cap).
// Missing root is not an error (empty report). Unreadable entries skip.
func walkMatching(root string, maxFiles int, match func(path string, d fs.DirEntry) bool, fn func(path string) error) (files int, err error) {
	if root == "" {
		return 0, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if !info.IsDir() {
		return 0, nil
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if !match(path, d) {
			return nil
		}
		if maxFiles > 0 && files >= maxFiles {
			return fs.SkipAll
		}
		files++
		return fn(path)
	})
	if err == fs.SkipAll {
		err = nil
	}
	return files, err
}

// forEachJSONLLine reads path line-by-line and calls fn with non-empty lines.
func forEachJSONLLine(path string, fn func(line []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	// Session files can be large; 10 MiB/line is generous for usage events.
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for sc.Scan() {
		line := bytesTrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		// Copy: scanner reuses the buffer.
		cp := append([]byte(nil), line...)
		if err := fn(cp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func bytesTrimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\r' || b[i] == '\n') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\r' || b[j-1] == '\n') {
		j--
	}
	return b[i:j]
}

func isClaudeSessionJSONL(path string) bool {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".jsonl") {
		return false
	}
	name := strings.TrimSuffix(base, ".jsonl")
	switch name {
	case "updates", "chat_history", "events", "rewind_points",
		"prompt_history", "hunk_records", "btw_history":
		return false
	}
	// Fixture and live Claude files use session ids (uuid or short ids).
	return name != ""
}

func isGrokUpdatesJSONL(path string) bool {
	return filepath.Base(path) == "updates.jsonl"
}

func isCodexRolloutJSONL(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".jsonl") && strings.HasPrefix(base, "rollout-")
}
