package treeguard

// The provider-independent half of 🎯T391.
//
// The compare-and-swap refusal lives at a Claude Code hook boundary, so it can
// only ever protect a worker whose harness has hooks. A grok/claudia worker
// inherits nothing, and a python script that computes its target path at run
// time defeats the command recognizer even under Claude. Both leave the same
// residue: content disappears and nobody says so.
//
// The journal closes that by inverting the question. Instead of asking "is
// this write safe", it records the content of every guarded file the guard has
// SEEN, so any later disagreement between disk and the journal is a change
// that did not pass through the guard — whatever wrote it, under whatever
// provider, through whatever path. A sweep names the lines that disappeared in
// such a change.
//
// It is a detector, not a gate: it cannot refuse a write that already
// happened. That is the honest trade for covering write paths and providers
// the gate cannot reach, and it is why the sweep is wired at the git
// pre-commit boundary (every provider passes through it) as well as after
// Bash.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// journalDir holds one entry per guarded path, fleet-wide rather than
// per-session: the question it answers ("has the guard seen this content?")
// is not a property of any one worker.
const journalDir = "journal"

// UnguardedLog is the append-only record of detected unguarded changes, under
// the store root. A worker that lost lines can find them here after the fact.
const UnguardedLog = "unguarded.jsonl"

// Via values record what put a content into the journal.
const (
	ViaTool  = "tool"  // an allowed Write/Edit, or a Read that observed disk
	ViaBash  = "bash"  // an allowed shell command, re-read afterwards
	ViaSweep = "sweep" // the sweep itself, adopting current disk as the new base
)

// JournalEntry is the last content of a guarded path that the guard saw.
type JournalEntry struct {
	Path    string    `json:"path"`
	Hash    string    `json:"hash"`
	At      time.Time `json:"at"`
	Session string    `json:"session"`
	Via     string    `json:"via"`
}

// Journal records content the guard has seen for absPath.
func (s *Store) Journal(absPath string, content []byte, now time.Time, session, via string) error {
	dir := filepath.Join(s.Root, journalDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	base := filepath.Join(dir, pathKey(absPath))
	if err := writeAtomic(base+".blob", content); err != nil {
		return err
	}
	meta, err := json.Marshal(JournalEntry{
		Path:    absPath,
		Hash:    HashBytes(content),
		At:      now,
		Session: session,
		Via:     via,
	})
	if err != nil {
		return err
	}
	return writeAtomic(base+".json", meta)
}

// JournalLookup returns the last content the guard saw for absPath, or
// (nil, nil, nil) when it has never seen the file.
func (s *Store) JournalLookup(absPath string) (*JournalEntry, []byte, error) {
	base := filepath.Join(s.Root, journalDir, pathKey(absPath))
	meta, err := os.ReadFile(base + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var entry JournalEntry
	if err := json.Unmarshal(meta, &entry); err != nil {
		return nil, nil, err
	}
	content, err := os.ReadFile(base + ".blob")
	if errors.Is(err, os.ErrNotExist) {
		return &entry, nil, nil
	}
	if err != nil {
		return &entry, nil, err
	}
	return &entry, content, nil
}

// SweepFinding is one guarded file whose content changed outside the guard in
// a way that dropped lines.
type SweepFinding struct {
	RelPath  string    `json:"path"`
	WasHash  string    `json:"was"`
	NowHash  string    `json:"now"`
	Lost     []string  `json:"lost"`
	LastSeen time.Time `json:"last_seen"`
	LastVia  string    `json:"last_via"`
	At       time.Time `json:"at"`
}

// Sweep compares every guarded file against the journal and reports the ones
// that changed without the guard seeing it AND lost lines in the process.
//
// Additions are not reported. An unguarded change that only adds content has
// taken nothing from anyone, and a detector that fires on every legitimate
// `sed -i` append would be muted within a day — the whole value here is that a
// report means something was lost.
//
// The sweep adopts current disk as the new base, so a loss is reported once
// rather than at every subsequent commit.
func (e *Env) Sweep(now time.Time) ([]SweepFinding, error) {
	var findings []SweepFinding
	for _, abs := range e.guardedFilesOnDisk() {
		content, err := readFileOrNil(abs)
		if err != nil || content == nil {
			continue
		}
		entry, seen, err := e.Store.JournalLookup(abs)
		if err != nil {
			return findings, err
		}
		diskHash := HashBytes(content)
		if entry == nil {
			// First sight: nothing to compare against. Adopting it silently is
			// the only honest bootstrap — reporting here would fire once for
			// every guarded file on a fresh store and teach workers to ignore
			// the sweep before it ever caught anything.
			if err := e.Store.Journal(abs, content, now, "", ViaSweep); err != nil {
				return findings, err
			}
			continue
		}
		if entry.Hash == diskHash {
			continue
		}
		lost := LostLines(seen, content)
		if err := e.Store.Journal(abs, content, now, "", ViaSweep); err != nil {
			return findings, err
		}
		if len(lost) == 0 {
			continue
		}
		rel, err := filepath.Rel(e.RepoRoot, abs)
		if err != nil {
			rel = abs
		}
		findings = append(findings, SweepFinding{
			RelPath:  filepath.ToSlash(rel),
			WasHash:  entry.Hash,
			NowHash:  diskHash,
			Lost:     lost,
			LastSeen: entry.At,
			LastVia:  entry.Via,
			At:       now,
		})
	}
	return findings, nil
}

// guardedFilesOnDisk expands the guard patterns to the files that exist.
func (e *Env) guardedFilesOnDisk() []string {
	seen := make(map[string]bool)
	var out []string
	for _, pattern := range e.Guarded {
		matches, err := filepath.Glob(filepath.Join(e.RepoRoot, filepath.FromSlash(pattern)))
		if err != nil {
			continue
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || info.IsDir() || seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

// LostLines returns non-blank lines present in was and absent from now, capped
// at MaxAtRisk. Comparison is on trimmed text for the same reason
// DetectLostAdditions uses it: reindentation is not a loss.
func LostLines(was, now []byte) []string {
	if was == nil {
		return nil
	}
	present := lineSet(now)
	seen := make(map[string]bool)
	var out []string
	for line := range strings.SplitSeq(string(was), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || present[t] || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == MaxAtRisk {
			break
		}
	}
	return out
}

// RecordSweep appends findings to the unguarded-change log.
func (e *Env) RecordSweep(findings []SweepFinding) error {
	if len(findings) == 0 {
		return nil
	}
	if err := os.MkdirAll(e.Store.Root, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(e.Store.Root, UnguardedLog),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, finding := range findings {
		if err := enc.Encode(finding); err != nil {
			return err
		}
	}
	return nil
}

// FormatSweepReport renders findings for a worker's terminal. Empty findings
// render as "" so a caller can print unconditionally.
func FormatSweepReport(findings []SweepFinding) string {
	if len(findings) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("treeguard: UNGUARDED CHANGE — content disappeared from a shared hot file " +
		"without passing the write guard (🎯T391).\n")
	for _, f := range findings {
		b.WriteString("  " + f.RelPath + ": " + shortHash(f.WasHash) + " → " + shortHash(f.NowHash) +
			", these lines are gone:\n")
		for _, line := range f.Lost {
			b.WriteString("    - " + line + "\n")
		}
	}
	b.WriteString("If they were not yours to remove, recover them from git or from the " +
		"other worker and re-apply. Written to " + UnguardedLog + ".\n")
	return b.String()
}
