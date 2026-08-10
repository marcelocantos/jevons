// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Dir is where supervision state lives under the daemon's state dir.
// Two processes read it — the watchdog writes, the daemon reports — and
// neither may assume the other exists.
func Dir(stateDir string) string { return filepath.Join(stateDir, "watchdog") }

func statePath(dir string) string   { return filepath.Join(dir, "state.json") }
func outagesPath(dir string) string { return filepath.Join(dir, "outages.jsonl") }

// LoadState reads the remembered state. A missing file is a healthy
// start, not an error; a malformed one is an error, because silently
// resetting supervision state is how an outage becomes invisible.
func LoadState(dir string) (State, error) {
	b, err := os.ReadFile(statePath(dir))
	if os.IsNotExist(err) {
		return State{}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("supervise: read state: %w", err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return State{}, nil
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, fmt.Errorf("supervise: parse %s: %w", statePath(dir), err)
	}
	return st, nil
}

// SaveState replaces the state file atomically.
func SaveState(dir string, st State) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("supervise: state dir: %w", err)
	}
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("supervise: marshal state: %w", err)
	}
	return writeAtomic(statePath(dir), append(b, '\n'))
}

// Outage is one closed outage: the daemon stopped serving, the
// supervisor acted, serving resumed. Written by the watchdog, read and
// reported by the daemon once it is back up — which is why it is a file
// and not a message.
type Outage struct {
	// ID is the outage's start in RFC3339 nanoseconds, which is unique
	// per outage without needing a random source.
	ID          string    `json:"id"`
	DownSince   time.Time `json:"down_since"`
	RecoveredAt time.Time `json:"recovered_at"`
	Attempts    int       `json:"attempts"`
	// Detail is free text for the owner, e.g. the last restart error.
	Detail string `json:"detail,omitempty"`
	// Reported is set once the owner has been told, so a daemon that
	// restarts twice does not report the same outage twice.
	Reported bool `json:"reported,omitempty"`
}

// Downtime is how long the daemon was not serving.
func (o Outage) Downtime() time.Duration { return o.RecoveredAt.Sub(o.DownSince) }

// Text is what the owner reads. Concrete about what happened and
// explicit that no owner action was needed, because the point of the
// notice is that the machinery, not the owner, noticed.
func (o Outage) Text() string {
	attempts := "1 restart"
	if o.Attempts != 1 {
		attempts = fmt.Sprintf("%d restarts", o.Attempts)
	}
	msg := fmt.Sprintf(
		"daemon outage: the daily jevonsd stopped serving at %s and was down for %s. The watchdog brought it back after %s — no owner action was needed.",
		o.DownSince.Local().Format("15:04:05"), o.Downtime().Round(time.Second), attempts)
	if o.Detail != "" {
		msg += " " + o.Detail
	}
	return msg
}

// AppendOutage records a closed outage. Append-only: the watchdog may be
// killed at any moment, and a partial line is recoverable where a
// half-rewritten file is not.
func AppendOutage(dir string, o Outage) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("supervise: state dir: %w", err)
	}
	b, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("supervise: marshal outage: %w", err)
	}
	f, err := os.OpenFile(outagesPath(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("supervise: open outages: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("supervise: append outage: %w", err)
	}
	return f.Sync()
}

// LoadOutages reads every recorded outage. A truncated final line is
// dropped rather than fatal — the watchdog appends and can be killed
// mid-write, and losing the tail of the record is better than losing the
// whole history to a parse error.
func LoadOutages(dir string) ([]Outage, error) {
	f, err := os.Open(outagesPath(dir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("supervise: open outages: %w", err)
	}
	defer f.Close()

	var out []Outage
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var o Outage
		if err := json.Unmarshal([]byte(line), &o); err != nil {
			continue
		}
		out = append(out, o)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("supervise: read outages: %w", err)
	}
	return out, nil
}

// MarkReported rewrites the log with the named outages flagged as told.
// Rewrite rather than append because this is the only writer of the
// flag and the file is small; the write is atomic.
func MarkReported(dir string, ids map[string]bool) error {
	all, err := LoadOutages(dir)
	if err != nil {
		return err
	}
	var buf strings.Builder
	for _, o := range all {
		if ids[o.ID] {
			o.Reported = true
		}
		b, err := json.Marshal(o)
		if err != nil {
			return fmt.Errorf("supervise: marshal outage: %w", err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	return writeAtomic(outagesPath(dir), []byte(buf.String()))
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("supervise: dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("supervise: temp: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("supervise: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("supervise: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("supervise: close: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("supervise: rename: %w", err)
	}
	return nil
}
