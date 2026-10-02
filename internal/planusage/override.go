// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Override is the owner's verdict on a plan's band (🎯T948). A plan that
// reads hot or exhausted can still be the right place for the fleet — the
// owner can reset it and cannot reset the others — and the thresholds have
// no way to know that. While a plan carries an override, its bars paint in
// the override's band, no seat is moved or parked off it, and it stays a
// destination. The readings themselves are untouched: the bars still show
// what has been spent.
type Override struct {
	Band WeeklyBand `json:"band"`
	// Reason is free text, shown verbatim on the plan's "?" in the cockpit.
	Reason string    `json:"reason"`
	SetBy  string    `json:"set_by,omitempty"`
	SetAt  time.Time `json:"set_at"`
}

// OverrideFile is the overrides' durable home under the state dir.
const OverrideFile = "plan-overrides.json"

// OverrideStore holds the owner's overrides, keyed by plan (claude, codex,
// grok, cursor).
type OverrideStore struct {
	path     string
	mu       sync.Mutex
	last     map[string]Override
	onChange func()
}

// NewOverrideStore reads overrides from path. An empty path stores none.
func NewOverrideStore(path string) *OverrideStore {
	return &OverrideStore{path: path}
}

// OnChange registers f to run after a Set or Clear (the cockpit re-fans).
func (o *OverrideStore) OnChange(f func()) {
	o.mu.Lock()
	o.onChange = f
	o.mu.Unlock()
}

// Load reads the overrides. A missing file is none; a file that cannot be
// parsed is an error.
func (o *OverrideStore) Load() (map[string]Override, error) {
	if o == nil || o.path == "" {
		return map[string]Override{}, nil
	}
	b, err := os.ReadFile(o.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Override{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("plan overrides %s: %w", o.path, err)
	}
	var m map[string]Override
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("plan overrides %s: %w", o.path, err)
	}
	for plan, ov := range m {
		if ov.Band == "" || strings.TrimSpace(ov.Reason) == "" {
			return nil, fmt.Errorf("plan overrides %s: %s needs a band and a reason", o.path, plan)
		}
	}
	return m, nil
}

// current is Load, falling back to the last good read when the file has
// gone bad: an owner's override does not lapse because of a typo. The
// error is logged, not swallowed.
func (o *OverrideStore) current() map[string]Override {
	m, err := o.Load()
	o.mu.Lock()
	defer o.mu.Unlock()
	if err != nil {
		slog.Error("plan overrides unreadable; keeping the last good set (🎯T948)", "err", err)
		return o.last
	}
	o.last = m
	return m
}

// Set overrides plan's band.
func (o *OverrideStore) Set(plan string, ov Override) error {
	return o.update(func(m map[string]Override) { m[strings.ToLower(plan)] = ov })
}

// Clear removes plan's override.
func (o *OverrideStore) Clear(plan string) error {
	return o.update(func(m map[string]Override) { delete(m, strings.ToLower(plan)) })
}

func (o *OverrideStore) update(change func(map[string]Override)) error {
	if o == nil || o.path == "" {
		return errors.New("plan overrides need a state directory")
	}
	m, err := o.Load()
	if err != nil {
		return err
	}
	change(m)
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.path), 0o700); err != nil {
		return err
	}
	tmp := o.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, o.path); err != nil {
		return err
	}
	o.mu.Lock()
	o.last = m
	f := o.onChange
	o.mu.Unlock()
	if f != nil {
		f()
	}
	return nil
}

// Apply marks each overridden plan's backend in snap. It copies what it
// changes.
func (o *OverrideStore) Apply(snap Snapshot) Snapshot {
	m := o.current()
	if len(m) == 0 || len(snap.Backends) == 0 {
		return snap
	}
	out := snap
	out.Backends = make([]Backend, len(snap.Backends))
	copy(out.Backends, snap.Backends)
	for i := range out.Backends {
		if ov, ok := m[strings.ToLower(out.Backends[i].Provider)]; ok {
			ov := ov
			out.Backends[i].Override = &ov
		}
	}
	return out
}

// IsDestBandOverride reports whether an override band leaves the plan a
// place seats stay on and may be sent to.
func IsDestBandOverride(b WeeklyBand) bool {
	switch b {
	case BandOK, BandUnder, BandLocked:
		return true
	}
	return false
}

// IsKeepOffBandOverride reports whether an override band is one the owner
// sets to keep seats off a plan (🎯T987): exhausted, the shape of the
// 2026-09-30 "quota is dangerously low" override.
func IsKeepOffBandOverride(b WeeklyBand) bool {
	return b == BandExhausted
}

// OwnerKeepOffReason is the exclusion a resolver cites for a plan whose
// owner override is not a dest band: "owner override exhausted: <reason>".
// Empty when the plan carries no override, or one seats may go to.
func OwnerKeepOffReason(be Backend) string {
	if be.Override == nil || IsDestBandOverride(be.Override.Band) {
		return ""
	}
	why := "owner override " + string(be.Override.Band)
	if r := strings.TrimSpace(be.Override.Reason); r != "" {
		why += ": " + r
	}
	return why
}
