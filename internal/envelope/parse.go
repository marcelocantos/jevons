// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	// FenceInfo is the markdown fence info string. Opening at line 1 of
	// the author's message; not YAML front matter.
	FenceInfo = "jevons"
	// Sigil prefixes every slot line. Spelled out; never abbreviated.
	Sigil = "jevons:"
)

// BannerHeading opens the note prepended to a malformed load-bearing
// envelope on the message path. Silence is the normal case.
const BannerHeading = "⚠ ENVELOPE CHECK (🎯T509): this message claims a load-bearing kind with missing or malformed slots."

// Message is one parsed envelope plus its free-prose payload.
type Message struct {
	Kind    Kind
	Target  string
	SHA     string
	GateID  string
	Daily   string
	Risk    Risk
	Verdict Verdict
	Status  Progress
	Name    string // target-file-request title, optional elsewhere
	// Phase is the mission phase (scout|implement) — 🎯T536.3.
	Phase Phase
	// SilentLedger is the 🎯T536.1 silent-decision ledger state.
	SilentLedger SilentLedgerState
	// Decisions is the ranked silent-decision list (least-confident first)
	// when SilentLedger is SilentLedgerRanked.
	Decisions []SilentDecision
	// FogKnown / FogUnknown / FogBlindspot are the 🎯T536.3 fog-of-war
	// sweep slots (repeatable).
	FogKnown     []string
	FogUnknown   []string
	FogBlindspot []string
	// Extra holds unknown jevons: keys so a newer emitter is not refused.
	Extra   map[string]string
	Payload string
	// Raw is the fenced block body (slot lines only), for chatter fingerprints.
	Raw string
	// AtLine1 is true when the fence opened at line 1 of the author's
	// content (after known daemon prefixes).
	AtLine1 bool
}

// HasOracle is true when the envelope carries an executable-oracle ref
// (SHA and/or gate id) — the 🎯T31 field-read path.
func (m *Message) HasOracle() bool {
	if m == nil {
		return false
	}
	return strings.TrimSpace(m.SHA) != "" || strings.TrimSpace(m.GateID) != ""
}

// HasRisk is true when the envelope carries accepted-risk / class-3 /
// residual — the 🎯T31.1 field-read path.
func (m *Message) HasRisk() bool {
	if m == nil {
		return false
	}
	return m.Risk.IsAccepted()
}

// HasDaily is true when the envelope cites activated daily-path evidence
// (🎯T194 field-read path).
func (m *Message) HasDaily() bool {
	if m == nil {
		return false
	}
	return strings.TrimSpace(m.Daily) != ""
}

// SlotsFingerprint is kind plus the machine-checkable slots, excluding
// payload. Identical fingerprints within a cycle are chatter.
func (m *Message) SlotsFingerprint() string {
	if m == nil {
		return ""
	}
	return strings.Join([]string{
		string(m.Kind),
		"target=" + m.Target,
		"sha=" + m.SHA,
		"gate-id=" + m.GateID,
		"daily=" + m.Daily,
		"risk=" + string(m.Risk),
		"verdict=" + string(m.Verdict),
		"status=" + string(m.Status),
		"name=" + m.Name,
		"phase=" + string(m.Phase),
		ledgerFingerprint(m),
		fogFingerprint(m),
	}, "\n")
}

// Parse extracts a jevons envelope from text.
//
//	(nil, nil)  — no envelope; callers fall back to prose heuristics
//	(msg, nil)  — valid envelope
//	(msg, err)  — envelope present but malformed / missing required slots
//
// The fence is sought at line 1 of the author's content after stripping
// known daemon prefixes ([Agent … responded], identity header, check
// banners). A fence deeper in the body is treated as a quotation and is
// not an envelope.
func Parse(text string) (*Message, error) {
	body, _ := StripPrefixes(text)
	if body == "" {
		return nil, nil
	}
	fence, payload, ok := splitFence(body)
	if !ok {
		return nil, nil
	}
	msg, err := parseSlots(fence)
	if msg == nil {
		return nil, err
	}
	msg.Payload = strings.TrimLeft(payload, "\n")
	msg.Raw = fence
	msg.AtLine1 = true
	if err != nil {
		return msg, err
	}
	if valErr := Validate(msg); valErr != nil {
		return msg, valErr
	}
	return msg, nil
}

// StripPrefixes peels daemon framing so the author's envelope can sit at
// line 1 of what they wrote. Returns the remaining body and whether
// anything was stripped.
func StripPrefixes(text string) (string, bool) {
	s := strings.TrimLeft(text, "\uFEFF")
	stripped := false
	for {
		next, ok := stripOnePrefix(s)
		if !ok {
			return s, stripped
		}
		s = next
		stripped = true
	}
}

func stripOnePrefix(s string) (string, bool) {
	s = strings.TrimLeft(s, "\r")
	if strings.HasPrefix(s, "[Agent ") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			return s[i+1:], true
		}
		return "", true
	}
	if strings.HasPrefix(s, "[Who you are") {
		// Identity header runs until a blank line followed by the standing
		// brief or the author's content. A lone blank line ends it.
		rest := s
		if i := strings.Index(rest, "\n\n"); i >= 0 {
			return rest[i+2:], true
		}
		return "", true
	}
	if strings.HasPrefix(s, "⚠ FALSE-GREEN") || strings.HasPrefix(s, "⚠ ENVELOPE CHECK") {
		if i := strings.Index(s, "\n\n"); i >= 0 {
			return s[i+2:], true
		}
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			return s[i+1:], true
		}
		return "", true
	}
	if strings.HasPrefix(s, "[Jevons fleet standing brief") {
		const end = "\n---\n"
		if i := strings.Index(s, end); i >= 0 {
			return s[i+len(end):], true
		}
	}
	return s, false
}

// splitFence returns the fence body and the payload after it when text
// opens with a ```jevons fence. Incomplete fences (no closer) return ok=false
// so a mid-stream paint is not flagged as malformed.
func splitFence(text string) (fence, payload string, ok bool) {
	s := strings.TrimLeft(text, " \t")
	if !strings.HasPrefix(s, "```") {
		return "", "", false
	}
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return "", "", false
	}
	info := strings.TrimSpace(strings.TrimPrefix(s[:nl], "```"))
	fields := strings.Fields(info)
	if len(fields) == 0 || !strings.EqualFold(fields[0], FenceInfo) {
		return "", "", false
	}
	rest := s[nl+1:]
	closer := "\n```"
	ci := strings.Index(rest, closer)
	if ci < 0 {
		// Closing fence as the last characters, no trailing newline.
		if strings.HasSuffix(rest, "\n```") || rest == "```" {
			if rest == "```" {
				return "", "", true
			}
			return rest[:len(rest)-4], "", true
		}
		return "", "", false
	}
	fence = rest[:ci]
	payload = rest[ci+len(closer):]
	payload = strings.TrimPrefix(payload, "\n")
	return fence, payload, true
}

func parseSlots(fence string) (*Message, error) {
	msg := &Message{Extra: map[string]string{}}
	kindSeen := false
	for _, line := range strings.Split(fence, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		key, value, ok := parseSlotLine(trim)
		if !ok {
			return msg, fmt.Errorf("slot line is not a %s key value pair: %q", Sigil, trim)
		}
		if err := applySlot(msg, key, value, &kindSeen); err != nil {
			return msg, err
		}
	}
	if !kindSeen {
		return msg, fmt.Errorf("missing %s kind", Sigil)
	}
	return msg, nil
}

func parseSlotLine(line string) (key, value string, ok bool) {
	if !strings.HasPrefix(strings.ToLower(line), Sigil) {
		return "", "", false
	}
	rest := strings.TrimSpace(line[len(Sigil):])
	if rest == "" {
		return "", "", false
	}
	key, value, _ = strings.Cut(rest, " ")
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func applySlot(msg *Message, key, value string, kindSeen *bool) error {
	switch key {
	case "kind":
		k, ok := ParseKind(value)
		if !ok {
			return fmt.Errorf("unknown kind %q", value)
		}
		msg.Kind = k
		*kindSeen = true
	case "target":
		msg.Target = canonicalTarget(value)
	case "oracle":
		applyOracle(msg, value)
	case "sha":
		msg.SHA = strings.TrimSpace(value)
	case "gate-id", "gateid", "gate":
		msg.GateID = strings.TrimSpace(value)
	case "daily":
		msg.Daily = strings.TrimSpace(value)
	case "risk":
		r, ok := ParseRisk(value)
		if !ok {
			return fmt.Errorf("unknown risk %q", value)
		}
		msg.Risk = r
	case "verdict":
		v, ok := ParseVerdict(value)
		if !ok {
			return fmt.Errorf("unknown verdict %q", value)
		}
		msg.Verdict = v
	case "status":
		p, ok := ParseProgress(value)
		if !ok {
			return fmt.Errorf("unknown status %q", value)
		}
		msg.Status = p
	case "name":
		msg.Name = strings.TrimSpace(value)
	case "phase", "mission-phase", "mission_phase":
		p, ok := ParsePhase(value)
		if !ok {
			return fmt.Errorf("unknown phase %q (want scout|implement)", value)
		}
		msg.Phase = p
	case "silent-ledger", "silent_ledger", "ledger":
		st, err := parseSilentLedger(value)
		if err != nil {
			return err
		}
		msg.SilentLedger = st
	case "silent-decision", "silent_decision", "decision":
		d, err := parseSilentDecision(value)
		if err != nil {
			return err
		}
		msg.Decisions = append(msg.Decisions, d)
		if msg.SilentLedger == SilentLedgerAbsent {
			msg.SilentLedger = SilentLedgerRanked
		}
	case "fog-known", "fog_known":
		if v := strings.TrimSpace(value); v != "" {
			msg.FogKnown = append(msg.FogKnown, unquoteSlot(v))
		}
	case "fog-unknown", "fog_unknown":
		if v := strings.TrimSpace(value); v != "" {
			msg.FogUnknown = append(msg.FogUnknown, unquoteSlot(v))
		}
	case "fog-blindspot", "fog_blindspot", "blindspot":
		if v := strings.TrimSpace(value); v != "" {
			msg.FogBlindspot = append(msg.FogBlindspot, unquoteSlot(v))
		}
	default:
		msg.Extra[key] = value
	}
	return nil
}

func applyOracle(msg *Message, value string) {
	for _, tok := range strings.Fields(value) {
		k, v, cut := strings.Cut(tok, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if !cut {
			// Bare token: treat as SHA if hex-shaped, else daily marker.
			if isHexSHA(tok) {
				msg.SHA = tok
			} else if msg.Daily == "" {
				msg.Daily = tok
			}
			continue
		}
		switch k {
		case "sha":
			msg.SHA = v
		case "gate-id", "gateid", "gate":
			msg.GateID = v
		case "daily":
			msg.Daily = v
		}
	}
}

func isHexSHA(s string) bool {
	if n := len(s); n < 7 || n > 40 {
		return false
	}
	for _, r := range s {
		if !unicode.Is(unicode.ASCII_Hex_Digit, r) {
			return false
		}
	}
	return true
}

func canonicalTarget(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "🎯")
	if len(s) >= 2 && (s[0] == 'T' || s[0] == 't') && s[1] >= '0' && s[1] <= '9' {
		return "T" + s[1:]
	}
	return s
}

// Format renders a Message as a line-1 ```jevons fence plus payload.
func Format(m *Message) string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("```")
	b.WriteString(FenceInfo)
	b.WriteByte('\n')
	writeSlot(&b, "kind", string(m.Kind))
	writeSlot(&b, "target", m.Target)
	if m.SHA != "" || m.GateID != "" || m.Daily != "" {
		var parts []string
		if m.SHA != "" {
			parts = append(parts, "sha="+m.SHA)
		}
		if m.GateID != "" {
			parts = append(parts, "gate-id="+m.GateID)
		}
		if m.Daily != "" {
			parts = append(parts, "daily="+m.Daily)
		}
		writeSlot(&b, "oracle", strings.Join(parts, " "))
	}
	if m.Risk != RiskNone && m.Risk != RiskNoneSlot {
		writeSlot(&b, "risk", string(m.Risk))
	} else if m.Risk == RiskNoneSlot {
		writeSlot(&b, "risk", "none")
	}
	writeSlot(&b, "verdict", string(m.Verdict))
	writeSlot(&b, "status", string(m.Status))
	writeSlot(&b, "name", m.Name)
	writeSlot(&b, "phase", string(m.Phase))
	switch m.SilentLedger {
	case SilentLedgerEmpty:
		writeSlot(&b, "silent-ledger", "none")
	case SilentLedgerRanked:
		writeSlot(&b, "silent-ledger", "ranked")
		for _, d := range m.Decisions {
			writeSlot(&b, "silent-decision", formatSilentDecision(d))
		}
	}
	for _, s := range m.FogKnown {
		writeSlot(&b, "fog-known", quoteIfNeeded(s))
	}
	for _, s := range m.FogUnknown {
		writeSlot(&b, "fog-unknown", quoteIfNeeded(s))
	}
	for _, s := range m.FogBlindspot {
		writeSlot(&b, "fog-blindspot", quoteIfNeeded(s))
	}
	b.WriteString("```\n")
	if p := strings.TrimLeft(m.Payload, "\n"); p != "" {
		b.WriteByte('\n')
		b.WriteString(p)
		if !strings.HasSuffix(p, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeSlot(b *strings.Builder, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString(Sigil)
	b.WriteByte(' ')
	b.WriteString(key)
	b.WriteByte(' ')
	b.WriteString(value)
	b.WriteByte('\n')
}

// Banner renders a malformed-envelope flag the way FALSE-GREEN rides in
// front of a report: prepended, not a refusal, so the payload still lands.
func Banner(err error) string {
	if err == nil {
		return ""
	}
	return BannerHeading + "\n  • " + err.Error()
}

// Annotate prepends Banner when err is set; otherwise returns text unchanged.
func Annotate(text string, err error) string {
	if err == nil {
		return text
	}
	return Banner(err) + "\n\n" + text
}
