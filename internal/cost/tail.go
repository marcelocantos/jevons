// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// tailFile reads the unread suffix of one session JSONL, parses billable
// events, and returns them along with the new offset (the byte position
// after the last complete line — a partial trailing line is left for the
// next poll). A file shorter than the stored offset was truncated (e.g.
// a session rewind); it is re-read from the start, with request-id dedup
// keeping the replay idempotent.
func tailFile(path string, offset int64, attribute func(sessionID string) string, now time.Time) ([]*Event, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if fi.Size() < offset {
		offset = 0 // truncated/rewound — replay; dedup absorbs it
	}
	if fi.Size() == offset {
		return nil, offset, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}

	// Session id fallback: the filename is <uuid>.jsonl.
	session := strings.TrimSuffix(filepath.Base(path), ".jsonl")

	// Tool-result lines can run to megabytes, so read whole lines with a
	// Reader rather than a Scanner with its token-size ceiling.
	r := bufio.NewReaderSize(f, 64*1024)
	var events []*Event
	pos := offset
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			// Partial trailing line (no newline yet): leave it for the
			// next poll by not advancing pos past it.
			break
		}
		pos += int64(len(line))
		if e := ParseLine(line, session, now); e != nil {
			e.Worker = attribute(e.SessionID)
			events = append(events, e)
		}
	}
	return events, pos, nil
}
