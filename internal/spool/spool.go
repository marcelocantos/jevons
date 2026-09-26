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
	hits, err := seatIndex(dir)
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
	hits, err := seatIndex(dir)
	if err != nil {
		return ""
	}
	return hits[seat]
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

// seatIndex maps a seat to the newest dated log that names it. The
// signature is file name, size, and mtime, so an append rebuilds and a
// quiet poll does not read the spool again.
type indexedSeats struct {
	sig  string
	hits map[string]string
}

var (
	seatIndexMu sync.Mutex
	seatIndexes = map[string]*indexedSeats{}
)

func seatIndex(dir string) (map[string]string, error) {
	names, err := listDayFiles(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	sig, err := daySig(dir, names)
	if err != nil {
		return nil, err
	}
	seatIndexMu.Lock()
	defer seatIndexMu.Unlock()
	if idx, ok := seatIndexes[dir]; ok && idx.sig == sig {
		return idx.hits, nil
	}
	hits := map[string]string{}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := scanSeatNames(path, hits); err != nil {
			return nil, err
		}
	}
	seatIndexes[dir] = &indexedSeats{sig: sig, hits: hits}
	return hits, nil
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

// scanSeatNames records seats named in path. The seat field is at the
// front of each line; the snapshot after it is not decoded.
func scanSeatNames(path string, hits map[string]string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		seat, err := nextSeat(r)
		if seat != "" {
			hits[seat] = path
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func nextSeat(r *bufio.Reader) (string, error) {
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
			return "", err
		}
		if err == io.EOF && len(chunk) == 0 && len(prefix) == 0 {
			return "", io.EOF
		}
		return seatField(prefix), err
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
