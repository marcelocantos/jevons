// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T387 — the spawn path must confirm an opening brief from evidence about
// the AGENT, not from the return value of the send call.
//
// RED against the pre-fix tree: before turn_evidence.go, deliverStartPrompt
// consulted only ConfirmSendBeganTurn(res.Status, err), and res.Status is
// "sent" whenever proc.Send() returned nil. Every "turn never began" case
// below therefore passed, which is exactly how jv-t375 and jv-t387 reported
// prompt_delivered=true while running nothing at all.

// ── fakes ───────────────────────────────────────────────────────────────

// fakeObserver stands in for a live claudia agent. It reports what the AGENT
// did (transcript, events, liveness) and knows nothing about sends.
type fakeObserver struct {
	mu        sync.Mutex
	jsonlPath string
	alive     bool
	subs      map[int64]claudia.EventFunc
	nextTok   int64
}

func newFakeObserver(jsonlPath string) *fakeObserver {
	return &fakeObserver{jsonlPath: jsonlPath, alive: true, subs: map[int64]claudia.EventFunc{}}
}

func (f *fakeObserver) SubscribeEvents(fn claudia.EventFunc) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextTok++
	f.subs[f.nextTok] = fn
	return f.nextTok
}

func (f *fakeObserver) UnsubscribeEvents(tok int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.subs, tok)
}

func (f *fakeObserver) JSONLPath() string { return f.jsonlPath }

func (f *fakeObserver) Alive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive
}

func (f *fakeObserver) die() {
	f.mu.Lock()
	f.alive = false
	f.mu.Unlock()
}

func (f *fakeObserver) publish(ev claudia.Event) {
	f.mu.Lock()
	subs := make([]claudia.EventFunc, 0, len(f.subs))
	for _, fn := range f.subs {
		subs = append(subs, fn)
	}
	f.mu.Unlock()
	for _, fn := range subs {
		fn(ev)
	}
}

// witnessYielding builds a turnWitness that always reports ev.
func witnessYielding(ev TurnEvidence) turnWitness {
	return func(string) turnWatch {
		return func() TurnEvidence { return ev }
	}
}

const (
	shortWindow = 150 * time.Millisecond
	settleWait  = 400 * time.Millisecond
)

// ── the pure predicate ──────────────────────────────────────────────────

// A successful send call, on its own, is not evidence of anything.
func TestConfirmTurnBeganRejectsSendSuccessAlone(t *testing.T) {
	t.Parallel()

	// The precise pre-fix belief: status "sent", no send error, nothing
	// observed of the agent. ConfirmSendBeganTurn is satisfied by this.
	if err := ConfirmSendBeganTurn("sent", nil); err != nil {
		t.Fatalf("send-call gate should still pass on a clean send: %v", err)
	}
	err := ConfirmTurnBegan("sent", nil, TurnEvidence{Detail: "no transcript was ever created"})
	if err == nil {
		t.Fatal("a send that returned nil must not confirm a begun turn")
	}
	if !strings.Contains(err.Error(), "turn not begun") {
		t.Fatalf("error must say the turn did not begin: %v", err)
	}
	// The operator has to be able to tell WHY, or this is just another
	// opaque failure to paper over.
	if !strings.Contains(err.Error(), "no transcript was ever created") {
		t.Fatalf("error must carry the observation detail: %v", err)
	}
}

// Either observation confirms; a failed or merely queued send never does,
// even when evidence is somehow present.
func TestConfirmTurnBeganMatrix(t *testing.T) {
	t.Parallel()

	positive := []struct {
		name string
		ev   TurnEvidence
	}{
		{"transcript grew", TurnEvidence{ConversationGrew: true}},
		{"session event", TurnEvidence{SessionEvent: true}},
		{"both", TurnEvidence{ConversationGrew: true, SessionEvent: true}},
	}
	for _, tc := range positive {
		for _, status := range []string{"sent", "rehydrated_sent", "interrupted_sent"} {
			if err := ConfirmTurnBegan(status, nil, tc.ev); err != nil {
				t.Fatalf("%s/%s: healthy spawn must confirm: %v", tc.name, status, err)
			}
		}
	}

	// The send gate stays NECESSARY: evidence cannot rescue a send that
	// never submitted anything.
	if err := ConfirmTurnBegan("queued", nil, TurnEvidence{SessionEvent: true}); err == nil {
		t.Fatal("queued send must not confirm even with evidence")
	}
	if err := ConfirmTurnBegan("sent", fmt.Errorf("tmux gone"), TurnEvidence{SessionEvent: true}); err == nil {
		t.Fatal("send error must surface even with evidence")
	}
	if err := ConfirmTurnBegan("", nil, TurnEvidence{SessionEvent: true}); err == nil {
		t.Fatal("empty status must not confirm")
	}
}

// ── the observation itself ──────────────────────────────────────────────

// Durable-transcript backend (Claude-shaped): growth past the pre-send
// baseline is the evidence.
func TestObserveTurnDurableTranscriptGrowth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	// A resumed agent starts with history already on disk; the baseline is
	// what makes "grew" mean this turn rather than last week's.
	if err := os.WriteFile(path, []byte(`{"type":"user","text":"yesterday"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	obs := newFakeObserver(path)

	watch := observeTurn(obs, 5*time.Second)
	go func() {
		time.Sleep(20 * time.Millisecond)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		_, _ = f.WriteString(`{"type":"user","text":"the opening brief"}` + "\n")
	}()

	ev := watch()
	if !ev.ConversationGrew {
		t.Fatalf("appended transcript must confirm: %+v", ev)
	}
	if !ConfirmTurnBeganOK(t, ev) {
		t.Fatal("healthy spawn must pass the full predicate")
	}
}

// The failure that actually happened: the transcript is never created.
func TestObserveTurnNoTranscriptEverCreated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "never-born.jsonl")
	obs := newFakeObserver(path)

	ev := observeTurn(obs, shortWindow)()
	if ev.Positive() {
		t.Fatalf("absent transcript must not confirm: %+v", ev)
	}
	if !strings.Contains(ev.Detail, "was ever created") || !strings.Contains(ev.Detail, path) {
		t.Fatalf("detail must name the missing transcript: %q", ev.Detail)
	}
}

// OVER-BROADNESS GUARD, and the reason events are not evidence on a
// durable-transcript backend: claudia's tailJSONL republishes an existing
// transcript from byte zero on attach, so a resumed agent emits a flood of
// historical events. Accepting those would confirm this turn from last
// week's conversation — the same class of lie as trusting the send call.
func TestObserveTurnIgnoresReplayedHistoryOnDurableBackend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"user","text":"history"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	obs := newFakeObserver(path)

	watch := observeTurn(obs, shortWindow)
	// Replay: many events, zero growth.
	go func() {
		for i := 0; i < 25; i++ {
			obs.publish(claudia.Event{Type: "assistant", Text: "replayed history"})
			time.Sleep(2 * time.Millisecond)
		}
	}()

	ev := watch()
	if ev.Positive() {
		t.Fatalf("replayed history must not confirm a new turn: %+v", ev)
	}
	if !strings.Contains(ev.Detail, "still") {
		t.Fatalf("detail should report an unmoved transcript: %q", ev.Detail)
	}
}

// Live-stream backend (Grok over ACP): claudia leaves JSONLPath empty and
// does not tail, so there is no replay and a published event IS the evidence.
func TestObserveTurnLiveStreamBackendUsesEvents(t *testing.T) {
	t.Parallel()
	obs := newFakeObserver("") // no durable transcript

	watch := observeTurn(obs, 5*time.Second)
	go func() {
		time.Sleep(20 * time.Millisecond)
		obs.publish(claudia.Event{Type: "assistant", Text: "starting work"})
	}()

	ev := watch()
	if !ev.SessionEvent {
		t.Fatalf("live stream event must confirm: %+v", ev)
	}
}

func TestObserveTurnLiveStreamSilenceIsFailure(t *testing.T) {
	t.Parallel()
	obs := newFakeObserver("")
	ev := observeTurn(obs, shortWindow)()
	if ev.Positive() {
		t.Fatalf("silent ACP stream must not confirm: %+v", ev)
	}
	if !strings.Contains(ev.Detail, "no session event") {
		t.Fatalf("detail must say no event arrived: %q", ev.Detail)
	}
}

// A process that exits fails immediately rather than burning the window.
func TestObserveTurnDeadProcessFailsFast(t *testing.T) {
	t.Parallel()
	obs := newFakeObserver("")
	obs.die()
	start := time.Now()
	ev := observeTurn(obs, 30*time.Second)()
	if ev.Positive() {
		t.Fatalf("dead process must not confirm: %+v", ev)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("dead process should short-circuit, took %s", elapsed)
	}
	if !strings.Contains(ev.Detail, "exited") {
		t.Fatalf("detail must name the exit: %q", ev.Detail)
	}
}

// No observable process at all is failure, not a free pass. This is the
// direction of the whole target: absence of evidence is not confirmation.
func TestObserveTurnUnobservableIsFailure(t *testing.T) {
	t.Parallel()
	ev := observeTurn(nil, shortWindow)()
	if ev.Positive() {
		t.Fatalf("unobservable agent must not confirm: %+v", ev)
	}
}

// ── the spawn path, both directions (acceptance 4) ──────────────────────

// startPromptFixture builds a registered agent plus the send seam, so the
// only variable under test is what the witness observed.
func startPromptFixture(t *testing.T, name string, provider claudia.Provider) (*Server, *claudia.Registry, *fakeSender, string) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "sess-" + name,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Provider: provider, TargetID: "T387",
	}); err != nil {
		t.Fatal(err)
	}
	fs := &fakeSender{alive: true}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetSenderResolver(func(string) (agentSender, bool, error) { return fs, false, nil })
	return s, reg, fs, dir
}

// Direction 1 (acceptance 1 + 2, healthy): a spawn whose agent shows a sign
// of life succeeds and is marked running. This is the over-broadness guard on
// the whole change — a confirmation nothing can satisfy strands the fleet.
func TestDeliverStartPromptConfirmedTurnSucceeds(t *testing.T) {
	t.Parallel()
	s, _, fs, _ := startPromptFixture(t, "jv-t387-ok", claudia.ProviderClaude)
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		ConversationGrew: true, Detail: "transcript grew 0→812 bytes",
	}))

	if err := s.deliverStartPrompt("jv-t387-ok", "Execute 🎯T387."); err != nil {
		t.Fatalf("healthy spawn must succeed: %v", err)
	}
	if len(fs.sent) != 1 {
		t.Fatalf("sends=%d want 1", len(fs.sent))
	}
	if !strings.Contains(fs.sent[0], "Jevons fleet standing brief") {
		t.Fatal("opening brief must still carry the standing brief")
	}
	if !s.agentHasTurnBegan("jv-t387-ok") {
		t.Fatal("confirmed turn must mark the agent running")
	}
	if got := ClassifyAgentListStatus(true, s.agentHasTurnBegan("jv-t387-ok"), false); got != AgentStatusRunning {
		t.Fatalf("agent_list status: %s want running", got)
	}
}

// Direction 2 (acceptance 2 + 4): the send call succeeds, the agent does
// nothing, and the spawn REPORTS THAT rather than reporting success.
//
// This is jv-t375 / jv-t387 exactly. Pre-fix this test fails: the send
// returned nil, so deliverStartPrompt returned nil and the caller stamped
// prompt_delivered=true.
func TestDeliverStartPromptUnbegunTurnIsAnError(t *testing.T) {
	t.Parallel()
	s, _, fs, _ := startPromptFixture(t, "jv-t387-dead", claudia.ProviderClaude)
	s.SetTurnWitness(witnessYielding(TurnEvidence{
		Detail: "no transcript was ever created at /x/sess.jsonl within 45s",
	}))

	err := s.deliverStartPrompt("jv-t387-dead", "Execute 🎯T387.")
	if err == nil {
		t.Fatal("a spawn whose turn never began must not report success")
	}
	// The send genuinely happened — that is the whole trap.
	if len(fs.sent) != 1 {
		t.Fatalf("the send itself should have gone out: sends=%d", len(fs.sent))
	}
	if fs.sendErr != nil {
		t.Fatal("fixture must have a CLEAN send; the point is that it lied")
	}
	for _, want := range []string{"start prompt not delivered", "jv-t387-dead", "turn not begun", "no transcript was ever created"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must contain %q: %v", want, err)
		}
	}
	if s.agentHasTurnBegan("jv-t387-dead") {
		t.Fatal("unconfirmed turn must not be marked running")
	}
	if got := ClassifyAgentListStatus(true, s.agentHasTurnBegan("jv-t387-dead"), false); got != AgentStatusNeverBriefed {
		t.Fatalf("agent_list status: %s want never_briefed", got)
	}
}

// Acceptance 3: the rule is provider-agnostic, because the fleet is mixed.
func TestDeliverStartPromptAcrossProviders(t *testing.T) {
	t.Parallel()
	for _, provider := range []claudia.Provider{claudia.ProviderGrok, claudia.ProviderClaude, claudia.ProviderCodex} {
		t.Run(string(provider), func(t *testing.T) {
			t.Parallel()
			name := "jv-t387-" + string(provider)

			sOK, _, _, _ := startPromptFixture(t, name+"-ok", provider)
			sOK.SetTurnWitness(witnessYielding(TurnEvidence{SessionEvent: true}))
			if err := sOK.deliverStartPrompt(name+"-ok", "brief"); err != nil {
				t.Fatalf("%s: confirmed turn must succeed: %v", provider, err)
			}

			sBad, _, _, _ := startPromptFixture(t, name+"-bad", provider)
			sBad.SetTurnWitness(witnessYielding(TurnEvidence{Detail: "nothing observed"}))
			if err := sBad.deliverStartPrompt(name+"-bad", "brief"); err == nil {
				t.Fatalf("%s: unbegun turn must error", provider)
			}
		})
	}
}

// ── the seat must not stay bound to the target ──────────────────────────

// A seat this spawn minted and could not brief is retired, so 🎯T222 admits
// the next implementer and the 🎯T155 leaf is not permanently masked by a
// worker that never ran.
func TestReleaseUnbriefedSeatFreesTheTarget(t *testing.T) {
	t.Parallel()
	s, reg, _, dir := startPromptFixture(t, "jv-t387-phantom", claudia.ProviderGrok)

	prevStatus := loadTargetStatusForKickoff
	t.Cleanup(func() { loadTargetStatusForKickoff = prevStatus })
	loadTargetStatusForKickoff = func(string, string) (string, bool) { return "identified", true }

	// While the phantom seat is registered, a second implementer is refused.
	if msg := s.refuseEngagedOrClosedTarget("jv-t387-second", dir, "T387", false); msg == "" {
		t.Fatal("fixture wrong: registered seat should engage the target")
	}

	if !s.releaseUnbriefedSeat("jv-t387-phantom", false) {
		t.Fatal("a seat minted by this call must be retired")
	}
	if reg.Def("jv-t387-phantom") != nil {
		t.Fatal("unbriefed seat still registered")
	}
	if msg := s.refuseEngagedOrClosedTarget("jv-t387-second", dir, "T387", false); msg != "" {
		t.Fatalf("target must be free for the next implementer: %q", msg)
	}
}

// An agent that already existed is stopped but kept: a failed re-brief must
// not delete an established seat. Over-broadness guard on the teardown.
func TestReleaseUnbriefedSeatKeepsPreexistingAgent(t *testing.T) {
	t.Parallel()
	s, reg, _, _ := startPromptFixture(t, "jv-t387-established", claudia.ProviderGrok)

	if s.releaseUnbriefedSeat("jv-t387-established", true) {
		t.Fatal("a pre-existing agent must not be retired")
	}
	if reg.Def("jv-t387-established") == nil {
		t.Fatal("established seat was destroyed by a failed re-brief")
	}
}

// ── window configuration ────────────────────────────────────────────────

// The window is an operator dial, and an unusable value keeps the default
// rather than silently disabling the confirmation.
func TestTurnConfirmWindow(t *testing.T) {
	if got := turnConfirmWindow(); got != defaultTurnConfirmWindow {
		t.Fatalf("unset: got %s want %s", got, defaultTurnConfirmWindow)
	}
	t.Setenv(TurnConfirmWindowEnv, "250ms")
	if got := turnConfirmWindow(); got != 250*time.Millisecond {
		t.Fatalf("override: got %s", got)
	}
	for _, bad := range []string{"nonsense", "0s", "-5s"} {
		t.Setenv(TurnConfirmWindowEnv, bad)
		if got := turnConfirmWindow(); got != defaultTurnConfirmWindow {
			t.Fatalf("%q must fall back to the default, got %s", bad, got)
		}
	}
}

// ConfirmTurnBeganOK is a readability helper for the observation tests.
func ConfirmTurnBeganOK(t *testing.T, ev TurnEvidence) bool {
	t.Helper()
	return ConfirmTurnBegan("sent", nil, ev) == nil
}
