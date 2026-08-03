// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package targetfile implements filing dedupe and kickoff engagement gates
// for 🎯T222: near-duplicate bullseye targets must not allocate a second id,
// and play/kickoff must not spawn a second implementer on an engaged or
// closed target.
package targetfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// OpenLeaf is an active (or any) ledger row used for near-duplicate matching.
type OpenLeaf struct {
	ID         string
	Name       string
	Acceptance []string
	Status     string
	// MissionKey is an explicit same-mission key when present on the leaf
	// (from context tag or ledger field). Empty = not keyed.
	MissionKey string
}

// Proposal is a candidate track payload before bullseye allocates an id.
type Proposal struct {
	Name       string
	Acceptance []string
	// MissionKey optional explicit same-mission key (e.g. from context).
	MissionKey string
	// Force allows a deliberate split (owner override residual).
	Force bool
}

// Duplicate names an existing open leaf that matches a proposal.
type Duplicate struct {
	ExistingID string
	Reason     string // exact_name | near_name | mission_key | acceptance
}

// FindDuplicate returns a near-duplicate open leaf for the proposal, or nil.
// When p.Force is true, always returns nil (deliberate split residual).
// Only open (identified/converging) leaves are considered — set_aside and
// achieved do not block a new filing.
func FindDuplicate(open []OpenLeaf, p Proposal) *Duplicate {
	if p.Force {
		return nil
	}
	name := NormalizeText(p.Name)
	if name == "" && strings.TrimSpace(p.MissionKey) == "" {
		return nil
	}
	acc := NormalizeText(strings.Join(p.Acceptance, " "))
	mk := NormalizeText(p.MissionKey)

	// Prefer explicit mission_key match first (strongest signal).
	if mk != "" {
		for _, leaf := range open {
			if !IsOpenStatus(leaf.Status) {
				continue
			}
			if NormalizeText(leaf.MissionKey) == mk {
				return &Duplicate{ExistingID: leaf.ID, Reason: "mission_key"}
			}
		}
	}

	// Exact normalized name.
	if name != "" {
		for _, leaf := range open {
			if !IsOpenStatus(leaf.Status) {
				continue
			}
			if NormalizeText(leaf.Name) == name {
				return &Duplicate{ExistingID: leaf.ID, Reason: "exact_name"}
			}
		}
		// Near name: containment, long-token share, or multi-token overlap.
		for _, leaf := range open {
			if !IsOpenStatus(leaf.Status) {
				continue
			}
			en := NormalizeText(leaf.Name)
			if en == "" {
				continue
			}
			if strings.Contains(en, name) || strings.Contains(name, en) ||
				shareLongToken(name, en, 12) || significantTokenOverlap(name, en, 3, 4) {
				return &Duplicate{ExistingID: leaf.ID, Reason: "near_name"}
			}
		}
	}

	// Acceptance near-match when both non-empty (secondary).
	if acc != "" {
		for _, leaf := range open {
			if !IsOpenStatus(leaf.Status) {
				continue
			}
			la := NormalizeText(strings.Join(leaf.Acceptance, " "))
			if la == "" {
				continue
			}
			if la == acc || strings.Contains(la, acc) || strings.Contains(acc, la) ||
				shareLongToken(acc, la, 16) || significantTokenOverlap(acc, la, 3, 4) {
				return &Duplicate{ExistingID: leaf.ID, Reason: "acceptance"}
			}
		}
	}
	return nil
}

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

// NormalizeText lowercases, collapses whitespace, strips leading 🎯.
func NormalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "🎯")
	return strings.Join(strings.Fields(s), " ")
}

// ExtractMissionKey pulls an explicit same-mission key from free-form context.
// Recognizes "mission_key:FOO", "mission_key=FOO", "same_mission:FOO".
func ExtractMissionKey(context string) string {
	ctx := strings.TrimSpace(context)
	if ctx == "" {
		return ""
	}
	// Scan tokens and lines for key=value / key:value.
	for _, line := range strings.Split(ctx, "\n") {
		line = strings.TrimSpace(line)
		for _, tok := range strings.Fields(line) {
			tok = strings.Trim(tok, ".,;()[]\"'")
			lower := strings.ToLower(tok)
			for _, prefix := range []string{"mission_key:", "mission_key=", "same_mission:", "same_mission="} {
				if strings.HasPrefix(lower, prefix) {
					// Preserve original case of value after prefix length.
					val := strings.TrimSpace(tok[len(prefix):])
					return val
				}
			}
		}
	}
	return ""
}

func shareLongToken(a, b string, minLen int) bool {
	ta := strings.Fields(a)
	tb := strings.Fields(b)
	set := map[string]struct{}{}
	for _, t := range ta {
		if len(t) >= minLen {
			set[t] = struct{}{}
		}
	}
	for _, t := range tb {
		if _, ok := set[t]; ok {
			return true
		}
	}
	return false
}

// significantTokenOverlap is true when a and b share at least minShared
// tokens of length >= minLen (after dropping stopwords). Catches same-
// mission filings with different wording (T220/T221 incident).
func significantTokenOverlap(a, b string, minShared, minLen int) bool {
	if minShared <= 0 {
		minShared = 3
	}
	if minLen <= 0 {
		minLen = 4
	}
	sa := significantTokens(a, minLen)
	sb := significantTokens(b, minLen)
	if len(sa) == 0 || len(sb) == 0 {
		return false
	}
	n := 0
	for t := range sa {
		if _, ok := sb[t]; ok {
			n++
			if n >= minShared {
				return true
			}
		}
	}
	return false
}

var nameStopwords = map[string]struct{}{
	"the": {}, "a": {}, "an": {}, "and": {}, "or": {}, "of": {}, "to": {},
	"for": {}, "in": {}, "on": {}, "is": {}, "are": {}, "as": {}, "with": {},
	"from": {}, "that": {}, "this": {}, "by": {}, "be": {}, "not": {},
}

func significantTokens(s string, minLen int) map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range strings.Fields(s) {
		t = strings.Trim(t, ".,;:()[]\"'-")
		if len(t) < minLen {
			continue
		}
		if _, stop := nameStopwords[t]; stop {
			continue
		}
		out[t] = struct{}{}
	}
	return out
}

// --- Ledger load (hermetic: pass YAML bytes; production reads file) ---

type ledgerDoc struct {
	Targets map[string]ledgerTarget `yaml:"targets"`
}

type ledgerTarget struct {
	Name        string   `yaml:"name"`
	Status      string   `yaml:"status"`
	Acceptance  []string `yaml:"acceptance"`
	Context     string   `yaml:"context"`
	MissionKey  string   `yaml:"mission_key"`
}

// LoadOpenLeavesFromYAML parses bullseye.yaml bytes into open leaves
// (identified/converging only). MissionKey from field or context tag.
func LoadOpenLeavesFromYAML(data []byte) ([]OpenLeaf, error) {
	var doc ledgerDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse ledger: %w", err)
	}
	if doc.Targets == nil {
		return nil, nil
	}
	ids := make([]string, 0, len(doc.Targets))
	for id := range doc.Targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]OpenLeaf, 0, len(ids))
	for _, id := range ids {
		t := doc.Targets[id]
		if !IsOpenStatus(t.Status) {
			continue
		}
		mk := strings.TrimSpace(t.MissionKey)
		if mk == "" {
			mk = ExtractMissionKey(t.Context)
		}
		out = append(out, OpenLeaf{
			ID:         id,
			Name:       t.Name,
			Acceptance: append([]string{}, t.Acceptance...),
			Status:     t.Status,
			MissionKey: mk,
		})
	}
	return out, nil
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

// LoadOpenLeavesFromCwd discovers bullseye.yaml under cwd and loads open leaves.
func LoadOpenLeavesFromCwd(cwd string) ([]OpenLeaf, error) {
	path, err := DiscoverLedgerPath(cwd)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadOpenLeavesFromYAML(data)
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

// AttachMessage formats the product response when filing attaches to an
// existing id instead of allocating a new one.
func AttachMessage(dup *Duplicate, name string) string {
	if dup == nil {
		return ""
	}
	id := strings.TrimPrefix(dup.ExistingID, "🎯")
	return fmt.Sprintf(
		"Attached to existing 🎯%s — %s (near-duplicate: %s; no new id allocated)\n__TARGET_FILED__:%s\n",
		id, name, dup.Reason, id,
	)
}
