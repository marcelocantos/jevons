// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package rsi implements ambient recursive self-improvement (🎯T92 / 🎯T243).
//
// Product path (🎯T243): Coach drip-reads owner chat (priority), eventlog, and
// session transcripts; forms structured Judgments; delivers them to the
// overseer. The coach never files bullseye — overseer alone decides file /
// alert / brief PO / ignore. Overseer retunes coach prompt + runtime dials
// (bi-directional SI). Continuous read uses a durable byte-offset cursor
// (not full history re-ingest each cycle).
//
// Residual mint path (🎯T92 / T92.2): ExtractCandidates + RunCycle + Loop may
// still file bullseye when explicitly opted in (JEVONS_RSI_MINT / DEEPER).
// Phrase-list direct mint is not the product path; extract may feed coach
// proposals only.
//
// Schedule/stream hooks live in the daemon; this package is pure + injectable
// so hermetic tests prove coach→overseer without Grok.
package rsi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// DefaultMinCount is the frequency threshold: a single anecdote is not enough.
const DefaultMinCount = 2

// DefaultMaxMint caps auto-filed targets per cycle so the frontier is not flooded.
const DefaultMaxMint = 2

// Evidence is one unit of activity for retrospective analysis.
type Evidence struct {
	// Kind classifies the surface: lifecycle_error, tool_failure,
	// event_push_error, reaped_idle, stuck_work, cost_anomaly, chat_gap,
	// transcript_friction, other.
	Kind      string
	Component string
	Decision  string
	Outcome   string
	Message   string
	// SourceID is session/thread/agent id when known.
	SourceID string
	// TS is when the evidence was observed (zero = unknown).
	TS time.Time
	// Fields carries extra structured attrs (stringified for fingerprinting).
	Fields map[string]string
}

// Candidate is a concrete improvement proposal ready to file as a bullseye target.
type Candidate struct {
	Name        string
	Acceptance  []string
	Context     string
	Fingerprint string
	Count       int
	Kinds       []string
	EvidenceIDs []string
	// LatestTS is the newest evidence timestamp in the cluster (zero = unknown).
	// Outcome suppression (🎯T333) re-proposes only when this is newer than the outcome.
	LatestTS time.Time
	// Phrase is the friction phrase (chat/session kinds) or commit subject
	// (git kinds) of the sample. The retro value bar (🎯T353) reads it.
	Phrase string
}

// ExistingTarget is a ledger entry used for near-duplicate suppression.
type ExistingTarget struct {
	ID          string
	Name        string
	Fingerprint string
}

// Filed records a successful mint.
type Filed struct {
	ID          string
	Name        string
	Fingerprint string
}

// SkipReason explains why a candidate was not filed.
type SkipReason struct {
	Fingerprint string
	Name        string
	Reason      string
}

// CycleResult is the outcome of one retrospective cycle.
type CycleResult struct {
	Proposed []Candidate
	Filed    []Filed
	Skipped  []SkipReason
}

// Filer files a bullseye target. Production uses bullseye CLI; tests inject a fake.
type Filer interface {
	File(args FileArgs) (id string, err error)
}

// FileArgs is the bullseye track payload (no functional options).
type FileArgs struct {
	Cwd        string
	Name       string
	Acceptance []string
	Context    string
}

// CycleArgs configures one retrospective cycle.
type CycleArgs struct {
	Cwd      string
	Evidence []Evidence
	Existing []ExistingTarget
	// PriorFingerprints are recently minted fingerprints (ledger) to suppress repeats.
	PriorFingerprints []string
	Filer             Filer
	// MinCount defaults to DefaultMinCount when ≤0.
	MinCount int
	// MaxMint defaults to DefaultMaxMint when ≤0; 0 after default still caps.
	MaxMint int
	// DryRun extracts and dedupes but does not call Filer.
	DryRun bool
}

// RunCycle extracts candidates from evidence, applies noise control, and mints.
func RunCycle(args CycleArgs) (CycleResult, error) {
	minCount := args.MinCount
	if minCount <= 0 {
		minCount = DefaultMinCount
	}
	maxMint := args.MaxMint
	if maxMint <= 0 {
		maxMint = DefaultMaxMint
	}

	proposed := ExtractCandidates(args.Evidence, minCount)
	keep, skipped := Dedupe(proposed, args.Existing, args.PriorFingerprints)

	out := CycleResult{
		Proposed: proposed,
		Skipped:  skipped,
	}
	if args.DryRun || args.Filer == nil {
		if !args.DryRun && args.Filer == nil && len(keep) > 0 {
			return out, fmt.Errorf("rsi: filer required when not dry-run and candidates remain")
		}
		for _, c := range keep {
			out.Skipped = append(out.Skipped, SkipReason{
				Fingerprint: c.Fingerprint,
				Name:        c.Name,
				Reason:      "dry_run",
			})
		}
		return out, nil
	}
	if strings.TrimSpace(args.Cwd) == "" {
		return out, fmt.Errorf("rsi: cwd required to mint")
	}

	n := 0
	for _, c := range keep {
		if n >= maxMint {
			out.Skipped = append(out.Skipped, SkipReason{
				Fingerprint: c.Fingerprint,
				Name:        c.Name,
				Reason:      "max_mint_per_cycle",
			})
			continue
		}
		id, err := args.Filer.File(FileArgs{
			Cwd:        args.Cwd,
			Name:       c.Name,
			Acceptance: c.Acceptance,
			Context:    c.Context,
		})
		if err != nil {
			out.Skipped = append(out.Skipped, SkipReason{
				Fingerprint: c.Fingerprint,
				Name:        c.Name,
				Reason:      "file_error: " + err.Error(),
			})
			continue
		}
		out.Filed = append(out.Filed, Filed{
			ID:          id,
			Name:        c.Name,
			Fingerprint: c.Fingerprint,
		})
		n++
	}
	return out, nil
}

// evidenceBucket is a cluster of similar evidence rows.
type evidenceBucket struct {
	key    string
	count  int
	kinds  map[string]struct{}
	ids    []string
	sample Evidence
	maxTS  time.Time
}

// ExtractCandidates clusters evidence into improvement candidates.
// Rule-based grouping by kind+component+decision+normalized msg (T92.1).
// Deeper surfaces (chatlog + session transcripts) feed the same cluster path (T92.2).
func ExtractCandidates(ev []Evidence, minCount int) []Candidate {
	if minCount <= 0 {
		minCount = DefaultMinCount
	}
	groups := map[string]*evidenceBucket{}
	for _, e := range ev {
		if !actionable(e) {
			continue
		}
		key := clusterKey(e)
		b, ok := groups[key]
		if !ok {
			b = &evidenceBucket{key: key, kinds: map[string]struct{}{}, sample: e}
			groups[key] = b
		}
		b.count++
		kind := strings.TrimSpace(e.Kind)
		if kind == "" {
			kind = "other"
		}
		b.kinds[kind] = struct{}{}
		if e.SourceID != "" && len(b.ids) < 8 {
			b.ids = append(b.ids, e.SourceID)
		}
		if e.TS.After(b.maxTS) {
			b.maxTS = e.TS
		}
	}

	var keys []string
	for k, b := range groups {
		if b.count >= minCount {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	out := make([]Candidate, 0, len(keys))
	for _, k := range keys {
		out = append(out, candidateFromBucket(groups[k]))
	}
	return out
}

// Dedupe drops candidates that match existing targets or prior fingerprints.
func Dedupe(cands []Candidate, existing []ExistingTarget, prior []string) (keep []Candidate, skipped []SkipReason) {
	priorSet := map[string]struct{}{}
	for _, p := range prior {
		p = strings.TrimSpace(p)
		if p != "" {
			priorSet[p] = struct{}{}
		}
	}
	existingFP := map[string]struct{}{}
	existingNames := map[string]string{} // normalized name → id
	for _, e := range existing {
		if fp := strings.TrimSpace(e.Fingerprint); fp != "" {
			existingFP[fp] = struct{}{}
		}
		nn := normalizeName(e.Name)
		if nn != "" {
			existingNames[nn] = e.ID
		}
	}

	for _, c := range cands {
		fp := c.Fingerprint
		if fp == "" {
			fp = FingerprintOf(c.Name, c.Context)
		}
		if _, ok := priorSet[fp]; ok {
			skipped = append(skipped, SkipReason{Fingerprint: fp, Name: c.Name, Reason: "prior_mint"})
			continue
		}
		if _, ok := existingFP[fp]; ok {
			skipped = append(skipped, SkipReason{Fingerprint: fp, Name: c.Name, Reason: "existing_fingerprint"})
			continue
		}
		nn := normalizeName(c.Name)
		if id, ok := existingNames[nn]; ok {
			skipped = append(skipped, SkipReason{Fingerprint: fp, Name: c.Name, Reason: "existing_name:" + id})
			continue
		}
		// Near-dup: existing name contains our core phrase or vice versa.
		if id, hit := nearDupName(nn, existingNames); hit {
			skipped = append(skipped, SkipReason{Fingerprint: fp, Name: c.Name, Reason: "near_dup_name:" + id})
			continue
		}
		c.Fingerprint = fp
		keep = append(keep, c)
	}
	return keep, skipped
}

// FingerprintOf hashes a stable identity for a candidate name+context core.
func FingerprintOf(name, context string) string {
	core := normalizeName(name) + "|" + normalizeName(context)
	sum := sha256.Sum256([]byte(core))
	return hex.EncodeToString(sum[:16])
}

// EventsToEvidence maps eventlog-shaped rows into Evidence.
// outcome=error, level=error/warn with failure-ish msg, or explicit failure kinds.
func EventsToEvidence(rows []EventRow) []Evidence {
	out := make([]Evidence, 0, len(rows))
	for _, r := range rows {
		e := Evidence{
			Component: r.Component,
			Decision:  r.Decision,
			Outcome:   r.Outcome,
			Message:   r.Msg,
			SourceID:  r.Corr,
			Fields:    r.Fields,
		}
		if r.TS != "" {
			if t, err := time.Parse(time.RFC3339Nano, r.TS); err == nil {
				e.TS = t
			} else if t, err := time.Parse(time.RFC3339, r.TS); err == nil {
				e.TS = t
			}
		}
		e.Kind = classifyEvent(r)
		if e.Kind == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// EventRow is a minimal eventlog projection (avoids importing eventlog in pure tests).
type EventRow struct {
	TS        string
	Level     string
	Msg       string
	Component string
	Decision  string
	Outcome   string
	Corr      string
	Fields    map[string]string
}

// StreamReaped builds stream evidence for idle-reaped threads (daemon GC hook).
func StreamReaped(threadIDs []string, at time.Time) []Evidence {
	out := make([]Evidence, 0, len(threadIDs))
	for _, id := range threadIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, Evidence{
			Kind:      "reaped_idle",
			Component: "butler",
			Decision:  "reap_idle",
			Outcome:   "ok",
			Message:   "reaped idle thread process",
			SourceID:  id,
			TS:        at,
		})
	}
	return out
}

func actionable(e Evidence) bool {
	kind := strings.TrimSpace(e.Kind)
	switch kind {
	case "lifecycle_error", "tool_failure", "event_push_error",
		"stuck_work", "cost_anomaly", "chat_gap", "transcript_friction",
		"git_rework", "git_revert":
		return true
	case "reaped_idle":
		// Stream marker alone is not a mint signal (noise); only clusters with errors matter.
		return false
	default:
		// Other kinds with error outcome still count.
		return strings.EqualFold(e.Outcome, "error") || strings.EqualFold(e.Outcome, "fail")
	}
}

func classifyEvent(r EventRow) string {
	level := strings.ToLower(strings.TrimSpace(r.Level))
	outcome := strings.ToLower(strings.TrimSpace(r.Outcome))
	comp := strings.ToLower(strings.TrimSpace(r.Component))
	dec := strings.ToLower(strings.TrimSpace(r.Decision))
	msg := strings.ToLower(r.Msg)

	if outcome == "error" || outcome == "fail" || level == "error" {
		if comp == "event_push" {
			return "event_push_error"
		}
		if strings.Contains(comp, "tool") || strings.Contains(msg, "tool") {
			return "tool_failure"
		}
		if strings.Contains(comp, "cost") || strings.Contains(msg, "cost") {
			return "cost_anomaly"
		}
		return "lifecycle_error"
	}
	// Warn-level signals: cost anomalies, stuck work, and failure-ish text.
	if level == "warn" {
		if strings.Contains(msg, "stuck") || strings.Contains(dec, "stuck") {
			return "stuck_work"
		}
		if strings.Contains(msg, "cost") || strings.Contains(comp, "cost") ||
			strings.Contains(msg, "anomaly") || strings.Contains(msg, "burn") {
			return "cost_anomaly"
		}
		if strings.Contains(msg, "fail") || strings.Contains(msg, "error") ||
			strings.Contains(msg, "timeout") {
			return "lifecycle_error"
		}
	}
	if strings.Contains(msg, "stuck") && (level == "warn" || level == "error") {
		return "stuck_work"
	}
	return ""
}

func clusterKey(e Evidence) string {
	kind := strings.TrimSpace(e.Kind)
	if kind == "" {
		kind = "other"
	}
	comp := strings.ToLower(strings.TrimSpace(e.Component))
	dec := strings.ToLower(strings.TrimSpace(e.Decision))
	msg := normalizeMsg(e.Message)
	// Cap message contribution so transient detail does not explode clusters.
	if len(msg) > 80 {
		msg = msg[:80]
	}
	return kind + "|" + comp + "|" + dec + "|" + msg
}

func normalizeMsg(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Collapse runs of digits (ids, ports) for stable clustering.
	var b strings.Builder
	prevDigit := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			if !prevDigit {
				b.WriteByte('#')
				prevDigit = true
			}
			continue
		}
		prevDigit = false
		if r == '\n' || r == '\t' {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "🎯")
	return strings.Join(strings.Fields(s), " ")
}

func nearDupName(nn string, existing map[string]string) (id string, hit bool) {
	if nn == "" {
		return "", false
	}
	for en, eid := range existing {
		if en == "" {
			continue
		}
		if strings.Contains(en, nn) || strings.Contains(nn, en) {
			return eid, true
		}
		// Shared long token (≥12 chars) is a near-dup signal for short names.
		if shareLongToken(nn, en, 12) {
			return eid, true
		}
	}
	return "", false
}

func shareLongToken(a, b string, minLen int) bool {
	ta := strings.Fields(a)
	tb := strings.Fields(b)
	set := map[string]struct{}{}
	for _, t := range ta {
		if len(t) >= minLen {
			set[t] = struct{}{}
		}
	}
	for _, t := range tb {
		if _, ok := set[t]; ok {
			return true
		}
	}
	return false
}

func candidateFromBucket(b *evidenceBucket) Candidate {
	e := b.sample
	comp := strings.TrimSpace(e.Component)
	if comp == "" {
		comp = "system"
	}
	dec := strings.TrimSpace(e.Decision)
	if dec == "" {
		dec = "operation"
	}
	kinds := make([]string, 0, len(b.kinds))
	for k := range b.kinds {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	kindLabel := strings.Join(kinds, ",")

	name := fmt.Sprintf("%s %s failures are diagnosed or eliminated", comp, dec)
	// Prefer stuck/cost/chat/session wording when those kinds dominate.
	if _, ok := b.kinds["stuck_work"]; ok {
		name = fmt.Sprintf("Stuck work in %s is detected and unblocked early", comp)
	}
	if _, ok := b.kinds["cost_anomaly"]; ok {
		name = fmt.Sprintf("Cost anomalies in %s are explained or clamped", comp)
	}
	if _, ok := b.kinds["chat_gap"]; ok {
		name = fmt.Sprintf("Owner-chat friction (%s) is reduced or filed as a target", phraseFromSample(e))
	}
	if _, ok := b.kinds["transcript_friction"]; ok {
		name = fmt.Sprintf("Session transcript friction (%s) is diagnosed or eliminated", phraseFromSample(e))
	}
	// History-derived kinds (🎯T353 retrospective mine).
	if _, ok := b.kinds["git_rework"]; ok {
		name = fmt.Sprintf("Repeated fix churn in %s is diagnosed or eliminated at the root", comp)
	}
	if _, ok := b.kinds["git_revert"]; ok {
		name = fmt.Sprintf("Reverted changes in %s stop recurring (root cause understood)", comp)
	}

	acceptance := []string{
		fmt.Sprintf("Repeated %s/%s signals (kind=%s, n≥%d in an ambient RSI window) no longer accumulate without a filed target, fix, or explicit set-aside.",
			comp, dec, kindLabel, b.count),
		"Hermetic or live evidence shows either a reduced recurrence rate or an owner-visible disposition of the failure mode.",
	}
	ctx := fmt.Sprintf(
		"Ambient RSI (🎯T92/T92.2) cycle from evidence: kind=%s component=%s decision=%s count=%d sample_msg=%q sources=%v. Origin: ambient-rsi (eventlog + owner-chat + session transcript extract; noise-controlled).",
		kindLabel, comp, dec, b.count, truncate(e.Message, 160), b.ids,
	)
	fp := FingerprintOf(name, comp+"|"+dec+"|"+kindLabel)
	return Candidate{
		Name:        name,
		Acceptance:  acceptance,
		Context:     ctx,
		Fingerprint: fp,
		Count:       b.count,
		Kinds:       kinds,
		EvidenceIDs: append([]string{}, b.ids...),
		LatestTS:    b.maxTS,
		Phrase:      phraseFromSample(e),
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// phraseFromSample pulls the friction phrase from Fields or message for naming.
func phraseFromSample(e Evidence) string {
	if e.Fields != nil {
		if p := strings.TrimSpace(e.Fields["phrase"]); p != "" {
			return p
		}
	}
	msg := strings.TrimSpace(e.Message)
	if i := strings.Index(msg, ": "); i >= 0 && i+2 < len(msg) {
		return truncate(msg[i+2:], 40)
	}
	if msg != "" {
		return truncate(msg, 40)
	}
	return "repeated pain"
}
