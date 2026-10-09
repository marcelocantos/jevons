// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package eventlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	MaxPageEvents      = 2000
	MaxPageScanBytes   = 4 << 20
	MaxStreamScanBytes = 2 << 20
	maxEventLineBytes  = 1 << 20
	logReadChunk       = 64 << 10
)

var ErrCursor = errors.New("eventlog: cursor out of range")

// Query matches event fields without interpreting the event message as a command.
// Zero time values leave the corresponding bound open.
type Query struct {
	Component string
	Decision  string
	Source    string
	Level     string
	Contains  string
	Since     time.Time
	Until     time.Time
}

func (q Query) matches(ev Event) bool {
	if q.Component != "" && ev.Component != q.Component ||
		q.Decision != "" && ev.Decision != q.Decision ||
		q.Source != "" && ev.Source != q.Source ||
		q.Level != "" && ev.Level != q.Level ||
		q.Contains != "" && !strings.Contains(strings.ToLower(ev.Msg), strings.ToLower(q.Contains)) {
		return false
	}
	if !q.Since.IsZero() || !q.Until.IsZero() {
		ts, err := time.Parse(time.RFC3339Nano, ev.TS)
		if err != nil || !q.Since.IsZero() && ts.Before(q.Since) || !q.Until.IsZero() && ts.After(q.Until) {
			return false
		}
	}
	return true
}

// PageResult carries a byte cursor into one append-only journal. A cursor
// remains stable when new rows append. NextCursor is nil only at file start.
type PageResult struct {
	Events       []Event
	NextCursor   *int64
	HeadCursor   int64
	ScannedBytes int64
}

// Page scans backward from before (nil means the current end of file),
// stopping at a fixed work budget even when a filter is very selective.
// The caller can use NextCursor to continue through older rows.
func Page(path string, before *int64, limit int, query Query) (PageResult, error) {
	if limit < 1 || limit > MaxPageEvents {
		return PageResult{}, fmt.Errorf("eventlog: limit must be 1..%d", MaxPageEvents)
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		if before != nil && *before != 0 {
			return PageResult{}, ErrCursor
		}
		return PageResult{Events: []Event{}}, nil
	}
	if err != nil {
		return PageResult{}, fmt.Errorf("eventlog: open page: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return PageResult{}, fmt.Errorf("eventlog: stat page: %w", err)
	}
	result := PageResult{Events: make([]Event, 0, limit), HeadCursor: info.Size()}
	end := info.Size()
	if before != nil {
		end = *before
	}
	if end < 0 || end > info.Size() {
		return PageResult{}, ErrCursor
	}
	position, lineEnd := end, end
	line := make([]byte, 0, 256) // reversed until the preceding newline
	tooLong := false
	for position > 0 && result.ScannedBytes < MaxPageScanBytes && len(result.Events) < limit {
		length := int64(logReadChunk)
		if length > position {
			length = position
		}
		if length > MaxPageScanBytes-result.ScannedBytes {
			length = MaxPageScanBytes - result.ScannedBytes
		}
		start := position - length
		block := make([]byte, length)
		if _, err := f.ReadAt(block, start); err != nil && err != io.EOF {
			return PageResult{}, fmt.Errorf("eventlog: read page: %w", err)
		}
		for i := len(block) - 1; i >= 0; i-- {
			at := start + int64(i)
			result.ScannedBytes++
			if block[i] != '\n' {
				if len(line) < maxEventLineBytes {
					line = append(line, block[i])
				} else {
					tooLong = true
				}
				continue
			}
			if len(line) > 0 && !tooLong {
				for left, right := 0, len(line)-1; left < right; left, right = left+1, right-1 {
					line[left], line[right] = line[right], line[left]
				}
				var ev Event
				if json.Unmarshal(line, &ev) == nil && query.matches(ev) {
					result.Events = append(result.Events, ev)
					if len(result.Events) == limit {
						cursor := at + 1
						if cursor > 0 {
							result.NextCursor = &cursor
						}
						return result, nil
					}
				}
			}
			line = line[:0]
			tooLong = false
			lineEnd = at
		}
		position = start
	}
	if position == 0 {
		if len(line) > 0 && !tooLong {
			for left, right := 0, len(line)-1; left < right; left, right = left+1, right-1 {
				line[left], line[right] = line[right], line[left]
			}
			var ev Event
			if json.Unmarshal(line, &ev) == nil && query.matches(ev) {
				result.Events = append(result.Events, ev)
			}
		}
		return result, nil
	}
	// The next page starts before the incomplete line, so no row is lost.
	if lineEnd > 0 {
		result.NextCursor = &lineEnd
	}
	return result, nil
}

// Collect is the production jevons_logs_tail read: newest matching rows,
// walking Page until Limit or the start of the file. HTTP GET /api/logs
// stays one Page so the client can pass next_cursor; MCP has no cursor,
// so a default call must not return count=0 just because the newest 4 MiB
// is a browser hydrate flood (🎯T411).
func Collect(path string, opt TailOptions) ([]Event, error) {
	limit := opt.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > MaxPageEvents {
		limit = MaxPageEvents
	}
	query := Query{
		Component: strings.TrimSpace(opt.Component),
		Decision:  strings.TrimSpace(opt.Decision),
		Source:    strings.TrimSpace(opt.Source),
		Contains:  strings.TrimSpace(opt.Contains),
	}
	var (
		out    []Event
		before *int64
	)
	for len(out) < limit {
		page, err := Page(path, before, limit-len(out), query)
		if err != nil {
			if len(out) == 0 {
				return nil, err
			}
			return out, err
		}
		out = append(out, page.Events...)
		if page.NextCursor == nil {
			break
		}
		if before != nil && *page.NextCursor >= *before {
			break
		}
		before = page.NextCursor
	}
	if out == nil {
		out = []Event{}
	}
	return out, nil
}

type CursorEvent struct {
	Cursor int64
	Event  Event
}

type AfterResult struct {
	Events       []CursorEvent
	NextCursor   int64
	HeadCursor   int64
	ScannedBytes int64
}

// ReadAfter scans forward from a byte cursor. It advances past complete
// nonmatching rows, but never advances across a partially appended line.
func ReadAfter(path string, after int64, limit int, query Query) (AfterResult, error) {
	if limit < 1 || limit > MaxPageEvents {
		return AfterResult{}, fmt.Errorf("eventlog: limit must be 1..%d", MaxPageEvents)
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		if after != 0 {
			return AfterResult{}, ErrCursor
		}
		return AfterResult{Events: []CursorEvent{}}, nil
	}
	if err != nil {
		return AfterResult{}, fmt.Errorf("eventlog: open stream: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return AfterResult{}, fmt.Errorf("eventlog: stat stream: %w", err)
	}
	if after < 0 || after > info.Size() {
		return AfterResult{}, ErrCursor
	}
	result := AfterResult{Events: make([]CursorEvent, 0, limit), NextCursor: after, HeadCursor: info.Size()}
	position := after
	line := make([]byte, 0, 256)
	for position < info.Size() && result.ScannedBytes < MaxStreamScanBytes && len(result.Events) < limit {
		length := int64(logReadChunk)
		if length > info.Size()-position {
			length = info.Size() - position
		}
		if length > MaxStreamScanBytes-result.ScannedBytes {
			length = MaxStreamScanBytes - result.ScannedBytes
		}
		block := make([]byte, length)
		if _, err := f.ReadAt(block, position); err != nil && err != io.EOF {
			return AfterResult{}, fmt.Errorf("eventlog: read stream: %w", err)
		}
		for i, b := range block {
			result.ScannedBytes++
			if b != '\n' {
				line = append(line, b)
				if len(line) > maxEventLineBytes {
					return AfterResult{}, fmt.Errorf("eventlog: stream line exceeds %d bytes", maxEventLineBytes)
				}
				continue
			}
			cursor := position + int64(i) + 1
			result.NextCursor = cursor
			var ev Event
			if json.Unmarshal(bytes.TrimSpace(line), &ev) == nil && query.matches(ev) {
				result.Events = append(result.Events, CursorEvent{Cursor: cursor, Event: ev})
			}
			line = line[:0]
			if len(result.Events) == limit {
				return result, nil
			}
		}
		position += length
	}
	return result, nil
}
