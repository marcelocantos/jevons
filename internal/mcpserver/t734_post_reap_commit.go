// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/marcelocantos/jevons/internal/attrib"
	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// 🎯T734 — a reaped seat that keeps committing is not silent work.
//
// T165 / T195 remove the registry row. The pane can outlive that removal
// (jv-t717-replay-digest landed 21ac010c four minutes after product:reap_achieve
// of T717). Those commits were ungated: no PO reviewed them, the attestation
// under-recorded the tree, and the next worker on an adjacent leaf discovered
// them by accident.
//
// Product: at reap_done / reap_achieve, snapshot HEAD in the seat's workdir.
// The fleet-health sweep then lists commits after that SHA. Any that mention
// the closed target raise one parent notice naming the seat, the commits, and
// the target. A reap with nothing in flight produces no notice.

const (
	postReapCommitPrefix = "[post-reap-commit "
	postReapCommitWindow = 30 * time.Minute
)

// gitCommit is one commit the leftover pane (or anyone) landed after a reap.
type gitCommit struct {
	SHA     string
	Subject string
	Body    string
}

// postReapWatch is the HEAD snapshot taken as a seat leaves.
type postReapWatch struct {
	Seat     string
	Parent   string
	TargetID string
	WorkDir  string
	Head     string
	ReapedAt time.Time
	Notified map[string]struct{}
}

// commitMentionsTarget is true when text names targetID as a token
// (T734 / 🎯T734), not as a prefix of a longer id (T7340, T734.1).
func commitMentionsTarget(text, targetID string) bool {
	id := normalizeTargetGrepID(targetID)
	if id == "" || text == "" {
		return false
	}
	needle := strings.ToLower(id)
	if !strings.HasPrefix(needle, "t") {
		needle = "t" + needle
	}
	lower := strings.ToLower(text)
	for i := 0; i+len(needle) <= len(lower); i++ {
		if lower[i:i+len(needle)] != needle {
			continue
		}
		if i > 0 && targetIDTokenRune(rune(lower[i-1])) {
			continue
		}
		if j := i + len(needle); j < len(lower) && targetIDTokenRune(rune(lower[j])) {
			continue
		}
		return true
	}
	return false
}

func targetIDTokenRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.'
}

// FilterPostReapCommits keeps commits that name the closed target.
// Empty target or empty input yields nothing — a quiet reap stays quiet.
func FilterPostReapCommits(commits []gitCommit, targetID string) []gitCommit {
	if normalizeTargetGrepID(targetID) == "" {
		return nil
	}
	var out []gitCommit
	for _, c := range commits {
		if commitMentionsTarget(c.Subject+"\n"+c.Body, targetID) {
			out = append(out, c)
		}
	}
	return out
}

// FormatPostReapCommitNotice is the parent-facing 🎯T734 review: a reaped
// seat landed commits after it left, under a closed target's label.
func FormatPostReapCommitNotice(seat, targetID, successor string, commits []gitCommit) string {
	tid := FormatTargetID(targetID)
	if tid == "" {
		tid = "the closed target"
	}
	seat = strings.TrimSpace(seat)
	if seat == "" {
		seat = "reaped seat"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s🎯T734] reaped seat %s landed ", postReapCommitPrefix, seat)
	for i, c := range commits {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(shortCommitSHA(c.SHA))
	}
	if len(commits) == 0 {
		b.WriteString("no-sha")
	}
	fmt.Fprintf(&b, " after the reap of closed target %s. ", tid)
	if successor = strings.TrimSpace(successor); successor != "" {
		fmt.Fprintf(&b, "Live successor %s is engaged on %s; attribute this work to that seat or treat the achieve as no longer covering the tree.", successor, tid)
	} else {
		b.WriteString("The achieve no longer covers the tree; review the commits before they are inherited by the next worker.")
	}
	for _, c := range commits {
		fmt.Fprintf(&b, "\n- %s %s", c.SHA, strings.TrimSpace(c.Subject))
	}
	return b.String()
}

func shortCommitSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func listCommitsAfter(workdir, since string) ([]gitCommit, error) {
	workdir = strings.TrimSpace(workdir)
	since = strings.TrimSpace(since)
	if workdir == "" || since == "" {
		return nil, nil
	}
	out, err := attrib.Git(workdir, "log", "--format=%H%x00%s%x00%b%x1e", "--reverse", since+"..HEAD")
	if err != nil {
		return nil, err
	}
	var commits []gitCommit
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimSpace(rec)
		if rec == "" {
			continue
		}
		parts := strings.SplitN(rec, "\x00", 3)
		if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" {
			continue
		}
		c := gitCommit{SHA: strings.TrimSpace(parts[0]), Subject: strings.TrimSpace(parts[1])}
		if len(parts) == 3 {
			c.Body = strings.TrimSpace(parts[2])
		}
		commits = append(commits, c)
	}
	return commits, nil
}

func snapshotHEAD(workdir string) (string, string, error) {
	root, err := attrib.RepoRoot(workdir)
	if err != nil {
		return "", "", err
	}
	head, err := attrib.Git(root, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	return root, strings.TrimSpace(head), nil
}

func reapReasonWatchesCommits(reason string) bool {
	switch strings.TrimSpace(reason) {
	case fleetlog.ReasonReapDone, fleetlog.ReasonReapAchieve:
		return true
	default:
		return false
	}
}

// armPostReapCommitWatch snapshots HEAD for a reap_done / reap_achieve
// removal so a leftover pane's later commits can be attributed.
func (s *Server) armPostReapCommitWatch(name string, rm fleetlog.Removal) {
	if s == nil || !reapReasonWatchesCommits(rm.Reason) {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	n, ok := s.RemovalAccount().NoticeFor(name)
	if !ok {
		return
	}
	if strings.TrimSpace(n.WorkDir) == "" || strings.TrimSpace(n.TargetID) == "" {
		return
	}
	root, head, err := snapshotHEAD(n.WorkDir)
	if err != nil || head == "" {
		slog.Debug("T734 post-reap watch not armed",
			"component", "post_reap_commit", "agent", name, "err", err)
		return
	}
	w := postReapWatch{
		Seat:     name,
		Parent:   strings.TrimSpace(n.Parent),
		TargetID: n.TargetID,
		WorkDir:  root,
		Head:     head,
		ReapedAt: n.At,
		Notified: map[string]struct{}{},
	}
	if w.ReapedAt.IsZero() {
		w.ReapedAt = s.sweepClock()
	}
	s.mu.Lock()
	if s.postReapWatches == nil {
		s.postReapWatches = map[string]postReapWatch{}
	}
	s.postReapWatches[name] = w
	s.mu.Unlock()
	slog.Info("T734 armed post-reap commit watch",
		"component", "post_reap_commit", "agent", name,
		"target", n.TargetID, "head", shortCommitSHA(head))
}

func (s *Server) liveSuccessorForTarget(seat, targetID string) string {
	if s == nil || s.registry == nil {
		return ""
	}
	want := NormalizeTargetID(targetID)
	if want == "" {
		return ""
	}
	seat = strings.TrimSpace(seat)
	for _, def := range s.registry.List() {
		if def.Name == seat {
			continue
		}
		if NormalizeTargetID(def.TargetID) != want {
			continue
		}
		if DurableFleetAgent(def.Name, def.Purpose, s.isOverseerAgent) {
			continue
		}
		return def.Name
	}
	return ""
}

// SweepPostReapCommits delivers one parent notice per reaped seat that
// landed closed-target commits after its snapshot. Quiet reaps stay quiet.
func (s *Server) SweepPostReapCommits() {
	if s == nil {
		return
	}
	now := s.sweepClock()
	s.mu.Lock()
	watches := make([]postReapWatch, 0, len(s.postReapWatches))
	for _, w := range s.postReapWatches {
		watches = append(watches, w)
	}
	s.mu.Unlock()
	for _, w := range watches {
		s.sweepOnePostReapWatch(w, now)
	}
}

func (s *Server) sweepOnePostReapWatch(w postReapWatch, now time.Time) {
	if w.Seat == "" {
		return
	}
	if s.registry != nil && s.registry.Def(w.Seat) != nil {
		s.dropPostReapWatch(w.Seat)
		return
	}
	if !w.ReapedAt.IsZero() && now.Sub(w.ReapedAt) > postReapCommitWindow {
		s.dropPostReapWatch(w.Seat)
		return
	}
	commits, err := listCommitsAfter(w.WorkDir, w.Head)
	if err != nil {
		slog.Debug("T734 post-reap git log failed",
			"component", "post_reap_commit", "agent", w.Seat, "err", err)
		return
	}
	hits := FilterPostReapCommits(commits, w.TargetID)
	if len(hits) == 0 {
		return
	}
	s.mu.Lock()
	cur, ok := s.postReapWatches[w.Seat]
	if !ok {
		s.mu.Unlock()
		return
	}
	if cur.Notified == nil {
		cur.Notified = map[string]struct{}{}
	}
	var fresh []gitCommit
	for _, c := range hits {
		if _, seen := cur.Notified[c.SHA]; seen {
			continue
		}
		cur.Notified[c.SHA] = struct{}{}
		fresh = append(fresh, c)
	}
	s.postReapWatches[w.Seat] = cur
	s.mu.Unlock()
	if len(fresh) == 0 {
		return
	}
	s.deliverPostReapCommitNotice(cur, fresh)
}

func (s *Server) dropPostReapWatch(name string) {
	s.mu.Lock()
	delete(s.postReapWatches, name)
	s.mu.Unlock()
}

func (s *Server) deliverPostReapCommitNotice(w postReapWatch, commits []gitCommit) {
	parent := strings.TrimSpace(w.Parent)
	if parent == "" {
		parent = "jevons-po"
	}
	msg := FormatPostReapCommitNotice(w.Seat, w.TargetID, s.liveSuccessorForTarget(w.Seat, w.TargetID), commits)
	if _, err := s.deliverByName(parent, msg, OriginAgent, false); err != nil {
		slog.Warn("T734 post-reap commit notice undelivered to parent; escalating to overseer",
			"component", "post_reap_commit", "parent", parent, "agent", w.Seat, "err", err)
		s.notifyFleetHealth(w.Seat, fmt.Sprintf("PO %s unreachable (%v) for: %s", parent, err, msg))
	}
}
