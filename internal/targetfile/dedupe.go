// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package targetfile implements kickoff engagement gates for agent_start /
// frontier play (🎯T222 engagement residual, kept under 🎯T226).
//
// Near-duplicate filing attach was removed by 🎯T226: explicit file always
// allocates a new bullseye id (pre-T222 file behaviour).
package targetfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// IsOpenStatus reports whether a bullseye status is still an open leaf.
func IsOpenStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	// Empty status treated as open (legacy / incomplete rows).
	if s == "" {
		return true
	}
	return s == "identified" || s == "converging"
}

// IsClosedStatus reports set_aside / achieved (not kickable).
func IsClosedStatus(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	return s == "set_aside" || s == "set-aside" || s == "achieved"
}

// --- Ledger load (status lookup for kickoff) ---

type ledgerDoc struct {
	Targets map[string]ledgerTarget `yaml:"targets"`
}

type ledgerTarget struct {
	Status string `yaml:"status"`
}

// LookupTargetStatus returns the status of id in a ledger YAML document.
func LookupTargetStatus(data []byte, id string) (status string, ok bool) {
	id = strings.TrimSpace(strings.TrimPrefix(id, "🎯"))
	if id == "" {
		return "", false
	}
	var doc ledgerDoc
	if err := yaml.Unmarshal(data, &doc); err != nil || doc.Targets == nil {
		return "", false
	}
	t, found := doc.Targets[id]
	if !found {
		return "", false
	}
	return t.Status, true
}

// DiscoverLedgerPath walks cwd and parents for bullseye.yaml (in-repo).
// Does not shell to bullseye CLI (hermetic-friendly; external shadow not
// covered — residual when only external ledger exists).
func DiscoverLedgerPath(cwd string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", fmt.Errorf("empty cwd")
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		cand := filepath.Join(dir, "bullseye.yaml")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no bullseye.yaml under %s", abs)
}

// LoadTargetStatusFromCwd looks up target status in the nearest ledger.
func LoadTargetStatusFromCwd(cwd, targetID string) (status string, ok bool) {
	path, err := DiscoverLedgerPath(cwd)
	if err != nil {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return LookupTargetStatus(data, targetID)
}

// --- Engagement / kickoff gates ---

// KickoffDecision is the result of gating frontier play / agent_start.
type KickoffDecision struct {
	// Allow is true when kickoff/spawn may proceed.
	Allow bool
	// Reason is a stable code: ok | already_engaged | set_aside | achieved | closed.
	Reason string
	// Engaged names of existing implementers when Reason=already_engaged.
	Engaged []string
	// Message is human-readable for tool/UI errors.
	Message string
}

// GateKickoff decides whether play/kickoff or a new work agent for targetID
// is allowed. force=true is the deliberate-split residual (owner override).
//
// engagedOthers: work agents already bound to targetID excluding the agent
// about to resume (same name). status is bullseye status when known (empty
// skips status check).
func GateKickoff(status string, engagedOthers []string, force bool) KickoffDecision {
	if force {
		return KickoffDecision{Allow: true, Reason: "ok", Message: "force override"}
	}
	if IsClosedStatus(status) {
		s := strings.ToLower(strings.TrimSpace(status))
		reason := s
		if s == "set-aside" {
			reason = "set_aside"
		}
		return KickoffDecision{
			Allow:   false,
			Reason:  reason,
			Message: fmt.Sprintf("target is %s — not available for kickoff (focus or re-open first)", reason),
		}
	}
	if len(engagedOthers) > 0 {
		names := append([]string{}, engagedOthers...)
		sort.Strings(names)
		return KickoffDecision{
			Allow:   false,
			Reason:  "already_engaged",
			Engaged: names,
			Message: fmt.Sprintf("target already has engaged implementer(s): %s — focus existing engagement or stop first",
				strings.Join(names, ", ")),
		}
	}
	return KickoffDecision{Allow: true, Reason: "ok"}
}
