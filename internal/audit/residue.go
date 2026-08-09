// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Residue entry status values (🎯T357 acceptance #3). An audit that only
// appends is noise; the residue ledger is the memory that turns repeated
// passes into a converging list.
const (
	// StatusOpen is a finding seen in the latest covering pass.
	StatusOpen = "open"
	// StatusResolved is a finding absent from a pass that covered its scope.
	StatusResolved = "resolved"
	// StatusReopened is a resolved finding seen again.
	StatusReopened = "reopened"
)

// Disposition values for owner/overseer handling of a finding.
const (
	DispositionPending          = "pending"
	DispositionFiled            = "filed"
	DispositionIgnoreWithReason = "ignore_with_reason"
	DispositionAccepted         = "accepted"
)

// ResidueEntry is the durable record of one finding across audit passes.
type ResidueEntry struct {
	Fingerprint string    `json:"fingerprint"`
	Scope       ScopeKind `json:"scope"`
	Path        string    `json:"path,omitempty"`
	Line        int       `json:"line,omitempty"`
	Title       string    `json:"title"`
	Detail      string    `json:"detail,omitempty"`
	Evidence    string    `json:"evidence,omitempty"`
	Severity    Severity  `json:"severity"`
	// MaxSeverity remembers the worst rating ever assigned, so a later
	// pass that downgrades a finding does not erase that it once read
	// critical.
	MaxSeverity Severity `json:"max_severity,omitempty"`

	Status     string    `json:"status"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	ResolvedAt time.Time `json:"resolved_at,omitzero"`
	SeenCount  int       `json:"seen_count"`
	Reopens    int       `json:"reopens,omitempty"`

	LastReportID string `json:"last_report_id,omitempty"`
	// Notified records the last in-cycle notification for this entry, so a
	// standing critical finding does not re-alert every pass.
	NotifiedAt time.Time `json:"notified_at,omitzero"`

	SuggestedTarget *SuggestedTarget `json:"suggested_target,omitempty"`
	// Disposition is the overseer's handling; audit never files itself.
	Disposition   string    `json:"disposition,omitempty"`
	DispositionAt time.Time `json:"disposition_at,omitzero"`
	Reason        string    `json:"reason,omitempty"`
	TargetID      string    `json:"target_id,omitempty"`
}

// Open reports whether the entry is currently outstanding.
func (e ResidueEntry) Open() bool {
	return e.Status == StatusOpen || e.Status == StatusReopened
}

// MergeResult reports what one pass did to the residue ledger.
type MergeResult struct {
	New      []ResidueEntry `json:"new,omitempty"`
	Updated  []ResidueEntry `json:"updated,omitempty"`
	Reopened []ResidueEntry `json:"reopened,omitempty"`
	Resolved []ResidueEntry `json:"resolved,omitempty"`
	// Notify is the subset warranting an in-cycle alert (new or reopened
	// at/above the threshold, subject to the re-alert window).
	Notify []ResidueEntry `json:"notify,omitempty"`
	// OpenTotal is the outstanding count after the merge.
	OpenTotal int `json:"open_total"`
}

// Changed reports whether the merge moved the ledger at all.
func (m MergeResult) Changed() bool {
	return len(m.New)+len(m.Updated)+len(m.Reopened)+len(m.Resolved) > 0
}

// ResidueStore is the durable finding ledger. One entry per fingerprint;
// atomic write-and-rename; malformed state is a hard error, never a
// silent reset.
type ResidueStore struct {
	path string
	mu   sync.Mutex
}

// OpenResidueStore opens stateDir/audit/residue.json.
func OpenResidueStore(stateDir string) (*ResidueStore, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("audit residue: stateDir required")
	}
	dir := filepath.Join(stateDir, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit residue: mkdir: %w", err)
	}
	return &ResidueStore{path: filepath.Join(dir, ResidueFileName)}, nil
}

// Path returns the ledger file path.
func (s *ResidueStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Entries returns all residue entries, newest activity last.
func (s *ResidueStore) Entries() ([]ResidueEntry, error) {
	if s == nil {
		return nil, fmt.Errorf("audit residue: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// OpenEntries returns outstanding entries only.
func (s *ResidueStore) OpenEntries() ([]ResidueEntry, error) {
	all, err := s.Entries()
	if err != nil {
		return nil, err
	}
	var out []ResidueEntry
	for _, e := range all {
		if e.Open() {
			out = append(out, e)
		}
	}
	return out, nil
}

// MergeArgs shapes a residue merge.
type MergeArgs struct {
	Report Report
	// CoveredScopes are the scopes this pass actually scanned. Residue in
	// scopes the pass did not cover is left alone: a partial scan must
	// never silently resolve findings it could not have seen.
	CoveredScopes []ScopeKind
	// NotifySeverity is the in-cycle alert threshold.
	NotifySeverity Severity
	// RenotifyAfter suppresses repeat alerts for a still-open entry
	// (0 = 24h). New and reopened entries always qualify.
	RenotifyAfter time.Duration
	// MaxNotify caps the notify set (0 = unbounded).
	MaxNotify int
	Now       time.Time
}

// Merge folds a report into the residue ledger and reports the delta.
//
// Semantics that make audits converge rather than accumulate:
//   - a finding seen again updates its entry in place (seen count, latest
//     severity/detail, last report) — it never mints a duplicate;
//   - a resolved finding seen again reopens, keeping its history;
//   - an entry absent from a pass that covered its scope resolves;
//   - entries in scopes the pass did not cover are untouched.
func (s *ResidueStore) Merge(args MergeArgs) (MergeResult, error) {
	var res MergeResult
	if s == nil {
		return res, fmt.Errorf("audit residue: nil store")
	}
	now := args.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	threshold := args.NotifySeverity
	if threshold == "" {
		threshold = SeverityCritical
	}
	renotify := args.RenotifyAfter
	if renotify <= 0 {
		renotify = 24 * time.Hour
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.load()
	if err != nil {
		return res, err
	}
	byFP := map[string]int{}
	for i, e := range entries {
		byFP[e.Fingerprint] = i
	}
	covered := map[ScopeKind]bool{}
	for _, k := range args.CoveredScopes {
		covered[k] = true
	}

	seenNow := map[string]bool{}
	for _, f := range args.Report.Findings {
		fp := strings.TrimSpace(f.Fingerprint)
		if fp == "" {
			continue
		}
		seenNow[fp] = true
		idx, exists := byFP[fp]
		if !exists {
			e := ResidueEntry{
				Fingerprint:     fp,
				Scope:           f.Scope,
				Path:            f.Path,
				Line:            f.Line,
				Title:           f.Title,
				Detail:          f.Detail,
				Evidence:        f.Evidence,
				Severity:        f.Severity,
				MaxSeverity:     f.Severity,
				Status:          StatusOpen,
				FirstSeen:       now,
				LastSeen:        now,
				SeenCount:       1,
				LastReportID:    args.Report.ID,
				SuggestedTarget: f.SuggestedTarget,
				Disposition:     DispositionPending,
			}
			entries = append(entries, e)
			byFP[fp] = len(entries) - 1
			res.New = append(res.New, e)
			continue
		}
		e := entries[idx]
		wasResolved := e.Status == StatusResolved
		e.Scope = f.Scope
		e.Path = f.Path
		e.Line = f.Line
		e.Title = f.Title
		if f.Detail != "" {
			e.Detail = f.Detail
		}
		if f.Evidence != "" {
			e.Evidence = f.Evidence
		}
		e.Severity = f.Severity
		if f.Severity.Rank() > e.MaxSeverity.Rank() {
			e.MaxSeverity = f.Severity
		}
		e.LastSeen = now
		e.SeenCount++
		e.LastReportID = args.Report.ID
		if f.SuggestedTarget != nil {
			e.SuggestedTarget = f.SuggestedTarget
		}
		if e.Disposition == "" {
			e.Disposition = DispositionPending
		}
		if wasResolved {
			e.Status = StatusReopened
			e.Reopens++
			e.ResolvedAt = time.Time{}
			// A finding that comes back is pending again: a prior "ignore"
			// or "filed" verdict was made against evidence that no longer
			// holds, so it goes back in front of the overseer.
			if e.Disposition == DispositionIgnoreWithReason {
				e.Disposition = DispositionPending
			}
			entries[idx] = e
			res.Reopened = append(res.Reopened, e)
			continue
		}
		e.Status = StatusOpen
		entries[idx] = e
		res.Updated = append(res.Updated, e)
	}

	// Resolve entries the pass could have seen but did not.
	for i := range entries {
		e := entries[i]
		if seenNow[e.Fingerprint] || !e.Open() {
			continue
		}
		if !covered[e.Scope] {
			continue
		}
		e.Status = StatusResolved
		e.ResolvedAt = now
		e.LastReportID = args.Report.ID
		entries[i] = e
		res.Resolved = append(res.Resolved, e)
	}

	// Notify set: new or reopened at/above threshold, plus still-open
	// entries past the re-alert window.
	var notify []ResidueEntry
	for _, e := range append(append([]ResidueEntry{}, res.New...), res.Reopened...) {
		if e.Severity.AtLeast(threshold) {
			notify = append(notify, e)
		}
	}
	for _, e := range res.Updated {
		if !e.Severity.AtLeast(threshold) {
			continue
		}
		if e.NotifiedAt.IsZero() || now.Sub(e.NotifiedAt) >= renotify {
			notify = append(notify, e)
		}
	}
	sort.SliceStable(notify, func(i, j int) bool {
		return notify[i].Severity.Rank() > notify[j].Severity.Rank()
	})
	if args.MaxNotify > 0 && len(notify) > args.MaxNotify {
		notify = notify[:args.MaxNotify]
	}
	for _, n := range notify {
		if i, ok := byFP[n.Fingerprint]; ok {
			entries[i].NotifiedAt = now
		}
	}
	res.Notify = notify

	for _, e := range entries {
		if e.Open() {
			res.OpenTotal++
		}
	}
	if err := s.save(entries); err != nil {
		return res, err
	}
	return res, nil
}

// SetDisposition records overseer handling of a finding.
func (s *ResidueStore) SetDisposition(fingerprint, disposition, reason, targetID string, now time.Time) (ResidueEntry, error) {
	var out ResidueEntry
	if s == nil {
		return out, fmt.Errorf("audit residue: nil store")
	}
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return out, fmt.Errorf("audit residue: fingerprint required")
	}
	disp := strings.TrimSpace(disposition)
	switch disp {
	case DispositionPending, DispositionFiled, DispositionIgnoreWithReason, DispositionAccepted:
	default:
		return out, fmt.Errorf("audit residue: unknown disposition %q", disposition)
	}
	if disp == DispositionIgnoreWithReason && strings.TrimSpace(reason) == "" {
		return out, fmt.Errorf("audit residue: %s requires a reason", DispositionIgnoreWithReason)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return out, err
	}
	for i := range entries {
		if entries[i].Fingerprint != fp {
			continue
		}
		entries[i].Disposition = disp
		entries[i].DispositionAt = now
		if r := strings.TrimSpace(reason); r != "" {
			entries[i].Reason = r
		}
		if t := strings.TrimSpace(targetID); t != "" {
			entries[i].TargetID = t
		}
		out = entries[i]
		if err := s.save(entries); err != nil {
			return out, err
		}
		return out, nil
	}
	return out, fmt.Errorf("audit residue: no entry with fingerprint %s", fp)
}

func (s *ResidueStore) load() ([]ResidueEntry, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("audit residue: read: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}
	var entries []ResidueEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("audit residue: parse %s: %w", s.path, err)
	}
	return entries, nil
}

func (s *ResidueStore) save(entries []ResidueEntry) error {
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].FirstSeen.Before(entries[j].FirstSeen)
	})
	return writeJSONAtomic(s.path, entries)
}
