// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"bufio"
	"errors"
	"io"
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

// maxJSONLLine bounds a line we are willing to hold in memory. A usage
// frame is a few KiB; anything past this is a pasted blob or an embedded
// image in a tool result and carries no usage.
const maxJSONLLine = 10 * 1024 * 1024

// forEachJSONLLine calls fn for each non-empty line in path, and reports
// how many lines were too long to parse.
//
// An over-long line must not end the walk. bufio.Scanner fails the whole
// file on one oversized token, and because the error propagated up through
// WalkDir it silently truncated every remaining session file — a spend
// report that quietly stops counting is worse than one that refuses to
// run (🎯T392.6). A bufio.Reader steps over the offending line instead,
// and the count travels back so callers can say what they skipped.
func forEachJSONLLine(path string, fn func(line []byte) error) (skipped int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := readLimitedLine(r, maxJSONLLine)
		if len(line) > 0 {
			if trimmed := bytesTrimSpace(line); len(trimmed) > 0 {
				if ferr := fn(trimmed); ferr != nil {
					return skipped, ferr
				}
			}
		} else if err == errLineTooLong {
			skipped++
		}
		switch err {
		case nil, errLineTooLong:
			continue
		case io.EOF:
			return skipped, nil
		default:
			return skipped, err
		}
	}
}

var errLineTooLong = errors.New("jsonl line exceeds limit")

// readLimitedLine returns the next line, or errLineTooLong with an empty
// line when it exceeds limit — having consumed through the newline so the
// next call resumes cleanly on the following record.
func readLimitedLine(r *bufio.Reader, limit int) ([]byte, error) {
	var buf []byte
	tooLong := false
	for {
		chunk, err := r.ReadSlice('\n')
		if !tooLong {
			if len(buf)+len(chunk) > limit {
				tooLong, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if tooLong && err == nil {
			return nil, errLineTooLong
		}
		if err == io.EOF && len(buf) == 0 && !tooLong {
			return nil, io.EOF
		}
		if tooLong {
			return nil, errLineTooLong
		}
		return buf, err
	}
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

// forEachJSONLLineErr is forEachJSONLLine for the per-harness collectors,
// which report token totals rather than a gated measurement and have no
// field to carry a skipped-line count. They still benefit from the change
// that matters: one oversized line now costs that line, not the remainder
// of the walk. The spend report keeps the count, because it is the one
// whose number gates a target.
func forEachJSONLLineErr(path string, fn func(line []byte) error) error {
	_, err := forEachJSONLLine(path, fn)
	return err
}
