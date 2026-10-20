// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// 🎯T1085: a request for one seat's records costs that seat's lines, not the
// spool.
//
// The inspect stream, the transcript tool and the recovery check each read a
// seat's records on request. ReadSeat gets them by reading every line of
// every day file, 11 GB on 2026-10-20, for each request. The seat index
// already reads each appended byte once to learn which seats a file names;
// it now also keeps, per file and seat, where that seat's lines are. Small
// lines that follow one another are one span, read in one call. A line over
// bigLineBytes is a span of its own and is decoded once, then remembered
// without its Snapshot and UsageFact: those lines are turn_ends carrying the
// seat's whole agent state, 99.6% of the spool's bytes, and no request reader
// renders that state.
//
// Every line a span holds is put through ReadSeat's own test — it contains
// the seat's key and decodes to a record of that seat — so the index only
// narrows where to look. It files a line under the seat its head names at
// the top level, and a line whose head does not plainly name one is decoded
// whole when scanned and filed under the seat it decodes to.

// bigLineBytes is the size above which a line is decoded once and
// remembered rather than read again on each request.
const bigLineBytes = 64 << 10

// seatSpan is whole lines of one day file, [off, off+n), each naming the
// seat it is filed under. big is set for a single line over bigLineBytes.
type seatSpan struct {
	off, n int64
	big    *bigLine
}

// bigLine remembers what a large line decoded to.
type bigLine struct {
	done atomic.Pointer[bigResult]
}

// bigResult is a decoded large line: rec is nil when the line is not one of
// the seat's records.
type bigResult struct{ rec *Record }

// addSpan files the complete line [off, off+n) under seat.
func (fi *fileIndex) addSpan(seat string, off, n int64) {
	spans := fi.spans[seat]
	if n > bigLineBytes {
		fi.spans[seat] = append(spans, seatSpan{off: off, n: n, big: &bigLine{}})
		return
	}
	if k := len(spans) - 1; k >= 0 && spans[k].big == nil && spans[k].off+spans[k].n == off {
		spans[k].n += n
		return
	}
	fi.spans[seat] = append(spans, seatSpan{off: off, n: n})
}

// lineOwner is the seat a whole line belongs to by ReadSeat's rule, or "".
func lineOwner(whole []byte) string {
	rec, ok := decodeCompact(whole)
	if !ok || rec.Seat == "" || !bytes.Contains(whole, seatKey(rec.Seat)) {
		return ""
	}
	return rec.Seat
}

// headSeat is the value of the line's top-level "seat" key when its head
// shows it plainly: an object whose top-level "seat" key is followed
// directly by a string with no escapes. ok is false otherwise — no such key
// in the head, a space before the value, an escape, a head cut short — and
// the index then decodes the whole line instead. The sidecar writes ts,
// seat, then type, so its lines always answer here.
func headSeat(head []byte) (string, bool) {
	i := 0
	for i < len(head) && (head[i] == ' ' || head[i] == '\t' || head[i] == '\r') {
		i++
	}
	if i >= len(head) || head[i] != '{' {
		return "", false
	}
	depth, wantKey := 1, true
	for i++; i < len(head); i++ {
		switch c := head[i]; c {
		case '"':
			j, escaped := i+1, false
			for j < len(head) && head[j] != '"' {
				if head[j] == '\\' {
					escaped = true
					j++
				}
				j++
			}
			if j >= len(head) {
				return "", false
			}
			if depth == 1 && wantKey {
				if !escaped && string(head[i+1:j]) == "seat" {
					v := head[j+1:]
					if len(v) < 2 || v[0] != ':' || v[1] != '"' {
						return "", false
					}
					v = v[2:]
					e := bytes.IndexAny(v, "\"\\")
					if e <= 0 || v[e] != '"' {
						return "", false
					}
					return string(v[:e]), true
				}
				wantKey = false
			}
			i = j
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth <= 0 {
				return "", false
			}
		case ',':
			if depth == 1 {
				wantKey = true
			}
		}
	}
	return "", false
}

// compactLine is Record without the fields no request reader renders.
// encoding/json skips their keys unread, and still checks the whole line is
// valid first, so a line decodes here exactly when it decodes as a Record.
type compactLine struct {
	TS        time.Time       `json:"-"`
	RawTS     string          `json:"ts"`
	Seat      string          `json:"seat"`
	SessionID string          `json:"session_id,omitempty"`
	Type      string          `json:"type"`
	Stop      string          `json:"stop,omitempty"`
	Text      string          `json:"text,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Provider  string          `json:"provider,omitempty"`
	Model     string          `json:"model,omitempty"`
	CostUSD   *float64        `json:"costUSD,omitempty"`
	Snapshot  json.RawMessage `json:"-"`
	UsageFact json.RawMessage `json:"-"`
}

// decodeCompact is decodeLine without Snapshot and UsageFact.
func decodeCompact(raw []byte) (Record, bool) {
	line := bytes.TrimSpace(raw)
	if len(line) == 0 {
		return Record{}, false
	}
	var c compactLine
	if err := json.Unmarshal(line, &c); err != nil {
		return Record{}, false
	}
	rec := Record(c)
	rec.TS = parseRecordTS(rec.RawTS)
	return rec, true
}

// seatLine is raw as one of seat's records by ReadSeat's rule.
func seatLine(raw, key []byte, seat string) (Record, bool) {
	if !bytes.Contains(raw, key) {
		return Record{}, false
	}
	rec, ok := decodeCompact(raw)
	if !ok || rec.Seat != seat {
		return Record{}, false
	}
	return rec, true
}

// SeatRecords is what ReadSeat returns for seat, with each record's Snapshot
// and UsageFact dropped (🎯T1085). It reads the seat's own lines, found by
// the seat index, rather than the whole spool, and a large line only the
// first time it is asked for.
func SeatRecords(dir, seat string) ([]Record, error) {
	var out []Record
	err := EachSeatRecord(dir, seat, func(rec Record) bool {
		out = append(out, rec)
		return true
	})
	return out, err
}

// EachSeatRecord calls visit with SeatRecords' records in order until visit
// returns false, reading no further than it needs to.
func EachSeatRecord(dir, seat string, visit func(Record) bool) error {
	if dir == "" || seat == "" {
		return nil
	}
	type dayFile struct {
		path       string
		spans      []seatSpan
		tail, size int64
	}
	seatIndexMu.Lock()
	idx, names, err := refreshIndexLocked(dir)
	if err != nil {
		seatIndexMu.Unlock()
		return err
	}
	// Copy what the index says, so the reading below holds no lock: a run the
	// index extends later is read as it stood here.
	var days []dayFile
	for _, name := range names {
		fi := idx.files[name]
		d := dayFile{path: filepath.Join(dir, name), tail: fi.offset, size: fi.size}
		d.spans = append(d.spans, fi.spans[seat]...)
		if len(d.spans) > 0 || d.size > d.tail {
			days = append(days, d)
		}
	}
	seatIndexMu.Unlock()

	key := seatKey(seat)
	for _, d := range days {
		more, err := eachInDay(d.path, d.spans, d.tail, d.size, key, seat, visit)
		if err != nil || !more {
			return err
		}
	}
	return nil
}

// eachInDay visits seat's records in one day file: its spans, then the
// unterminated last line [tail, size) if it is the seat's, as ReadSeat reads
// a last line with no newline. more is false once visit has said stop.
func eachInDay(path string, spans []seatSpan, tail, size int64, key []byte, seat string, visit func(Record) bool) (more bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	for _, s := range spans {
		if s.big != nil {
			res := s.big.done.Load()
			if res == nil {
				buf, err := readSpan(f, s.off, s.n)
				if err != nil {
					return false, err
				}
				res = &bigResult{}
				if rec, ok := seatLine(buf, key, seat); ok {
					res.rec = &rec
				}
				s.big.done.Store(res)
			}
			if res.rec != nil && !visit(*res.rec) {
				return false, nil
			}
			continue
		}
		buf, err := readSpan(f, s.off, s.n)
		if err != nil {
			return false, err
		}
		for len(buf) > 0 {
			line := buf
			if i := bytes.IndexByte(buf, '\n'); i >= 0 {
				line, buf = buf[:i+1], buf[i+1:]
			} else {
				buf = nil
			}
			if rec, ok := seatLine(line, key, seat); ok && !visit(rec) {
				return false, nil
			}
		}
	}
	if size <= tail {
		return true, nil
	}
	// The tail is one line still being written. Read its head first: one
	// that names another seat need not be read whole.
	head, err := readSpan(f, tail, min(size-tail, headLen))
	if err != nil {
		return false, err
	}
	if s, ok := headSeat(head); ok && s != seat {
		return true, nil
	}
	buf := head
	if size-tail > int64(len(head)) {
		if buf, err = readSpan(f, tail, size-tail); err != nil {
			return false, err
		}
	}
	if rec, ok := seatLine(buf, key, seat); ok && !visit(rec) {
		return false, nil
	}
	return true, nil
}

// readSpan reads n bytes at off, counted in ScannedBytes.
func readSpan(f *os.File, off, n int64) ([]byte, error) {
	buf := make([]byte, n)
	got, err := f.ReadAt(buf, off)
	scannedBytes.Add(int64(got))
	if int64(got) == n {
		return buf, nil
	}
	if err == nil || err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return nil, err
}
