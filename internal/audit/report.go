// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Severity is the finding severity ladder.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// severityRank orders the ladder for threshold comparisons.
var severityRank = map[Severity]int{
	SeverityInfo:     1,
	SeverityLow:      2,
	SeverityMedium:   3,
	SeverityHigh:     4,
	SeverityCritical: 5,
}

// NormalizeSeverity maps free text onto the ladder ("" when unknown).
// Models write "Critical", "SEV: high", "warn" — normalize rather than
// discard an otherwise good finding over its severity spelling.
func NormalizeSeverity(s string) Severity {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.TrimPrefix(t, "sev:")
	t = strings.TrimSpace(t)
	switch t {
	case "info", "informational", "note", "nit":
		return SeverityInfo
	case "low", "minor":
		return SeverityLow
	case "medium", "moderate", "warn", "warning":
		return SeverityMedium
	case "high", "major", "severe":
		return SeverityHigh
	case "critical", "crit", "blocker", "fatal":
		return SeverityCritical
	}
	return ""
}

// Rank returns the ladder position (0 for unknown).
func (s Severity) Rank() int { return severityRank[s] }

// AtLeast reports whether s meets or exceeds threshold.
func (s Severity) AtLeast(threshold Severity) bool {
	return s.Rank() >= threshold.Rank() && s.Rank() > 0
}

// SuggestedTarget is the auditor's proposed bullseye filing for a finding.
// The auditor suggests; the overseer disposes — this package never files
// (same split as the RSI coach, 🎯T243).
type SuggestedTarget struct {
	Name       string   `json:"name"`
	Acceptance []string `json:"acceptance,omitempty"`
}

// Finding is one actionable audit result.
type Finding struct {
	// Fingerprint identifies the finding across passes (residue key).
	Fingerprint string    `json:"fingerprint"`
	Scope       ScopeKind `json:"scope"`
	Path        string    `json:"path,omitempty"`
	Line        int       `json:"line,omitempty"`
	Severity    Severity  `json:"severity"`
	Title       string    `json:"title"`
	Detail      string    `json:"detail,omitempty"`
	// Evidence is the concrete pointer that makes the finding checkable.
	Evidence string `json:"evidence,omitempty"`
	// SuggestedTarget is optional; findings may be observations only.
	SuggestedTarget *SuggestedTarget `json:"suggested_target,omitempty"`
}

// Report is one audit pass artifact.
type Report struct {
	ID         string    `json:"id"`
	Reason     string    `json:"reason,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	Provider   string    `json:"provider,omitempty"`
	Model      string    `json:"model,omitempty"`
	// Scopes covered by the manifest this report answers.
	Scopes []ScopeKind `json:"scopes,omitempty"`
	// MissingScopes records a degraded (partial) pass.
	MissingScopes []ScopeKind `json:"missing_scopes,omitempty"`
	// Bounds and manifest stats make the pass auditable in its own right.
	Bounds     Bounds `json:"bounds,omitzero"`
	FilesInMan int    `json:"files_in_manifest"`
	Truncated  bool   `json:"truncated"`

	Findings []Finding `json:"findings"`
	// Summary is the auditor's short prose overview (optional).
	Summary string `json:"summary,omitempty"`
	// Dropped counts findings rejected by shape validation or the cap.
	Dropped int `json:"dropped,omitempty"`
	// Error is set when the pass failed after dispatch.
	Error string `json:"error,omitempty"`
}

// CriticalCount counts findings at or above a severity threshold.
func (r Report) CountAtLeast(threshold Severity) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity.AtLeast(threshold) {
			n++
		}
	}
	return n
}

// Fingerprint derives the stable residue key for a finding. Line numbers
// are deliberately excluded: a finding that shifts lines as code moves is
// the same finding, and must update its residue entry rather than mint a
// duplicate.
func Fingerprint(scope ScopeKind, path, title, workdir string) string {
	h := sha256.New()
	h.Write([]byte(string(scope)))
	h.Write([]byte("|"))
	h.Write([]byte(NormalizePath(path, workdir)))
	h.Write([]byte("|"))
	h.Write([]byte(normalizeTitle(title)))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// NormalizePath makes a finding path comparable across passes and
// machines: workdir-relative and slash-normalized where possible.
func NormalizePath(path, workdir string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if w := strings.TrimSpace(workdir); w != "" && filepath.IsAbs(p) {
		if rel, err := filepath.Rel(filepath.Clean(w), p); err == nil && !strings.HasPrefix(rel, "..") {
			p = rel
		}
	}
	return filepath.ToSlash(p)
}

// normalizeTitle collapses cosmetic differences so the same finding keeps
// its identity when the auditor rephrases it slightly.
func normalizeTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	t = strings.Trim(t, ".!:;, ")
	return strings.Join(strings.Fields(t), " ")
}

// ParseArgs shapes report parsing.
type ParseArgs struct {
	// Workdir normalizes finding paths and fingerprints.
	Workdir string
	// MaxFindings caps accepted findings (0 = package default).
	MaxFindings int
	// DefaultScope is applied when a finding names no valid scope.
	DefaultScope ScopeKind
}

// ParseReport reads an auditor's answer into a validated report body.
//
// Model output is not a wire format: answers arrive wrapped in prose, in
// fenced code blocks, or with a trailing note. This extracts the JSON
// object, then validates finding-by-finding — a malformed finding is
// dropped and counted, never allowed to void an otherwise good pass.
func ParseReport(raw []byte, args ParseArgs) (Report, error) {
	var rep Report
	body, ok := extractJSONObject(raw)
	if !ok {
		return rep, fmt.Errorf("audit report: no JSON object in auditor output (%d bytes)", len(raw))
	}
	// Parse into a lenient shape first: severity and scope arrive as free
	// text, so they are normalized rather than type-checked by json.
	var wire struct {
		Summary  string `json:"summary"`
		Findings []struct {
			Scope           string           `json:"scope"`
			Path            string           `json:"path"`
			File            string           `json:"file"`
			Line            int              `json:"line"`
			Severity        string           `json:"severity"`
			Title           string           `json:"title"`
			Detail          string           `json:"detail"`
			Description     string           `json:"description"`
			Evidence        string           `json:"evidence"`
			SuggestedTarget *SuggestedTarget `json:"suggested_target"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return rep, fmt.Errorf("audit report: parse: %w", err)
	}

	max := args.MaxFindings
	if max <= 0 {
		max = DefaultMaxFindings
	}
	rep.Summary = strings.TrimSpace(wire.Summary)
	seen := map[string]bool{}
	for _, w := range wire.Findings {
		title := strings.TrimSpace(w.Title)
		if title == "" {
			rep.Dropped++
			continue
		}
		sev := NormalizeSeverity(w.Severity)
		if sev == "" {
			// An unrated finding is still worth keeping, at the bottom of
			// the ladder — but it will never cross a notify threshold.
			sev = SeverityInfo
		}
		scope := NormalizeScope(w.Scope)
		if scope == "" {
			scope = args.DefaultScope
			if scope == "" {
				scope = ScopeCode
			}
		}
		path := strings.TrimSpace(w.Path)
		if path == "" {
			path = strings.TrimSpace(w.File)
		}
		detail := strings.TrimSpace(w.Detail)
		if detail == "" {
			detail = strings.TrimSpace(w.Description)
		}
		f := Finding{
			Scope:           scope,
			Path:            NormalizePath(path, args.Workdir),
			Line:            w.Line,
			Severity:        sev,
			Title:           title,
			Detail:          detail,
			Evidence:        strings.TrimSpace(w.Evidence),
			SuggestedTarget: normalizeSuggestedTarget(w.SuggestedTarget),
		}
		f.Fingerprint = Fingerprint(f.Scope, f.Path, f.Title, args.Workdir)
		if seen[f.Fingerprint] {
			rep.Dropped++
			continue
		}
		seen[f.Fingerprint] = true
		if len(rep.Findings) >= max {
			rep.Dropped++
			continue
		}
		rep.Findings = append(rep.Findings, f)
	}
	sortFindings(rep.Findings)
	return rep, nil
}

func normalizeSuggestedTarget(t *SuggestedTarget) *SuggestedTarget {
	if t == nil {
		return nil
	}
	name := strings.TrimSpace(t.Name)
	if name == "" {
		return nil
	}
	out := &SuggestedTarget{Name: name}
	for _, a := range t.Acceptance {
		if s := strings.TrimSpace(a); s != "" {
			out.Acceptance = append(out.Acceptance, s)
		}
	}
	return out
}

// sortFindings orders severe-first, then by scope and path, so a report
// reads top-down and the notify set is the head of the list.
func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if a, b := fs[i].Severity.Rank(), fs[j].Severity.Rank(); a != b {
			return a > b
		}
		if fs[i].Scope != fs[j].Scope {
			return fs[i].Scope < fs[j].Scope
		}
		if fs[i].Path != fs[j].Path {
			return fs[i].Path < fs[j].Path
		}
		return fs[i].Title < fs[j].Title
	})
}

// extractJSONObject finds the report object in model output, tolerating
// fenced blocks and surrounding prose.
//
// The first balanced object is not necessarily the report: prose like
// "consider the block {like this}" produces an earlier candidate. So every
// balanced top-level object is considered, and the first one that parses
// and carries a "findings" key wins, falling back to the first object that
// is merely valid JSON.
func extractJSONObject(raw []byte) ([]byte, bool) {
	var fallback []byte
	for _, cand := range jsonObjectCandidates(raw) {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(cand, &probe); err != nil {
			continue
		}
		if _, ok := probe["findings"]; ok {
			return cand, true
		}
		if fallback == nil {
			fallback = cand
		}
	}
	if fallback != nil {
		return fallback, true
	}
	return nil, false
}

// jsonObjectCandidates returns every balanced top-level {...} span, in
// order. Braces inside strings are ignored so quoted prose cannot derail
// the scan.
func jsonObjectCandidates(s []byte) [][]byte {
	var out [][]byte
	start := -1
	depth := 0
	inStr := false
	esc := false
	for i := range s {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 && start >= 0 {
					out = append(out, s[start:i+1])
					start = -1
				}
			}
		}
	}
	return out
}

// ReportsDir is the directory holding per-cycle report artifacts.
func ReportsDir(stateDir string) string {
	return filepath.Join(stateDir, DirName, ReportsDirName)
}

// ReportPath is the artifact path for a report id.
func ReportPath(stateDir, id string) string {
	return filepath.Join(ReportsDir(stateDir), id+".json")
}

// NewReportID builds a sortable report id from the cycle start.
func NewReportID(now time.Time) string {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return now.UTC().Format("20060102T150405Z")
}

// SaveReport writes a report artifact atomically and prunes old ones.
func SaveReport(stateDir string, rep Report, keep int) error {
	if strings.TrimSpace(rep.ID) == "" {
		return fmt.Errorf("audit report: id required")
	}
	path := ReportPath(stateDir, rep.ID)
	if err := writeJSONAtomic(path, rep); err != nil {
		return err
	}
	pruneReports(filepath.Dir(path), keep)
	return nil
}

// pruneReports keeps the newest `keep` report artifacts. Ids are
// timestamp-sortable, so lexical order is chronological.
func pruneReports(dir string, keep int) {
	if keep <= 0 {
		keep = DefaultKeepReports
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// LoadReport reads a report artifact by id.
func LoadReport(stateDir, id string) (Report, error) {
	var rep Report
	data, err := os.ReadFile(ReportPath(stateDir, id))
	if err != nil {
		return rep, err
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		return rep, fmt.Errorf("audit report: parse %s: %w", id, err)
	}
	return rep, nil
}

// ListReportIDs returns known report ids, newest last.
func ListReportIDs(stateDir string) ([]string, error) {
	dir := filepath.Join(stateDir, DirName, ReportsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(ids)
	return ids, nil
}
