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

func TestStitchMuxOlderPreservesSuffixIDs(t *testing.T) {
	prev := []muxwin.Event{
		{ID: "e:1", Index: 1, Type: "user", Body: json.RawMessage(`{"t":1}`)},
		{ID: "e:2", Index: 2, Type: "user", Body: json.RawMessage(`{"t":2}`)},
	}
	full := []muxwin.Event{
		{ID: "e:1", Index: 1, Type: "user", Body: json.RawMessage(`{"t":0}`)},
		{ID: "e:2", Index: 2, Type: "user", Body: json.RawMessage(`{"t":1}`)},
		{ID: "e:3", Index: 3, Type: "user", Body: json.RawMessage(`{"t":2}`)},
	}
	got := stitchMuxOlder(prev, full)
	if len(got) != 3 {
		t.Fatalf("n=%d", len(got))
	}
	if got[1].ID != "e:1" || got[2].ID != "e:2" {
		t.Fatalf("suffix ids must stay e:1/e:2, got %s %s", got[1].ID, got[2].ID)
	}
	if got[0].ID == "e:1" {
		t.Fatal("new older event must not reuse e:1")
	}
	if got[0].Index != 1 || got[1].Index != 2 || got[2].Index != 3 {
		t.Fatalf("indices=%d,%d,%d", got[0].Index, got[1].Index, got[2].Index)
	}
}

func TestMuxPageBeforeGrowsTruncatedTail(t *testing.T) {
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

	buf := &replayBuf{}
	if err := s.writeMuxWindow(t.Context(), buf, nil, "jevons", -muxwin.DefaultFollow, 0, false); err != nil {
		t.Fatal(err)
	}
	var oldest string
	var lastMeta map[string]any
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
	if oldest == "" {
		t.Fatal("no delivered frame")
	}
	if muxMetaOlder(lastMeta) == 0 {
		t.Fatalf("truncated journal must keep older>0 at cache start: %+v", lastMeta)
	}

	seenHead := false
	before := oldest
	for i := 0; i < 16 && !seenHead; i++ {
		pageBuf := &replayBuf{}
		s.writeMuxPageBefore(t.Context(), pageBuf, nil, "jevons", before, 50)
		var pageOlder int
		var firstNew string
		for _, m := range pageBuf.frames {
			if m["t"] == "frame" {
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
				if id, _ := body["id"].(string); id != "" && firstNew == "" {
					firstNew = id
				}
			}
			if m["t"] == "page" {
				body, _ := m["body"].(map[string]any)
				pageOlder = muxMetaOlder(body)
			}
		}
		if firstNew != "" {
			before = firstNew
		} else if pageOlder == 0 {
			break
		}
	}
	if !seenHead {
		t.Fatal("page-up never reached the journal prefix past the first-paint tail")
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
