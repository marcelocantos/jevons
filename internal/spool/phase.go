// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package spool

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// 🎯T1084: a seat's phase costs what was appended, not what the spool weighs.
//
// The 30-second seat-observation tick classified every sidecar seat by
// reading its whole dated spool — ReadSeat, AsJSONL, turnev's fold. With 55
// registered seats over 11 GB a pass never fitted in its interval and the
// daemon sat at a full core with two seats running.
//
// The fold does not need history. Every line AsJSONL renders from the spool
// is an assistant record — text, a tool_use, or an end_turn — and none is a
// queue operation, a user message or a system record. So the fold's pending
// count never moves, and its answer is set by the newest record that moves
// it at all: an end_turn is idle, a tool_use or non-blank text is working.
// That record is all a caller needs, and the seat index already reads every
// appended byte once to learn which seats a file names. It now also keeps,
// per file and seat, the newest line that may be that record.
//
// A text line is small and is decoded whole when scanned. A turn_end carries
// the seat's whole agent state, megabytes, and is most of the spool's
// weight; its head says what it is, and it is decoded, once, only when it is
// the newest candidate a caller asks about. If it does not decode, the file
// is read up to it for the seat's previous record — the same rule
// ReadSeatSince applies, paid once and remembered.

// phaseMark is one line that may be a seat's newest phase-moving record.
type phaseMark struct {
	seat    string
	off, n  int64   // the line's bytes in its day file
	checked bool    // decoded; rec is the answer
	rec     *Record // nil once checked when the line does not move the phase
}

// movesPhase reports whether rec renders to a line that moves turnev's phase
// fold: AsJSONL writes it, and turnev's fold reads it as a terminal, a tool
// call, or authored text. Mirrors turnev.phaseFold.step for the three shapes
// AsJSONL writes; spool cannot import turnev, so an oracle test in this
// package's external tests holds the two together.
func movesPhase(rec Record) bool {
	switch rec.Type {
	case "turn_end", "tool_call":
		return true
	case "text":
		return strings.TrimSpace(VisibleAssistantText(rec.Text)) != ""
	}
	return false
}

// markLine is what the index keeps for a line, or nil when the line cannot
// move any seat's phase. whole is set only for text lines.
func markLine(head, whole []byte, off, n int64) *phaseMark {
	switch LineType(head) {
	case "text":
		rec, ok := decodeLine(whole)
		if !ok || rec.Seat == "" || !bytes.Contains(whole, seatKey(rec.Seat)) || !movesPhase(rec) {
			return nil
		}
		return &phaseMark{seat: rec.Seat, off: off, n: n, checked: true, rec: compact(rec)}
	case "turn_end", "tool_call":
		seat := seatField(head)
		if seat == "" {
			return nil
		}
		return &phaseMark{seat: seat, off: off, n: n}
	}
	return nil
}

// compact drops what a phase reading never renders, so a remembered
// turn_end does not hold the seat's agent state.
func compact(rec Record) *Record {
	rec.Snapshot = nil
	rec.UsageFact = nil
	return &rec
}

// resolve decodes m once, by ReadSeatSince's rule for its seat.
func (m *phaseMark) resolve(path string) error {
	if m.checked {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, m.n)
	got, err := f.ReadAt(buf, m.off)
	scannedBytes.Add(int64(got))
	if err != nil && !(err == io.EOF && int64(got) == m.n) {
		return err
	}
	m.checked = true
	rec, ok := decodeLine(buf)
	if ok && rec.Seat == m.seat && bytes.Contains(buf, seatKey(m.seat)) && movesPhase(rec) {
		m.rec = compact(rec)
	}
	return nil
}

// LatestPhaseRecord is the newest of seat's records, across all dated files,
// that moves turnev's phase fold once AsJSONL renders it (🎯T1084). ok is
// false when seat has no such record. Its Snapshot and UsageFact are
// dropped. Folding this one record gives the same phase as folding all of
// ReadSeat; the index makes it cost the bytes appended since the last call.
func LatestPhaseRecord(dir, seat string) (Record, bool, error) {
	if dir == "" || seat == "" {
		return Record{}, false, nil
	}
	seatIndexMu.Lock()
	defer seatIndexMu.Unlock()
	idx, names, err := refreshIndexLocked(dir)
	if err != nil {
		return Record{}, false, err
	}
	for i := len(names) - 1; i >= 0; i-- {
		path := filepath.Join(dir, names[i])
		fi := idx.files[names[i]]
		if m := fi.tail; m != nil && m.seat == seat {
			if err := m.resolve(path); err != nil {
				return Record{}, false, err
			}
			if m.rec != nil {
				return *m.rec, true, nil
			}
		}
		for {
			m := fi.phase[seat]
			if m == nil {
				break
			}
			if err := m.resolve(path); err != nil {
				return Record{}, false, err
			}
			if m.rec != nil {
				return *m.rec, true, nil
			}
			// The newest candidate is not a record. Read the file up to it
			// for the one before; remember the answer.
			prev, err := lastPhaseMarkBefore(path, seat, m.off)
			if err != nil {
				return Record{}, false, err
			}
			if prev == nil {
				delete(fi.phase, seat)
			} else {
				fi.phase[seat] = prev
			}
		}
	}
	return Record{}, false, nil
}

// lastPhaseMarkBefore is the newest line before limit in path that moves
// seat's phase, decoded by ReadSeatSince's rule, or nil.
func lastPhaseMarkBefore(path, seat string, limit int64) (*phaseMark, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	key := seatKey(seat)
	r := bufio.NewReaderSize(countingReader{io.LimitReader(f, limit)}, 64*1024)
	var found *phaseMark
	var line []byte
	var off int64
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if len(line) > 0 && bytes.Contains(line, key) {
			if rec, ok := decodeLine(line); ok && rec.Seat == seat && movesPhase(rec) {
				found = &phaseMark{seat: seat, off: off, n: int64(len(line)), checked: true, rec: compact(rec)}
			}
		}
		off += int64(len(line))
		line = line[:0]
		if err == io.EOF {
			return found, nil
		}
		if err != nil {
			return found, err
		}
	}
}
