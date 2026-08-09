// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package research is the ambient research staff cycle (🎯T356): standing
// cycles explore the surrounding context (repos, frontier, eventlog, sessions)
// and news feeds, and fold what they find into durable, versioned research
// notes rather than one-shot chat dumps.
//
// Two rails hold the doctrine:
//
//   - No silent overwrite. A prior conclusion is never edited away; a changed
//     claim supersedes the old one explicitly, with provenance on both.
//   - Bounded cycles. An unchanged observation writes no revision and delivers
//     no brief — the cycle goes quiet when there is nothing new to say.
package research

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Finding status values.
const (
	// StatusCurrent is the live claim for a key.
	StatusCurrent = "current"
	// StatusSuperseded marks a claim explicitly replaced by a later revision.
	StatusSuperseded = "superseded"
)

const (
	// MaxRevisions caps retained revision history per note.
	MaxRevisions = 100
	// MaxFindings caps retained findings per note (superseded drop first).
	MaxFindings = 200
	// MaxEvidencePerFinding caps provenance rows kept on one finding.
	MaxEvidencePerFinding = 12
)

// Source is one provenance row: where a finding came from.
type Source struct {
	// Kind is git | frontier | eventlog | session | repo | feed.
	Kind string `json:"kind"`
	// Ref is the concrete reference: commit SHA, file path, target id, feed URL.
	Ref string `json:"ref"`
	// At is when the source material was produced (RFC3339, may be empty).
	At string `json:"at,omitempty"`
}

// Finding is one durable research claim about a stable subject (Key).
// Claims are never edited in place: a changed claim supersedes its predecessor.
type Finding struct {
	Key          string   `json:"key"`
	Claim        string   `json:"claim"`
	Evidence     []string `json:"evidence,omitempty"`
	Sources      []Source `json:"sources,omitempty"`
	Status       string   `json:"status"`
	FirstSeenRev int      `json:"first_seen_rev"`
	Revision     int      `json:"revision"`
	FirstSeenAt  string   `json:"first_seen_at,omitempty"`
	LastSeenAt   string   `json:"last_seen_at,omitempty"`
	SeenCount    int      `json:"seen_count,omitempty"`
	SupersededBy int      `json:"superseded_by,omitempty"`
	SupersededAt string   `json:"superseded_at,omitempty"`
}

// Supersession records one explicit replacement of a prior conclusion.
type Supersession struct {
	Key        string `json:"key"`
	PriorClaim string `json:"prior_claim"`
	NewClaim   string `json:"new_claim"`
	PriorRev   int    `json:"prior_rev"`
	Reason     string `json:"reason,omitempty"`
}

// Revision is one cycle's contribution to a note. Revisions accumulate; a
// cycle that observes nothing new appends none.
type Revision struct {
	N          int            `json:"n"`
	At         string         `json:"at"`
	Trigger    string         `json:"trigger"`
	Summary    string         `json:"summary,omitempty"`
	Added      []string       `json:"added,omitempty"`
	Confirmed  []string       `json:"confirmed,omitempty"`
	Superseded []Supersession `json:"superseded,omitempty"`
	Sources    []Source       `json:"sources,omitempty"`
}

// Note is a durable research artifact about one stable topic.
type Note struct {
	ID        string     `json:"id"`
	Topic     string     `json:"topic"`
	Title     string     `json:"title"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
	CheckedAt string     `json:"checked_at,omitempty"`
	Findings  []Finding  `json:"findings,omitempty"`
	Revisions []Revision `json:"revisions,omitempty"`
	// DroppedRevisions counts history trimmed by MaxRevisions, so a truncated
	// note never reads as a complete one.
	DroppedRevisions int `json:"dropped_revisions,omitempty"`
}

// RevisionInput is what a cycle proposes for one note.
type RevisionInput struct {
	Topic    string
	Title    string
	Trigger  string
	Summary  string
	At       time.Time
	Findings []Finding
	Sources  []Source
}

// Delta reports what one Apply changed. Changed is false when the cycle only
// re-observed what the note already said.
type Delta struct {
	NoteID     string
	Topic      string
	Revision   int
	Added      []string
	Confirmed  []string
	Superseded []Supersession
	Changed    bool
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// Slug converts a topic into a stable filesystem-safe note id.
func Slug(topic string) string {
	s := slugStrip.ReplaceAllString(strings.ToLower(strings.TrimSpace(topic)), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "note"
	}
	if len(s) > 80 {
		s = strings.Trim(s[:80], "-")
	}
	return s
}

// NewNote mints an empty note for a topic.
func NewNote(topic, title string, at time.Time) Note {
	topic = strings.TrimSpace(topic)
	if title = strings.TrimSpace(title); title == "" {
		title = topic
	}
	ts := stamp(at)
	return Note{
		ID:        Slug(topic),
		Topic:     topic,
		Title:     title,
		CreatedAt: ts,
		UpdatedAt: ts,
	}
}

// Apply folds a cycle's findings into a note. Pure: the caller persists.
//
// Semantics (🎯T356 acceptance #2):
//   - unseen key            → added, status current
//   - same key, same claim  → confirmed (provenance refreshed, no revision)
//   - same key, new claim   → prior explicitly superseded, new claim added
//
// A revision is appended only when something was added or superseded, so a
// quiet context produces a quiet note.
func Apply(note Note, in RevisionInput) (Note, Delta, error) {
	if strings.TrimSpace(in.Topic) == "" && strings.TrimSpace(note.Topic) == "" {
		return note, Delta{}, fmt.Errorf("research: topic required")
	}
	if note.ID == "" {
		note = NewNote(firstNonEmpty(in.Topic, note.Topic), in.Title, in.At)
	}
	if t := strings.TrimSpace(in.Title); t != "" {
		note.Title = t
	}
	at := stamp(in.At)
	rev := note.nextRevision()
	delta := Delta{NoteID: note.ID, Topic: note.Topic, Revision: rev}

	for _, f := range in.Findings {
		key := strings.TrimSpace(f.Key)
		claim := strings.TrimSpace(f.Claim)
		if key == "" || claim == "" {
			continue
		}
		idx := note.currentIndex(key)
		if idx < 0 {
			note.Findings = append(note.Findings, Finding{
				Key:          key,
				Claim:        claim,
				Evidence:     capEvidence(f.Evidence),
				Sources:      f.Sources,
				Status:       StatusCurrent,
				FirstSeenRev: rev,
				Revision:     rev,
				FirstSeenAt:  at,
				LastSeenAt:   at,
				SeenCount:    1,
			})
			delta.Added = append(delta.Added, key)
			continue
		}
		prior := note.Findings[idx]
		if sameClaim(prior.Claim, claim) {
			prior.LastSeenAt = at
			prior.SeenCount++
			prior.Evidence = capEvidence(mergeStrings(prior.Evidence, f.Evidence))
			prior.Sources = mergeSources(prior.Sources, f.Sources)
			note.Findings[idx] = prior
			delta.Confirmed = append(delta.Confirmed, key)
			continue
		}
		// Explicit supersession — the prior conclusion stays on the record.
		prior.Status = StatusSuperseded
		prior.SupersededBy = rev
		prior.SupersededAt = at
		note.Findings[idx] = prior
		note.Findings = append(note.Findings, Finding{
			Key:          key,
			Claim:        claim,
			Evidence:     capEvidence(f.Evidence),
			Sources:      f.Sources,
			Status:       StatusCurrent,
			FirstSeenRev: prior.FirstSeenRev,
			Revision:     rev,
			FirstSeenAt:  prior.FirstSeenAt,
			LastSeenAt:   at,
			SeenCount:    1,
		})
		delta.Superseded = append(delta.Superseded, Supersession{
			Key:        key,
			PriorClaim: prior.Claim,
			NewClaim:   claim,
			PriorRev:   prior.Revision,
			Reason:     "new findings in cycle " + strings.TrimSpace(in.Trigger),
		})
	}

	note.CheckedAt = at
	delta.Changed = len(delta.Added) > 0 || len(delta.Superseded) > 0
	if !delta.Changed {
		return note, delta, nil
	}
	note.Revisions = append(note.Revisions, Revision{
		N:          rev,
		At:         at,
		Trigger:    strings.TrimSpace(in.Trigger),
		Summary:    strings.TrimSpace(in.Summary),
		Added:      delta.Added,
		Confirmed:  delta.Confirmed,
		Superseded: delta.Superseded,
		Sources:    in.Sources,
	})
	note.UpdatedAt = at
	note.trim()
	return note, delta, nil
}

// CurrentFindings returns live claims, newest revision first.
func (n Note) CurrentFindings() []Finding {
	var out []Finding
	for _, f := range n.Findings {
		if f.Status == StatusCurrent {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Revision > out[j].Revision })
	return out
}

// LatestRevision returns the most recent revision (zero value when none).
func (n Note) LatestRevision() Revision {
	if len(n.Revisions) == 0 {
		return Revision{}
	}
	return n.Revisions[len(n.Revisions)-1]
}

func (n Note) nextRevision() int {
	if len(n.Revisions) == 0 {
		return 1
	}
	return n.Revisions[len(n.Revisions)-1].N + 1
}

func (n Note) currentIndex(key string) int {
	for i, f := range n.Findings {
		if f.Key == key && f.Status == StatusCurrent {
			return i
		}
	}
	return -1
}

// trim bounds retained history. Superseded findings are dropped before live
// ones, and dropped revisions are counted so truncation is visible.
func (n *Note) trim() {
	if len(n.Revisions) > MaxRevisions {
		drop := len(n.Revisions) - MaxRevisions
		n.Revisions = append([]Revision(nil), n.Revisions[drop:]...)
		n.DroppedRevisions += drop
	}
	if len(n.Findings) <= MaxFindings {
		return
	}
	kept := make([]Finding, 0, MaxFindings)
	var superseded []Finding
	for _, f := range n.Findings {
		if f.Status == StatusCurrent {
			kept = append(kept, f)
		} else {
			superseded = append(superseded, f)
		}
	}
	sort.SliceStable(superseded, func(i, j int) bool {
		return superseded[i].SupersededBy > superseded[j].SupersededBy
	})
	for _, f := range superseded {
		if len(kept) >= MaxFindings {
			break
		}
		kept = append(kept, f)
	}
	n.Findings = kept
}

// RenderNote renders a note as markdown — the owner/PO-consumable artifact.
func RenderNote(n Note) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", firstNonEmpty(n.Title, n.Topic))
	fmt.Fprintf(&b, "- topic: `%s`\n- created: %s\n- updated: %s\n- last checked: %s\n- revisions: %d",
		n.Topic, n.CreatedAt, n.UpdatedAt, n.CheckedAt, len(n.Revisions))
	if n.DroppedRevisions > 0 {
		fmt.Fprintf(&b, " (+%d trimmed)", n.DroppedRevisions)
	}
	b.WriteString("\n\n## Current findings\n\n")
	current := n.CurrentFindings()
	if len(current) == 0 {
		b.WriteString("_(none yet)_\n")
	}
	for _, f := range current {
		fmt.Fprintf(&b, "### %s\n\n%s\n\n", f.Key, f.Claim)
		fmt.Fprintf(&b, "- first seen: rev %d (%s), last seen: %s, observed %dx\n",
			f.FirstSeenRev, f.FirstSeenAt, f.LastSeenAt, f.SeenCount)
		for _, e := range f.Evidence {
			fmt.Fprintf(&b, "- evidence: %s\n", e)
		}
		for _, s := range f.Sources {
			fmt.Fprintf(&b, "- source: %s %s %s\n", s.Kind, s.Ref, s.At)
		}
		b.WriteByte('\n')
	}

	var superseded []Finding
	for _, f := range n.Findings {
		if f.Status == StatusSuperseded {
			superseded = append(superseded, f)
		}
	}
	if len(superseded) > 0 {
		b.WriteString("## Superseded conclusions\n\n")
		for _, f := range superseded {
			fmt.Fprintf(&b, "- **%s** (rev %d → superseded by rev %d at %s): %s\n",
				f.Key, f.Revision, f.SupersededBy, f.SupersededAt, f.Claim)
		}
		b.WriteByte('\n')
	}

	b.WriteString("## Revision history\n\n")
	for i := len(n.Revisions) - 1; i >= 0; i-- {
		r := n.Revisions[i]
		fmt.Fprintf(&b, "### rev %d — %s (trigger: %s)\n\n", r.N, r.At, r.Trigger)
		if r.Summary != "" {
			fmt.Fprintf(&b, "%s\n\n", r.Summary)
		}
		if len(r.Added) > 0 {
			fmt.Fprintf(&b, "- added: %s\n", strings.Join(r.Added, ", "))
		}
		if len(r.Confirmed) > 0 {
			fmt.Fprintf(&b, "- confirmed: %s\n", strings.Join(r.Confirmed, ", "))
		}
		for _, s := range r.Superseded {
			fmt.Fprintf(&b, "- superseded `%s` (rev %d): %q → %q\n", s.Key, s.PriorRev, s.PriorClaim, s.NewClaim)
		}
		for _, s := range r.Sources {
			fmt.Fprintf(&b, "- source: %s %s\n", s.Kind, s.Ref)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func sameClaim(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func capEvidence(in []string) []string {
	out := dedupeStrings(in)
	if len(out) > MaxEvidencePerFinding {
		out = out[:MaxEvidencePerFinding]
	}
	return out
}

func mergeStrings(a, b []string) []string {
	return dedupeStrings(append(append([]string(nil), a...), b...))
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func mergeSources(a, b []Source) []Source {
	seen := make(map[string]bool, len(a)+len(b))
	var out []Source
	for _, s := range append(append([]Source(nil), a...), b...) {
		k := s.Kind + "\x00" + s.Ref
		if s.Ref == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
		if len(out) >= MaxEvidencePerFinding {
			break
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func stamp(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format(time.RFC3339)
}
