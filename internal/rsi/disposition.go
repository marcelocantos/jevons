// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Disposition values (🎯T333). Every delivered judgment starts pending; the
// overseer records one of the terminal dispositions via product tools.
const (
	DispositionPending          = "pending"
	DispositionFile             = "file"
	DispositionPark             = "park"
	DispositionIgnoreWithReason = "ignore_with_reason"
	DispositionActOther         = "act_other"
)

// Outcome values mirrored from the bullseye ledger for filed targets.
const (
	OutcomeAchieved = "achieved"
	OutcomeSetAside = "set_aside"
)

// DispositionEntry is the durable record of one judgment's product loop.
type DispositionEntry struct {
	Fingerprint   string    `json:"fingerprint"`
	Name          string    `json:"name,omitempty"`
	Observation   string    `json:"observation,omitempty"`
	Severity      string    `json:"severity,omitempty"`
	DeliveredAt   time.Time `json:"delivered_at"`
	Disposition   string    `json:"disposition"`
	DispositionAt time.Time `json:"disposition_at,omitzero"`
	Reason        string    `json:"reason,omitempty"`
	TargetID      string    `json:"target_id,omitempty"`
	TargetCwd     string    `json:"target_cwd,omitempty"`
	Evidence      string    `json:"evidence,omitempty"`
	// Mode is "" for the forward drip, RetroModeLabel for the retrospective
	// history pass (🎯T353) — owner-visible provenance in the T354 list.
	Mode      string    `json:"mode,omitempty"`
	Outcome   string    `json:"outcome,omitempty"`
	OutcomeAt time.Time `json:"outcome_at,omitzero"`
}

// DispositionStore is the durable judgment→disposition ledger (🎯T333).
// One entry per fingerprint; atomic write-and-rename; malformed is a hard error.
type DispositionStore struct {
	path string
	mu   sync.Mutex
}

// OpenDispositionStore opens stateDir/rsi/dispositions.json.
func OpenDispositionStore(stateDir string) (*DispositionStore, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("rsi dispositions: stateDir required")
	}
	dir := filepath.Join(stateDir, "rsi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("rsi dispositions: mkdir: %w", err)
	}
	return &DispositionStore{path: filepath.Join(dir, "dispositions.json")}, nil
}

// Path returns the store file path.
func (s *DispositionStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// RecordDelivered upserts delivered judgments as pending dispositions.
// Re-delivery of a fingerprint that already carries an outcome means the coach
// saw newer evidence past suppression: the loop reopens (pending, outcome cleared).
func (s *DispositionStore) RecordDelivered(js []Judgment, now time.Time) error {
	if s == nil {
		return fmt.Errorf("rsi dispositions: nil store")
	}
	if len(js) == 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return err
	}
	byFP := map[string]int{}
	for i, e := range entries {
		byFP[e.Fingerprint] = i
	}
	for _, j := range js {
		fp := strings.TrimSpace(j.Fingerprint)
		if fp == "" {
			continue
		}
		if i, ok := byFP[fp]; ok {
			entries[i].DeliveredAt = now
			entries[i].Name = j.Name
			entries[i].Observation = j.Observation
			entries[i].Severity = j.Severity
			entries[i].Mode = j.Mode
			// Never clobber an overseer-written evidence note (SetDisposition).
			if entries[i].Evidence == "" {
				entries[i].Evidence = SummarizeEvidence(j.Evidence)
			}
			if entries[i].Outcome != "" {
				entries[i].Disposition = DispositionPending
				entries[i].DispositionAt = time.Time{}
				entries[i].Outcome = ""
				entries[i].OutcomeAt = time.Time{}
			}
			continue
		}
		byFP[fp] = len(entries)
		entries = append(entries, DispositionEntry{
			Fingerprint: fp,
			Name:        j.Name,
			Observation: j.Observation,
			Severity:    j.Severity,
			DeliveredAt: now,
			Disposition: DispositionPending,
			Evidence:    SummarizeEvidence(j.Evidence),
			Mode:        j.Mode,
		})
	}
	return s.save(entries)
}

// MaxEvidenceSummaryRefs is how many evidence pointers the owner-visible
// summary names before collapsing the rest into a "+N more" tail (🎯T354).
const MaxEvidenceSummaryRefs = 3

// SummarizeEvidence renders judgment evidence pointers as one short owner-
// readable line: "owner_chat:chatlog-2026-08-09 (chat_gap); session:abc (+2 more)".
// Pure: no store, no clock.
func SummarizeEvidence(refs []EvidenceRef) string {
	var parts []string
	for _, r := range refs {
		src := strings.TrimSpace(r.Source)
		id := strings.TrimSpace(r.SourceID)
		kind := strings.TrimSpace(r.Kind)
		var b strings.Builder
		switch {
		case src != "" && id != "":
			b.WriteString(src + ":" + id)
		case src != "":
			b.WriteString(src)
		case id != "":
			b.WriteString(id)
		default:
			continue
		}
		if kind != "" {
			b.WriteString(" (" + kind + ")")
		}
		parts = append(parts, b.String())
		if len(parts) == MaxEvidenceSummaryRefs {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := strings.Join(parts, "; ")
	if rest := len(refs) - len(parts); rest > 0 {
		out += fmt.Sprintf(" (+%d more)", rest)
	}
	return out
}

// SetDispositionArgs is the overseer's disposition record for one judgment.
type SetDispositionArgs struct {
	Fingerprint string
	Disposition string // file | park | ignore_with_reason | act_other
	Reason      string
	TargetID    string // required for file
	TargetCwd   string // repo the target was filed in (outcome sync)
	Evidence    string
	Now         time.Time
}

// SetDisposition records the overseer's decision for a fingerprint.
// Unknown fingerprints are accepted (judgment may predate the store).
func (s *DispositionStore) SetDisposition(args SetDispositionArgs) (DispositionEntry, error) {
	if s == nil {
		return DispositionEntry{}, fmt.Errorf("rsi dispositions: nil store")
	}
	fp := strings.TrimSpace(args.Fingerprint)
	if fp == "" {
		return DispositionEntry{}, fmt.Errorf("rsi dispositions: fingerprint required")
	}
	disp := strings.TrimSpace(args.Disposition)
	switch disp {
	case DispositionFile, DispositionPark, DispositionIgnoreWithReason, DispositionActOther:
	default:
		return DispositionEntry{}, fmt.Errorf(
			"rsi dispositions: disposition must be one of file|park|ignore_with_reason|act_other, got %q", disp)
	}
	if disp == DispositionIgnoreWithReason && strings.TrimSpace(args.Reason) == "" {
		return DispositionEntry{}, fmt.Errorf("rsi dispositions: ignore_with_reason requires a reason")
	}
	if disp == DispositionFile && strings.TrimSpace(args.TargetID) == "" {
		return DispositionEntry{}, fmt.Errorf("rsi dispositions: file requires target_id")
	}
	now := args.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return DispositionEntry{}, err
	}
	idx := -1
	for i, e := range entries {
		if e.Fingerprint == fp {
			idx = i
			break
		}
	}
	if idx < 0 {
		entries = append(entries, DispositionEntry{Fingerprint: fp, DeliveredAt: now})
		idx = len(entries) - 1
	}
	e := &entries[idx]
	e.Disposition = disp
	e.DispositionAt = now
	e.Reason = strings.TrimSpace(args.Reason)
	if id := strings.TrimSpace(args.TargetID); id != "" {
		e.TargetID = strings.TrimPrefix(id, "🎯")
	}
	if cwd := strings.TrimSpace(args.TargetCwd); cwd != "" {
		e.TargetCwd = cwd
	}
	if ev := strings.TrimSpace(args.Evidence); ev != "" {
		e.Evidence = ev
	}
	out := *e
	return out, s.save(entries)
}

// Entries returns all disposition entries, most recently delivered first.
func (s *DispositionStore) Entries() ([]DispositionEntry, error) {
	if s == nil {
		return nil, fmt.Errorf("rsi dispositions: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].DeliveredAt.After(entries[j].DeliveredAt)
	})
	return entries, nil
}

// DispositionMetrics reports disposition rates over a window (🎯T333 #2).
// Mute spam and zero-disposition show up as a high Pending count.
type DispositionMetrics struct {
	Window   time.Duration
	Total    int
	Pending  int
	Filed    int
	Parked   int
	Ignored  int
	ActOther int
	Achieved int
	SetAside int
}

// Metrics counts entries whose delivery or disposition falls inside the window.
func (s *DispositionStore) Metrics(now time.Time, window time.Duration) (DispositionMetrics, error) {
	if s == nil {
		return DispositionMetrics{}, fmt.Errorf("rsi dispositions: nil store")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if window <= 0 {
		window = 14 * 24 * time.Hour
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return DispositionMetrics{}, err
	}
	m := DispositionMetrics{Window: window}
	cutoff := now.Add(-window)
	for _, e := range entries {
		latest := e.DeliveredAt
		if e.DispositionAt.After(latest) {
			latest = e.DispositionAt
		}
		if latest.Before(cutoff) {
			continue
		}
		m.Total++
		switch e.Disposition {
		case DispositionFile:
			m.Filed++
		case DispositionPark:
			m.Parked++
		case DispositionIgnoreWithReason:
			m.Ignored++
		case DispositionActOther:
			m.ActOther++
		default:
			m.Pending++
		}
		switch e.Outcome {
		case OutcomeAchieved:
			m.Achieved++
		case OutcomeSetAside:
			m.SetAside++
		}
	}
	return m, nil
}

// Suppression tells the coach cycle not to re-propose a fingerprint (🎯T333 #4).
type Suppression struct {
	// Always: target filed and still open — the gap is already tracked.
	Always bool
	// After: outcome recorded at this time — re-propose only with newer evidence.
	After time.Time
}

// Suppressions returns the fingerprints the coach must not re-propose.
func (s *DispositionStore) Suppressions(now time.Time) (map[string]Suppression, error) {
	if s == nil {
		return nil, fmt.Errorf("rsi dispositions: nil store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return nil, err
	}
	out := map[string]Suppression{}
	for _, e := range entries {
		switch {
		case e.Outcome == OutcomeAchieved || e.Outcome == OutcomeSetAside:
			out[e.Fingerprint] = Suppression{After: e.OutcomeAt}
		case e.Disposition == DispositionFile:
			out[e.Fingerprint] = Suppression{Always: true}
		}
	}
	return out, nil
}

// SyncOutcomes reads ledger status for filed-and-open entries and records
// achieved/set_aside outcomes. read errors skip the entry (ledger may be
// on another machine or mid-write); returns how many entries were updated.
func (s *DispositionStore) SyncOutcomes(read func(cwd, id string) (string, error), now time.Time) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("rsi dispositions: nil store")
	}
	if read == nil {
		return 0, fmt.Errorf("rsi dispositions: read func required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.load()
	if err != nil {
		return 0, err
	}
	updated := 0
	for i := range entries {
		e := &entries[i]
		if e.Disposition != DispositionFile || e.TargetID == "" || e.Outcome != "" {
			continue
		}
		status, err := read(e.TargetCwd, e.TargetID)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(status) {
		case OutcomeAchieved:
			e.Outcome = OutcomeAchieved
		case OutcomeSetAside:
			e.Outcome = OutcomeSetAside
		default:
			continue
		}
		e.OutcomeAt = now
		updated++
	}
	if updated == 0 {
		return 0, nil
	}
	return updated, s.save(entries)
}

// ReadBullseyeTargetStatus reads a target's status from the repo's
// bullseye.yaml, walking up from cwd (mirrors bullseye discovery).
func ReadBullseyeTargetStatus(cwd, id string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	id = strings.TrimPrefix(strings.TrimSpace(id), "🎯")
	if cwd == "" || id == "" {
		return "", fmt.Errorf("rsi dispositions: cwd and id required")
	}
	if strings.HasPrefix(cwd, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = filepath.Join(home, cwd[2:])
		}
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	var path string
	for {
		p := filepath.Join(dir, "bullseye.yaml")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			path = p
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("rsi dispositions: no bullseye.yaml above %s", cwd)
		}
		dir = parent
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc struct {
		Targets map[string]struct {
			Status string `yaml:"status"`
		} `yaml:"targets"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("rsi dispositions: parse %s: %w", path, err)
	}
	t, ok := doc.Targets[id]
	if !ok {
		return "", fmt.Errorf("rsi dispositions: target %s not in %s", id, path)
	}
	return t.Status, nil
}

type dispositionFile struct {
	Entries []DispositionEntry `json:"entries"`
}

func (s *DispositionStore) load() ([]DispositionEntry, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rsi dispositions: read: %w", err)
	}
	if len(bytesTrimSpace(data)) == 0 {
		return nil, nil
	}
	var df dispositionFile
	if err := json.Unmarshal(data, &df); err != nil {
		return nil, fmt.Errorf("rsi dispositions: parse %s: %w", s.path, err)
	}
	return df.Entries, nil
}

func (s *DispositionStore) save(entries []DispositionEntry) error {
	data, err := json.MarshalIndent(dispositionFile{Entries: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("rsi dispositions: marshal: %w", err)
	}
	data = append(data, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("rsi dispositions: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rsi dispositions: rename: %w", err)
	}
	return nil
}
