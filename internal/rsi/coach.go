// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DefaultCoachName is the durable coach agent / identity handle (🎯T243).
const DefaultCoachName = "rsi-coach"

// DefaultCoachRateCap is max judgments delivered to the overseer per cycle.
const DefaultCoachRateCap = 3

// DefaultCoachInterval is how often the coach schedule runs when enabled.
const DefaultCoachInterval = 15 * time.Minute

// CoachEventSource is the event kind stamped on overseer-directed pushes.
const CoachEventSource = "rsi-coach"

// Judgment is one coach surface to the overseer (never a bullseye write).
// Overseer alone decides file / alert / brief PO / ignore (🎯T243).
type Judgment struct {
	// Observation is the coach's one-line summary of what looks wrong.
	Observation string
	// Evidence carries session/turn pointers and short quotes.
	Evidence []EvidenceRef
	// Severity: low | medium | high | critical
	Severity string
	// SuggestedInquiry is what the overseer might investigate next.
	SuggestedInquiry string
	// SolutionSketches are optional non-binding ideas (not commits to act).
	SolutionSketches []string
	// Fingerprint dedupes repeated judgments (same as candidate identity).
	Fingerprint string
	// Name / Count / Kinds mirror the extract candidate when present.
	Name  string
	Count int
	Kinds []string
	// Mode is "" for the forward drip and RetroModeLabel for a bounded
	// retrospective history pass (🎯T353).
	Mode string
}

// EvidenceRef is a pointer into a transcript or event surface.
type EvidenceRef struct {
	// Source is owner_chat | session | eventlog | stream.
	Source string
	// SourceID is chatlog basename, session id, or correlation id.
	SourceID string
	// Quote is a short excerpt (may be empty).
	Quote string
	// Kind is the evidence kind when known (chat_gap, lifecycle_error, …).
	Kind string
}

// JudgmentDeliverer posts a formatted judgment to the overseer.
// Production: fire-and-forget agent_send / event_push into the overseer.
// Never a bullseye filer.
type JudgmentDeliverer interface {
	// DeliverJudgment posts wire text to the overseer. Must not block on a
	// full overseer reply (fire-and-forget / queue). Returns after accept.
	DeliverJudgment(text string) error
}

// CoachCycleArgs configures one pure coach cycle (extract → judge → deliver).
type CoachCycleArgs struct {
	// Evidence is the drip window (new + retained cluster buffer).
	Evidence []Evidence
	// PriorFingerprints suppress re-delivery of recent judgments.
	PriorFingerprints []string
	// Deliverer posts to overseer. Required unless DryRun.
	Deliverer JudgmentDeliverer
	// MinCount defaults to DefaultMinCount.
	MinCount int
	// RateCap defaults to DefaultCoachRateCap (max delivered per cycle).
	RateCap int
	// FocusFilters optional substrings matched against kind|component|decision|msg
	// (case-insensitive). Empty = no filter.
	FocusFilters []string
	// OutcomeSuppressions maps fingerprints to disposition-derived suppression
	// (🎯T333 #4): filed-and-open never re-proposes; achieved/set_aside
	// re-proposes only when the cluster has evidence newer than the outcome.
	OutcomeSuppressions map[string]Suppression
	// Retro marks this as a bounded retrospective history pass (🎯T353):
	// judgments carry Mode=retro and must clear the retro value bar
	// (ClassifyRetroValue) before delivery.
	Retro bool
	// RetroCoarseCount is the coarse-conclusion threshold for the retro value
	// bar (0 = DefaultRetroMinCount). Extraction stays fine-grained.
	RetroCoarseCount int
	// DryRun builds judgments but does not deliver.
	DryRun bool
}

// CoachCycleResult is the outcome of one coach cycle.
type CoachCycleResult struct {
	Judgments []Judgment
	Delivered []Judgment
	Skipped   []SkipReason
	WireTexts []string // formatted messages that would/were delivered
}

// RunCoachCycle extracts candidates from evidence, maps them to judgments,
// and delivers structured messages to the overseer. Never calls bullseye.
func RunCoachCycle(args CoachCycleArgs) (CoachCycleResult, error) {
	minCount := args.MinCount
	if minCount <= 0 {
		minCount = DefaultMinCount
	}
	rateCap := args.RateCap
	if rateCap <= 0 {
		rateCap = DefaultCoachRateCap
		if args.Retro {
			rateCap = DefaultRetroRateCap
		}
	}

	ev := filterEvidenceByFocus(args.Evidence, args.FocusFilters)
	proposed := ExtractCandidates(ev, minCount)
	// Reuse Dedupe with prior delivered fingerprints (no existing targets —
	// coach never consults the ledger for placement).
	keep, skipped := Dedupe(proposed, nil, args.PriorFingerprints)

	// Outcome feed (🎯T333 #4): dispositions suppress re-propose without new evidence.
	if len(args.OutcomeSuppressions) > 0 {
		kept := keep[:0]
		for _, c := range keep {
			sup, ok := args.OutcomeSuppressions[c.Fingerprint]
			if !ok {
				kept = append(kept, c)
				continue
			}
			if sup.Always {
				skipped = append(skipped, SkipReason{
					Fingerprint: c.Fingerprint,
					Name:        c.Name,
					Reason:      "disposition_filed_open",
				})
				continue
			}
			if !c.LatestTS.After(sup.After) {
				skipped = append(skipped, SkipReason{
					Fingerprint: c.Fingerprint,
					Name:        c.Name,
					Reason:      "outcome_suppressed",
				})
				continue
			}
			kept = append(kept, c)
		}
		keep = kept
	}

	// Retro value bar (🎯T353): coarse conclusions only — one-off git noise and
	// bare phrase-friction never reach the overseer from a history pass.
	if args.Retro {
		kept := keep[:0]
		for _, c := range keep {
			if v := ClassifyRetroValue(c, args.RetroCoarseCount); !v.OK {
				skipped = append(skipped, SkipReason{
					Fingerprint: c.Fingerprint,
					Name:        c.Name,
					Reason:      v.Reason,
				})
				continue
			}
			kept = append(kept, c)
		}
		keep = kept
	}

	out := CoachCycleResult{Skipped: skipped}
	judgments := make([]Judgment, 0, len(keep))
	for _, c := range keep {
		j := CandidateToJudgment(c, ev)
		if args.Retro {
			j.Mode = RetroModeLabel
		}
		judgments = append(judgments, j)
	}
	out.Judgments = judgments

	n := 0
	for _, j := range judgments {
		wire := FormatJudgmentMessage(j)
		out.WireTexts = append(out.WireTexts, wire)
		if n >= rateCap {
			out.Skipped = append(out.Skipped, SkipReason{
				Fingerprint: j.Fingerprint,
				Name:        j.Name,
				Reason:      "rate_cap_per_cycle",
			})
			continue
		}
		if args.DryRun || args.Deliverer == nil {
			if !args.DryRun && args.Deliverer == nil {
				return out, fmt.Errorf("rsi coach: deliverer required when not dry-run")
			}
			out.Skipped = append(out.Skipped, SkipReason{
				Fingerprint: j.Fingerprint,
				Name:        j.Name,
				Reason:      "dry_run",
			})
			continue
		}
		if err := args.Deliverer.DeliverJudgment(wire); err != nil {
			out.Skipped = append(out.Skipped, SkipReason{
				Fingerprint: j.Fingerprint,
				Name:        j.Name,
				Reason:      "deliver_error: " + err.Error(),
			})
			continue
		}
		out.Delivered = append(out.Delivered, j)
		n++
	}
	return out, nil
}

// CandidateToJudgment maps an extract candidate to a coach judgment.
// Phrase-list / eventlog extract may feed proposals; never write bullseye here.
func CandidateToJudgment(c Candidate, allEvidence []Evidence) Judgment {
	sev := severityForCandidate(c)
	obs := strings.TrimSpace(c.Name)
	if obs == "" {
		obs = "Repeated friction signal observed"
	}
	refs := evidenceRefsForCandidate(c, allEvidence)
	inquiry := fmt.Sprintf(
		"Is this a real product gap worth a bullseye target, owner alert, PO brief, or deliberate ignore? Cluster count=%d kinds=%v sources=%v",
		c.Count, c.Kinds, c.EvidenceIDs,
	)
	sketches := solutionSketches(c)
	fp := c.Fingerprint
	if fp == "" {
		fp = FingerprintOf(c.Name, c.Context)
	}
	return Judgment{
		Observation:      obs,
		Evidence:         refs,
		Severity:         sev,
		SuggestedInquiry: inquiry,
		SolutionSketches: sketches,
		Fingerprint:      fp,
		Name:             c.Name,
		Count:            c.Count,
		Kinds:            append([]string{}, c.Kinds...),
	}
}

// FormatJudgmentMessage builds the overseer-directed wire text (event body).
// Callers wrap with butler.FormatEventPush(CoachEventSource, …) when pushing.
func FormatJudgmentMessage(j Judgment) string {
	var b strings.Builder
	sev := strings.TrimSpace(j.Severity)
	if sev == "" {
		sev = "medium"
	}
	b.WriteString(fmt.Sprintf("RSI coach judgment (severity=%s)\n\n", sev))
	if j.Mode == RetroModeLabel {
		b.WriteString("Mode: retrospective (bounded history pass over transcripts, git, eventlog — 🎯T353)\n")
	}
	b.WriteString("Observation: ")
	b.WriteString(strings.TrimSpace(j.Observation))
	b.WriteByte('\n')
	if len(j.Evidence) > 0 {
		b.WriteString("Evidence:\n")
		for _, e := range j.Evidence {
			src := firstNonEmpty(e.Source, "unknown")
			id := strings.TrimSpace(e.SourceID)
			line := fmt.Sprintf("  - source=%s", src)
			if id != "" {
				line += " id=" + id
			}
			if k := strings.TrimSpace(e.Kind); k != "" {
				line += " kind=" + k
			}
			if q := strings.TrimSpace(e.Quote); q != "" {
				line += fmt.Sprintf(" quote=%q", truncate(q, 120))
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	b.WriteString("Severity: ")
	b.WriteString(sev)
	b.WriteByte('\n')
	if iq := strings.TrimSpace(j.SuggestedInquiry); iq != "" {
		b.WriteString("Suggested inquiry: ")
		b.WriteString(iq)
		b.WriteByte('\n')
	}
	if len(j.SolutionSketches) > 0 {
		b.WriteString("Solution sketches:\n")
		for _, s := range j.SolutionSketches {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			b.WriteString("  - ")
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	if fp := strings.TrimSpace(j.Fingerprint); fp != "" {
		b.WriteString("Fingerprint: ")
		b.WriteString(fp)
		b.WriteByte('\n')
	}
	b.WriteString("\nOverseer: you alone decide — file target(s) where, alert owner, brief PO, ignore with reason, or other. ")
	b.WriteString("Coach does not call bullseye file/achieve/set_aside and does not implement product work (🎯T243).")
	return b.String()
}

// DefaultCoachSystemPrompt is the standing coach identity (retunable by overseer).
func DefaultCoachSystemPrompt() string {
	return strings.TrimSpace(`
You are the ambient RSI coach (rsi-coach) for Jevons (🎯T243).

Role:
- Continuously observe owner main chat (priority) and other in-flight transcripts.
- Form judgments: observation, evidence (session/turn pointers, quotes), severity,
  suggested inquiry, optional solution sketches.
- Deliver judgments ONLY to the overseer (jevons) via structured event messages.
- Do NOT implement product work. Do NOT call bullseye file/achieve/set_aside yourself.
- Do NOT write the intent ledger. Overseer alone decides outcomes.

Noise control:
- Prefer repeated, load-bearing friction over one-off anecdotes.
- Alert fatigue is a dial the overseer retunes (loop frequency, rate cap, focus filters),
  not a hard veto you invent alone.

When unsure whether something is a real gap, still surface it with lower severity and a clear inquiry —
the overseer decides file vs drop.
`)
}

func severityForCandidate(c Candidate) string {
	kinds := map[string]struct{}{}
	for _, k := range c.Kinds {
		kinds[k] = struct{}{}
	}
	if _, ok := kinds["lifecycle_error"]; ok && c.Count >= 4 {
		return "high"
	}
	if _, ok := kinds["event_push_error"]; ok && c.Count >= 3 {
		return "high"
	}
	if _, ok := kinds["stuck_work"]; ok {
		return "high"
	}
	if _, ok := kinds["cost_anomaly"]; ok {
		return "medium"
	}
	if _, ok := kinds["chat_gap"]; ok {
		if c.Count >= 4 {
			return "high"
		}
		return "medium"
	}
	if _, ok := kinds["transcript_friction"]; ok {
		return "medium"
	}
	if _, ok := kinds["git_revert"]; ok {
		if c.Count >= 3 {
			return "high"
		}
		return "medium"
	}
	if _, ok := kinds["git_rework"]; ok {
		if c.Count >= 6 {
			return "high"
		}
		return "medium"
	}
	if c.Count >= 5 {
		return "high"
	}
	if c.Count >= 3 {
		return "medium"
	}
	return "low"
}

func evidenceRefsForCandidate(c Candidate, all []Evidence) []EvidenceRef {
	idSet := map[string]struct{}{}
	for _, id := range c.EvidenceIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			idSet[id] = struct{}{}
		}
	}
	kindSet := map[string]struct{}{}
	for _, k := range c.Kinds {
		kindSet[k] = struct{}{}
	}
	var refs []EvidenceRef
	seen := map[string]struct{}{}
	for _, e := range all {
		if len(refs) >= 6 {
			break
		}
		match := false
		if e.SourceID != "" {
			if _, ok := idSet[e.SourceID]; ok {
				match = true
			}
		}
		if !match && len(kindSet) > 0 {
			if _, ok := kindSet[e.Kind]; ok {
				// Loose match by kind when ids missing; require component token in name.
				match = true
			}
		}
		if !match {
			continue
		}
		key := e.SourceID + "|" + e.Kind + "|" + normalizeMsg(e.Message)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		src := "eventlog"
		if e.Fields != nil {
			if s := strings.TrimSpace(e.Fields["source"]); s != "" {
				src = s
			}
		}
		switch e.Kind {
		case "chat_gap":
			src = "owner_chat"
		case "transcript_friction":
			src = "session"
		case "reaped_idle":
			src = "stream"
		}
		quote := e.Message
		if e.Fields != nil {
			if p := strings.TrimSpace(e.Fields["phrase"]); p != "" {
				quote = p
			}
		}
		refs = append(refs, EvidenceRef{
			Source:   src,
			SourceID: e.SourceID,
			Quote:    truncate(quote, 120),
			Kind:     e.Kind,
		})
	}
	// Fallback: ids only.
	if len(refs) == 0 {
		for _, id := range c.EvidenceIDs {
			if len(refs) >= 4 {
				break
			}
			refs = append(refs, EvidenceRef{Source: "unknown", SourceID: id})
		}
	}
	return refs
}

func solutionSketches(c Candidate) []string {
	var out []string
	for _, k := range c.Kinds {
		switch k {
		case "chat_gap":
			out = append(out, "File a Build-plane target with owner-visible acceptance if friction is load-bearing.")
			out = append(out, "If one-off owner venting, ignore with reason and optionally retune coach focus filters.")
		case "transcript_friction":
			out = append(out, "Inspect the named session for stuck tool loops; brief PO only if a mission is open.")
		case "lifecycle_error", "event_push_error", "tool_failure":
			out = append(out, "Check eventlog component/decision cluster; fix harness path or file target with oracle.")
		case "stuck_work":
			out = append(out, "Verify idle-nudge / fleet-recover already acted; escalate residual stuck mission to PO.")
		case "cost_anomaly":
			out = append(out, "Inspect cost monitor; clamp or explain burn before more fleet spawn.")
		case "git_rework":
			out = append(out, "Read the cited commits together: repeated repair in one scope usually means a missing oracle, not a missing fix.")
		case "git_revert":
			out = append(out, "Check whether the reverted change lacked a gate; a target with acceptance beats a re-attempt.")
		}
	}
	if len(out) == 0 {
		out = append(out, "Confirm recurrence, then file with acceptance or explicitly ignore.")
	}
	// Stable unique.
	sort.Strings(out)
	uniq := out[:0]
	var prev string
	for _, s := range out {
		if s == prev {
			continue
		}
		uniq = append(uniq, s)
		prev = s
	}
	if len(uniq) > 4 {
		uniq = uniq[:4]
	}
	return uniq
}

func filterEvidenceByFocus(ev []Evidence, filters []string) []Evidence {
	var cleaned []string
	for _, f := range filters {
		f = strings.ToLower(strings.TrimSpace(f))
		if f != "" {
			cleaned = append(cleaned, f)
		}
	}
	if len(cleaned) == 0 {
		return ev
	}
	out := make([]Evidence, 0, len(ev))
	for _, e := range ev {
		hay := strings.ToLower(e.Kind + "|" + e.Component + "|" + e.Decision + "|" + e.Message + "|" + e.SourceID)
		for _, f := range cleaned {
			if strings.Contains(hay, f) {
				out = append(out, e)
				break
			}
		}
	}
	return out
}
