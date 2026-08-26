// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/muxwin"
	"github.com/marcelocantos/jevons/internal/statedb"
)

func TestParseTranscriptChannel(t *testing.T) {
	name, ok := parseTranscriptChannel("transcript:jevons-po")
	if !ok || name != "jevons-po" {
		t.Fatalf("got %q ok=%v", name, ok)
	}
	if _, ok := parseTranscriptChannel("fleet"); ok {
		t.Fatal("fleet is not a transcript channel")
	}
	if _, ok := parseTranscriptChannel("transcript:"); ok {
		t.Fatal("empty name")
	}
}

func TestEncodeMuxEnvelope(t *testing.T) {
	b, err := encodeMux("transcript:jevons", "meta", map[string]any{"older": 0, "total": 3})
	if err != nil {
		t.Fatal(err)
	}
	var env muxEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatal(err)
	}
	if env.V != 1 || env.Ch != "transcript:jevons" || env.T != "meta" {
		t.Fatalf("env=%+v", env)
	}
}

func TestOverseerDownSampleAndMuxMeta(t *testing.T) {
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	if got := s.overseerDownSample(); got != "the overseer is not running" {
		t.Fatalf("no proc: %q", got)
	}
	s.SetOverseerDownReason("session/load failed")
	if got := s.overseerDownSample(); got != "session/load failed" {
		t.Fatalf("reason: %q", got)
	}
	meta := s.muxTranscriptMeta(muxwin.Resolved{Lo: 1, Hi: 0, Following: true}, 0, false)
	if meta["overseer_down"] != "session/load failed" {
		t.Fatalf("meta=%+v", meta)
	}
	if _, ok := meta["owner_ux"]; !ok {
		t.Fatalf("missing owner_ux: %+v", meta)
	}
	s.SetOverseerDownReason("")
	// Still down: no Alive process.
	if got := s.overseerDownSample(); got != "the overseer is not running" {
		t.Fatalf("cleared reason but no proc: %q", got)
	}
}

func TestMuxHubFanMetaReachesWatchers(t *testing.T) {
	h := newMuxHub()
	a := &muxSession{send: make(chan []byte, 2), transcripts: map[string]*muxWatch{"jevons": {
		subscribed: true,
	}}}
	b := &muxSession{send: make(chan []byte, 2), transcripts: map[string]*muxWatch{"jevons-po": {
		subscribed: true,
	}}}
	h.add(a)
	h.add(b)
	h.fanMeta("jevons", map[string]any{"overseer_down": "the overseer is not running"})
	select {
	case got := <-a.send:
		var env muxEnvelope
		if err := json.Unmarshal(got, &env); err != nil {
			t.Fatal(err)
		}
		if env.T != "meta" || env.Ch != "transcript:jevons" {
			t.Fatalf("env=%+v", env)
		}
		var body map[string]any
		if err := json.Unmarshal(env.Body, &body); err != nil {
			t.Fatal(err)
		}
		if body["overseer_down"] != "the overseer is not running" {
			t.Fatalf("body=%+v", body)
		}
	default:
		t.Fatal("expected meta on jevons watcher")
	}
	select {
	case <-b.send:
		t.Fatal("po watcher must not get overseer meta")
	default:
	}
}

func TestMuxPageBodyEmptyClearsOlder(t *testing.T) {
	got := muxPageBody(12, 40, nil)
	if got["older"] != 0 || got["start"] != 0 {
		t.Fatalf("empty page must stop paging: %+v", got)
	}
	lines := []json.RawMessage{json.RawMessage(`{"type":"user"}`)}
	got = muxPageBody(10, 40, lines)
	if got["older"] != 10 || got["start"] != 10 {
		t.Fatalf("non-empty page must publish start/older: %+v", got)
	}
}

func TestMuxHubFansOnlyWatchers(t *testing.T) {
	h := newMuxHub()
	follow := &muxWatch{
		visible:    muxwin.Resolved{Lo: 1, Hi: 0, Following: true},
		sub:        muxwin.Resolved{Lo: 1, Hi: 0, Following: true},
		subscribed: true,
		sent:       map[string]struct{}{},
	}
	a := &muxSession{send: make(chan []byte, 4), transcripts: map[string]*muxWatch{"jevons": follow}}
	b := &muxSession{send: make(chan []byte, 4), transcripts: map[string]*muxWatch{"jevons-po": {
		visible:    muxwin.Resolved{Lo: 1, Hi: 0, Following: true},
		sub:        muxwin.Resolved{Lo: 1, Hi: 0, Following: true},
		subscribed: true,
		sent:       map[string]struct{}{},
	}}}
	h.add(a)
	h.add(b)
	h.fanTranscript("jevons", `{"type":"user","message":{"content":"hi"}}`)
	select {
	case got := <-a.send:
		var env muxEnvelope
		if err := json.Unmarshal(got, &env); err != nil {
			t.Fatal(err)
		}
		if env.T != "frame" || env.Ch != "transcript:jevons" {
			t.Fatalf("a got %+v", env)
		}
		var body struct {
			ID    string `json:"id"`
			Index int    `json:"index"`
			Op    string `json:"op"`
		}
		if err := json.Unmarshal(env.Body, &body); err != nil {
			t.Fatal(err)
		}
		if body.ID != "e:1" || body.Index != 1 || body.Op != "put" {
			t.Fatalf("body=%+v", body)
		}
	default:
		t.Fatal("watcher a got nothing")
	}
	select {
	case <-b.send:
		t.Fatal("po watcher must not get overseer live")
	default:
	}
}

func TestWriteMuxReplayEmptyThenMeta(t *testing.T) {
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	frames := inspectReplay(t, s, "jevons")
	if len(frames) < 1 {
		t.Fatal("legacy inspect still hydrates for old UI")
	}
	// Mux replay of an empty journal is just meta — no conversation_reset.
	buf := &replayBuf{}
	if err := s.writeMuxReplay(t.Context(), buf, "jevons"); err != nil {
		t.Fatal(err)
	}
	if len(buf.frames) == 0 {
		t.Fatal("want meta")
	}
	last := buf.frames[len(buf.frames)-1]
	if last["t"] != "meta" {
		t.Fatalf("last=%v want t=meta", last)
	}
	if last["ch"] != "transcript:jevons" {
		t.Fatalf("ch=%v", last["ch"])
	}
	for _, m := range buf.frames {
		if m["t"] == "frame" && m["type"] == "conversation_reset" {
			t.Fatalf("mux must not send inspect conversation_reset: %v", m)
		}
	}
}

func TestMuxWindowSealedReplayLiveAppendAndBefore(t *testing.T) {
	dir := t.TempDir()
	clog, err := chatlog.Open(filepath.Join(dir, "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clog.Close() })
	for i := 0; i < 110; i++ {
		if err := clog.Append(`{"type":"user","message":{"content":[{"type":"text","text":"pad"}]}}`); err != nil {
			t.Fatal(err)
		}
	}
	lines := []string{
		`{"type":"user","message":{"content":[{"type":"text","text":"u0"}]}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"u1"}]}}`,
		`{"type":"assistant","stream_id":"s1","message":{"content":[{"type":"text","text":"Hel"}]}}`,
		`{"type":"assistant","stream_id":"s1","message":{"content":[{"type":"text","text":"lo"}]}}`,
		`{"type":"assistant","stream_id":"s1","message":{"stop_reason":"end_turn"}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"u2"}]}}`,
		`{"type":"assistant","stream_id":"s2","message":{"content":[{"type":"text","text":"Hi"}]}}`,
	}
	for _, ln := range lines {
		if err := clog.Append(ln); err != nil {
			t.Fatal(err)
		}
	}
	s := New("test", dir)
	s.overseerName = "jevons"
	s.SetChatLog(clog)
	s.mux = newMuxHub()

	sess := &muxSession{send: make(chan []byte, 32), transcripts: map[string]*muxWatch{}}
	s.mux.add(sess)
	buf := &replayBuf{}
	if err := s.writeMuxWindow(t.Context(), buf, sess, "jevons", -2, 0, true); err != nil {
		t.Fatal(err)
	}
	var ids []string
	var lastMeta map[string]any
	for _, m := range buf.frames {
		if m["t"] == "meta" {
			lastMeta, _ = m["body"].(map[string]any)
			continue
		}
		if m["t"] != "frame" {
			continue
		}
		body, _ := m["body"].(map[string]any)
		id, _ := body["id"].(string)
		if id != "" {
			ids = append(ids, id)
		}
		if id == "e:113" {
			ev, _ := body["event"].(map[string]any)
			if ev == nil {
				raw, _ := json.Marshal(body["event"])
				_ = json.Unmarshal(raw, &ev)
			}
			if proseFromEvent(ev) != "Hello" {
				t.Fatalf("e:113 should be sealed Hello, got %q body=%v", proseFromEvent(ev), body["event"])
			}
		}
	}
	if lastMeta == nil || lastMeta["following"] != true {
		t.Fatalf("meta=%v", lastMeta)
	}
	if !containsAll(ids, "e:113", "e:114", "e:115") {
		t.Fatalf("connect delivery %s missing Hello/u2/Hi", stringsJoin(ids))
	}
	if containsAll(ids, "e:1") {
		t.Fatal("halo 100 must not deliver the oldest pad on a 115-event log")
	}
	oldest := ids[0]

	// Live append while following.
	s.mux.fanTranscript("jevons", `{"type":"assistant","stream_id":"s2","message":{"content":[{"type":"text","text":" there"}]}}`)
	select {
	case got := <-sess.send:
		var env muxEnvelope
		if err := json.Unmarshal(got, &env); err != nil {
			t.Fatal(err)
		}
		var body struct {
			ID   string `json:"id"`
			Op   string `json:"op"`
			Text string `json:"text"`
		}
		_ = json.Unmarshal(env.Body, &body)
		if body.ID != "e:115" || body.Op != "append" || body.Text != " there" {
			t.Fatalf("live=%+v raw=%s", body, got)
		}
	default:
		t.Fatal("following session should get live append")
	}

	// Freeze, then a new EOF user must not fan — it is outside [lo, n+1).
	// In-window appends to the frozen range still stream (CQRS until a new window).
	w := sess.ensure("jevons")
	w.visible = muxwin.Freeze(w.visible, 115)
	w.sub = muxwin.Freeze(w.sub, 115)
	s.mux.fanTranscript("jevons", `{"type":"assistant","stream_id":"s2","message":{"content":[{"type":"text","text":"!"}]}}`)
	select {
	case got := <-sess.send:
		if !strings.Contains(string(got), `"op":"append"`) {
			t.Fatalf("frozen window must still stream in-range append: %s", got)
		}
	default:
		t.Fatal("frozen window missed in-range append")
	}
	s.mux.fanTranscript("jevons", `{"type":"user","message":{"content":[{"type":"text","text":"u3"}]}}`)
	select {
	case got := <-sess.send:
		t.Fatalf("frozen session got EOF: %s", got)
	default:
	}

	pageBuf := &replayBuf{}
	s.writeMuxPageBefore(t.Context(), pageBuf, sess, "jevons", oldest, 2)
	var pageIDs []string
	for _, m := range pageBuf.frames {
		if m["t"] != "frame" {
			continue
		}
		body, _ := m["body"].(map[string]any)
		if id, _ := body["id"].(string); id != "" {
			pageIDs = append(pageIDs, id)
		}
	}
	if len(pageIDs) == 0 {
		t.Fatalf("page before %s delivered nothing", oldest)
	}
	if containsAll(pageIDs, oldest) {
		t.Fatalf("page before %s must not re-send the cursor: %v", oldest, pageIDs)
	}
}

func TestMuxPageBeforeKeepsFollowingLiveFan(t *testing.T) {
	dir := t.TempDir()
	clog, err := chatlog.Open(filepath.Join(dir, "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clog.Close() })
	for i := 0; i < 8; i++ {
		if err := clog.Append(`{"type":"user","message":{"content":[{"type":"text","text":"pad"}]}}`); err != nil {
			t.Fatal(err)
		}
	}
	s := New("test", dir)
	s.overseerName = "jevons"
	s.SetChatLog(clog)
	s.mux = newMuxHub()
	sess := &muxSession{send: make(chan []byte, 16), transcripts: map[string]*muxWatch{}}
	s.mux.add(sess)
	buf := &replayBuf{}
	if err := s.writeMuxWindow(t.Context(), buf, sess, "jevons", -3, 0, true); err != nil {
		t.Fatal(err)
	}
	w := sess.ensure("jevons")
	if !w.visible.Following {
		t.Fatal("open must follow")
	}
	var firstID string
	for _, m := range buf.frames {
		if m["t"] != "frame" {
			continue
		}
		body, _ := m["body"].(map[string]any)
		if id, _ := body["id"].(string); id != "" {
			firstID = id
			break
		}
	}
	if firstID == "" {
		t.Fatal("no window frame")
	}
	pageBuf := &replayBuf{}
	s.writeMuxPageBefore(t.Context(), pageBuf, sess, "jevons", firstID, 2)
	if !sess.ensure("jevons").visible.Following {
		t.Fatal("page-older must not freeze a following window")
	}
	var pageMeta map[string]any
	for _, m := range pageBuf.frames {
		if m["t"] == "page" {
			pageMeta, _ = m["body"].(map[string]any)
		}
	}
	if pageMeta == nil || pageMeta["following"] != true {
		t.Fatalf("page meta must keep following: %v", pageMeta)
	}
	s.mux.fanTranscript("jevons", `{"type":"user","message":{"content":[{"type":"text","text":"live-after-page"}]}}`)
	select {
	case got := <-sess.send:
		if !strings.Contains(string(got), "live-after-page") && !strings.Contains(string(got), `"op":"put"`) {
			t.Fatalf("live fan after page: %s", got)
		}
	default:
		t.Fatal("following session got no live user after page-older")
	}
}

func proseFromEvent(ev map[string]any) string {
	if ev == nil {
		return ""
	}
	msg, _ := ev["message"].(map[string]any)
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

func stringsJoin(ids []string) string {
	b, _ := json.Marshal(ids)
	return string(b)
}

func TestMuxCoalescedFirstPaintFoldsTailOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	var b strings.Builder
	b.WriteString(`{"type":"user","message":{"content":[{"type":"text","text":"HEADMARKER"}]}}` + "\n")
	pad := `{"type":"user","message":{"content":[{"type":"text","text":"` + strings.Repeat("p", 160) + `"}]}}` + "\n"
	for b.Len() < 3<<20 {
		b.WriteString(pad)
	}
	b.WriteString(`{"type":"user","message":{"content":[{"type":"text","text":"TAILMARKER"}]}}` + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	clog, err := chatlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clog.Close() })
	s := New("test", dir)
	s.overseerName = "jevons"
	s.SetChatLog(clog)
	s.mux = newMuxHub()

	evs := s.muxCoalesced("jevons", true)
	if len(evs) == 0 {
		t.Fatal("empty fold")
	}
	if len(evs) > 20000 {
		t.Fatalf("first paint folded the whole journal: n=%d", len(evs))
	}
	if !strings.Contains(string(evs[len(evs)-1].Body), "TAILMARKER") {
		t.Fatalf("tail missing: %s", evs[len(evs)-1].Body)
	}
	for _, ev := range evs {
		if strings.Contains(string(ev.Body), "HEADMARKER") {
			t.Fatal("first paint included journal prefix")
		}
	}
}

func containsAll(have []string, want ...string) bool {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func TestMuxWindowMetaTruncatedKeepsOlder(t *testing.T) {
	m := muxWindowMeta(muxwin.Resolved{Lo: 1, Hi: 0, Following: true}, 40, true)
	if older, _ := m["older"].(int); older == 0 {
		t.Fatalf("truncated cache-start must keep older: %+v", m)
	}
	if m["truncated"] != true {
		t.Fatalf("truncated flag: %+v", m)
	}
	m = muxWindowMeta(muxwin.Resolved{Lo: 1, Hi: 0, Following: true}, 40, false)
	if older, _ := m["older"].(int); older != 0 {
		t.Fatalf("real journal start must clear older: %+v", m)
	}
}

func TestMuxPageBeforeDoesNotRenumberSentIndexes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	var b strings.Builder
	b.WriteString(`{"type":"user","message":{"content":[{"type":"text","text":"HEADMARKER"}]}}` + "\n")
	pad := `{"type":"user","message":{"content":[{"type":"text","text":"` + strings.Repeat("p", 120) + `"}]}}` + "\n"
	for b.Len() < 6000 {
		b.WriteString(pad)
	}
	b.WriteString(`{"type":"user","message":{"content":[{"type":"text","text":"TAILMARKER"}]}}` + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	clog, err := chatlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clog.Close() })
	s := New("test", dir)
	s.overseerName = "jevons"
	s.muxTailBytes = 400
	s.SetChatLog(clog)
	s.mux = newMuxHub()

	evs := s.muxCoalesced("jevons", true)
	if len(evs) == 0 {
		t.Fatal("empty first paint")
	}
	for _, ev := range evs {
		if strings.Contains(string(ev.Body), "HEADMARKER") {
			t.Fatal("first paint included journal prefix")
		}
	}
	if !s.muxTruncated("jevons") {
		t.Fatal("tiny tail of a 6k journal must be truncated")
	}
	want := make(map[string]int, len(evs))
	for _, ev := range evs {
		want[ev.ID] = ev.Index
	}

	buf := &replayBuf{}
	if err := s.writeMuxWindow(t.Context(), buf, nil, "jevons", -muxwin.DefaultFollow, 0, false); err != nil {
		t.Fatal(err)
	}
	var oldest string
	for _, m := range buf.frames {
		if m["t"] != "frame" {
			continue
		}
		body, _ := m["body"].(map[string]any)
		if id, _ := body["id"].(string); id != "" && oldest == "" {
			oldest = id
		}
	}
	if oldest == "" {
		t.Fatal("no delivered frame")
	}

	pageBuf := &replayBuf{}
	s.writeMuxPageBefore(t.Context(), pageBuf, nil, "jevons", oldest, 50)
	for _, m := range pageBuf.frames {
		if m["t"] != "frame" {
			continue
		}
		body, _ := m["body"].(map[string]any)
		raw, _ := body["event"].(string)
		if raw == "" {
			if ev, ok := body["event"].(map[string]any); ok {
				b, _ := json.Marshal(ev)
				raw = string(b)
			}
		}
		if strings.Contains(raw, "HEADMARKER") {
			t.Fatal("JSONL fallback must not grow/renumber to reach journal prefix")
		}
		id, _ := body["id"].(string)
		if strings.HasPrefix(id, "e:o:") {
			t.Fatalf("grown id %s is stitchMuxOlder residue", id)
		}
	}

	after := s.mux.eventsFor("jevons")
	for _, ev := range after {
		if idx, ok := want[ev.ID]; ok && ev.Index != idx {
			t.Fatalf("sent id %s reindexed %d → %d", ev.ID, idx, ev.Index)
		}
	}
}

func muxMetaOlder(m map[string]any) int {
	if m == nil {
		return 0
	}
	switch v := m["older"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func TestMuxPageBeforeReadsAbsoluteIndexesFromStateDB(t *testing.T) {
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var rows []statedb.Event
	for i := 1; i <= 80; i++ {
		body := `{"type":"user","message":{"content":[{"type":"text","text":"m` + itoa(i) + `"}]}}`
		if i == 1 {
			body = `{"type":"user","message":{"content":[{"type":"text","text":"HEADMARKER"}]}}`
		}
		if i == 80 {
			body = `{"type":"user","message":{"content":[{"type":"text","text":"TAILMARKER"}]}}`
		}
		rows = append(rows, statedb.Event{
			Index: i, ID: "e:" + itoa(i), Type: "user", Kind: 1, Body: body,
		})
	}
	if err := db.ReplaceAll("jevons", rows); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	s.SetStateDB(db)

	evs := s.muxCoalesced("jevons", true)
	if len(evs) == 0 {
		t.Fatal("empty first paint")
	}
	if evs[0].Index != evs[len(evs)-1].Index-len(evs)+1 {
		t.Fatalf("suffix indexes not absolute: first=%d last=%d n=%d", evs[0].Index, evs[len(evs)-1].Index, len(evs))
	}
	if evs[len(evs)-1].Index != 80 {
		t.Fatalf("last index=%d", evs[len(evs)-1].Index)
	}
	for _, ev := range evs {
		if strings.Contains(string(ev.Body), "HEADMARKER") {
			t.Fatal("first paint included journal prefix")
		}
	}

	buf := &replayBuf{}
	if err := s.writeMuxWindow(t.Context(), buf, nil, "jevons", -muxwin.DefaultFollow, 0, false); err != nil {
		t.Fatal(err)
	}
	var lastMeta map[string]any
	var oldest string
	for _, m := range buf.frames {
		if m["t"] == "frame" {
			body, _ := m["body"].(map[string]any)
			if id, _ := body["id"].(string); id != "" && oldest == "" {
				oldest = id
			}
		}
		if m["t"] == "meta" {
			lastMeta, _ = m["body"].(map[string]any)
		}
	}
	if muxMetaN(lastMeta) != 80 {
		t.Fatalf("meta n want 80 got %+v", lastMeta)
	}
	if muxMetaOlder(lastMeta) == 0 {
		t.Fatalf("older must stay >0 when n>follow: %+v", lastMeta)
	}

	pageBuf := &replayBuf{}
	s.writeMuxPageBefore(t.Context(), pageBuf, nil, "jevons", oldest, 50)
	seenHead := false
	var indexes []int
	for _, m := range pageBuf.frames {
		if m["t"] != "frame" {
			continue
		}
		body, _ := m["body"].(map[string]any)
		raw, _ := body["event"].(string)
		if raw == "" {
			if ev, ok := body["event"].(map[string]any); ok {
				b, _ := json.Marshal(ev)
				raw = string(b)
			}
		}
		if strings.Contains(raw, "HEADMARKER") {
			seenHead = true
		}
		if idx, ok := body["index"].(float64); ok {
			indexes = append(indexes, int(idx))
		}
	}
	if !seenHead {
		t.Fatal("page-up from statedb never reached index 1")
	}
	if len(indexes) == 0 || indexes[0] >= evs[0].Index {
		t.Fatalf("page indexes must be older than first-paint start %d: %v", evs[0].Index, indexes)
	}
}

func TestMuxFirstPaintIsUserTurnsNotEvents(t *testing.T) {
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var rows []statedb.Event
	idx := 1
	for i := 0; i < 40; i++ {
		rows = append(rows,
			statedb.Event{Index: idx, ID: "u:" + itoa(i), Type: "user", Kind: 1, Body: `{"type":"user"}`},
			statedb.Event{Index: idx + 1, ID: "a:" + itoa(i), Type: "assistant", Kind: 2, Body: `{"type":"assistant"}`},
			statedb.Event{Index: idx + 2, ID: "t:" + itoa(i), Type: "tool_use", Kind: 3, Body: `{"type":"tool_use"}`},
		)
		idx += 3
	}
	if err := db.ReplaceAll("jevons", rows); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	s.SetStateDB(db)

	evs := s.muxCoalesced("jevons", true)
	users := 0
	for _, ev := range evs {
		if ev.Type == "user" {
			users++
		}
	}
	if users != muxwin.DefaultFollow {
		t.Fatalf("first paint user turns=%d want %d (events=%d first_idx=%d)", users, muxwin.DefaultFollow, len(evs), evs[0].Index)
	}
	if evs[0].Index == evs[len(evs)-1].Index-muxwin.DefaultFollow+1 {
		t.Fatal("first paint is still last-N events, not last-N user turns")
	}

	buf := &replayBuf{}
	if err := s.writeMuxWindow(t.Context(), buf, nil, "jevons", -muxwin.DefaultFollow, 0, false); err != nil {
		t.Fatal(err)
	}
	var sentUsers int
	for _, m := range buf.frames {
		if m["t"] != "frame" {
			continue
		}
		body, _ := m["body"].(map[string]any)
		if typ, _ := body["type"].(string); typ == "user" {
			sentUsers++
		}
	}
	if sentUsers != muxwin.DefaultFollow {
		t.Fatalf("wired first paint user turns=%d want %d", sentUsers, muxwin.DefaultFollow)
	}
}

func TestImportJSONLOnceThenPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chatlog", "jevons.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","message":{"content":[{"type":"text","text":"one"}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"text","text":"two"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	clog, err := chatlog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clog.Close() })
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s := New("test", dir)
	s.overseerName = "jevons"
	s.SetChatLog(clog)
	s.SetStateDB(db)
	s.ImportTranscripts()
	n, err := db.N("jevons")
	if err != nil || n != 2 {
		t.Fatalf("import n=%d err=%v", n, err)
	}
	s.ImportTranscripts()
	n, _ = db.N("jevons")
	if n != 2 {
		t.Fatalf("second import re-folded: n=%d", n)
	}
	s.persistChatLine(`{"type":"user","message":{"content":[{"type":"text","text":"three"}]}}`)
	n, _ = db.N("jevons")
	if n != 3 {
		t.Fatalf("persist n=%d", n)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "\n") != 2 {
		t.Fatalf("dual-wrote JSONL after import: %q", raw)
	}
	if err := os.WriteFile(path, append(raw, []byte(`{"type":"user","message":{"content":[{"type":"text","text":"ghost"}]}}`+"\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	evs := s.muxCoalesced("jevons", true)
	for _, ev := range evs {
		if strings.Contains(string(ev.Body), "ghost") {
			t.Fatal("mux read JSONL after import")
		}
	}
	if last := evs[len(evs)-1]; !strings.Contains(string(last.Body), "three") || last.Index != 3 {
		t.Fatalf("persist row=%+v", last)
	}
	older := s.statedbBefore("jevons", 3, 50)
	if len(older) != 2 || older[0].Index != 1 || older[1].Index != 2 {
		t.Fatalf("page by index=%+v", older)
	}
}

func TestMuxFanLiveUserOnAbsoluteSuffix(t *testing.T) {
	h := newMuxHub()
	evs := make([]muxwin.Event, 0, 30)
	for i := 51; i <= 80; i++ {
		evs = append(evs, muxwin.Event{ID: "e:" + itoa(i), Index: i, Type: "user", Kind: muxwin.KindUser})
	}
	h.replaceCacheN("jevons", evs, 0, true, 80)
	sess := &muxSession{
		send: make(chan []byte, 8),
		transcripts: map[string]*muxWatch{"jevons": {
			subscribed: true,
			visible:    muxwin.Resolved{Lo: 51, Hi: 0, Following: true},
			sub:        muxwin.Resolved{Lo: 51, Hi: 0, Following: true},
			sent:       map[string]struct{}{},
		}},
	}
	h.add(sess)
	folds, _ := h.applyLine("jevons", `{"type":"user","message":{"content":[{"type":"text","text":"ping-send-check"}]}}`)
	if len(folds) != 1 || folds[0].Event.Index != 81 {
		idx := 0
		if len(folds) > 0 {
			idx = folds[0].Event.Index
		}
		t.Fatalf("live index=%d folds=%d want 81 (cache-relative mint is dropped by [51,0))", idx, len(folds))
	}
	h.mu.Lock()
	h.fanFoldsLocked("jevons", folds)
	h.mu.Unlock()
	select {
	case payload := <-sess.send:
		if !strings.Contains(string(payload), "ping-send-check") {
			t.Fatalf("payload %s", payload)
		}
	default:
		t.Fatal("following watcher missed live user echo on statedb suffix")
	}
}

func muxMetaN(m map[string]any) int {
	if m == nil {
		return 0
	}
	switch v := m["n"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}
