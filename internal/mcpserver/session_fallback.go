// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"unicode/utf8"

	"github.com/marcelocantos/jevons/internal/agentreport"
)

// sessionFallbackMax is how much stored report text rides into a session
// that replaced one the provider could not load. The provider transcript
// is gone; this is what is still on disk.
const sessionFallbackMax = 3500

// planWallText is Cursor's whole-turn refusal. It is not context.
const planWallText = "Upgrade your plan to continue"

// sessionFallbackSeed is the opening context for a seat whose previous
// provider session was not there. oldSession is the id that failed to
// load. Reports are newest last, as agentreport.List returns them.
// Empty when nothing substantive was stored.
func sessionFallbackSeed(agent, oldSession string, recs []agentreport.Record) string {
	var parts []string
	used := 0
	for i := len(recs) - 1; i >= 0 && len(parts) < 2; i-- {
		text := strings.TrimSpace(recs[i].Text)
		if text == "" || text == planWallText || utf8.RuneCountInString(text) < 80 {
			continue
		}
		if utf8.RuneCountInString(text) > 1800 {
			text = string([]rune(text)[:1800]) + "\n…"
		}
		parts = append(parts, text)
		used += utf8.RuneCountInString(text)
		if used >= sessionFallbackMax {
			break
		}
	}
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("The previous provider session ")
	if strings.TrimSpace(oldSession) != "" {
		b.WriteString(oldSession)
		b.WriteString(" ")
	}
	b.WriteString("is not there")
	if agent != "" {
		b.WriteString(" (")
		b.WriteString(agent)
		b.WriteString(")")
	}
	b.WriteString(". It was not retried. This is a new session. What is still on disk follows.\n\n")
	for i, p := range parts {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		b.WriteString(p)
	}
	return b.String()
}

// loadSessionFallbackReports reads the newest stored reports, bodies
// included, newest last. A missing store is empty.
func loadSessionFallbackReports(stateDir, agent string) []agentreport.Record {
	recs, err := agentreport.List(stateDir, agent)
	if err != nil || len(recs) == 0 {
		return nil
	}
	start := 0
	if len(recs) > 8 {
		start = len(recs) - 8
	}
	out := make([]agentreport.Record, 0, len(recs)-start)
	for _, rec := range recs[start:] {
		full, err := agentreport.Load(stateDir, agent, rec.ID)
		if err != nil {
			continue
		}
		out = append(out, full)
	}
	return out
}
