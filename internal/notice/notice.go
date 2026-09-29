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
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
// when the text is not a typed finish-report / scout-report / escalation
// (envelope.Parse
// returns nil, or the kind is not terminal) — the caller in that case has
// nothing structured to record and free-text prose remains the only surface,
// which is unchanged behaviour, not a regression.
func FromReport(agent, parent, text string, at time.Time) (n Notice, ok bool) {
	m, _ := envelope.Parse(text)
	if m == nil {
		return Notice{}, false
	}
	switch m.Kind {
	case envelope.KindFinishReport, envelope.KindScoutReport, envelope.KindEscalation:
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
		Outcome:   classifyOutcome(m),
		Summary:   summaryLine(m.Payload, text),
	}
	n.ID = NewID(at, agent, text)
	return n, true
}

// classifyOutcome derives done / blocked / needs-design / other, strongest
// signal first:
//
//  1. an explicit "jevons: outcome done|blocked|needs-design" slot (carried in
//     envelope.Extra, so no schema change is needed to emit it);
//  2. an escalation envelope, which is a blocked / needs-owner raise by kind;
//  3. the payload's headline paragraph saying blocked or needs-design — only
//     the headline, because a done report routinely names a design-gated
//     follow-up or an "unblocked" dependency further down, and whole-text
//     substring matching filed those as blocked;
//  4. done, which needs a finish-report carrying an executable oracle or
//     explicit accepted-risk (🎯T31/T31.1) AND no failing verdict — a report
//     citing verdict RED ("J3 fails 3 of 3 runs") is not done however much
//     evidence it carries.
//
// A claimed-done with neither oracle nor risk is recorded as other, not done:
// that ambiguity is what this inbox exists to surface.
func classifyOutcome(m *envelope.Message) Outcome {
	if o, ok := ParseOutcome(m.Extra["outcome"]); ok {
		return o
	}
	head := headline(m.Payload)
	if m.Kind == envelope.KindEscalation {
		if saysNeedsDesign(head) {
			return OutcomeNeedsDesign
		}
		return OutcomeBlocked
	}
	switch {
	case saysNeedsDesign(head):
		return OutcomeNeedsDesign
	case saysBlocked(head):
		return OutcomeBlocked
	}
	switch m.Verdict {
	case envelope.VerdictNone, envelope.VerdictGreen:
	case envelope.VerdictRed:
		return OutcomeBlocked
	default:
		// SUSPECT / DIRTY / EMPTY / UNKNOWN / KILLED / VOID: not a pass, and
		// not necessarily blocked either — the parent has to read it.
		return OutcomeOther
	}
	if m.Kind == envelope.KindFinishReport && (m.HasOracle() || m.HasRisk()) {
		return OutcomeDone
	}
	return OutcomeOther
}

// ParseOutcome maps a slot or query value onto an Outcome. Spaces and
// underscores collapse to a hyphen so "needs design" matches.
func ParseOutcome(raw string) (Outcome, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.Join(strings.Fields(s), "-")
	switch o := Outcome(s); o {
	case OutcomeDone, OutcomeBlocked, OutcomeNeedsDesign, OutcomeOther:
		return o, true
	}
	return "", false
}

// headline is the payload's first paragraph: text up to the first blank line
// after some content, skipping fence lines.
func headline(payload string) string {
	var b strings.Builder
	for _, line := range strings.Split(payload, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") {
			continue
		}
		if t == "" {
			if b.Len() > 0 {
				break
			}
			continue
		}
		b.WriteString(t)
		b.WriteByte(' ')
	}
	return b.String()
}

var (
	needsDesignRE = regexp.MustCompile(`(?i)\b(needs[- ]design|design[- ]gated|parked[- ]for[- ]design)\b`)
	blockedRE     = regexp.MustCompile(`(?i)\b(blocked|blocker|blocking)\b`)
	// negationRE matches a negating word immediately before a hit:
	// "not blocked", "no longer design-gated", "nothing blocking".
	negationRE = regexp.MustCompile(`(?i)\b(not|no|never|nothing|no longer|isn't|wasn't|aren't)\s+$`)
)

func saysNeedsDesign(s string) bool { return hasUnnegated(needsDesignRE, s) }
func saysBlocked(s string) bool     { return hasUnnegated(blockedRE, s) }

func hasUnnegated(re *regexp.Regexp, s string) bool {
	for _, loc := range re.FindAllStringIndex(s, -1) {
		if !negationRE.MatchString(s[:loc[0]]) {
			return true
		}
	}
	return false
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
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var n Notice
		if err := json.Unmarshal([]byte(line), &n); err != nil {
			// One corrupt line must not sink the rest of the inbox, but it
			// is not skipped silently either.
			slog.Warn("notice inbox: corrupt line skipped",
				"path", path, "line", lineNo, "err", err)
			continue
		}
		out = append(out, n)
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}

// ListAll reads every parent's inbox — the overseer's fleet-wide view,
// including notices filed under "unowned" because the reporting agent's
// parent could not be resolved (e.g. it was already reaped). Notices are
// merged oldest first.
func ListAll(stateDir string) ([]Notice, error) {
	paths, err := filepath.Glob(filepath.Join(stateDir, "notices", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []Notice
	for _, p := range paths {
		got, err := List(stateDir, strings.TrimSuffix(filepath.Base(p), ".jsonl"))
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

// Query selects notices for a reader. A blank Parent is the fleet-wide
// (overseer) view; a blank Outcome keeps every outcome; Limit > 0 keeps only
// the most recent Limit notices.
type Query struct {
	Parent  string
	Outcome Outcome
	Limit   int
}

// Select runs q against the durable inbox, oldest first.
func Select(stateDir string, q Query) ([]Notice, error) {
	var (
		all []Notice
		err error
	)
	if strings.TrimSpace(q.Parent) == "" {
		all, err = ListAll(stateDir)
	} else {
		all, err = List(stateDir, q.Parent)
	}
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, n := range all {
		if q.Outcome == "" || n.Outcome == q.Outcome {
			out = append(out, n)
		}
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[len(out)-q.Limit:]
	}
	return out, nil
}
