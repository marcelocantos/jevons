// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package noopwedge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Turn granularity is the whole point of this package (see the doc comment),
// so the grouping rules are spelled out rather than borrowed:
//
//   - A TURN OPENS on an authored user message: content that is a plain
//     string, or a content array containing at least one text block. Tool
//     results arrive as user messages too, and they are not turn boundaries —
//     grouping on them would split one agent turn into a dozen, each with a
//     fraction of its tool calls, and a turn holding only the final "done"
//     text would read as a no-op.
//   - ASSISTANT MESSAGES accumulate into the open turn: their text blocks
//     concatenate, their tool_use blocks count. Several assistant messages per
//     turn is the normal shape, which is why "Now the hermetic test:" is not a
//     no-op — the tool calls land in sibling messages of the same turn.
//   - EVERYTHING ELSE is skipped: system messages, the harness's own
//     bookkeeping lines (mode, permission-mode, bridge-session, attachment,
//     file-history-snapshot), and unparseable lines.
//
// Both transcript dialects this fleet runs are covered: Claude nests the
// message under "message", Grok puts role and content at the top level.

// maxLineBytes bounds one JSONL line. Transcripts routinely carry inlined
// file contents and large tool results; a line over this is skipped rather
// than aborting the scan, since a single unreadable line must not make a
// working agent look silent.
const maxLineBytes = 8 << 20

// jsonlLine is the union of the two transcript dialects.
type jsonlLine struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content any    `json:"content"`
	Message *struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	} `json:"message"`
}

// TurnsFromFile reads a session transcript and groups it into turns. A
// missing file yields no turns and no error: a session whose JSONL does not
// exist has never begun a turn, which Classify reports as Unobserved rather
// than as a wedge.
func TurnsFromFile(path string) ([]Turn, error) { return scanTurns(path, nil) }

// ScanShape is stage 1 straight off disk, and it is what makes the fleet view
// affordable: it stops reading the moment a turn becomes real.
//
// The early exit is sound because realness is monotone within a turn (see
// realTurn), so a turn that has already done something cannot un-do it later
// in the file. The cost is therefore the prefix up to the agent's first real
// turn — usually its first — for every healthy agent, and a wedged agent's
// whole transcript is small by construction. A view that had to parse half a
// megabyte per row on every poll would simply not be wired up, and an
// unwired discriminator detects nothing.
func ScanShape(path string) (Shape, error) {
	shape := ShapeNone
	_, err := scanTurns(path, func(cur Turn) bool {
		if realTurn(cur) {
			shape = ShapeReal
			return true
		}
		if !silent(cur) {
			shape = ShapeAllNoOp
		}
		return false
	})
	if err != nil {
		return shape, err
	}
	return shape, nil
}

// scanTurns walks a transcript, grouping it into turns. onUpdate, when
// non-nil, is called with the open turn each time an assistant message has
// been folded into it, and halts the scan when it returns true.
func scanTurns(path string, onUpdate func(Turn) bool) ([]Turn, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open transcript %s: %w", path, err)
	}
	defer f.Close()

	var turns []Turn
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var line jsonlLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			continue
		}
		role, content := line.Role, line.Content
		if line.Message != nil {
			role, content = line.Message.Role, line.Message.Content
		}
		switch strings.TrimSpace(role) {
		case "user":
			if authoredUser(content) {
				turns = append(turns, Turn{})
			}
		case "assistant":
			if len(turns) == 0 {
				continue // assistant output before any authored prompt
			}
			text, tools := assistantParts(content)
			cur := &turns[len(turns)-1]
			cur.Text += text
			cur.ToolCalls += tools
			if onUpdate != nil && onUpdate(*cur) {
				return turns, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return turns, fmt.Errorf("read transcript %s: %w", path, err)
	}
	return turns, nil
}

// authoredUser reports whether a user message carries prompt text the owner,
// overseer, or daemon authored — as opposed to a tool result being fed back.
func authoredUser(content any) bool {
	switch c := content.(type) {
	case string:
		return strings.TrimSpace(c) != ""
	case []any:
		for _, raw := range c {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if block["type"] == "text" {
				if s, _ := block["text"].(string); strings.TrimSpace(s) != "" {
					return true
				}
			}
		}
	}
	return false
}

// assistantParts extracts an assistant message's text and its tool_use count.
func assistantParts(content any) (text string, tools int) {
	switch c := content.(type) {
	case string:
		return c, 0
	case []any:
		var b strings.Builder
		for _, raw := range c {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch block["type"] {
			case "text":
				if s, _ := block["text"].(string); s != "" {
					b.WriteString(s)
				}
			case "tool_use":
				tools++
			}
		}
		return b.String(), tools
	}
	return "", 0
}
