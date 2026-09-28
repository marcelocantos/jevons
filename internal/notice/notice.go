// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package notice is the durable, queryable terminal-outcome inbox (🎯T254.4).
//
// A worker's terminal report already carries a typed envelope (🎯T509) and a
// silent-decision ledger (🎯T536.1); that machinery is not duplicated here.
// This package's job is narrower: when a work agent's stored terminal report
// is a finish-report or scout-report, extract its outcome shape (done /
// blocked / needs-design / other) and append one small structured record to
// a durable per-parent inbox file, so the parent PO and overseer have a
// queryable notice surface instead of having to re-read raw transcript prose
// for every worker that finished this session.
//
// Free text is not replaced: the full report remains readable via
// jevons_agent_report_read. This is structure added on top (acceptance 3).
package notice

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// Outcome is the coarse terminal shape a notice records.
type Outcome string

const (
	OutcomeDone        Outcome = "done"
	OutcomeBlocked     Outcome = "blocked"
	OutcomeNeedsDesign Outcome = "needs-design"
	OutcomeOther       Outcome = "other"
)

// Notice is one durable structured record of a worker's terminal outcome.
type Notice struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	Agent     string    `json:"agent"`
	Parent    string    `json:"parent"`
	Kind      string    `json:"kind"`    // envelope.Kind: finish-report | scout-report
	Outcome   Outcome   `json:"outcome"` // done | blocked | needs-design | other
	Target    string    `json:"target,omitempty"`
	SHA       string    `json:"sha,omitempty"`
	GateID    string    `json:"gate_id,omitempty"`
	Verdict   string    `json:"verdict,omitempty"`
	HasOracle bool      `json:"has_oracle"`
	HasRisk   bool      `json:"has_risk"`
	Summary   string    `json:"summary"` // first non-empty prose line, elided
}

// FromReport extracts a Notice from a stored terminal report, or ok=false
// when the text is not a typed finish-report / scout-report (envelope.Parse
// returns nil, or the kind is not terminal) — the caller in that case has
// nothing structured to record and free-text prose remains the only surface,
// which is unchanged behaviour, not a regression.
func FromReport(agent, parent, text string, at time.Time) (n Notice, ok bool) {
	m, _ := envelope.Parse(text)
	if m == nil {
		return Notice{}, false
	}
	switch m.Kind {
	case envelope.KindFinishReport, envelope.KindScoutReport:
	default:
		return Notice{}, false
	}
	n = Notice{
		Time:      at,
		Agent:     strings.TrimSpace(agent),
		Parent:    strings.TrimSpace(parent),
		Kind:      m.Kind.String(),
		Target:    strings.TrimSpace(m.Target),
		SHA:       strings.TrimSpace(m.SHA),
		GateID:    strings.TrimSpace(m.GateID),
		Verdict:   m.Verdict.String(),
		HasOracle: m.HasOracle(),
		HasRisk:   m.HasRisk(),
		Outcome:   classifyOutcome(m, text),
		Summary:   summaryLine(m.Payload, text),
	}
	n.ID = NewID(at, agent, text)
	return n, true
}

// classifyOutcome derives done / blocked / needs-design / other. done
// requires the finish-report/scout-report to also carry an executable oracle
// or explicit accepted-risk (🎯T31/T31.1) — a claimed-done with neither is
// not recorded as done here; that is exactly the ambiguity this inbox exists
// to surface, not paper over.
func classifyOutcome(m *envelope.Message, raw string) Outcome {
	low := strings.ToLower(raw)
	switch {
	case strings.Contains(low, "needs-design") || strings.Contains(low, "needs design") ||
		strings.Contains(low, "design-gated") || strings.Contains(low, "parked-for-design"):
		return OutcomeNeedsDesign
	case strings.Contains(low, "blocked"):
		return OutcomeBlocked
	}
	if m.Kind == envelope.KindFinishReport && (m.HasOracle() || m.HasRisk()) {
		return OutcomeDone
	}
	return OutcomeOther
}

// summaryLine returns the first non-empty payload line (falling back to the
// raw text), trimmed and bounded so the inbox listing stays scannable.
func summaryLine(payload, raw string) string {
	src := strings.TrimSpace(payload)
	if src == "" {
		src = strings.TrimSpace(raw)
	}
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		if len(line) > 240 {
			line = line[:240] + "…"
		}
		return line
	}
	return ""
}

// NewID mints a stable, sortable id: time prefix + short content hash, so two
// notices in the same second from the same agent still get distinct ids.
func NewID(at time.Time, agent, text string) string {
	return at.UTC().Format("20060102T150405.000000000Z") + "-" + shortHash(agent+"\x00"+text)
}

func shortHash(s string) string {
	// FNV-1a, good enough for a display disambiguator, not a security hash.
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	const hex = "0123456789abcdef"
	buf := make([]byte, 8)
	for i := 0; i < 8; i++ {
		buf[i] = hex[(h>>(uint(i)*4))&0xf]
	}
	return string(buf)
}

func safeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return "", os.ErrInvalid
	}
	return name, nil
}

func inboxPath(stateDir, parent string) (string, error) {
	p, err := safeName(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(stateDir, "notices", p+".jsonl"), nil
}

// Append persists n to the parent's durable inbox file (append-only JSONL).
// stateDir is the daemon state root; a blank parent falls back to "unowned"
// so a report from a parentless/degraded agent is still recorded, not
// dropped on the floor.
func Append(stateDir string, n Notice) error {
	parent := n.Parent
	if parent == "" {
		parent = "unowned"
	}
	path, err := inboxPath(stateDir, parent)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// List reads every notice recorded for parent, oldest first. A missing inbox
// file (parent never received a structured notice) returns an empty slice,
// not an error — that mirrors agentreport.List's "missing agent is empty".
func List(stateDir, parent string) ([]Notice, error) {
	path, err := inboxPath(stateDir, parent)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Notice
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var n Notice
		if err := json.Unmarshal([]byte(line), &n); err != nil {
			continue // one corrupt line must not sink the rest of the inbox
		}
		out = append(out, n)
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}
