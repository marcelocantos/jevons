// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 🎯T774 — A BACKLOG DRAINS AS ONE DIGEST, NOT ONE RECIPIENT TURN PER ENTRY.
//
// 2026-09-21: claudia-po held 242 pending messages (240,754 bytes: 105 worker
// reports, 52 held-backlog relays, 41 worker-idle events, 10 false-green
// checks) and drained them at one Opus turn each. Most were superseded before
// they were ever read — forty-one idle notices for a handful of workers, a
// relay for a seat that had since reported again — so the recipient paid for
// hundreds of turns to learn what a page would have said.
//
// Past DigestThreshold, the drain claims every Pending entry at once and
// delivers one message: newest state per sender first, superseded entries
// collapsed, the collapse itself stated with counts. Nothing disappears: the
// originals are archived by entry id (ReadArchived) before the queue forgets
// them, and the digest names those ids.
//
// What a digest never touches: an Attempting or Uncertain entry (🎯T726 —
// held stays held, nothing is silently dropped or replayed), and any
// message it cannot classify (delivered verbatim, in order).

// DigestThreshold is the largest backlog still delivered one message per turn.
// "More than a handful" — above it, the seat gets a digest.
const DigestThreshold = 5

// digestDirName is the archive of originals folded into digests, beside the
// per-agent queues. A directory, so Agents (which lists only .json files at
// the top level) never mistakes it for a queue.
const digestDirName = "digests"

type msgKind int

const (
	kindOther msgKind = iota
	kindReport
	kindIdle
	kindHeld
)

type classified struct {
	e      Entry
	kind   msgKind
	sender string
}

func firstLines(text string, n int) []string {
	lines := strings.SplitN(text, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return lines
}

// classify recognises the machine-generated shapes that supersede each other.
// Anything else is kindOther: the digest never guesses about it.
func classify(e Entry) classified {
	c := classified{e: e, kind: kindOther}
	text := strings.TrimSpace(e.Text)
	switch {
	case strings.HasPrefix(text, "[event: worker-idle]"):
		for _, line := range strings.Split(text, "\n") {
			if w, ok := strings.CutPrefix(line, "Worker: "); ok && strings.TrimSpace(w) != "" {
				c.kind, c.sender = kindIdle, strings.TrimSpace(w)
				return c
			}
		}
	case strings.HasPrefix(text, "[held backlog from "):
		rest := strings.TrimPrefix(text, "[held backlog from ")
		if i := strings.IndexAny(rest, " \n]"); i > 0 {
			c.kind, c.sender = kindHeld, rest[:i]
			return c
		}
	}
	// A report may carry a banner line or two ahead of its header.
	for _, line := range firstLines(text, 4) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "[Agent "); ok {
			if name, _, found := strings.Cut(rest, " responded]"); found && name != "" {
				c.kind, c.sender = kindReport, name
				return c
			}
		}
	}
	return c
}

// reportID pulls "report_id=…" from a report's header, or "".
func reportID(text string) string {
	for _, line := range firstLines(text, 4) {
		if i := strings.Index(line, "report_id="); i >= 0 {
			id := line[i+len("report_id="):]
			if j := strings.IndexAny(id, " \t"); j >= 0 {
				id = id[:j]
			}
			return id
		}
	}
	return ""
}

func ids(cs []classified) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = c.e.ID
		if rid := reportID(c.e.Text); rid != "" && c.kind == kindReport {
			parts[i] += " (report_id=" + rid + ")"
		}
	}
	return strings.Join(parts, ", ")
}

// BuildDigest renders entries (oldest first) as one message addressed to seat.
// Pure: it reads nothing and archives nothing.
func BuildDigest(seat string, entries []Entry) string {
	var reports, idles, helds map[string][]classified
	reports, idles, helds = map[string][]classified{}, map[string][]classified{}, map[string][]classified{}
	var other []classified
	for _, e := range entries {
		c := classify(e)
		switch c.kind {
		case kindReport:
			reports[c.sender] = append(reports[c.sender], c)
		case kindIdle:
			idles[c.sender] = append(idles[c.sender], c)
		case kindHeld:
			helds[c.sender] = append(helds[c.sender], c)
		default:
			other = append(other, c)
		}
	}

	// newestFirst orders sender groups by their newest entry, latest first.
	newestFirst := func(m map[string][]classified) []string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := m[keys[i]], m[keys[j]]
			ta, tb := a[len(a)-1].e.EnqueuedAt, b[len(b)-1].e.EnqueuedAt
			if !ta.Equal(tb) {
				return ta.After(tb)
			}
			return keys[i] < keys[j]
		})
		return keys
	}

	var body strings.Builder
	collapsed := 0
	stamp := func(t time.Time) string { return t.UTC().Format("15:04:05Z") }

	if len(other) > 0 {
		fmt.Fprintf(&body, "\n== Messages (%d, delivered in full, oldest first) ==\n", len(other))
		for _, c := range other {
			fmt.Fprintf(&body, "\n-- %s, entry id=%s --\n%s\n", stamp(c.e.EnqueuedAt), c.e.ID, strings.TrimSpace(c.e.Text))
		}
	}

	if len(reports) > 0 {
		fmt.Fprintf(&body, "\n== Worker reports (%d senders, newest sender first) ==\n", len(reports))
		for _, sender := range newestFirst(reports) {
			rs := reports[sender]
			newest := rs[len(rs)-1]
			fmt.Fprintf(&body, "\n-- %s: newest of %d report(s), %s, entry id=%s --\n%s\n",
				sender, len(rs), stamp(newest.e.EnqueuedAt), newest.e.ID, strings.TrimSpace(newest.e.Text))
			if len(rs) > 1 {
				collapsed += len(rs) - 1
				fmt.Fprintf(&body, "   superseded by the above: %d earlier report(s) from %s, readable by entry id: %s\n",
					len(rs)-1, sender, ids(rs[:len(rs)-1]))
			}
			// A relay for a seat that has since reported is folded into its report.
			if hs := helds[sender]; len(hs) > 0 {
				collapsed += len(hs)
				fmt.Fprintf(&body, "   folded in: %d held-backlog relay(s) for %s (it has since reported), entry id: %s\n",
					len(hs), sender, ids(hs))
				delete(helds, sender)
			}
		}
	}

	if len(helds) > 0 {
		fmt.Fprintf(&body, "\n== Held-backlog relays (%d seats, newest first) ==\n", len(helds))
		for _, sender := range newestFirst(helds) {
			hs := helds[sender]
			newest := hs[len(hs)-1]
			fmt.Fprintf(&body, "\n-- %s: newest of %d relay(s), %s, entry id=%s --\n%s\n",
				sender, len(hs), stamp(newest.e.EnqueuedAt), newest.e.ID, strings.TrimSpace(newest.e.Text))
			if len(hs) > 1 {
				collapsed += len(hs) - 1
				fmt.Fprintf(&body, "   superseded by the above: %d earlier relay(s), entry id: %s\n", len(hs)-1, ids(hs[:len(hs)-1]))
			}
		}
	}

	if len(idles) > 0 {
		fmt.Fprintf(&body, "\n== Worker-idle events (%d workers, newest first) ==\n", len(idles))
		for _, sender := range newestFirst(idles) {
			is := idles[sender]
			newest := is[len(is)-1]
			fmt.Fprintf(&body, "\n-- %s: latest of %d notice(s), %s, entry id=%s --\n%s\n",
				sender, len(is), stamp(newest.e.EnqueuedAt), newest.e.ID, strings.TrimSpace(newest.e.Text))
			if len(is) > 1 {
				collapsed += len(is) - 1
				fmt.Fprintf(&body, "   superseded by the above: %d earlier notice(s), entry id: %s\n", len(is)-1, ids(is[:len(is)-1]))
			}
		}
	}

	var head strings.Builder
	fmt.Fprintf(&head, "[digest of %d queued message(s) for %s — %d collapsed, %d delivered in full]\n",
		len(entries), seat, collapsed, len(entries)-collapsed)
	head.WriteString("These were held while you were busy and are delivered as one message instead of one turn each. " +
		"Newest state per sender comes first. Collapsed entries were superseded by a newer one in this digest; " +
		"nothing was discarded — every original is stored and readable by entry id " +
		"(jevons_sendq_reconcile name=" + seat + " action=read entry_id=<id>; worker reports also via jevons_agent_report_read).\n")
	return head.String() + body.String()
}

// archiveFile is one digest's originals, kept so a collapsed message is never
// unreadable.
type archiveFile struct {
	DigestID string    `json:"digest_id"`
	Agent    string    `json:"agent"`
	At       time.Time `json:"at"`
	Members  []Entry   `json:"members"`
}

// ClaimDigest folds every Pending entry into one Attempting entry when there
// are more than DigestThreshold of them, archiving the originals first. It
// returns ok=false — touching nothing — when the backlog is small, when an
// attempt this store owns is in flight, or when there is nothing pending.
// Held (Attempting/Uncertain) entries stay exactly where they are.
//
// The merged entry resolves like any other: Confirmed removes it (the
// archive keeps the originals), Unverified holds it Uncertain for
// reconciliation, DefinitelyNotSent returns it to Pending — where a later
// ClaimDigest unpacks it back into its members rather than nesting a digest
// inside a digest.
func (s *Store) ClaimDigest(agent string) (Entry, bool, error) {
	if s == nil {
		return Entry{}, false, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil || len(f.Entries) == 0 {
		return Entry{}, false, err
	}
	if _, _, blocked := claimable(f.Entries, s.active[agent]); blocked {
		return Entry{}, false, nil
	}
	var members []Entry
	first := -1
	for i, e := range f.Entries {
		if e.State != Pending {
			continue
		}
		if first < 0 {
			first = i
		}
		if e.Members != "" {
			if orig, ok := s.archivedMembers(e.ID); ok {
				members = append(members, orig...)
				continue
			}
		}
		members = append(members, e)
	}
	if len(members) <= DigestThreshold {
		return Entry{}, false, nil
	}
	sort.SliceStable(members, func(i, j int) bool { return members[i].EnqueuedAt.Before(members[j].EnqueuedAt) })

	merged := Entry{
		ID:         NewID(),
		Text:       BuildDigest(agent, members),
		EnqueuedAt: members[0].EnqueuedAt,
		State:      Attempting,
		AttemptID:  NewID(),
	}
	memberIDs := make([]string, len(members))
	for i, m := range members {
		memberIDs[i] = m.ID
	}
	merged.Members = strings.Join(memberIDs, ",")
	if err := s.archive(archiveFile{DigestID: merged.ID, Agent: agent, At: time.Now().UTC(), Members: members}); err != nil {
		return Entry{}, false, err
	}
	kept := make([]Entry, 0, len(f.Entries)+1)
	for i, e := range f.Entries {
		if i == first {
			kept = append(kept, merged)
		}
		if e.State != Pending {
			kept = append(kept, e)
		}
	}
	f.Entries = kept
	if err := s.save(f); err != nil {
		return Entry{}, false, err
	}
	if s.active == nil {
		s.active = map[string]string{}
	}
	s.active[agent] = merged.AttemptID
	return merged, true, nil
}

func (s *Store) archive(a archiveFile) error {
	if !s.Durable() {
		if s.memArchive == nil {
			s.memArchive = map[string]archiveFile{}
		}
		s.memArchive[a.DigestID] = a
		return nil
	}
	dir := filepath.Join(s.dir, digestDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("sendq: create %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("sendq: encode digest archive: %w", err)
	}
	path := filepath.Join(dir, a.DigestID+".json")
	if err := os.WriteFile(path+".tmp", data, 0o644); err != nil {
		return fmt.Errorf("sendq: write digest archive: %w", err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return fmt.Errorf("sendq: commit digest archive: %w", err)
	}
	return nil
}

// archivedMembers returns the originals of one digest entry. Caller holds mu.
func (s *Store) archivedMembers(digestID string) ([]Entry, bool) {
	if !s.Durable() {
		a, ok := s.memArchive[digestID]
		return a.Members, ok
	}
	data, err := os.ReadFile(filepath.Join(s.dir, digestDirName, digestID+".json"))
	if err != nil {
		return nil, false
	}
	var a archiveFile
	if json.Unmarshal(data, &a) != nil {
		return nil, false
	}
	return a.Members, true
}

// ReadArchived returns the original queue entry with this id from any digest
// it was folded into, or an error naming that it is not archived.
func (s *Store) ReadArchived(id string) (Entry, error) {
	if s == nil {
		return Entry{}, fmt.Errorf("sendq: no store")
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Durable() {
		for _, a := range s.memArchive {
			for _, m := range a.Members {
				if m.ID == id {
					return m, nil
				}
			}
		}
		return Entry{}, fmt.Errorf("sendq: entry %q is not in any digest archive", id)
	}
	ents, err := os.ReadDir(filepath.Join(s.dir, digestDirName))
	if err != nil && !os.IsNotExist(err) {
		return Entry{}, fmt.Errorf("sendq: list digest archive: %w", err)
	}
	for _, ent := range ents {
		if !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, digestDirName, ent.Name()))
		if err != nil {
			continue
		}
		var a archiveFile
		if json.Unmarshal(data, &a) != nil {
			continue
		}
		for _, m := range a.Members {
			if m.ID == id {
				return m, nil
			}
		}
	}
	return Entry{}, fmt.Errorf("sendq: entry %q is not in any digest archive", id)
}
