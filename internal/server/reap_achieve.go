// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/marcelocantos/claudia"
	"gopkg.in/yaml.v3"
)

// 🎯T195: after a mission target is achieved on the bullseye ledger, reap
// work agents engaged on that target (TargetID equality) even when their
// terminal report was imperfect or missing. Complements T165 report-path
// reaping. POs / overseer / asides stay; deliberate stop without kill is
// unchanged. Multi-target agents without a matching TargetID stay.

// durableFleetAgentForAchieve mirrors mcpserver.DurableFleetAgent so this
// package does not import mcpserver (cycle). Keep in sync.
func durableFleetAgentForAchieve(name, purpose string, isOverseer func(string) bool) bool {
	if name == "" {
		return true
	}
	if isOverseer != nil && isOverseer(name) {
		return true
	}
	p := strings.TrimSpace(purpose)
	if p == "" {
		p = claudia.PurposeWork
	}
	if p == claudia.PurposeOverseer || p == claudia.PurposeAside {
		return true
	}
	lower := strings.ToLower(name)
	if lower == "po" || strings.HasSuffix(lower, "-po") {
		return true
	}
	return false
}

// ReapWorkAgentsOnTargetAchieve removes non-durable work agents whose
// TargetID matches the achieved target (and their descendant subtrees).
// Returns names removed. No-ops when none engaged or only durable roles match.
func ReapWorkAgentsOnTargetAchieve(reg *claudia.Registry, targetID string, isOverseer func(string) bool) ([]string, error) {
	if reg == nil {
		return nil, fmt.Errorf("agent registry not available")
	}
	want := NormalizeTargetID(targetID)
	if want == "" {
		return nil, fmt.Errorf("target_id is required")
	}
	engaged := AgentsEngagedOnTarget(reg, want)
	if len(engaged) == 0 {
		return nil, nil
	}
	// Filter durable roles (PO/overseer/aside). Owner engagement stop may
	// still kill more broadly via stopEngagement; auto-achieve reaping is
	// implementer hygiene only.
	var toReap []string
	for _, name := range engaged {
		def := reg.Def(name)
		if def == nil {
			continue
		}
		if durableFleetAgentForAchieve(name, def.Purpose, isOverseer) {
			continue
		}
		toReap = append(toReap, name)
	}
	if len(toReap) == 0 {
		return nil, nil
	}
	sort.Strings(toReap)
	var removed []string
	for _, name := range toReap {
		if reg.Def(name) == nil {
			continue
		}
		desc := reg.Descendants(name)
		for i := len(desc) - 1; i >= 0; i-- {
			if err := reg.Remove(desc[i]); err != nil {
				return removed, fmt.Errorf("reap achieve descendant %q of %q: %w", desc[i], name, err)
			}
			removed = append(removed, desc[i])
		}
		if err := reg.Remove(name); err != nil {
			return removed, fmt.Errorf("reap achieve %q: %w", name, err)
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// listAchievedTargetIDs reads a bullseye ledger and returns target IDs with
// status=achieved (normalized). set_aside is not treated as achieve-reap.
func listAchievedTargetIDs(ledgerPath string) (map[string]bool, error) {
	out := make(map[string]bool)
	if ledgerPath == "" {
		return out, nil
	}
	data, err := os.ReadFile(ledgerPath)
	if err != nil {
		return nil, err
	}
	var doc bullseyeLedger
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse ledger: %w", err)
	}
	for id, t := range doc.Targets {
		if strings.EqualFold(strings.TrimSpace(t.Status), "achieved") {
			nid := NormalizeTargetID(id)
			if nid != "" {
				out[nid] = true
			}
		}
	}
	return out, nil
}

// newlyAchievedTargetIDs returns ids present in curr but not prev.
func newlyAchievedTargetIDs(prev, curr map[string]bool) []string {
	if len(curr) == 0 {
		return nil
	}
	var out []string
	for id := range curr {
		if prev == nil || !prev[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// seedAchievedFromLedger records the current achieved set without reaping
// (boot / first watch bind). Historical achieves must not mass-kill agents.
func (s *Server) seedAchievedFromLedger(ledgerPath string) {
	if s == nil || ledgerPath == "" {
		return
	}
	curr, err := listAchievedTargetIDs(ledgerPath)
	if err != nil {
		slog.Debug("T195 seed achieved set failed", "ledger", ledgerPath, "err", err)
		return
	}
	s.mu.Lock()
	s.achievedTargetsSeen = curr
	s.mu.Unlock()
}

// maybeReapOnLedgerAchieve diffs the ledger achieved set against the last
// seen set and reaps work agents engaged on newly achieved targets (🎯T195).
func (s *Server) maybeReapOnLedgerAchieve(ledgerPath string) {
	if s == nil || ledgerPath == "" {
		return
	}
	curr, err := listAchievedTargetIDs(ledgerPath)
	if err != nil {
		slog.Debug("T195 list achieved failed", "ledger", ledgerPath, "err", err)
		return
	}

	s.mu.Lock()
	prev := s.achievedTargetsSeen
	if prev == nil {
		// First observation: seed only (no reap on cold start).
		s.achievedTargetsSeen = curr
		s.mu.Unlock()
		return
	}
	newly := newlyAchievedTargetIDs(prev, curr)
	s.achievedTargetsSeen = curr
	reg := s.registry
	overseer := s.overseerName
	if overseer == "" {
		overseer = defaultOverseerName
	}
	s.mu.Unlock()

	if reg == nil || len(newly) == 0 {
		return
	}
	isO := func(n string) bool { return n == overseer }
	var anyRemoved bool
	for _, tid := range newly {
		removed, err := ReapWorkAgentsOnTargetAchieve(reg, tid, isO)
		if err != nil {
			slog.Warn("T195 reap on achieve failed", "target_id", tid, "err", err)
			continue
		}
		if len(removed) > 0 {
			anyRemoved = true
			slog.Info("auto-reaped work agents after target achieve",
				"target_id", tid, "removed", removed)
		}
	}
	if anyRemoved {
		s.NotifyAgentsChanged()
	}
}
