// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ConfigFileName is the durable policy override, alongside budget.json.
const ConfigFileName = "capacity.json"

// ConfigPath returns the policy path under a state dir.
func ConfigPath(stateDir string) string { return filepath.Join(stateDir, ConfigFileName) }

// MarshalJSON emits the duration in time.Duration string form ("5m").
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON accepts a duration string ("5m") or nanoseconds.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		dur, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("bad duration %q: %w", s, err)
		}
		*d = Duration(dur)
		return nil
	}
	var ns int64
	if err := json.Unmarshal(data, &ns); err != nil {
		return fmt.Errorf("bad duration %s", data)
	}
	*d = Duration(ns)
	return nil
}

// LoadPolicy layers path's fields over the shipped defaults. A missing file
// yields the defaults and is not an error; a malformed file IS an error —
// silently falling back would mask a typo'd policy exactly when the owner
// most needs the one they wrote.
func LoadPolicy(path string) (*Policy, error) {
	pol := DefaultPolicy()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return pol, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, pol); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return pol.normalized(), nil
}

// Save writes the policy (pretty-printed) for a single-writer daemon.
func (p *Policy) Save(path string) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// normalized repairs values that would silently disable the policy: an
// out-of-range fraction, or a degrade line below the owner reserve (which
// would make degradation unreachable).
func (p *Policy) normalized() *Policy {
	clamp := func(f, def float64) float64 {
		if f < 0 || f > 1 {
			return def
		}
		return f
	}
	def := DefaultPolicy()
	p.OwnerReserveFraction = clamp(p.OwnerReserveFraction, def.OwnerReserveFraction)
	p.DegradeFraction = clamp(p.DegradeFraction, def.DegradeFraction)
	if p.DegradeFraction < p.OwnerReserveFraction {
		p.DegradeFraction = p.OwnerReserveFraction
	}
	if p.RetryAfter <= 0 {
		p.RetryAfter = def.RetryAfter
	}
	if p.MaxConcurrentBackground < 0 {
		p.MaxConcurrentBackground = def.MaxConcurrentBackground
	}
	return p
}
