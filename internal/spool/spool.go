// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package spool reads ~/.jevons/spool/events-YYYY-MM-DD.log, the dated
// session log the Oh My Pi sidecar writes (🎯T866). Older dates first.
package spool

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirEnv overrides the default ~/.jevons/spool (tests and isolates).
const DirEnv = "JEVONS_SPOOL_DIR"

// SidecarProvider reports whether Launch for this provider id talks to
// the Oh My Pi sidecar rather than a vendor CLI (🎯T866.5).
func SidecarProvider(id string) bool {
	switch id {
	case "anthropic", "openai-codex", "cursor", "xai-oauth", "grok":
		return true
	default:
		return false
	}
}

// ResumeFromSpool reports whether fail-closed resume for this seat
// reads the dated spool. Subscription fleet ids — including grok,
// claude, and codex — go through the sidecar (🎯T866.5 / T866.6).
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
func SeatHasHistory(dir, seat string) bool {
	recs, err := ReadSeat(dir, seat)
	return err == nil && len(recs) > 0
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
	names, err := listDayFiles(dir)
	if err != nil {
		return ""
	}
	var last string
	for _, name := range names {
		path := filepath.Join(dir, name)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		found := false
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
			if rec.Seat == seat {
				found = true
				break
			}
		}
		_ = f.Close()
		if found {
			last = path
		}
	}
	return last
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
	return names, nil
}
