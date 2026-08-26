// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package muxwin

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DefaultFollow is the connect visible window: last N *user turns*
// when statedb is the source (vanilla historyReplayTurns), else last N
// coalesced events on the JSONL-tail fallback ([-N, 0)).
const DefaultFollow = 30

// Event is one coalesced transcript event. ID is identity (append/dedup);
// Index is the dense 1-based position in the coalesced stream.
type Event struct {
	ID    string
	Index int
	Kind  Kind
	Type  string
	TS    string
	Body  json.RawMessage
}

// KindOfType maps a journal type to a halo class. User/assistant spend
// halo; steps chrome and status do not.
func KindOfType(typ string) Kind {
	switch typ {
	case "user":
		return KindUser
	case "assistant":
		return KindAssistant
	case "status":
		return KindStatus
	case "tool_use", "tool_result", "agent_note":
		return KindSteps
	default:
		return KindOther
	}
}

// EventsFromLines folds a journal the same way ApplyLive does, then
// assigns 1-based indices + ids. Assistant text tokens join; each
// tool_use is a KindSteps event. chatlog.CoalesceStreamLines is
// text-only (it drops tool_use into a sealed assistant) so hydrate
// must not use it — live and reload would disagree on step count.
func EventsFromLines(lines []string) []Event {
	if len(lines) == 0 {
		return nil
	}
	// In-place fold. ApplyLiveAll clones per line (COW for the live
	// hub). Doing that here is O(n²) on the daily journal (~350k lines)
	// and starves mux hydrate — empty transcript, daemon at multi-GB.
	var out []Event
	open := make(map[string]int)
	tools := make(map[string]int)
	for _, ln := range lines {
		out, _ = foldLine(out, ln, open, tools)
	}
	return out
}

// nextIndex is the next 1-based coalesced index after prev.
// The hub cache is often a statedb suffix (absolute indexes 9970..10000
// with len=30). Minting len(prev)+1 there produces index 31, which a
// following window [9971, 0) drops — owner send journals and the React
// composer never sees the echo.
func nextIndex(prev []Event) int {
	if len(prev) == 0 {
		return 1
	}
	if i := prev[len(prev)-1].Index; i >= 1 {
		return i + 1
	}
	return 1
}

func parseEvent(line string, index int) Event {
	var probe struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
	}
	_ = json.Unmarshal([]byte(line), &probe)
	body := json.RawMessage(strings.TrimSpace(line))
	if !json.Valid(body) {
		body = json.RawMessage(`{}`)
	}
	return Event{
		ID:    fmt.Sprintf("e:%d", index),
		Index: index,
		Kind:  KindOfType(probe.Type),
		Type:  probe.Type,
		TS:    probe.Timestamp,
		Body:  body,
	}
}

// KindsOf returns 0-based kinds for Subscribe (index i at kinds[i]).
func KindsOf(events []Event) []Kind {
	out := make([]Kind, len(events))
	for i, e := range events {
		out[i] = e.Kind
	}
	return out
}

// HaveFromIDs maps sent event ids onto 1-based indices present in events.
func HaveFromIDs(events []Event, sent map[string]struct{}) map[int]struct{} {
	have := make(map[int]struct{})
	for _, e := range events {
		if _, ok := sent[e.ID]; ok {
			have[e.Index] = struct{}{}
		}
	}
	return have
}

// Slice returns events whose 1-based index is in indices, in index order.
func Slice(events []Event, indices []int) []Event {
	by := make(map[int]Event, len(events))
	for _, e := range events {
		by[e.Index] = e
	}
	out := make([]Event, 0, len(indices))
	for _, i := range indices {
		if e, ok := by[i]; ok {
			out = append(out, e)
		}
	}
	return out
}

func indexOfID(events []Event, id string) int {
	for _, e := range events {
		if e.ID == id {
			return e.Index
		}
	}
	return -1
}

// Before is page-up: K coalesced events strictly older than id.
func Before(events []Event, id string, limit int) (Resolved, error) {
	if limit <= 0 {
		limit = 50
	}
	idx := indexOfID(events, id)
	if idx < 1 {
		return Resolved{}, fmt.Errorf("muxwin: unknown before id %q", id)
	}
	lo := idx - limit
	if lo < 1 {
		lo = 1
	}
	return Resolved{Lo: lo, Hi: idx, Following: false}, nil
}

// BeforeUnsent is page-up that walks past slices the client already has
// (connect halo / leave-live Subscribe). It keeps going until `limit`
// unsent events or the start of the journal.
func BeforeUnsent(events []Event, id string, limit int, have map[int]struct{}) (Resolved, error) {
	if limit <= 0 {
		limit = 50
	}
	idx := indexOfID(events, id)
	if idx < 1 {
		return Resolved{}, fmt.Errorf("muxwin: unknown before id %q", id)
	}
	if idx < 2 {
		return Resolved{Lo: 1, Hi: 1, Following: false}, nil
	}
	lo := idx
	n := 0
	for i := idx - 1; i >= 1 && n < limit; i-- {
		lo = i
		if _, ok := have[i]; !ok {
			n++
		}
	}
	if n == 0 {
		return Resolved{Lo: 1, Hi: idx, Following: false}, nil
	}
	return Resolved{Lo: lo, Hi: idx, Following: false}, nil
}

// LiveFold is one event minted or updated by a single journal line.
// Text is the token delta on assistant append/put so the client can
// grow the bubble without waiting for a later full-body replace.
type LiveFold struct {
	Event Event
	Op    string
	Text  string
}

// ApplyLive folds one raw journal line into a coalesced stream and
// returns the last fold (existing callers). Prefer ApplyLiveAll when
// one line can mint several events (text + tool_use).
func ApplyLive(prev []Event, line string) (next []Event, changed Event, op string) {
	next, folds := ApplyLiveAll(prev, line)
	if len(folds) == 0 {
		return prev, Event{}, ""
	}
	last := folds[len(folds)-1]
	return next, last.Event, last.Op
}

// ApplyLiveAll folds one raw journal line. Assistant text tokens append
// to the open matching stream; each tool_use mints a KindSteps event
// (🎯T119.10). tool_result / lossless progress do not mint — they are
// not a second step. Anything else (or a sealed/new stream) mints the
// next index.
func ApplyLiveAll(prev []Event, line string) (next []Event, folds []LiveFold) {
	return foldLine(cloneEvents(prev), line, nil, nil)
}

// foldLine mutates dst in place. Callers that share a slice with the
// hub must clone first (ApplyLiveAll). open maps stream_id → index of
// the unsealed assistant (hydrate); tools maps toolCallId → step index.
// Nil maps mean scan backward (live).
func foldLine(dst []Event, line string, open, byID map[string]int) (next []Event, folds []LiveFold) {
	line = strings.TrimSpace(line)
	if line == "" {
		return dst, nil
	}
	var h struct {
		Type      string `json:"type"`
		StreamID  string `json:"stream_id"`
		Timestamp string `json:"timestamp"`
		Recorded  string `json:"recorded"`
		Message   *struct {
			Content    any    `json:"content"`
			StopReason string `json:"stop_reason"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &h); err != nil {
		ev := parseEvent(line, nextIndex(dst))
		return append(dst, ev), []LiveFold{{Event: ev, Op: "put"}}
	}
	if h.Type == "progress" {
		return mergeProgress(dst, line, byID)
	}
	if h.Recorded == "lossless" || h.Type == "tool_result" || h.Type == "result" ||
		h.Type == "system" {
		return dst, nil
	}
	next = dst
	if h.Type == "assistant" {
		sid := strings.TrimSpace(h.StreamID)
		text := contentText(h.Message)
		blocks := contentTools(h.Message)
		stop := ""
		if h.Message != nil {
			stop = h.Message.StopReason
		}
		if text != "" || stop != "" {
			if i, ok := openAssistant(next, sid, open); ok {
				next[i] = appendAssistant(next[i], text, stop, h.Timestamp)
				folds = append(folds, LiveFold{Event: next[i], Op: "append", Text: text})
				if stop != "" && open != nil {
					delete(open, sid)
				}
			} else {
				ev := assistantTextEvent(nextIndex(next), sid, text, stop, h.Timestamp)
				next = append(next, ev)
				folds = append(folds, LiveFold{Event: ev, Op: "put", Text: text})
				if open != nil && stop == "" {
					open[sid] = len(next) - 1
				}
			}
		}
		for _, blk := range blocks {
			ev := toolUseEvent(nextIndex(next), blk, h.Timestamp)
			next = append(next, ev)
			folds = append(folds, LiveFold{Event: ev, Op: "put"})
			if byID != nil {
				if id, _ := blk["id"].(string); strings.TrimSpace(id) != "" {
					byID[strings.TrimSpace(id)] = len(next) - 1
				}
			}
		}
		return next, folds
	}
	ev := parseEvent(line, nextIndex(next))
	return append(next, ev), []LiveFold{{Event: ev, Op: "put"}}
}

// ToolStamp is a real MCP tools/call name+args to pair onto a generic
// KindSteps row (Cursor ACP title is often the useless "MCP: tool").
type ToolStamp struct {
	Name  string
	Input map[string]any
}

// HasGenericSteps reports an unmatched "MCP: tool" (or other generic) step.
func HasGenericSteps(evs []Event) bool {
	_, ok := oldestGenericSteps(evs)
	return ok
}

// ApplyStamps pairs stamps onto the oldest generic KindSteps, FIFO.
// Leftover stamps had no generic row yet.
func ApplyStamps(prev []Event, stamps []ToolStamp) (next []Event, folds []LiveFold, applied, rest []ToolStamp) {
	next = prev
	for i, st := range stamps {
		n, f := applyStamp(next, st)
		if len(f) == 0 {
			return next, folds, applied, stamps[i:]
		}
		next = n
		folds = append(folds, f...)
		applied = append(applied, st)
	}
	return next, folds, applied, nil
}

func applyStamp(prev []Event, st ToolStamp) ([]Event, []LiveFold) {
	name := strings.TrimSpace(st.Name)
	if name == "" || genericToolName(name) {
		return prev, nil
	}
	i, ok := oldestGenericSteps(prev)
	if !ok {
		return prev, nil
	}
	prev = cloneEvents(prev)
	prev[i] = enrichToolUse(prev[i], name, st.Input)
	return prev, []LiveFold{{Event: prev[i], Op: "append"}}
}

func oldestGenericSteps(evs []Event) (int, bool) {
	for i, e := range evs {
		if e.Kind == KindSteps && genericToolName(eventToolName(e)) {
			return i, true
		}
	}
	return 0, false
}

func eventToolName(ev Event) string {
	var h struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(ev.Body, &h)
	return strings.TrimSpace(h.Name)
}

// mergeProgress applies a lossless ACP tool_call_update onto an existing
// KindSteps row (same toolCallId). Status-only updates do not mint a
// second step (🎯T63). Cursor MCP rows often have no id — pair onto the
// oldest generic "MCP: tool" step (🎯T64.2).
func mergeProgress(prev []Event, line string, byID map[string]int) ([]Event, []LiveFold) {
	var wrap struct {
		Raw json.RawMessage `json:"raw"`
	}
	if json.Unmarshal([]byte(line), &wrap) != nil || len(wrap.Raw) == 0 {
		return prev, nil
	}
	var probe struct {
		SessionUpdate string          `json:"sessionUpdate"`
		Title         string          `json:"title"`
		Name          string          `json:"name"`
		ToolCallID    string          `json:"toolCallId"`
		RawInput      json.RawMessage `json:"rawInput"`
		Arguments     json.RawMessage `json:"arguments"`
		Update        struct {
			SessionUpdate string          `json:"sessionUpdate"`
			Title         string          `json:"title"`
			Name          string          `json:"name"`
			ToolCallID    string          `json:"toolCallId"`
			RawInput      json.RawMessage `json:"rawInput"`
			Arguments     json.RawMessage `json:"arguments"`
		} `json:"update"`
	}
	if json.Unmarshal(wrap.Raw, &probe) != nil {
		return prev, nil
	}
	id := strings.TrimSpace(probe.Update.ToolCallID)
	if id == "" {
		id = strings.TrimSpace(probe.ToolCallID)
	}
	title := strings.TrimSpace(probe.Update.Title)
	if title == "" {
		title = strings.TrimSpace(probe.Title)
	}
	name := strings.TrimSpace(probe.Update.Name)
	if name == "" {
		name = strings.TrimSpace(probe.Name)
	}
	input := decodeJSONObject(probe.Update.RawInput)
	if input == nil {
		input = decodeJSONObject(probe.Update.Arguments)
	}
	if input == nil {
		input = decodeJSONObject(probe.RawInput)
	}
	if input == nil {
		input = decodeJSONObject(probe.Arguments)
	}
	if genericToolName(title) && genericToolName(name) && len(input) == 0 {
		return prev, nil
	}
	label := title
	if genericToolName(label) {
		label = name
	}
	if genericToolName(label) && input != nil {
		if tn, _ := input["tool_name"].(string); strings.TrimSpace(tn) != "" {
			label = strings.TrimSpace(tn)
		}
	}
	if genericToolName(label) && len(input) == 0 {
		return prev, nil
	}
	if id != "" {
		if byID != nil {
			if i, ok := byID[id]; ok && i >= 0 && i < len(prev) {
				prev[i] = enrichToolUse(prev[i], label, input)
				return prev, []LiveFold{{Event: prev[i], Op: "append"}}
			}
		} else {
			for i := len(prev) - 1; i >= 0; i-- {
				if prev[i].Kind != KindSteps {
					continue
				}
				if eventToolID(prev[i]) != id {
					continue
				}
				prev[i] = enrichToolUse(prev[i], label, input)
				return prev, []LiveFold{{Event: prev[i], Op: "append"}}
			}
		}
	}
	if i, ok := oldestGenericSteps(prev); ok {
		prev[i] = enrichToolUse(prev[i], label, input)
		return prev, []LiveFold{{Event: prev[i], Op: "append"}}
	}
	return prev, nil
}

func genericToolName(s string) bool {
	t := strings.ToLower(strings.Join(strings.Fields(s), " "))
	return t == "" || t == "tool" || t == "tool_use" || t == "mcp: tool" || t == "mcp:tool"
}

func decodeJSONObject(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil && len(m) > 0 {
		return m
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		if json.Unmarshal([]byte(s), &m) == nil && len(m) > 0 {
			return m
		}
	}
	return nil
}

func eventToolID(ev Event) string {
	var h struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(ev.Body, &h)
	return strings.TrimSpace(h.ID)
}

func enrichToolUse(ev Event, name string, input map[string]any) Event {
	var m map[string]any
	if json.Unmarshal(ev.Body, &m) != nil {
		return ev
	}
	if name != "" && !genericToolName(name) {
		m["name"] = name
	}
	if len(input) > 0 {
		if cur, _ := m["input"].(map[string]any); len(cur) == 0 {
			m["input"] = input
		}
	}
	b, err := json.Marshal(m)
	if err == nil {
		ev.Body = b
	}
	return ev
}

func cloneEvents(in []Event) []Event {
	if len(in) == 0 {
		return nil
	}
	out := make([]Event, len(in))
	copy(out, in)
	return out
}

func eventStreamID(ev Event) string {
	var h struct {
		StreamID string `json:"stream_id"`
	}
	_ = json.Unmarshal(ev.Body, &h)
	return strings.TrimSpace(h.StreamID)
}

func openAssistant(evs []Event, sid string, open map[string]int) (int, bool) {
	if open != nil {
		i, ok := open[sid]
		if !ok || i < 0 || i >= len(evs) {
			return 0, false
		}
		if evs[i].Type != "assistant" || assistantSealed(evs[i]) {
			delete(open, sid)
			return 0, false
		}
		return i, true
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != "assistant" {
			continue
		}
		if sid != "" && eventStreamID(evs[i]) != sid {
			continue
		}
		if assistantSealed(evs[i]) {
			return 0, false
		}
		return i, true
	}
	return 0, false
}

func assistantSealed(ev Event) bool {
	var h struct {
		Message *struct {
			StopReason string `json:"stop_reason"`
		} `json:"message"`
	}
	_ = json.Unmarshal(ev.Body, &h)
	if h.Message == nil {
		return false
	}
	return h.Message.StopReason == "end_turn" ||
		h.Message.StopReason == "stop_sequence" ||
		h.Message.StopReason == "max_tokens"
}

func contentTools(msg *struct {
	Content    any    `json:"content"`
	StopReason string `json:"stop_reason"`
}) []map[string]any {
	if msg == nil {
		return nil
	}
	c, ok := msg.Content.([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, raw := range c {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := m["type"].(string); t == "tool_use" {
			out = append(out, m)
		}
	}
	return out
}

func assistantTextEvent(index int, sid, text, stop, ts string) Event {
	msg := map[string]any{"role": "assistant"}
	if text != "" {
		msg["content"] = []any{map[string]any{"type": "text", "text": text}}
	} else {
		msg["content"] = []any{}
	}
	if stop != "" {
		msg["stop_reason"] = stop
	}
	m := map[string]any{"type": "assistant", "message": msg}
	if sid != "" {
		m["stream_id"] = sid
	}
	if ts != "" {
		m["timestamp"] = ts
	}
	b, err := json.Marshal(m)
	if err != nil {
		b = []byte(`{"type":"assistant"}`)
	}
	return Event{
		ID:    fmt.Sprintf("e:%d", index),
		Index: index,
		Kind:  KindAssistant,
		Type:  "assistant",
		TS:    ts,
		Body:  b,
	}
}

func toolUseEvent(index int, blk map[string]any, ts string) Event {
	m := map[string]any{"type": "tool_use"}
	for _, k := range []string{"name", "input", "id", "kind"} {
		if v, ok := blk[k]; ok {
			m[k] = v
		}
	}
	name, _ := m["name"].(string)
	if genericToolName(name) {
		if in, _ := m["input"].(map[string]any); in != nil {
			if tn, _ := in["tool_name"].(string); strings.TrimSpace(tn) != "" {
				m["name"] = strings.TrimSpace(tn)
			}
		}
	}
	if ts != "" {
		m["timestamp"] = ts
	}
	b, err := json.Marshal(m)
	if err != nil {
		b = []byte(`{"type":"tool_use"}`)
	}
	return Event{
		ID:    fmt.Sprintf("e:%d", index),
		Index: index,
		Kind:  KindSteps,
		Type:  "tool_use",
		TS:    ts,
		Body:  b,
	}
}

func contentText(msg *struct {
	Content    any    `json:"content"`
	StopReason string `json:"stop_reason"`
}) string {
	if msg == nil {
		return ""
	}
	switch c := msg.Content.(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, raw := range c {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			t, _ := m["type"].(string)
			if t != "" && t != "text" && t != "output_text" {
				continue
			}
			s, _ := m["text"].(string)
			b.WriteString(s)
		}
		return b.String()
	default:
		return ""
	}
}

func appendAssistant(ev Event, text, stop, ts string) Event {
	var m map[string]any
	if json.Unmarshal(ev.Body, &m) != nil {
		m = map[string]any{"type": "assistant"}
	}
	msg, _ := m["message"].(map[string]any)
	if msg == nil {
		msg = map[string]any{"role": "assistant"}
	}
	if text != "" {
		switch c := msg["content"].(type) {
		case string:
			msg["content"] = c + text
		case []any:
			joined := false
			for i := range c {
				blk, ok := c[i].(map[string]any)
				if !ok || joined {
					continue
				}
				t, _ := blk["type"].(string)
				if t == "text" || t == "output_text" {
					blk["text"] = fmt.Sprint(blk["text"]) + text
					c[i] = blk
					joined = true
				}
			}
			if !joined {
				c = append(c, map[string]any{"type": "text", "text": text})
			}
			msg["content"] = c
		default:
			msg["content"] = []any{map[string]any{"type": "text", "text": text}}
		}
	}
	if stop != "" {
		msg["stop_reason"] = stop
	}
	m["message"] = msg
	if ts != "" {
		if _, ok := m["timestamp"]; !ok {
			m["timestamp"] = ts
		}
		ev.TS = ts
	}
	b, err := json.Marshal(m)
	if err == nil {
		ev.Body = b
	}
	return ev
}
