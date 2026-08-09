// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agentreport

import (
	"fmt"
	"strings"
)

// Section is one addressable part of a report.
//
// 🎯T388 acceptance 3: the overseer must be able to re-request a specific part
// of a report WITHOUT the agent re-deriving it. Re-derivation is not a
// retrieval — an agent asked to "resend the last bit" runs its model again and
// can produce different content, which is how a correction gets quietly
// rewritten. Sections are cut from the stored bytes, so what comes back is
// what was written.
type Section struct {
	// Heading is the section title as written ("" for text before the first
	// heading).
	Heading string
	// Level is the ATX heading depth (0 for the preamble).
	Level int
	// Text is the section body including its heading line.
	Text string
}

// preambleHeading names the region before a report's first heading, so it can
// be requested like any other part.
const preambleHeading = "(preamble)"

// Sections splits a report on markdown ATX headings. Agent reports are
// markdown (🎯T381 renders them as such), so headings are the structure the
// author actually wrote rather than one imposed here. A report with no
// headings is a single preamble section — still addressable, still not
// re-derived.
func Sections(text string) []Section {
	lines := strings.Split(text, "\n")
	var out []Section
	cur := Section{Heading: preambleHeading}
	var b strings.Builder

	flush := func() {
		cur.Text = strings.TrimRight(b.String(), "\n")
		if strings.TrimSpace(cur.Text) != "" {
			out = append(out, cur)
		}
		b.Reset()
	}

	for _, line := range lines {
		if h, level, ok := parseATXHeading(line); ok {
			flush()
			cur = Section{Heading: h, Level: level}
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	flush()
	return out
}

// parseATXHeading recognises "## Heading" style lines. Fenced code blocks can
// contain lines starting with '#' (shell comments), but a report is prose with
// headings, and mis-splitting inside a fence still returns the same bytes —
// only the boundary moves — so the simple rule is the honest one here.
func parseATXHeading(line string) (heading string, level int, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return "", 0, false
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	if i > 6 || i >= len(trimmed) || trimmed[i] != ' ' {
		return "", 0, false
	}
	h := strings.TrimSpace(strings.TrimRight(trimmed[i:], " #"))
	if h == "" {
		return "", 0, false
	}
	return h, i, true
}

// FindSection returns the first section whose heading matches want, comparing
// case-insensitively on substring so the overseer can ask for "asks" and get
// "## 3. Asks / decisions needed". An empty want returns the whole report.
func FindSection(text, want string) (Section, error) {
	want = strings.TrimSpace(want)
	secs := Sections(text)
	if want == "" {
		return Section{Heading: "", Text: text}, nil
	}
	needle := strings.ToLower(want)
	for _, s := range secs {
		if strings.Contains(strings.ToLower(s.Heading), needle) {
			return s, nil
		}
	}
	return Section{}, fmt.Errorf("no section matching %q (have: %s)", want, strings.Join(HeadingList(secs), ", "))
}

// HeadingList names the sections available, for an error message or a listing.
func HeadingList(secs []Section) []string {
	out := make([]string, 0, len(secs))
	for _, s := range secs {
		out = append(out, s.Heading)
	}
	return out
}
