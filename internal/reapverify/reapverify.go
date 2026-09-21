// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package reapverify records the achieve-or-reopen decision a reaped
// implementer leaves behind (🎯T753).
//
// 🎯T165 / 🎯T195 remove a work seat's registry row on its terminal report.
// The ledger row it was bound to does not move: it stays identified. So the
// moment the row's implementer is gone, that target is back in the ready set
// looking exactly like work nobody has started — while its commits are
// already on master.
//
// Observed on 2026-09-21: jv-t739 and jv-t747 were both absent from
// /api/agents with their implementation landed (T739 at 010051b8, T747 at
// e7c9cb5d / 066f7a03 / b14b212b), both rows still read Identified, and
// view=frontier listed both among the ready leaves, indistinguishable from
// unstarted work. Two costs, and they are different costs: the achieve-or-
// reopen decision is owed but nothing records that anyone owes it, and the
// unattended sweep (🎯T155 / 🎯T193) will spawn a fresh worker onto the leaf,
// paying for the work twice and risking a reimplementation that conflicts
// with the one already in the tree.
//
// This package is the record, not the verdict. It says: this seat was
// reaped, these are the commits it landed under this target, and this party
// owes the achieve-or-reopen call. The PO still adjudicates — 🎯T753 only
// makes the decision visibly owed rather than lost.
//
// A reap that landed NOTHING produces no record on purpose. A seat that said
// done without committing leaves a leaf that really is unstarted, and
// suppressing that leaf would strand it. Only landed work suppresses.
package reapverify

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

// StoreFileName is the durable record under <stateDir>/fleet.
const StoreFileName = "reap-verify.json"

// Commit is one commit the reaped seat landed under the target.
type Commit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

// Pending is one reaped implementer's target awaiting achieve-or-reopen.
type Pending struct {
	// ForceEngageAtReap prevents an existing engagement tag from immediately
	// releasing the hold. Remove it before adding it again to reopen.
	ForceEngageAtReap bool `json:"force_engage_at_reap,omitempty"`
	// TargetID is the ledger row the reaped seat was bound to.
	TargetID string `json:"target_id"`
	// Seat is the reaped agent's registry name.
	Seat string `json:"seat"`
	// Owes names the party that owes the achieve-or-reopen decision. The
	// seat's registry parent, since that is who the reap notifies and who
	// adjudicates; never the reaped seat, which no longer exists.
	Owes string `json:"owes"`
	// Repo is the resolved repository root the commits are in. Scoping the
	// record to a repo keeps another checkout's worker on the same target id
	// from making this leaf look done (🎯T389).
	Repo string `json:"repo"`
	// WorkDir is the seat's workdir verbatim, for the reader.
	WorkDir string `json:"workdir,omitempty"`
	// ReapedAt stamps the removal.
	ReapedAt time.Time `json:"reaped_at"`
	// ReapReason is the classifier that fired on the terminal report.
	ReapReason string `json:"reap_reason,omitempty"`
	// Commits are the commits naming TargetID that were already in the tree
	// when the seat left. Never empty in a stored record.
	Commits []Commit `json:"commits"`
}

// NormalizeTargetID trims and upper-cases a bullseye id so T753, t753 and
// " 🎯T753 " are one key. Digit-only ids gain the T.
func NormalizeTargetID(id string) string {
	id = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "🎯"))
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	id = strings.ToUpper(id)
	if id[0] >= '0' && id[0] <= '9' {
		id = "T" + id
	}
	return id
}

// key scopes a record to one repo's copy of a target id.
func key(targetID, repo string) string {
	return NormalizeTargetID(targetID) + "\x00" + normalizeRepoPath(repo)
}

// normalizeRepoPath canonicalizes a repo root so /var and /private/var match.
func normalizeRepoPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if abs, err := filepath.EvalSymlinks(p); err == nil {
		return abs
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// SHAs lists the recorded commit SHAs in order.
func (p Pending) SHAs() []string {
	out := make([]string, 0, len(p.Commits))
	for _, c := range p.Commits {
		if s := strings.TrimSpace(c.SHA); s != "" {
			out = append(out, s)
		}
	}
	return out
}

type storeFile struct {
	Pending map[string]Pending `json:"pending"`
}

// Store is the durable pending-verification record.
//
// Durable because the daemon restarts more often than a PO adjudicates: an
// in-memory record would be erased by a bounce, and the leaf would silently
// rejoin the ready set — which is the bug.
type Store struct {
	path string
	mu   sync.Mutex
	data storeFile
	now  func() time.Time
}

// Open loads or creates <stateDir>/fleet/reap-verify.json. A malformed file
// is a hard error, never a silent reset: a store that quietly forgets an
// owed decision reproduces the invisibility this package exists to end.
func Open(stateDir string) (*Store, error) {
	if strings.TrimSpace(stateDir) == "" {
		return nil, fmt.Errorf("reapverify: StateDir required")
	}
	dir := filepath.Join(stateDir, "fleet")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{
		path: filepath.Join(dir, StoreFileName),
		data: storeFile{Pending: map[string]Pending{}},
		now:  time.Now,
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, fmt.Errorf("reapverify: empty state file %s", s.path)
	}
	s.data.Pending = nil
	if err := json.Unmarshal(b, &s.data); err != nil {
		return nil, fmt.Errorf("reapverify: parse %s: %w", s.path, err)
	}
	if s.data.Pending == nil {
		return nil, fmt.Errorf("reapverify: missing pending map in %s", s.path)
	}
	for k, p := range s.data.Pending {
		if p.TargetID == "" || len(p.Commits) == 0 || k != key(p.TargetID, p.Repo) {
			return nil, fmt.Errorf("reapverify: invalid pending record %q", k)
		}
	}
	return s, nil
}

// SetClock overrides the clock (tests).
func (s *Store) SetClock(now func() time.Time) {
	if s == nil || now == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

func (s *Store) clockLocked() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// Record stores one reaped implementer's owed decision. A record with no
// commits is refused: nothing landed, so nothing is owed and the leaf is
// genuinely unstarted.
func (s *Store) Record(p Pending) error {
	if s == nil {
		return nil
	}
	p.TargetID = NormalizeTargetID(p.TargetID)
	if p.TargetID == "" {
		return fmt.Errorf("reapverify: target id required")
	}
	if len(p.Commits) == 0 {
		return fmt.Errorf("reapverify: %s: refusing a record with no commits", p.TargetID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ReapedAt.IsZero() {
		p.ReapedAt = s.clockLocked()
	}
	p.ReapedAt = p.ReapedAt.UTC()

	k := key(p.TargetID, p.Repo)
	old, existed := s.data.Pending[k]
	s.data.Pending[k] = p
	if err := s.persistLocked(); err != nil {
		if existed {
			s.data.Pending[k] = old
		} else {
			delete(s.data.Pending, k)
		}
		return err
	}
	return nil
}

// PendingFor returns the record for a target in a repo, if any.
//
// An empty repo matches any record for the target: a caller that cannot
// resolve a repo root must not silently lose the suppression. A non-empty
// repo matches the same repo only.
func (s *Store) PendingFor(targetID, repo string) (Pending, bool) {
	if s == nil {
		return Pending{}, false
	}
	id := NormalizeTargetID(targetID)
	if id == "" {
		return Pending{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	repo = strings.TrimSpace(repo)
	if repo != "" {
		p, ok := s.data.Pending[key(id, repo)]
		if !ok {
			return Pending{}, false
		}
		return p, true
	}
	for _, p := range s.data.Pending {
		if NormalizeTargetID(p.TargetID) == id {
			return p, true
		}
	}
	return Pending{}, false
}

// Resolve drops the record for a target — the achieve-or-reopen decision has
// been made, so the leaf is either closed or deliberately back in play. An
// empty repo resolves every repo's record for that id.
func (s *Store) Resolve(targetID, repo string) error {
	if s == nil {
		return nil
	}
	id := NormalizeTargetID(targetID)
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	repo = strings.TrimSpace(repo)
	old := make(map[string]Pending, len(s.data.Pending))
	for k, v := range s.data.Pending {
		old[k] = v
	}
	before := len(s.data.Pending)
	if repo != "" {
		delete(s.data.Pending, key(id, repo))
	} else {
		for k, p := range s.data.Pending {
			if NormalizeTargetID(p.TargetID) == id {
				delete(s.data.Pending, k)
			}
		}
	}
	if len(s.data.Pending) == before {
		return nil
	}
	if err := s.persistLocked(); err != nil {
		s.data.Pending = old
		return err
	}
	return nil
}

// List returns the records, ordered by target id.
func (s *Store) List() []Pending {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Pending
	for _, p := range s.data.Pending {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TargetID < out[j].TargetID })
	return out
}

// persistLocked writes atomically (write-and-rename). Caller holds mu.
func (s *Store) persistLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// FormatOwedDecisionNotice is the parent-facing account of what the reap
// left behind: the seat, the commits it landed, and the call that is now
// owed. Phrased as a decision to make, not a failure to explain — the work
// may well be finished, and the point is that somebody says so on the record
// instead of the row drifting back into the ready set.
func FormatOwedDecisionNotice(p Pending) string {
	tid := p.TargetID
	if tid == "" {
		tid = "the reaped target"
	} else {
		tid = "🎯" + tid
	}
	seat := strings.TrimSpace(p.Seat)
	if seat == "" {
		seat = "the reaped seat"
	}
	owes := strings.TrimSpace(p.Owes)
	if owes == "" {
		owes = "the product owner"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[reaped-target-pending 🎯T753] %s was reaped on its finish report and %s is still open with its work already in the tree. %s owes the achieve-or-reopen decision; until it is made (add force-engage to explicitly reopen implementation; if already present at reap, remove it first), %s is held out of the unattended ready set so no second worker redoes the landed commits.",
		seat, tid, owes, tid)
	for _, c := range p.Commits {
		fmt.Fprintf(&b, "\n- %s %s", strings.TrimSpace(c.SHA), strings.TrimSpace(c.Subject))
	}
	return b.String()
}
