// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package transcript

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// Logical is a reconstructed predecessor conversation (🎯T621): compact,
// snip, parentUuid, Codex rollback, and Grok updates.jsonl applied so
// Distill and inspect agree on which turns still exist.
type Logical struct {
	Turns    []Turn
	Warnings []string
}

const (
	warnPreservedIncomplete = "preserved_segment_unavailable: Claude preserved-segment metadata was incomplete; pre-compact history was retained"
	warnPreservedMissing    = "preserved_segment_unavailable: Claude preserved-segment messages were missing or cyclic; pre-compact history was retained"
	warnParentCycle         = "parent_cycle: a cycle was detected in the Claude parent chain; only the recoverable suffix is shown"
)

// ReadLogical parses a transcript file into the predecessor's logical
// conversation. Unknown / linear files fall through to extractTurns.
func ReadLogical(path string) (Logical, error) {
	raws, err := readRawLines(path)
	if err != nil {
		return Logical{}, err
	}
	kept, warnings := reconstructRaw(path, raws)
	lines := parseLines(kept)
	turns := turnsFromLines(lines)
	if len(turns) == 0 && hasTranscriptPayload(lines) {
		return Logical{}, fmt.Errorf(
			"transcript %s has %d lines and no readable turns — %s",
			path, len(lines), describeLines(lines),
		)
	}
	return Logical{Turns: turns, Warnings: warnings}, nil
}

func reconstructRaw(path string, raws []string) (kept []string, warnings []string) {
	kind := classifyTranscript(path, raws)
	switch kind {
	case kindClaude:
		return reconstructClaude(raws)
	case kindCodex:
		return reconstructCodex(raws)
	default:
		return raws, nil
	}
}

type transcriptKind int

const (
	kindLinear transcriptKind = iota
	kindClaude
	kindCodex
	kindGrokUpdates
)

func classifyTranscript(path string, raws []string) transcriptKind {
	if strings.EqualFold(filepath.Base(path), "updates.jsonl") {
		return kindGrokUpdates
	}
	for _, raw := range raws {
		var rec map[string]any
		if json.Unmarshal([]byte(raw), &rec) != nil {
			continue
		}
		t, _ := rec["type"].(string)
		switch t {
		case "session_meta", "compacted", "response_item":
			return kindCodex
		case "system":
			if sub, _ := rec["subtype"].(string); sub == "compact_boundary" {
				return kindClaude
			}
		}
		if rec["message"] != nil && (t == "user" || t == "assistant") {
			return kindClaude
		}
	}
	return kindLinear
}

func reconstructClaude(raws []string) ([]string, []string) {
	records := parseMaps(raws)
	if !claudeHasUUID(records) {
		return raws, nil
	}
	// Parent-chain collapse is for compact/snip. A linear Claude file
	// still carries queue-operation / queued_command records that are
	// not user/assistant messages — walking the leaf would drop them
	// (🎯T422). Without a compact_boundary or snip, leave the file as-is.
	if !claudeNeedsReconstruct(records) {
		return raws, nil
	}
	var warnings []string
	messages, warnings := prepareClaudeMessages(records, warnings)
	leaf := claudeLeaf(messages, &warnings)
	if leaf == nil {
		return raws, warnings
	}
	chain, _ := claudeChain(messages, leaf, &warnings)
	var kept []string
	for _, rec := range chain {
		if skipClaudeRender(rec) {
			continue
		}
		b, err := json.Marshal(rec)
		if err != nil {
			continue
		}
		kept = append(kept, string(b))
	}
	if len(kept) == 0 {
		return raws, warnings
	}
	return kept, warnings
}

func skipClaudeRender(rec map[string]any) bool {
	t, _ := rec["type"].(string)
	if t != "user" && t != "assistant" {
		return true
	}
	for _, flag := range []string{"isMeta", "isSidechain", "isCompactSummary", "isVirtual", "isVisibleInTranscriptOnly"} {
		if truthy(rec[flag]) {
			return true
		}
	}
	return false
}

func truthy(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func claudeNeedsReconstruct(records []map[string]any) bool {
	for _, rec := range records {
		if isClaudeBoundary(rec) {
			return true
		}
		if rec["snipMetadata"] != nil || rec["snip_metadata"] != nil {
			return true
		}
	}
	return false
}

func claudeHasUUID(records []map[string]any) bool {
	for _, rec := range records {
		if uuidString(rec["uuid"]) != "" {
			return true
		}
	}
	return false
}

func prepareClaudeMessages(records []map[string]any, warnings []string) (map[string]map[string]any, []string) {
	lastNonPreserved := -1
	for i, rec := range records {
		if isClaudeBoundary(rec) && claudeSegment(rec) == nil {
			lastNonPreserved = i
		}
	}
	scoped := records
	if lastNonPreserved >= 0 {
		scoped = records[lastNonPreserved:]
	}
	messages := map[string]map[string]any{}
	var order []string
	for _, rec := range scoped {
		if truthy(rec["isSidechain"]) {
			continue
		}
		t, _ := rec["type"].(string)
		if t != "user" && t != "assistant" && t != "system" {
			continue
		}
		id := uuidString(rec["uuid"])
		if id == "" {
			continue
		}
		cp := map[string]any{}
		for k, v := range rec {
			cp[k] = v
		}
		messages[id] = cp
		order = append(order, id)
	}
	warnings = applyClaudePreservedSegment(messages, order, warnings)
	applyClaudeSnip(messages)
	return messages, warnings
}

func isClaudeBoundary(rec map[string]any) bool {
	t, _ := rec["type"].(string)
	sub, _ := rec["subtype"].(string)
	return t == "system" && sub == "compact_boundary"
}

func claudeSegment(rec map[string]any) map[string]string {
	meta, _ := rec["compactMetadata"].(map[string]any)
	if meta == nil {
		meta, _ = rec["compact_metadata"].(map[string]any)
	}
	if meta == nil {
		return nil
	}
	seg, _ := meta["preservedSegment"].(map[string]any)
	if seg == nil {
		seg, _ = meta["preserved_segment"].(map[string]any)
	}
	if seg == nil {
		return nil
	}
	return map[string]string{
		"head":   uuidString(first(seg, "headUuid", "head_uuid")),
		"anchor": uuidString(first(seg, "anchorUuid", "anchor_uuid")),
		"tail":   uuidString(first(seg, "tailUuid", "tail_uuid")),
	}
}

func first(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}

func uuidString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func claudeParent(rec map[string]any) string {
	if rec == nil {
		return ""
	}
	if p := uuidString(rec["parentUuid"]); p != "" {
		return p
	}
	return uuidString(rec["logicalParentUuid"])
}

func setClaudeParent(rec map[string]any, parent string) {
	if parent == "" {
		rec["parentUuid"] = nil
	} else {
		rec["parentUuid"] = parent
	}
	if _, ok := rec["logicalParentUuid"]; ok {
		if parent == "" {
			rec["logicalParentUuid"] = nil
		} else {
			rec["logicalParentUuid"] = parent
		}
	}
}

func applyClaudePreservedSegment(messages map[string]map[string]any, order []string, warnings []string) []string {
	absolute := -1
	lastSegIdx := -1
	var lastSeg map[string]string
	for i, id := range order {
		rec := messages[id]
		if rec == nil || !isClaudeBoundary(rec) {
			continue
		}
		absolute = i
		if seg := claudeSegment(rec); seg != nil {
			lastSeg = seg
			lastSegIdx = i
		}
	}
	if lastSeg == nil {
		return warnings
	}
	preserved := map[string]bool{}
	if lastSegIdx == absolute {
		head, anchor, tail := lastSeg["head"], lastSeg["anchor"], lastSeg["tail"]
		if head == "" || anchor == "" || tail == "" {
			return append(warnings, warnPreservedIncomplete)
		}
		seen := map[string]bool{}
		cur := messages[tail]
		reached := false
		for cur != nil {
			id := uuidString(cur["uuid"])
			if id == "" || seen[id] {
				break
			}
			seen[id] = true
			preserved[id] = true
			if id == head {
				reached = true
				break
			}
			p := claudeParent(cur)
			if p == "" {
				break
			}
			cur = messages[p]
		}
		if !reached {
			return append(warnings, warnPreservedMissing)
		}
		if h := messages[head]; h != nil {
			setClaudeParent(h, anchor)
		}
		for id, rec := range messages {
			if id != head && claudeParent(rec) == anchor {
				setClaudeParent(rec, tail)
			}
		}
	}
	if absolute < 0 {
		return warnings
	}
	for _, id := range order[:absolute] {
		if !preserved[id] {
			delete(messages, id)
		}
	}
	return warnings
}

func applyClaudeSnip(messages map[string]map[string]any) {
	removed := map[string]bool{}
	for _, rec := range messages {
		meta, _ := rec["snipMetadata"].(map[string]any)
		if meta == nil {
			meta, _ = rec["snip_metadata"].(map[string]any)
		}
		if meta == nil {
			continue
		}
		vals, _ := meta["removedUuids"].([]any)
		if vals == nil {
			vals, _ = meta["removed_uuids"].([]any)
		}
		for _, v := range vals {
			if id := uuidString(v); id != "" {
				removed[id] = true
			}
		}
	}
	if len(removed) == 0 {
		return
	}
	deletedParents := map[string]string{}
	for id := range removed {
		if rec := messages[id]; rec != nil {
			deletedParents[id] = claudeParent(rec)
		}
		delete(messages, id)
	}
	var resolve func(string) string
	resolve = func(start string) string {
		seen := map[string]bool{}
		cur := start
		for cur != "" && removed[cur] && !seen[cur] {
			seen[cur] = true
			cur = deletedParents[cur]
		}
		return cur
	}
	for _, rec := range messages {
		if p := claudeParent(rec); p != "" && removed[p] {
			setClaudeParent(rec, resolve(p))
		}
	}
}

func claudeLeaf(messages map[string]map[string]any, warnings *[]string) map[string]any {
	if len(messages) == 0 {
		return nil
	}
	parents := map[string]bool{}
	for _, rec := range messages {
		if p := claudeParent(rec); p != "" {
			parents[p] = true
		}
	}
	var candidates []map[string]any
	for _, rec := range messages {
		id := uuidString(rec["uuid"])
		if id == "" || parents[id] {
			continue
		}
		cur := rec
		seen := map[string]bool{}
		for cur != nil {
			cid := uuidString(cur["uuid"])
			if cid == "" || seen[cid] {
				*warnings = append(*warnings, warnParentCycle)
				break
			}
			seen[cid] = true
			t, _ := cur["type"].(string)
			if t == "user" || t == "assistant" {
				candidates = append(candidates, cur)
				break
			}
			p := claudeParent(cur)
			if p == "" {
				break
			}
			cur = messages[p]
		}
	}
	if len(candidates) == 0 {
		for _, rec := range messages {
			t, _ := rec["type"].(string)
			if t == "user" || t == "assistant" {
				candidates = append(candidates, rec)
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	// Prefer the last user/assistant in file-ish map iteration; take the
	// candidate whose uuid is not a parent of another candidate when
	// possible, else the last one seen.
	return candidates[len(candidates)-1]
}

func claudeChain(messages map[string]map[string]any, leaf map[string]any, warnings *[]string) ([]map[string]any, map[string]bool) {
	var chain []map[string]any
	seen := map[string]bool{}
	cur := leaf
	for cur != nil {
		id := uuidString(cur["uuid"])
		if id == "" {
			break
		}
		if seen[id] {
			*warnings = append(*warnings, warnParentCycle)
			break
		}
		seen[id] = true
		chain = append(chain, cur)
		p := claudeParent(cur)
		if p == "" {
			break
		}
		cur = messages[p]
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, seen
}

func reconstructCodex(raws []string) ([]string, []string) {
	records := parseMaps(raws)
	var base []any
	start := 0
	for i, rec := range records {
		if rec["type"] != "compacted" {
			continue
		}
		payload, _ := rec["payload"].(map[string]any)
		if payload == nil {
			continue
		}
		if hist, ok := payload["replacement_history"].([]any); ok {
			base = hist
			start = i + 1
		}
	}
	var turns []map[string]any
	for _, item := range base {
		if t := renderCodexItem(item); t != nil {
			turns = append(turns, t)
		}
	}
	for _, rec := range records[start:] {
		t, _ := rec["type"].(string)
		payload := rec["payload"]
		switch t {
		case "response_item":
			if item := renderCodexItem(payload); item != nil {
				turns = append(turns, item)
			}
		case "event_msg":
			p, _ := payload.(map[string]any)
			if p != nil && p["type"] == "thread_rolled_back" {
				n, _ := p["num_turns"].(float64)
				dropLastUserTurns(&turns, int(n))
			}
		}
	}
	var kept []string
	for _, t := range turns {
		b, err := json.Marshal(t)
		if err != nil {
			continue
		}
		kept = append(kept, string(b))
	}
	if len(kept) == 0 {
		return raws, nil
	}
	return kept, nil
}

func renderCodexItem(item any) map[string]any {
	m, ok := item.(map[string]any)
	if !ok {
		return nil
	}
	switch m["type"] {
	case "message":
		role, _ := m["role"].(string)
		if role != "user" && role != "assistant" {
			return nil
		}
		text := codexMessageText(m)
		if text == "" {
			return nil
		}
		return map[string]any{"type": role, "content": text}
	case "function_call", "local_shell_call", "custom_tool_call":
		name, _ := m["name"].(string)
		if name == "" {
			if m["type"] == "local_shell_call" {
				name = "local_shell"
			} else {
				name = "function"
			}
		}
		return map[string]any{
			"type":    "assistant",
			"content": "called inert foreign tool(s): " + name,
		}
	default:
		return nil
	}
}

func codexMessageText(item map[string]any) string {
	content, _ := item["content"].([]any)
	var parts []string
	for _, blk := range content {
		b, ok := blk.(map[string]any)
		if !ok {
			continue
		}
		switch b["type"] {
		case "input_text", "output_text", "text":
			if s, _ := b["text"].(string); strings.TrimSpace(s) != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func dropLastUserTurns(turns *[]map[string]any, n int) {
	if n <= 0 || turns == nil {
		return
	}
	var pos []int
	for i, t := range *turns {
		if t["type"] == "user" {
			pos = append(pos, i)
		}
	}
	if len(pos) == 0 {
		return
	}
	cut := pos[0]
	if n < len(pos) {
		cut = pos[len(pos)-n]
	}
	*turns = (*turns)[:cut]
}

func parseMaps(raws []string) []map[string]any {
	var out []map[string]any
	for _, raw := range raws {
		var rec map[string]any
		if json.Unmarshal([]byte(raw), &rec) != nil {
			continue
		}
		out = append(out, rec)
	}
	return out
}
