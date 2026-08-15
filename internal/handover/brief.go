// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/marcelocantos/jevons/internal/transcript"
)

// MaxBriefTokens is the hard cap on a distilled predecessor brief
// (🎯T392.1.1). A successor that is handed this — not the transcript —
// stays under the context ceiling on its first turn.
const MaxBriefTokens = 4000

// briefTailTurns is how many user/assistant turns to keep, from the end.
const briefTailTurns = 6

// TokenCount is a deterministic token estimate: one token per four
// Unicode runes, rounded up. It is not a vendor tokenizer; it is the
// cap the distill and the tests share so a brief cannot silently grow.
func TokenCount(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}

// Distill builds a bounded predecessor brief from a transcript file.
// The path may be empty: Distill then returns "". A missing or unreadable
// file is also "": the seed still cites the path, and the caller must
// not fall back to "read the JSONL".
func Distill(transcriptPath string) string {
	path := strings.TrimSpace(transcriptPath)
	if path == "" {
		return ""
	}
	turns, err := transcript.ReadPath(path)
	if err != nil || len(turns) == 0 {
		return ""
	}
	return distillTurns(turns, MaxBriefTokens)
}

func distillTurns(turns []transcript.Turn, capTokens int) string {
	if capTokens <= 0 {
		capTokens = MaxBriefTokens
	}
	recent := tailTurns(turns, briefTailTurns)
	var b strings.Builder
	b.WriteString("Recent turns (oldest of the tail first):\n")
	for _, t := range recent {
		role := t.Role
		if role == "" {
			role = "unknown"
		}
		fmt.Fprintf(&b, "- %s: %s\n", role, clipLine(t.Text, 400))
	}
	if promises := promiseLines(turns); len(promises) > 0 {
		b.WriteString("\nIn-flight promises:\n")
		for _, p := range promises {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	if threads := openThreads(turns); len(threads) > 0 {
		b.WriteString("\nOpen threads:\n")
		for _, th := range threads {
			fmt.Fprintf(&b, "- %s\n", th)
		}
	}
	out := strings.TrimSpace(b.String())
	if TokenCount(out) <= capTokens {
		return out
	}
	// Trim from the oldest recent-turn lines until the cap holds.
	return clipToTokens(out, capTokens)
}

func tailTurns(turns []transcript.Turn, n int) []transcript.Turn {
	if n <= 0 || len(turns) <= n {
		return turns
	}
	return turns[len(turns)-n:]
}

func promiseLines(turns []transcript.Turn) []string {
	var out []string
	for i := len(turns) - 1; i >= 0; i-- {
		t := turns[i]
		if t.Role != "assistant" {
			continue
		}
		for _, line := range strings.Split(t.Text, "\n") {
			line = strings.TrimSpace(line)
			if isPromise(line) {
				out = append(out, clipLine(line, 240))
				if len(out) >= 5 {
					return reverse(out)
				}
			}
		}
	}
	return reverse(out)
}

func openThreads(turns []transcript.Turn) []string {
	var out []string
	for i := len(turns) - 1; i >= 0; i-- {
		t := turns[i]
		if t.Role != "user" {
			continue
		}
		text := clipLine(strings.TrimSpace(t.Text), 240)
		if text == "" || LooksLikeSeed(text) {
			continue
		}
		out = append(out, text)
		if len(out) >= 3 {
			break
		}
	}
	return reverse(out)
}

func isPromise(line string) bool {
	low := strings.ToLower(strings.TrimSpace(line))
	if low == "" {
		return false
	}
	switch {
	case strings.Contains(low, "i'll "),
		strings.Contains(low, "i will "),
		strings.Contains(low, "promised"),
		strings.Contains(low, "in flight"),
		strings.Contains(low, "queued behind"),
		strings.HasPrefix(low, "next:"),
		strings.Contains(low, "open:"):
		return true
	default:
		return false
	}
}

func clipLine(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxRunes-1]) + "…"
}

func clipToTokens(s string, capTokens int) string {
	if TokenCount(s) <= capTokens {
		return s
	}
	// Walk runes until the estimate hits the cap.
	var b strings.Builder
	for _, r := range s {
		b.WriteRune(r)
		if TokenCount(b.String()) >= capTokens {
			break
		}
	}
	out := strings.TrimRightFunc(b.String(), unicode.IsSpace)
	if !strings.HasSuffix(out, "…") {
		out += "…"
	}
	return out
}

func reverse(in []string) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = in[len(in)-1-i]
	}
	return out
}
