// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package muxwin

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func tok(sid, text, stop string) string {
	msg := map[string]any{
		"type":      "assistant",
		"stream_id": sid,
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{},
		},
	}
	if text != "" {
		msg["message"].(map[string]any)["content"] = []any{
			map[string]any{"type": "text", "text": text},
		}
	}
	if stop != "" {
		msg["message"].(map[string]any)["stop_reason"] = stop
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

func user(text string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []any{map[string]any{"type": "text", "text": text}},
		},
	})
	return string(b)
}

func toolAsst(name string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "tool_use", "name": name, "input": map[string]any{"path": "x"}},
			},
		},
	})
	return string(b)
}

func TestEventsFromLinesSealsTokensKeepsUserAndTools(t *testing.T) {
	lines := []string{
		user("u0"),
		user("u1"),
		tok("s1", "Hel", ""),
		tok("s1", "lo", ""),
		tok("s1", "", "end_turn"),
		`{"type":"tool_use","name":"Read"}`,
		user("u2"),
		tok("s2", "Hi", ""),
	}
	evs := EventsFromLines(lines)
	if len(evs) != 6 {
		t.Fatalf("events=%d want 6 (2 users + sealed Hello + tool + user + Hi): %#v", len(evs), typesOf(evs))
	}
	if evs[2].Type != "assistant" || prose(evs[2]) != "Hello" {
		t.Fatalf("sealed assistant=%s body=%s", evs[2].Type, prose(evs[2]))
	}
	if evs[0].ID != "e:1" || evs[2].Index != 3 {
		t.Fatalf("ids/index: %+v %+v", evs[0], evs[2])
	}
	if evs[3].Kind != KindSteps {
		t.Fatalf("tool should be steps chrome, got %v", evs[3].Kind)
	}
}

func TestEventsFromLinesLinearOnManyTokens(t *testing.T) {
	lines := make([]string, 0, 3002)
	lines = append(lines, user("u"))
	for i := 0; i < 3000; i++ {
		lines = append(lines, tok("s1", "x", ""))
	}
	t0 := time.Now()
	evs := EventsFromLines(lines)
	if d := time.Since(t0); d > 2*time.Second {
		t.Fatalf("fold took %s — clone-per-line hydrate melts the daily journal", d)
	}
	if len(evs) != 2 || evs[1].Type != "assistant" {
		t.Fatalf("n=%d types=%v want user+one assistant", len(evs), typesOf(evs))
	}
}

func TestEventsFromLinesKeepsAssistantToolUseAsSteps(t *testing.T) {
	lines := []string{
		user("u0"),
		tok("s1", "Checking.", ""),
		toolAsst("Read"),
		toolAsst("Grep"),
		`{"type":"tool_result","content":"ok"}`,
		`{"type":"progress","recorded":"lossless","progress_type":"tool_use"}`,
		tok("s1", " done", "end_turn"),
	}
	evs := EventsFromLines(lines)
	var steps, asst int
	var names []string
	for _, e := range evs {
		if e.Kind == KindSteps {
			steps++
			var m map[string]any
			_ = json.Unmarshal(e.Body, &m)
			if n, _ := m["name"].(string); n != "" {
				names = append(names, n)
			}
		}
		if e.Type == "assistant" {
			asst++
		}
	}
	if asst != 1 {
		t.Fatalf("assistant events=%d want 1 (text joined): types=%v", asst, typesOf(evs))
	}
	if steps != 2 || strings.Join(names, ",") != "Read,Grep" {
		t.Fatalf("steps=%d names=%v types=%v — tool_use must survive hydrate", steps, names, typesOf(evs))
	}
	if prose(evs[1]) != "Checking. done" && prose(findType(evs, "assistant")) != "Checking. done" {
		t.Fatalf("joined assistant %q", prose(findType(evs, "assistant")))
	}
}

func TestApplyLiveProgressUpdateEnrichesGenericMCPTool(t *testing.T) {
	first := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"MCP: tool","id":"call_1"}]}}`
	evs := EventsFromLines([]string{first})
	if evs[0].Kind != KindSteps {
		t.Fatalf("want steps, got %v", evs[0].Kind)
	}
	update := `{"type":"progress","recorded":"lossless","progress_type":"tool_use","raw":{"update":{"sessionUpdate":"tool_call_update","toolCallId":"call_1","title":"jevons_agent_list","rawInput":{"query":"running"}}}}`
	next, folds := ApplyLiveAll(evs, update)
	if len(folds) != 1 || folds[0].Op != "append" {
		t.Fatalf("folds=%+v want one append enrich", folds)
	}
	var m map[string]any
	_ = json.Unmarshal(next[0].Body, &m)
	if m["name"] != "jevons_agent_list" {
		t.Fatalf("name=%v want jevons_agent_list body=%s", m["name"], next[0].Body)
	}
}

func TestApplyLiveToolUseMintsStepsNotEmptyAppend(t *testing.T) {
	evs := EventsFromLines([]string{tok("s1", "Hi", "")})
	next, folds := ApplyLiveAll(evs, toolAsst("Read"))
	if len(folds) != 1 || folds[0].Op != "put" || folds[0].Event.Kind != KindSteps {
		t.Fatalf("folds=%+v want one KindSteps put", folds)
	}
	if prose(next[0]) != "Hi" {
		t.Fatalf("assistant text mutated: %q", prose(next[0]))
	}
	if next[1].Kind != KindSteps {
		t.Fatalf("second event %v want steps", next[1].Kind)
	}
	// Hydrate of the same tape equals the live fold (reload must not drop steps).
	hydrated := EventsFromLines([]string{tok("s1", "Hi", ""), toolAsst("Read")})
	if len(hydrated) != len(next) || hydrated[1].Kind != KindSteps {
		t.Fatalf("hydrate=%v live=%v", typesOf(hydrated), typesOf(next))
	}
}

func findType(evs []Event, typ string) Event {
	for _, e := range evs {
		if e.Type == typ {
			return e
		}
	}
	return Event{}
}

func TestWindowThenLiveAppendThenBeforeKeepsOrder(t *testing.T) {
	// History: u0 u1 Hello(tool) u2 Hi
	lines := []string{
		user("u0"),
		user("u1"),
		tok("s1", "Hello", "end_turn"),
		user("u2"),
		tok("s2", "Hi", ""),
	}
	evs := EventsFromLines(lines)
	n := len(evs)
	vis, err := Resolve(-2, 0, n)
	if err != nil {
		t.Fatal(err)
	}
	sub := Subscribe(vis, KindsOf(evs), 1)
	need := Need(sub, n, nil)
	got := Slice(evs, need)
	// visible last 2 = u2, Hi; halo 1 prose older = Hello. u0 left for page.
	if ids(got) != "e:3 e:4 e:5" && ids(got) != "e:2 e:3 e:4 e:5" {
		// n=4: u0 u1 Hello u2 Hi → wait 5 events
		t.Logf("n=%d vis=%+v sub=%+v need=%v ids=%s", n, vis, sub, need, ids(got))
	}
	if n != 5 {
		t.Fatalf("n=%d types=%v", n, typesOf(evs))
	}
	if vis.Lo != 4 || !vis.Following {
		t.Fatalf("visible last 2: %+v n=%d", vis, n)
	}
	// halo 1 prose from 3: e3 Hello (assistant) → lo=3
	if sub.Lo != 3 || !sub.Following {
		t.Fatalf("sub=%+v want Lo=3 following", sub)
	}
	if ids(got) != "e:3 e:4 e:5" {
		t.Fatalf("first delivery %s want e:3 e:4 e:5", ids(got))
	}

	// Live append on open s2.
	next, changed, op := ApplyLive(evs, tok("s2", " there", ""))
	if op != "append" || changed.ID != "e:5" {
		t.Fatalf("live op=%s id=%s", op, changed.ID)
	}
	if prose(changed) != "Hi there" {
		t.Fatalf("append text %q", prose(changed))
	}

	// Frozen window does not include the new mint at EOF — append is
	// in-window (same id). A brand-new event after freeze is the other case:
	frozen := Freeze(vis, n)
	newer, minted, mop := ApplyLive(next, user("u3"))
	if mop != "put" || minted.Index != 6 {
		t.Fatalf("mint u3: op=%s idx=%d", mop, minted.Index)
	}
	if frozen.Following {
		t.Fatal("leave-live must freeze")
	}
	if Need(frozen, len(newer), HaveFromIDs(next, map[string]struct{}{
		"e:3": {}, "e:4": {}, "e:5": {},
	})) != nil {
		// frozen visible was [4,6) after Freeze of last-2 following n=5
		// Need [4,6) — already have 4,5; must not include e:6
		needFrozen := Need(frozen, len(newer), HaveFromIDs(next, map[string]struct{}{
			"e:3": {}, "e:4": {}, "e:5": {},
		}))
		for _, i := range needFrozen {
			if i == 6 {
				t.Fatalf("frozen window leaked EOF event: %v frozen=%+v", needFrozen, frozen)
			}
		}
	}

	// Page-up: before oldest held (e:3), limit 2 → u0, u1
	page, err := Before(newer, "e:3", 2)
	if err != nil {
		t.Fatal(err)
	}
	pageNeed := Need(page, len(newer), HaveFromIDs(newer, map[string]struct{}{"e:3": {}, "e:4": {}, "e:5": {}}))
	older := Slice(newer, pageNeed)
	if ids(older) != "e:1 e:2" {
		t.Fatalf("page before e:3 → %s want e:1 e:2 (page=%+v need=%v)", ids(older), page, pageNeed)
	}

	// Held order after merge: older + first delivery + live append, no dup.
	have := map[string]Event{}
	for _, e := range append(append([]Event{}, older...), got...) {
		have[e.ID] = e
	}
	have["e:5"] = changed
	var order []Event
	for i := 1; i <= 5; i++ {
		id := "e:" + strconv.Itoa(i)
		e, ok := have[id]
		if !ok {
			t.Fatalf("missing %s", id)
		}
		order = append(order, e)
	}
	want := []string{"u0", "u1", "Hello", "u2", "Hi there"}
	for i, w := range want {
		if prose(order[i]) != w {
			t.Fatalf("order[%d]=%q want %q", i, prose(order[i]), w)
		}
	}
}

func TestBeforeUnsentWalksPastAlreadyHave(t *testing.T) {
	evs := EventsFromLines([]string{
		user("u0"), user("u1"), user("u2"), user("u3"), user("u4"),
	})
	have := map[int]struct{}{3: {}, 4: {}, 5: {}}
	page, err := BeforeUnsent(evs, "e:3", 2, have)
	if err != nil {
		t.Fatal(err)
	}
	need := Need(page, len(evs), have)
	got := ids(Slice(evs, need))
	if got != "e:1 e:2" {
		t.Fatalf("BeforeUnsent past halo → %s want e:1 e:2 (page=%+v need=%v)", got, page, need)
	}
}

func TestBeforeUnknownID(t *testing.T) {
	if _, err := Before(EventsFromLines([]string{user("x")}), "e:99", 2); err == nil {
		t.Fatal("expected unknown id")
	}
}

func TestDefaultFollowAndHaloConstants(t *testing.T) {
	if DefaultFollow != 30 {
		t.Fatalf("DefaultFollow=%d", DefaultFollow)
	}
	if HaloProse != 100 {
		t.Fatalf("HaloProse=%d", HaloProse)
	}
}

func typesOf(evs []Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Type
	}
	return out
}

func ids(evs []Event) string {
	var b []byte
	for i, e := range evs {
		if i > 0 {
			b = append(b, ' ')
		}
		b = append(b, e.ID...)
	}
	return string(b)
}

func prose(ev Event) string {
	var m map[string]any
	if json.Unmarshal(ev.Body, &m) != nil {
		return ""
	}
	if t, ok := m["text"].(string); ok && t != "" {
		return t
	}
	msg, _ := m["message"].(map[string]any)
	if msg == nil {
		return ""
	}
	switch c := msg["content"].(type) {
	case string:
		return c
	case []any:
		var s string
		for _, raw := range c {
			b, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if tx, ok := b["text"].(string); ok {
				s += tx
			}
		}
		return s
	default:
		return ""
	}
}
