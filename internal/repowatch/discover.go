// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package repowatch

import (
	"strings"
)

// Seat is one fleet seat the cycle may watch through.
type Seat struct {
	Name    string
	WorkDir string
	Role    string
	Purpose string
}

// SeatLister returns the current fleet seats. Injectable so tests need no
// live registry.
type SeatLister interface {
	List() []Seat
}

// FuncSeatLister adapts a function to SeatLister.
type FuncSeatLister func() []Seat

// List implements SeatLister.
func (f FuncSeatLister) List() []Seat {
	if f == nil {
		return nil
	}
	return f()
}

// IsPOName reports whether a seat name is a product-owner handle.
// Mirrors internal/roles and staff_ops conventions (-po / _po / "po").
func IsPOName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	if n == "po" {
		return true
	}
	if strings.HasSuffix(n, "-po") || strings.HasSuffix(n, "_po") {
		return true
	}
	return strings.Contains(n, "-po-")
}

// WatchTarget is one (PO, workdir, repo) the cycle will poll.
type WatchTarget struct {
	PO      string
	Workdir string
	Repo    string // may be empty until ResolveRepo fills it
	Enabled bool
}

// DiscoverTargets builds the watch set from durable config overrides plus
// live PO seats. Config SetRepo entries win on repo identity; seats supply
// workdirs for POs that have no explicit override. A seat without a workdir
// is skipped. Opt-out is per-repo via Enabled=false on the override.
func DiscoverTargets(cfg Config, seats []Seat) []WatchTarget {
	var out []WatchTarget
	seenRepo := map[string]bool{}
	seenPO := map[string]bool{}

	// Explicit config repos first.
	for _, rc := range cfg.Repos {
		repo := strings.TrimSpace(rc.Repo)
		if repo == "" {
			continue
		}
		key := strings.ToLower(repo)
		if seenRepo[key] {
			continue
		}
		seenRepo[key] = true
		t := WatchTarget{
			PO:      strings.TrimSpace(rc.PO),
			Workdir: strings.TrimSpace(rc.Workdir),
			Repo:    repo,
			Enabled: rc.EnabledOrDefault(),
		}
		out = append(out, t)
		if t.PO != "" {
			seenPO[strings.ToLower(t.PO)] = true
		}
	}

	// Live PO seats that are not already covered by an explicit override.
	for _, s := range seats {
		name := strings.TrimSpace(s.Name)
		wd := strings.TrimSpace(s.WorkDir)
		if name == "" || wd == "" {
			continue
		}
		if !IsPOName(name) && !strings.EqualFold(s.Role, "product-owner") {
			continue
		}
		if seenPO[strings.ToLower(name)] {
			continue
		}
		seenPO[strings.ToLower(name)] = true
		out = append(out, WatchTarget{
			PO:      name,
			Workdir: wd,
			Enabled: true,
		})
	}
	return out
}
