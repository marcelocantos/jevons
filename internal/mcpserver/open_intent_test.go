// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 🎯T328: open owner intent recovery after control-plane bounce.

func TestExtractOpenOwnerIntentRecoversInstructionAfterRestartNudges(t *testing.T) {
	t.Parallel()
	// Mirrors the live incident: substantive work order, then bounce re-nudges.
	turns := []OwnerIntentTurn{
		{Text: "Let's explore leaders and visionaries.", Source: "owner_chat"},
		{Text: "I'm especially interested in Elon Musk and his documented approaches.", Source: "owner_chat"},
		{Text: "Both of your last two responses were brilliant. Let's grab whatever you think makes sense and shove it into the document.", Source: "owner_chat", TS: time.Date(2026, 8, 8, 10, 0, 33, 0, time.UTC)},
		{Text: "service restarted. Continue", Source: "owner_chat"},
		{Text: "I waited a while for you to keep going after the service restarted, but it doesn't seem like that happens", Source: "owner_chat"},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("want recoverable intent, residual=%q", got.Residual)
	}
	if !strings.Contains(got.Text, "shove it into the document") {
		t.Fatalf("want open work order, got %q", got.Text)
	}
	if strings.Contains(strings.ToLower(got.Text), "service restarted") {
		t.Fatalf("must skip restart re-nudge, got %q", got.Text)
	}
}

func TestExtractOpenOwnerIntentResiduals(t *testing.T) {
	t.Parallel()
	if r := ExtractOpenOwnerIntent(nil); r.Residual != ResidualNoUserTurns {
		t.Fatalf("empty: residual=%q", r.Residual)
	}
	if r := ExtractOpenOwnerIntent([]OwnerIntentTurn{
		{Text: "[Daemon restart 12:00] reattached"},
		{Text: "[event: daemon-restarted] fleet status"},
	}); r.Residual != ResidualOnlyHarness {
		t.Fatalf("harness: residual=%q", r.Residual)
	}
	if r := ExtractOpenOwnerIntent([]OwnerIntentTurn{
		{Text: "ok"},
		{Text: "thanks"},
		{Text: "continue"},
	}); r.Residual != ResidualAckOnly && r.Residual != ResidualNoRecoverableIntent {
		// "continue" is restart-nudge (skipped); ok/thanks are ack_only.
		t.Fatalf("acks: residual=%q", r.Residual)
	}
	// Short ack residual specifically.
	if r := ExtractOpenOwnerIntent([]OwnerIntentTurn{{Text: "thanks"}}); r.Residual != ResidualAckOnly {
		t.Fatalf("ack_only: residual=%q", r.Residual)
	}
}

func TestExtractOpenOwnerIntentKeepsShortWorkOrder(t *testing.T) {
	t.Parallel()
	got := ExtractOpenOwnerIntent([]OwnerIntentTurn{
		{Text: "Please fix the chat wire bug"},
	})
	if !got.Recoverable() {
		t.Fatalf("short work order must recover, residual=%q", got.Residual)
	}
	if !strings.Contains(got.Text, "chat wire") {
		t.Fatalf("got %q", got.Text)
	}
}

func TestFormatOverseerOpenIntentResumeNotSilentIdleOnly(t *testing.T) {
	t.Parallel()
	intent := OpenOwnerIntent{
		Text:   "Shove the Musk Algorithm into life-and-work-org-map.md",
		Source: "owner_chat",
		TS:     time.Date(2026, 8, 8, 10, 0, 0, 0, time.UTC),
	}
	workers := []WorkerIdleRef{
		{Name: "jv-t328", TargetID: "T328", Status: "running", Phase: "idle"},
	}
	text := FormatOverseerOpenIntentResume(intent, "jevons", workers)
	if !strings.Contains(text, "Shove the Musk Algorithm") {
		t.Fatal("must carry open instruction:", text)
	}
	if !strings.Contains(text, "Do NOT reply with only [silent]") {
		t.Fatal("must forbid silent-idle-only status dump:", text)
	}
	// Must not teach silent as the default RESPONSE RULE (T171 status path does).
	if strings.Contains(text, "your entire reply MUST start with exactly [silent]") {
		t.Fatal("open-intent resume must not mandate [silent] default:", text)
	}
	if !strings.Contains(text, "Continue the open instruction") &&
		!strings.Contains(text, "resume after jevonsd restart") {
		t.Fatal("must force continue language:", text)
	}
	if !strings.Contains(text, "jv-t328") {
		t.Fatal("fleet context should list workers:", text)
	}
	// Event wire shape for probes.
	wire := formatIdleNudgeWire(eventOwnerIntentResume, text)
	if !strings.HasPrefix(wire, "[event: owner-intent-resume]") {
		t.Fatalf("wire prefix: %q", wire)
	}
	if !strings.Contains(wire, "Shove the Musk") {
		t.Fatal("wire must not strip instruction")
	}
}

func TestLoadOpenOwnerIntentFromChatlog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Empty state → no_user_turns or no_chatlog.
	empty := LoadOpenOwnerIntent(dir, "jevons")
	if empty.Recoverable() {
		t.Fatalf("empty state should not recover: %+v", empty)
	}
	if empty.Residual != ResidualNoChatlog && empty.Residual != ResidualNoUserTurns {
		t.Fatalf("empty residual=%q", empty.Residual)
	}

	// Write a minimal chatlog JSONL with user turns (rsi.LoadChatLogTurns shape).
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chatDir, "jevons.jsonl")
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-08-08T10:00:00Z","message":{"role":"user","content":"Please shove the leader research into life-and-work-org-map.md now."}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}`,
		`{"type":"user","timestamp":"2026-08-08T10:02:00Z","message":{"role":"user","content":"service restarted. Continue"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(dir, "jevons")
	if !got.Recoverable() {
		t.Fatalf("want recoverable from chatlog, residual=%q", got.Residual)
	}
	if !strings.Contains(got.Text, "life-and-work-org-map") {
		t.Fatalf("got %q", got.Text)
	}
}

func TestLoadOpenOwnerIntentNoStateDir(t *testing.T) {
	t.Parallel()
	got := LoadOpenOwnerIntent("", "jevons")
	if got.Residual != ResidualNoChatlog {
		t.Fatalf("residual=%q", got.Residual)
	}
}

// Hermetic integration: NotifyDaemonRestarted overseer path uses owner-intent-resume.
func TestNotifyDaemonRestartedOverseerOpenIntentEvent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chatDir, "jevons.jsonl")
	body := `{"type":"user","timestamp":"2026-08-08T10:00:00Z","message":{"role":"user","content":"Both responses were great. Grab what makes sense and shove it into the document."}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// Minimal server with registry so NotifyDaemonRestarted runs the loop.
	// sendToAgent will fail without a real process — we only need the pure
	// intent + format path covered hermetically above. This test asserts
	// Load + Format composition that NotifyDaemonRestarted uses.
	intent := LoadOpenOwnerIntent(dir, "jevons")
	if !intent.Recoverable() {
		t.Fatalf("setup failed residual=%q", intent.Residual)
	}
	text := FormatOverseerOpenIntentResume(intent, "jevons", nil)
	if strings.Contains(text, "your entire reply MUST start with exactly [silent]") {
		t.Fatal("composed overseer resume must not be silent-idle-only")
	}
	if !strings.Contains(text, "shove it into the document") {
		t.Fatal(text)
	}
}

func TestIsOpenIntentRestartNudge(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"service restarted. Continue", true},
		{"continue", true},
		{"keep going", true},
		{"I waited a while for you to keep going after the service restarted", true},
		{"Is this a gap in the restart sequence?", true},
		{"Shove Musk Algorithm into the org map", false},
		{"Please fix chat", false},
	}
	for _, c := range cases {
		if got := isOpenIntentRestartNudge(c.in); got != c.want {
			t.Errorf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}

// 🎯T344: answered owner instructions must not re-fire as open intent.

func TestExtractOpenOwnerIntentAnsweredOrClosedJiggleEvidence(t *testing.T) {
	t.Parallel()
	// Live incident shape: owner jiggle question, later overseer evidence for T341.
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Why is all the text on the page jiggling up and down by a pixel or so? It's doing it constantly. What's re-rendering all the time? Why is it affecting the layout in such a subtle way?",
			TS:   time.Date(2026, 8, 8, 15, 43, 49, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "Independent overseer gate: SHA cf4bdce fix(web): stop continuous ~1px main-chat jiggle from pin thrash (T341). Product: shouldPinScroll sub-threshold gate. Hermetic PASS (maxTopDelta=0). Achieved 🎯T341.",
			TS:   time.Date(2026, 8, 8, 15, 55, 38, 0, time.UTC),
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if got.Recoverable() {
		t.Fatalf("answered jiggle must not recover open work, got text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want residual %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

func TestExtractOpenOwnerIntentStillRecoversOpenImplement(t *testing.T) {
	t.Parallel()
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Please implement open intent answered disposition for restart resume",
		},
		{
			Role: "assistant",
			Text: "working on it — looking at open_intent.go",
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("open implement without product evidence must recover, residual=%q", got.Residual)
	}
	if !strings.Contains(got.Text, "answered disposition") {
		t.Fatalf("got %q", got.Text)
	}
}

func TestExtractOpenOwnerIntentReAskAfterFixStillRecovers(t *testing.T) {
	t.Parallel()
	// Residual: brand-new owner re-ask after fix — newest turn is the re-ask.
	turns := []OwnerIntentTurn{
		{
			Role: "user",
			Text: "Why is all the text on the page jiggling up and down by a pixel?",
			TS:   time.Date(2026, 8, 8, 15, 0, 0, 0, time.UTC),
		},
		{
			Role: "assistant",
			Text: "SHA cf4bdce fix(web): jiggle from pin thrash (T341). Hermetic PASS. Achieved 🎯T341.",
			TS:   time.Date(2026, 8, 8, 15, 30, 0, 0, time.UTC),
		},
		{
			Role: "user",
			Text: "The text is still jiggling after hard-reload — please re-check the pin thrash fix",
			TS:   time.Date(2026, 8, 8, 16, 0, 0, 0, time.UTC),
		},
	}
	got := ExtractOpenOwnerIntent(turns)
	if !got.Recoverable() {
		t.Fatalf("fresh re-ask must recover, residual=%q", got.Residual)
	}
	if !strings.Contains(strings.ToLower(got.Text), "still jiggling") {
		t.Fatalf("want re-ask text, got %q", got.Text)
	}
}

func TestLoadOpenOwnerIntentAnsweredFromChatlogToolUse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(chatDir, "jevons.jsonl")
	// User jiggle + assistant tool_use achieve attestation (live chatlog shape).
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-08-08T15:43:49Z","message":{"role":"user","content":"Why is all the text on the page jiggling up and down by a pixel or so? It's doing it constantly. What's re-rendering all the time?"}}`,
		`{"type":"assistant","timestamp":"2026-08-08T15:55:38Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"use_tool","input":{"tool_name":"bullseye__bullseye_commit","tool_input":{"op":"achieve","id":"T341","attestation":"Independent overseer gate: SHA cf4bdce fix(web): stop continuous ~1px main-chat jiggle from pin thrash (T341). Hermetic PASS."}}}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(dir, "jevons")
	if got.Recoverable() {
		t.Fatalf("tool_use achieve evidence must close jiggle, got text=%q residual=%q", got.Text, got.Residual)
	}
	if got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("want %q, got %q", ResidualAnsweredOrClosed, got.Residual)
	}
}

func TestOwnerIntentAnsweredWithProductEvidence(t *testing.T) {
	t.Parallel()
	owner := "Why is all the text jiggling by a pixel from layout thrash?"
	if !OwnerIntentAnsweredWithProductEvidence(owner, []string{
		"SHA cf4bdce fix(web): stop jiggle pin thrash (T341). Hermetic PASS. Achieved.",
	}) {
		t.Fatal("expected answered with product evidence")
	}
	if OwnerIntentAnsweredWithProductEvidence(owner, []string{"looking into it"}) {
		t.Fatal("working chatter is not product evidence")
	}
	if OwnerIntentAnsweredWithProductEvidence(
		"Please implement the billing export CSV",
		[]string{"SHA cf4bdce fix(web): stop jiggle pin thrash (T341). Hermetic PASS. Achieved."},
	) {
		t.Fatal("unrelated evidence must not close a different complaint")
	}
}

// 🎯T362: client protocol control frames that leaked into the owner chatlog
// (reportComposerState sent ux_state on /ws/chat before the server filtered
// it) must never be recovered as the open owner instruction. Before this, every
// restart re-fired {"type":"ux_state","composer_blocked":false} as the mission.
func TestExtractOpenOwnerIntentIgnoresProtocolJSONTurns(t *testing.T) {
	t.Parallel()

	// A chatlog of nothing but leaked frames has no owner work in it.
	only := ExtractOpenOwnerIntent([]OwnerIntentTurn{
		{Text: `{"type":"ux_state","composer_blocked":false}`},
		{Text: `{"type":"ux_state","composer_blocked":true,"reason":"overseer_down"}`},
		{Text: `{"type":"ping"}`},
	})
	if only.Recoverable() {
		t.Fatalf("protocol frames must not be recoverable owner intent: %+v", only)
	}
	if only.Residual != ResidualOnlyHarness {
		t.Fatalf("residual=%q want %q", only.Residual, ResidualOnlyHarness)
	}

	// A leaked frame arriving after real owner work must not displace it.
	got := ExtractOpenOwnerIntent([]OwnerIntentTurn{
		{Text: "Shove the leader research into life-and-work-org-map.md now."},
		{Text: `{"type":"ux_state","composer_blocked":false}`},
	})
	if !got.Recoverable() {
		t.Fatalf("owner instruction lost behind a protocol frame: %+v", got)
	}
	if !strings.Contains(got.Text, "life-and-work-org-map") {
		t.Fatalf("got %q", got.Text)
	}

	// Owner prose that merely mentions a frame is still owner prose.
	prose := ExtractOpenOwnerIntent([]OwnerIntentTurn{
		{Text: `Please stop {"type":"ux_state","composer_blocked":false} appearing as my chat turns.`},
	})
	if !prose.Recoverable() {
		t.Fatalf("owner prose quoting a frame must stay recoverable: %+v", prose)
	}
}

func TestIsOpenIntentProtocolJSON(t *testing.T) {
	t.Parallel()
	frames := []string{
		`{"type":"ux_state","composer_blocked":false}`,
		`  {"type":"ux_state","composer_blocked":true,"reason":"overseer_down"}  `,
		`{"type":"ping"}`,
		`{"type":"interrupt"}`,
		`{"turns":2,"type":"rewind"}`,
		`{"name":"jv-x","type":"inspect_subscribe"}`,
	}
	for _, f := range frames {
		if !isOpenIntentProtocolJSON(f) {
			t.Errorf("want protocol frame: %s", f)
		}
	}
	prose := []string{
		"",
		"Fix the ux_state leak please.",
		`Look at {"type":"ux_state"} in the log and fix it.`,
		`{"composer_blocked":false}`,
		`{"type":""}`,
		`{"type":42}`,
		`["type","ux_state"]`,
		`{"type":"ux_state"`,
	}
	for _, p := range prose {
		if isOpenIntentProtocolJSON(p) {
			t.Errorf("want NOT a protocol frame: %q", p)
		}
	}
}

// End-to-end through the durable chatlog reader: a journal whose only user
// turns are leaked frames yields a residual, not a resume payload.
func TestLoadOpenOwnerIntentIgnoresLeakedUXStateChatlog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chatlog")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		`{"type":"user","timestamp":"2026-08-09T10:00:00Z","message":{"role":"user","content":"{\"type\":\"ux_state\",\"composer_blocked\":false}"}}`,
		`{"type":"user","timestamp":"2026-08-09T10:01:00Z","message":{"role":"user","content":"{\"type\":\"ux_state\",\"composer_blocked\":true,\"reason\":\"overseer_down\"}"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(chatDir, "jevons.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoadOpenOwnerIntent(dir, "jevons")
	if got.Recoverable() {
		t.Fatalf("leaked ux_state chatlog must not resume: %+v", got)
	}
	if got.Residual == "" {
		t.Fatal("want a named residual for a frames-only chatlog")
	}
}
