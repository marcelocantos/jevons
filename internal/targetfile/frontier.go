// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package targetfile

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// 🎯T254.1: hermetic frontier-leaf load for the daemon's unattended
// consumption sweep. Same readiness rule as the RHS frontier table
// (internal/server): active target (identified|converging) whose depends_on
// are all achieved/set_aside; unknown deps block conservatively. No bullseye
// CLI shell-out — nearest in-repo ledger only (external shadow residual).
//
// 🎯T337: graph-ready leaves that only unblock via set_aside deps still appear
// here (RHS frontier semantics), but carry SetAsideDeps so the consume
// classifier can park them (T7→T5 class) instead of auto-spawning.
//
// 🎯T338: parents with active hierarchical children (T10 with T10.2–T10.6)
// still appear when their own depends_on are done, but carry ActiveChildren
// so unattended consume parks them (true work leaves only).

// FrontierLeaf is one ready frontier target from a ledger.
type FrontierLeaf struct {
	ID         string
	Name       string
	Context    string
	Tags       []string
	Acceptance []string
	// Cost / Value are portfolio estimates from the ledger (0 when omitted).
	Cost  float64
	Value float64
	// SetAsideDeps lists depends_on that are set_aside (not achieved). Graph
	// still treats them as done; unattended consume must not auto-spawn.
	SetAsideDeps []string
	// ActiveChildren lists hierarchical descendants (id + ".") that are still
	// identified|converging. Unattended consume parks the parent (🎯T338).
	ActiveChildren []string
}

type frontierLedgerTarget struct {
	Name       string   `yaml:"name"`
	Status     string   `yaml:"status"`
	DependsOn  []string `yaml:"depends_on"`
	Context    string   `yaml:"context"`
	Tags       []string `yaml:"tags"`
	Acceptance []string `yaml:"acceptance"`
	Cost       float64  `yaml:"cost"`
	Value      float64  `yaml:"value"`
}

type frontierLedgerDoc struct {
	Targets map[string]frontierLedgerTarget `yaml:"targets"`
}

func isFrontierActiveStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "identified" || s == "converging"
}

// HierarchicalChildOf reports whether childID is a hierarchical descendant of
// parentID (T10.2 of T10, T10.2.1 of T10.2). Digit-safe: T10 is not a child of T1.
func HierarchicalChildOf(parentID, childID string) bool {
	parentID = strings.TrimSpace(parentID)
	childID = strings.TrimSpace(childID)
	if parentID == "" || childID == "" || parentID == childID {
		return false
	}
	return strings.HasPrefix(childID, parentID+".")
}

// FrontierLeaves extracts ready leaves from ledger YAML, ordered by natural
// target-id compare (T2 < T10 < T10.2).
func FrontierLeaves(data []byte) ([]FrontierLeaf, error) {
	var doc frontierLedgerDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse ledger: %w", err)
	}
	if doc.Targets == nil {
		return nil, nil
	}
	done := func(id string) bool {
		t, ok := doc.Targets[id]
		return ok && IsClosedStatus(t.Status)
	}
	setAside := func(id string) bool {
		t, ok := doc.Targets[id]
		return ok && IsSetAsideStatus(t.Status)
	}
	// Pre-index active hierarchical ids for ActiveChildren scans (🎯T338).
	var activeIDs []string
	for id, t := range doc.Targets {
		if isFrontierActiveStatus(t.Status) {
			activeIDs = append(activeIDs, id)
		}
	}
	sort.Strings(activeIDs)

	var leaves []FrontierLeaf
	for id, t := range doc.Targets {
		if !isFrontierActiveStatus(t.Status) {
			continue
		}
		ready := true
		var setAsideDeps []string
		for _, dep := range t.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if !done(dep) {
				ready = false
				break
			}
			if setAside(dep) {
				setAsideDeps = append(setAsideDeps, dep)
			}
		}
		if !ready {
			continue
		}
		var activeChildren []string
		for _, other := range activeIDs {
			if HierarchicalChildOf(id, other) {
				activeChildren = append(activeChildren, other)
			}
		}
		leaves = append(leaves, FrontierLeaf{
			ID:             id,
			Name:           strings.TrimSpace(t.Name),
			Context:        strings.TrimSpace(t.Context),
			Tags:           t.Tags,
			Acceptance:     t.Acceptance,
			Cost:           t.Cost,
			Value:          t.Value,
			SetAsideDeps:   setAsideDeps,
			ActiveChildren: activeChildren,
		})
	}
	sort.Slice(leaves, func(i, j int) bool {
		return targetIDNaturalLess(leaves[i].ID, leaves[j].ID)
	})
	return leaves, nil
}

// LoadFrontierLeavesFromCwd discovers the nearest in-repo ledger and returns
// its ready leaves plus the ledger path used.
func LoadFrontierLeavesFromCwd(cwd string) ([]FrontierLeaf, string, error) {
	path, err := DiscoverLedgerPath(cwd)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path, err
	}
	leaves, err := FrontierLeaves(data)
	return leaves, path, err
}

// targetIDNaturalLess orders bullseye ids by alternating digit / non-digit
// runs; digit runs compare by integer magnitude (T2 < T10 < T10.2).
func targetIDNaturalLess(a, b string) bool {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		aDig := a[ia] >= '0' && a[ia] <= '9'
		bDig := b[ib] >= '0' && b[ib] <= '9'
		if aDig && bDig {
			ea, eb := ia, ib
			for ea < len(a) && a[ea] >= '0' && a[ea] <= '9' {
				ea++
			}
			for eb < len(b) && b[eb] >= '0' && b[eb] <= '9' {
				eb++
			}
			da := strings.TrimLeft(a[ia:ea], "0")
			db := strings.TrimLeft(b[ib:eb], "0")
			if len(da) != len(db) {
				return len(da) < len(db)
			}
			if da != db {
				return da < db
			}
			ia, ib = ea, eb
			continue
		}
		if a[ia] != b[ib] {
			return a[ia] < b[ib]
		}
		ia++
		ib++
	}
	return len(a)-ia < len(b)-ib
}
