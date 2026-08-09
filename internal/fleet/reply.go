// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
)

// A directed turn's reply is assembled here rather than by
// claudia.Agent.WaitForResponse, which loses text two ways on this path
// (🎯T286):
//
//   - Chunk joining. WaitForResponse separates consecutive assistant
//     events with a newline. That is right for providers publishing one
//     event per content block (Claude's JSONL, Codex's item events) and
//     wrong for Grok's ACP stream, where each event is a token-level
//     delta: a worker answering BLUEOTTER42 came back as
//     "BLUE\nOTTER\n42", read as truncated by every consumer that takes
//     the first line.
//
//   - Turn attribution. WaitForResponse resolves a fixed settle after any
//     terminal stop_reason it sees, including one left over from an
//     earlier turn whose prompt result had not been fanned out when this
//     turn was sent — the 🎯T285 handover-seed window. The settle then
//     expires in the first gap between our own tokens and the caller gets
//     the leading chunk alone: "BLUE" instead of "BLUEOTTER42".
//
// Fixing the assembly upstream belongs in claudia, which owns the event
// stream and knows which providers stream deltas. Until then jevons pays
// for it here, on the one path whose contract is "return the whole
// reply".

const (
	// blockSettle is how long assembly lingers after a terminal
	// stop_reason for the remaining content blocks of the same message.
	// Claude Code versions differ on whether every block of a message
	// carries the message's stop_reason or only the last, so a terminal
	// is evidence the turn is ending, not proof it has ended. Matches
	// claudia's own settle, so block-shaped providers assemble exactly as
	// they did before.
	blockSettle = 250 * time.Millisecond

	// emptyTurnGuard bounds the wait for a turn that has produced no text
	// when a terminal arrives.
	//
	// Such a terminal is ambiguous: either this turn is genuinely silent
	// (tool calls only), or it is the tail of the previous turn, fanned
	// out after this turn's send because the provider cleared its
	// in-flight gate before publishing the event. Nothing in the event
	// distinguishes them, so assembly waits instead of guessing: any
	// activity — reply text, or a tool-call progress event — proves this
	// turn is alive and cancels the guard, and only silence for the whole
	// window resolves the turn as empty. The cost is bounded latency on
	// genuinely silent turns; the benefit is that a slow first token can
	// no longer be mistaken for one.
	emptyTurnGuard = 5 * time.Second
)

// chunkSeparator is what goes between consecutive assistant events of one
// turn for a provider. Grok's ACP stream publishes token deltas that must
// be concatenated verbatim — the same way the owner chat path accumulates
// them. Every other provider publishes one event per content block or
// whole message, where a newline preserves the author's paragraphing.
func chunkSeparator(p claudia.Provider) string {
	if p == claudia.ProviderGrok {
		return ""
	}
	return "\n"
}

// replyAssembler accumulates the assistant text of exactly one directed
// turn from an agent's event stream. Subscribe Observe before sending,
// call Started once the send has been issued, then Wait for the reply.
type replyAssembler struct {
	sep       string
	settleFor time.Duration
	guardFor  time.Duration
	done      chan string

	mu       sync.Mutex
	text     strings.Builder
	started  bool
	gotText  bool
	resolved bool
	settle   *time.Timer
	guard    *time.Timer
}

func newReplyAssembler(sep string) *replyAssembler {
	return &replyAssembler{
		sep:       sep,
		settleFor: blockSettle,
		guardFor:  emptyTurnGuard,
		done:      make(chan string, 1),
	}
}

// Started marks the point at which the caller's own turn was sent.
// Events seen before it belong to an earlier turn still draining and are
// dropped: this turn's first token cannot precede its own send.
func (r *replyAssembler) Started() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = true
}

// Observe is the claudia event subscriber for this turn.
func (r *replyAssembler) Observe(ev claudia.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started || r.resolved {
		return
	}
	if ev.Type != "assistant" {
		// Tool calls and other progress are proof the turn is working,
		// which is exactly what an armed empty-turn guard is waiting for.
		r.bumpGuardLocked()
		return
	}

	if ev.Text != "" {
		if r.sep != "" && r.text.Len() > 0 {
			r.text.WriteString(r.sep)
		}
		r.text.WriteString(ev.Text)
		r.gotText = true
	}

	switch {
	case r.settle != nil:
		// A terminal has already been honoured for this turn, so every
		// further event is more of the same message: restart the settle.
		r.armSettleLocked()
	case !ev.IsTerminalStop():
		// Mid-stream text. Cancel any guard: this turn is not silent.
		if ev.Text != "" {
			r.stopGuardLocked()
		}
	case r.gotText:
		r.armSettleLocked()
	default:
		// Terminal with nothing to show for it — see emptyTurnGuard.
		r.armGuardLocked()
	}
}

// Wait blocks until this turn's reply is complete, or ctx is done.
func (r *replyAssembler) Wait(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case out := <-r.done:
		return out, nil
	}
}

// Close stops pending timers and retires the assembler. Safe to call
// more than once; call it once the subscription is gone.
func (r *replyAssembler) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolved = true
	if r.settle != nil {
		r.settle.Stop()
		r.settle = nil
	}
	r.stopGuardLocked()
}

func (r *replyAssembler) armSettleLocked() {
	r.stopGuardLocked()
	if r.settle != nil {
		r.settle.Stop()
	}
	r.settle = time.AfterFunc(r.settleFor, r.resolve)
}

func (r *replyAssembler) armGuardLocked() {
	if r.guard != nil {
		r.guard.Stop()
	}
	r.guard = time.AfterFunc(r.guardFor, r.resolve)
}

func (r *replyAssembler) bumpGuardLocked() {
	if r.guard != nil {
		r.armGuardLocked()
	}
}

func (r *replyAssembler) stopGuardLocked() {
	if r.guard != nil {
		r.guard.Stop()
		r.guard = nil
	}
}

func (r *replyAssembler) resolve() {
	r.mu.Lock()
	if r.resolved {
		r.mu.Unlock()
		return
	}
	r.resolved = true
	out := r.text.String()
	r.mu.Unlock()

	select {
	case r.done <- out:
	default:
	}
}

// awaitReply sends one turn to a live agent and returns its whole reply.
//
// Subscribing before the send is load-bearing in both directions: a chunk
// published between send and subscribe would be lost from the front of
// the reply, and the assembler needs to be watching to recognise (and
// discard) a previous turn's trailing terminal.
func (f *Claudia) awaitReply(ag *claudia.Agent, provider claudia.Provider, text string) (string, error) {
	asm := newReplyAssembler(chunkSeparator(provider))
	defer asm.Close()

	token := ag.SubscribeEvents(asm.Observe)
	defer ag.UnsubscribeEvents(token)

	if err := ag.Send(text); err != nil {
		return "", err
	}
	asm.Started()

	ctx, cancel := context.WithTimeout(context.Background(), f.replyTimeout)
	defer cancel()
	return asm.Wait(ctx)
}

// providerOf reports the registered backend for an agent, empty when the
// row is gone (assembly then falls back to the block-shaped separator).
func (f *Claudia) providerOf(id string) claudia.Provider {
	if f == nil || f.reg == nil {
		return ""
	}
	if def := f.reg.Def(id); def != nil {
		return def.Provider
	}
	return ""
}
