// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package spool reads ~/.jevons/spool/events-YYYY-MM-DD.log, the dated
// session log the Oh My Pi sidecar writes (🎯T866). Older dates first.
package spool

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DirEnv overrides the default ~/.jevons/spool (tests and isolates).
const DirEnv = "JEVONS_SPOOL_DIR"

// SidecarProvider reports whether Launch for this provider id talks to
// the Oh My Pi sidecar rather than a vendor CLI (🎯T866.5).
func SidecarProvider(id string) bool {
	switch id {
	case "anthropic", "openai-codex", "cursor", "xai-oauth":
		return true
	default:
		return false
	}
}

// ResumeFromSpool reports whether fail-closed resume for this seat
// reads the dated spool. Native sidecar ids do. Fleet grok stays on
// vendor JSONL until remint (🎯T627.1 / T866.6). omp marks a remint.
func ResumeFromSpool(provider string, omp bool) bool {
	if omp {
		return true
	}
	return SidecarProvider(provider)
}

// Record is one sidecar line. Each names its seat.
type Record struct {
	TS       time.Time       `json:"-"`
	RawTS    string          `json:"ts"`
	Seat     string          `json:"seat"`
	Type     string          `json:"type"`
	Stop     string          `json:"stop,omitempty"`
	Text     string          `json:"text,omitempty"`
	CallID   string          `json:"call_id,omitempty"`
	Name     string          `json:"name,omitempty"`
	Provider string          `json:"provider,omitempty"`
	Model    string          `json:"model,omitempty"`
	CostUSD  *float64        `json:"costUSD,omitempty"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

// Dir is ~/.jevons/spool, or JEVONS_SPOOL_DIR when set.
func Dir() string {
	if p := strings.TrimSpace(os.Getenv(DirEnv)); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".jevons", "spool")
}

// SeatHasHistory reports whether any dated file holds a record for seat.
// It does not decode snapshots. GET /api/agents calls this for every
// running seat, and a turn_end line is the whole agent state (🎯T868).
func SeatHasHistory(dir, seat string) bool {
	if dir == "" || seat == "" {
		return false
	}
	hits, _, err := seatIndex(dir)
	return err == nil && hits[seat] != ""
}

// ReadSeat returns records for seat, older dates first.
func ReadSeat(dir, seat string) ([]Record, error) {
	if dir == "" || seat == "" {
		return nil, nil
	}
	names, err := listDayFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Record
	for _, name := range names {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var rec Record
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				continue
			}
			if rec.Seat != seat {
				continue
			}
			if rec.RawTS != "" {
				if ts, err := time.Parse(time.RFC3339Nano, rec.RawTS); err == nil {
					rec.TS = ts
				} else if ts, err := time.Parse(time.RFC3339, rec.RawTS); err == nil {
					rec.TS = ts
				}
			}
			out = append(out, rec)
		}
		_ = f.Close()
		if err := sc.Err(); err != nil {
			return out, err
		}
	}
	return out, nil
}

// LatestPath is the newest dated log that names seat. Empty when the
// seat has no spool history. Readers use this for mtime, not a vendor
// JSONL (🎯T866.4).
func LatestPath(dir, seat string) string {
	if dir == "" || seat == "" {
		return ""
	}
	hits, _, err := seatIndex(dir)
	if err != nil {
		return ""
	}
	return hits[seat]
}

// LatestSeatTime is the "ts" field of the newest spool line naming seat,
// across all dated files. LatestPath's file is shared by every sidecar
// seat writing that day, so the file's mtime is the day's last write from
// ANY seat, not this one's (🎯T893: all anthropic sidecar seats read back
// the identical age because they share one events-YYYY-MM-DD.log). The
// per-line "ts" field is per-seat; this is the value readers must use for
// recency, not os.Stat on LatestPath's result. The ok=false zero value
// means no readable ts was found for seat, not "just now".
func LatestSeatTime(dir, seat string) (time.Time, bool) {
	if dir == "" || seat == "" {
		return time.Time{}, false
	}
	_, ts, err := seatIndex(dir)
	if err != nil {
		return time.Time{}, false
	}
	t, ok := ts[seat]
	return t, ok
}

// Files returns dated log names, older first.
func Files(dir string) ([]string, error) {
	names, err := listDayFiles(dir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, n := range names {
		paths = append(paths, filepath.Join(dir, n))
	}
	return paths, nil
}

func listDayFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "events-") && strings.HasSuffix(n, ".log") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, nil
}

// seatIndex maps a seat to the newest dated log that names it, and
// separately to the "ts" of that seat's own newest line (🎯T893: the file
// is shared, the ts field is not). The signature is file name, size, and
// mtime, so an append rebuilds and a quiet poll does not read the spool
// again.
type indexedSeats struct {
	sig  string
	hits map[string]string
	ts   map[string]time.Time
}

var (
	seatIndexMu sync.Mutex
	seatIndexes = map[string]*indexedSeats{}
)

func seatIndex(dir string) (map[string]string, map[string]time.Time, error) {
	names, err := listDayFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, map[string]time.Time{}, nil
		}
		return nil, nil, err
	}
	sig, err := daySig(dir, names)
	if err != nil {
		return nil, nil, err
	}
	seatIndexMu.Lock()
	defer seatIndexMu.Unlock()
	if idx, ok := seatIndexes[dir]; ok && idx.sig == sig {
		return idx.hits, idx.ts, nil
	}
	hits := map[string]string{}
	ts := map[string]time.Time{}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := scanSeatNames(path, hits, ts); err != nil {
			return nil, nil, err
		}
	}
	seatIndexes[dir] = &indexedSeats{sig: sig, hits: hits, ts: ts}
	return hits, ts, nil
}

func daySig(dir string, names []string) (string, error) {
	var b strings.Builder
	for _, name := range names {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(fi.Size(), 10))
		b.WriteByte(':')
		b.WriteString(strconv.FormatInt(fi.ModTime().UnixNano(), 10))
		b.WriteByte(';')
	}
	return b.String(), nil
}

// scanSeatNames records, per seat named in path, the file (last one wins
// across files in date order) and the "ts" of that seat's own newest line
// (last-in-file wins within a file, since lines are chronological). Only
// the line prefix is read; the snapshot after it is not decoded.
func scanSeatNames(path string, hits map[string]string, ts map[string]time.Time) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		prefix, err := nextRecordPrefix(r)
		if len(prefix) > 0 {
			if seat := seatField(prefix); seat != "" {
				hits[seat] = path
				if t, ok := tsField(prefix); ok {
					ts[seat] = t
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func nextRecordPrefix(r *bufio.Reader) ([]byte, error) {
	var prefix []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(prefix) < 512 {
			room := 512 - len(prefix)
			if len(chunk) > room {
				prefix = append(prefix, chunk[:room]...)
			} else {
				prefix = append(prefix, chunk...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && err != io.EOF {
			return nil, err
		}
		if err == io.EOF && len(chunk) == 0 && len(prefix) == 0 {
			return nil, io.EOF
		}
		return prefix, err
	}
}

func seatField(prefix []byte) string {
	const key = `"seat":"`
	i := bytes.Index(prefix, []byte(key))
	if i < 0 {
		return ""
	}
	rest := prefix[i+len(key):]
	j := bytes.IndexByte(rest, '"')
	if j <= 0 {
		return ""
	}
	return string(rest[:j])
}

// tsField extracts the "ts" field from a record prefix, parsing it as
// RFC3339 (with or without sub-second precision). ok is false when the
// field is missing or unparseable — callers must not treat that as "now".
func tsField(prefix []byte) (time.Time, bool) {
	const key = `"ts":"`
	i := bytes.Index(prefix, []byte(key))
	if i < 0 {
		return time.Time{}, false
	}
	rest := prefix[i+len(key):]
	j := bytes.IndexByte(rest, '"')
	if j <= 0 {
		return time.Time{}, false
	}
	raw := string(rest[:j])
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	return time.Time{}, false
}
