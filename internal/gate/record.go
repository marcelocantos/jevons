// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// StoreDirEnv overrides the record directory. Tests set it; workers should
// not need to.
const StoreDirEnv = "JEVONS_GATE_DIR"

// UnknownStatus is what an unestablished exit status renders as. It is a word
// rather than a number precisely so it cannot be mistaken for success —
// 🎯T396 acceptance 3.
const UnknownStatus = "unknown"

// digestPrefix is how much of the output digest goes in the attestation line.
// Enough to tie a report to a stored log, short enough to paste.
const digestPrefix = 12

// tailBytes is how much of the output the record keeps inline, so the truth
// about a failing run survives even if the log file is cleaned up.
const tailBytes = 4096

// Record is one gate run: what was executed, what status it actually exited
// with, and whether its own output argues with that status.
//
// The JSON form is the in-band channel 🎯T396 needs. When the harness
// misreports a background command's status, this file still has the process's
// own, written by the process's own parent at the moment it was reaped.
type Record struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command []string `json:"command"`
	Dir     string   `json:"dir,omitempty"`

	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended"`

	// ExitStatus is meaningful only when StatusKnown. A reader that ignores
	// StatusKnown and treats the zero value as success is the exact defect
	// this package exists to stop, so Status() is the accessor to use.
	ExitStatus  int  `json:"exit_status"`
	StatusKnown bool `json:"status_known"`
	// StatusNote explains an unknown status (process could not start, killed
	// by a signal that yields no code).
	StatusNote string `json:"status_note,omitempty"`

	Verdict   Verdict   `json:"verdict"`
	Anomalies []Anomaly `json:"anomalies,omitempty"`

	OutputBytes  int    `json:"output_bytes"`
	OutputSHA256 string `json:"output_sha256"`
	OutputPath   string `json:"output_path,omitempty"`
	OutputTail   string `json:"output_tail,omitempty"`
}

// Status renders the exit status for humans: the number, or "unknown".
func (r *Record) Status() string {
	if r == nil || !r.StatusKnown {
		return UnknownStatus
	}
	return strconv.Itoa(r.ExitStatus)
}

// Duration is how long the gate ran.
func (r *Record) Duration() time.Duration {
	if r == nil || r.Started.IsZero() || r.Ended.IsZero() {
		return 0
	}
	return r.Ended.Sub(r.Started)
}

// Attestation is the single line a worker pastes into a finish report. It is
// the only sanctioned way to cite a gate result, because it is the only form
// that carries the status, the verdict and a handle to the stored output
// together — a green claim can then be checked rather than believed.
//
//	GATE test-go exit=0 GREEN id=9f13c0a2 out=6b1d9e4f2a01 dur=12.3s
func (r *Record) Attestation() string {
	if r == nil {
		return ""
	}
	digest := r.OutputSHA256
	if len(digest) > digestPrefix {
		digest = digest[:digestPrefix]
	}
	return fmt.Sprintf("GATE %s exit=%s %s id=%s out=%s dur=%s",
		r.Name, r.Status(), r.Verdict, r.ID, digest,
		r.Duration().Round(100*time.Millisecond))
}

// Summary is the multi-line form for a terminal: the attestation plus the
// reason a non-green run is not green.
func (r *Record) Summary() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(r.Attestation())
	if r.StatusNote != "" {
		fmt.Fprintf(&b, "\n  status: %s", r.StatusNote)
	}
	for _, a := range r.Anomalies {
		fmt.Fprintf(&b, "\n  output contradicts a pass (%s): %s", a.Marker, a.Line)
	}
	if r.OutputPath != "" {
		fmt.Fprintf(&b, "\n  output: %s", r.OutputPath)
	}
	return b.String()
}

// attestationRe parses the line back out of a finish report. Tolerant of
// surrounding prose and markdown, strict about the fields.
var attestationRe = regexp.MustCompile(
	`GATE\s+(\S+)\s+exit=(\S+)\s+(GREEN|RED|SUSPECT|UNKNOWN)\s+id=([0-9a-zA-Z]+)`)

// Attestation is a claim found in a report: what the worker says a gate did.
// Whether it is true is a question for the Store.
type CitedAttestation struct {
	Name    string
	Status  string
	Verdict Verdict
	ID      string
	Raw     string
}

// StatusIsZero reports whether the cited status is a literal zero. "unknown"
// is not zero, which is the whole point.
func (c CitedAttestation) StatusIsZero() bool { return c.Status == "0" }

// ParseAttestations extracts every gate attestation cited in text.
func ParseAttestations(text string) []CitedAttestation {
	ms := attestationRe.FindAllStringSubmatch(text, -1)
	out := make([]CitedAttestation, 0, len(ms))
	for _, m := range ms {
		out = append(out, CitedAttestation{
			Name: m[1], Status: m[2], Verdict: Verdict(m[3]), ID: m[4], Raw: m[0],
		})
	}
	return out
}

// Store holds gate records on disk. Concurrent workers share it; every write
// is write-and-rename so a reader never sees a half-written record.
type Store struct {
	Root string
}

// DefaultStoreRoot is ~/.jevons/gates unless StoreDirEnv is set. Mirrors
// treeguard's convention so the two guards' state sits together.
func DefaultStoreRoot() string {
	if dir := os.Getenv(StoreDirEnv); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "jevons-gates")
	}
	return filepath.Join(home, ".jevons", "gates")
}

// OpenStore returns a store rooted at root, creating it. An empty root means
// DefaultStoreRoot.
func OpenStore(root string) (*Store, error) {
	if root == "" {
		root = DefaultStoreRoot()
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("gate store %s: %w", root, err)
	}
	return &Store{Root: root}, nil
}

func (s *Store) recordPath(id string) string {
	return filepath.Join(s.Root, id+".json")
}

// LogPath is where a run's captured output lives.
func (s *Store) LogPath(id string) string {
	return filepath.Join(s.Root, id+".log")
}

// Save writes rec atomically. The caller has already written the log.
func (s *Store) Save(rec *Record) error {
	if s == nil || rec == nil || rec.ID == "" {
		return fmt.Errorf("gate save: no store or record id")
	}
	blob, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("gate save %s: %w", rec.ID, err)
	}
	return writeAtomic(s.recordPath(rec.ID), append(blob, '\n'), 0o644)
}

// Load returns the record for id, or ok=false when no such run was recorded.
// A malformed record is an error, never a silent miss: the store is evidence,
// and evidence that quietly disappears is how a false green survives.
func (s *Store) Load(id string) (*Record, bool, error) {
	if s == nil || id == "" {
		return nil, false, nil
	}
	blob, err := os.ReadFile(s.recordPath(id))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("gate load %s: %w", id, err)
	}
	var rec Record
	if err := json.Unmarshal(blob, &rec); err != nil {
		return nil, false, fmt.Errorf("gate load %s: malformed record: %w", id, err)
	}
	return &rec, true, nil
}

// Recent returns the most recent limit records, newest first.
func (s *Store) Recent(limit int) ([]*Record, error) {
	if s == nil {
		return nil, nil
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("gate recent: %w", err)
	}
	var recs []*Record
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		rec, ok, err := s.Load(id)
		if err != nil || !ok {
			continue
		}
		recs = append(recs, rec)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Started.After(recs[j].Started) })
	if limit > 0 && len(recs) > limit {
		recs = recs[:limit]
	}
	return recs, nil
}

// Lookup adapts a store to the FlagFalseGreen signature. A store that errors
// answers "no such record" rather than vouching for anything.
func (s *Store) Lookup(id string) (*Record, bool) {
	rec, ok, err := s.Load(id)
	if err != nil || !ok {
		return nil, false
	}
	return rec, true
}

// writeAtomic writes to a sibling temp file and renames, so a concurrent
// reader sees either the old record or the new one.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".gate-*")
	if err != nil {
		return fmt.Errorf("gate write %s: %w", path, err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("gate write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("gate write %s: %w", path, err)
	}
	if err := os.Chmod(name, mode); err != nil {
		return fmt.Errorf("gate write %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("gate write %s: %w", path, err)
	}
	return nil
}

// digest returns the hex sha256 of b.
func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// tailOf returns the last tailBytes of b, cut at a line boundary when it can.
func tailOf(b []byte) string {
	if len(b) <= tailBytes {
		return string(b)
	}
	cut := b[len(b)-tailBytes:]
	if i := strings.IndexByte(string(cut), '\n'); i >= 0 && i < len(cut)-1 {
		cut = cut[i+1:]
	}
	return string(cut)
}
