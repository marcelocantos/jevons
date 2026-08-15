// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover

import "strings"

// BriefSource is how a migrate brief was produced (🎯T285.1).
type BriefSource string

const (
	SourceNone    BriefSource = ""
	SourceSelf    BriefSource = "self-brief"
	SourceDistill BriefSource = "distill"
	SourceCompact BriefSource = "throwaway-compact"
)

// MinUsefulBriefTokens is the floor under which Distill is treated as
// too thin to seed a work session. Below this a model read of the
// predecessor belongs on a throwaway compact session, not the work one.
const MinUsefulBriefTokens = 24

// Brief is the handover text handed to the work session.
type Brief struct {
	Text             string
	Source           BriefSource
	CompactSessionID string
	Thin             bool
}

// GatherHooks let tests inject live / dead / thin fixtures without a
// model. Nil hooks are the product defaults: no live outgoing, no
// compact session.
type GatherHooks struct {
	// SelfBrief asks the outgoing session to write from memory. Empty
	// text or a non-nil error falls through — it must not block the switch.
	SelfBrief func(p Pending) (string, error)
	// Compact runs a throwaway session on the NEW provider. Returns that
	// session's id and the brief. Failure falls through to thin Distill.
	Compact func(p Pending) (sessionID, text string, err error)
}

// DistillTooThin reports a brief that cannot seed a work session.
// A short but real Distill (one user turn) is still a brief; thin
// means Distill extracted no turns at all.
func DistillTooThin(brief string) bool {
	s := strings.TrimSpace(brief)
	if s == "" {
		return true
	}
	if strings.Contains(strings.ToLower(s), "no distillable turns") {
		return true
	}
	content := 0
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Recent turns") ||
			strings.HasPrefix(line, "In-flight") || strings.HasPrefix(line, "Open threads") {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			content++
		}
	}
	return content == 0
}

// PlanThrowawayCompact is true when the work session must not be the
// session that reads the predecessor file.
func PlanThrowawayCompact(distilled string, haveSelfBrief bool) bool {
	return !haveSelfBrief && DistillTooThin(distilled)
}

// GatherBrief is the three-rung migrate brief: live self-brief, else
// Distill, else throwaway compact. Same-provider pending yields nothing.
func GatherBrief(p Pending, hooks GatherHooks) Brief {
	if !ProviderSwitch(p.From, p.To) {
		return Brief{}
	}
	if hooks.SelfBrief != nil {
		if text, err := hooks.SelfBrief(p); err == nil && strings.TrimSpace(text) != "" {
			return Brief{Text: strings.TrimSpace(text), Source: SourceSelf}
		}
	}
	distilled := Distill(p.TranscriptPath)
	if !DistillTooThin(distilled) {
		return Brief{Text: distilled, Source: SourceDistill}
	}
	if hooks.Compact != nil {
		sid, text, err := hooks.Compact(p)
		if err == nil && strings.TrimSpace(text) != "" {
			return Brief{
				Text:             strings.TrimSpace(text),
				Source:           SourceCompact,
				CompactSessionID: strings.TrimSpace(sid),
			}
		}
	}
	fallback := distilled
	if fallback == "" {
		fallback = "(no distillable turns — honour any in-flight work you can see, and do not reconstruct from the predecessor file.)"
	}
	return Brief{Text: fallback, Source: SourceDistill, Thin: true}
}
