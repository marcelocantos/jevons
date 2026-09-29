// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// 🎯T834: when J14's pre-bounce owner turn gets no reply within its bounded
// wait, the failure names why instead of a bare deadline. The wait itself is
// never lengthened; this only reads what the isolate already recorded.
//
// Gate 0b19696c (full grok run) is the specimen: J13 had left the overseer on
// claude, J14's own aside reply notice took the overseer at 22:01:51Z and the
// provider produced nothing for 120s, and the owner prompt, queued behind that
// turn, was answered at 22:03:53Z — after the deadline.

// ownerStallPhase is one overseer phase sample from the canonical socket's
// meta (🎯T555.2): phase, whose turn it is (empty = owner), and the down reason.
type ownerStallPhase struct {
	At            time.Time
	Phase         string
	Correspondent []string
	Down          string
}

func (p ownerStallPhase) working() bool {
	return p.Phase != "" && p.Phase != "idle" && p.Phase != "error"
}

func (p ownerStallPhase) String() string {
	s := p.Phase
	if s == "" {
		s = "none"
	}
	if len(p.Correspondent) > 0 {
		s += " for " + strings.Join(p.Correspondent, ",")
	}
	if p.Down != "" {
		s += " (down: " + p.Down + ")"
	}
	return s
}

// ownerStallWatch taps the owner mux for phase metas and counts the assistant
// rows the reply wait observes after the owner echo.
type ownerStallWatch struct {
	mu        sync.Mutex
	phases    []ownerStallPhase // distinct transitions, oldest first
	assistant int               // assistant rows after the echo
	ended     int               // of those, rows that reached a terminal stop
	lastEnded string            // text of the last ended row (not the requested token)
}

const ownerStallMaxPhases = 12

// tap forwards every frame unchanged, recording phase metas before forwarding.
func (w *ownerStallWatch) tap(ctx context.Context, in <-chan []byte) <-chan []byte {
	out := make(chan []byte)
	go func() {
		defer close(out)
		for {
			select {
			case data, ok := <-in:
				if !ok {
					return
				}
				w.notePhaseMeta(data)
				select {
				case out <- data:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

func (w *ownerStallWatch) notePhaseMeta(data []byte) {
	var env ownerMuxEnvelope
	if json.Unmarshal(data, &env) != nil || env.Channel != ownerMuxChannel || env.Type != "meta" {
		return
	}
	var meta struct {
		Phase *struct {
			Phase         string   `json:"phase"`
			Correspondent []string `json:"correspondent"`
		} `json:"phase"`
		Down *string `json:"overseer_down"`
	}
	if json.Unmarshal(env.Body, &meta) != nil || meta.Phase == nil {
		return
	}
	next := ownerStallPhase{At: time.Now(), Phase: meta.Phase.Phase, Correspondent: meta.Phase.Correspondent}
	if meta.Down != nil {
		next.Down = *meta.Down
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if n := len(w.phases); n > 0 && w.phases[n-1].String() == next.String() {
		return
	}
	w.phases = append(w.phases, next)
	if len(w.phases) > ownerStallMaxPhases {
		w.phases = w.phases[len(w.phases)-ownerStallMaxPhases:]
	}
}

// observe is the reply wait's post-echo observer.
func (w *ownerStallWatch) observe(frame ownerMuxFrame) {
	if frame.Type != "assistant" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.assistant++
	switch frame.Event.Message.Stop {
	case "end_turn", "stop_sequence", "max_tokens":
		w.ended++
		w.lastEnded = strings.TrimSpace(journeyContentText(frame.Event.Message.Content))
	}
}

func (w *ownerStallWatch) snapshot() (phases []ownerStallPhase, assistant, ended int, lastEnded string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]ownerStallPhase(nil), w.phases...), w.assistant, w.ended, w.lastEnded
}

// ownerStallEvidence is everything the classifier reads. Logs is the isolate
// daemon log from the start of the journey through the deadline.
type ownerStallEvidence struct {
	Requested        string // provider the journey asked for
	OverseerProvider string // registry's overseer provider at the deadline ("" = unread)
	SentAt           time.Time
	AtSend           ownerStallPhase   // last sample before the owner send
	Phases           []ownerStallPhase // transitions seen on the owner mux
	EchoIndex        int               // owner echo index (0 = never echoed)
	Assistant        int
	Ended            int
	LastEnded        string
	Logs             []byte
}

type ownerStallLog struct {
	at     time.Time
	fields map[string]string
	line   string
}

// ownerStallLogLines keeps only the daemon lines that bear on the overseer's
// delivery and liveness; everything else is noise for this verdict.
func ownerStallLogLines(logs []byte) []ownerStallLog {
	var out []ownerStallLog
	for _, line := range strings.Split(string(logs), "\n") {
		fields, err := queueJourneyLogFields(line)
		if err != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, fields["time"])
		if err != nil {
			continue
		}
		keep := false
		switch msg := fields["msg"]; {
		case msg == "notify_queue", msg == "notifying jevon":
			keep = true
		case msg == "agent_send" && fields["name"] == overseerName:
			keep = true
		case strings.HasPrefix(msg, "OVERSEER NOT RUNNING"),
			msg == "auto-start failed" && fields["agent"] == overseerName,
			strings.HasPrefix(msg, "cockpit: stuck-busy"),
			strings.Contains(msg, "🎯T903"):
			keep = true
		}
		if keep {
			out = append(out, ownerStallLog{at: at, fields: fields, line: strings.TrimSpace(line)})
		}
	}
	return out
}

func ownerStallQuote(l ownerStallLog) string {
	f := l.fields
	parts := []string{l.at.UTC().Format("15:04:05.000Z"), f["msg"]}
	for _, key := range []string{"decision", "agent", "origin", "status", "outcome", "owner_batch", "err_class", "provider"} {
		if v, ok := f[key]; ok {
			parts = append(parts, key+"="+v)
		}
	}
	if v := f["err"]; v != "" {
		parts = append(parts, "err="+trim(v, 120))
	}
	return strings.Join(parts, " ")
}

// diagnoseOwnerStall names every cause the evidence supports, in the order
// the target lists them: (a) the overseer was busy with a deferred notice,
// (b) the overseer was on another backend, (c) the provider did not answer;
// plus (d) the overseer was not running and (e) it answered with something
// else. An empty evidence set says so rather than guessing.
func diagnoseOwnerStall(ev ownerStallEvidence) string {
	logs := ownerStallLogLines(ev.Logs)
	var causes []string

	// (a) busy with a notice: the socket said a non-owner turn held the seat
	// when the owner sent, or the daemon logged the owner prompt as queued
	// behind a turn.
	var busyWhy []string
	if ev.AtSend.working() && len(ev.AtSend.Correspondent) > 0 {
		busyWhy = append(busyWhy, "at send the overseer was "+ev.AtSend.String())
	}
	// The notice that took the seat: "notifying jevon" names its agent, so
	// it outranks the bare fleet-batch drain line.
	var lastNotice, lastDrain *ownerStallLog
	for i := range logs {
		l := &logs[i]
		f := l.fields
		if !l.at.After(ev.SentAt) {
			if f["msg"] == "notifying jevon" {
				lastNotice = l
			} else if f["msg"] == "notify_queue" && f["decision"] == "drain" && f["owner_batch"] == "false" {
				lastDrain = l
			}
			continue
		}
		queuedOwner := f["msg"] == "agent_send" && f["origin"] == "owner" &&
			(f["outcome"] == "queued_behind_turn" || f["outcome"] == "escalating")
		deferredOwner := f["msg"] == "notify_queue" && f["decision"] == "defer" && f["owner_batch"] == "true"
		if queuedOwner || deferredOwner || strings.Contains(f["msg"], "🎯T903") {
			busyWhy = append(busyWhy, "daemon: "+ownerStallQuote(*l))
		}
	}
	if lastNotice == nil {
		lastNotice = lastDrain
	}
	if len(busyWhy) > 0 {
		if lastNotice != nil {
			busyWhy = append(busyWhy, "last notice before send: "+ownerStallQuote(*lastNotice))
		}
		causes = append(causes, "(a) overseer busy with a deferred notice — "+strings.Join(busyWhy, "; "))
	}

	// (b) another backend: J13 migrates the shared overseer (🎯T832).
	if ev.OverseerProvider != "" && ev.OverseerProvider != ev.Requested {
		causes = append(causes, fmt.Sprintf("(b) overseer on %s, not the requested %s (an earlier journey migrated it)",
			ev.OverseerProvider, ev.Requested))
	}

	// (c) the provider did not answer: a turn (the owner's, or the one it
	// queued behind) was on the provider through the deadline with no end.
	var last ownerStallPhase
	if n := len(ev.Phases); n > 0 {
		last = ev.Phases[n-1]
	}
	if ev.Ended == 0 && last.working() && last.Down == "" {
		since := ""
		for i := len(ev.Phases) - 1; i >= 0 && ev.Phases[i].working(); i-- {
			since = ev.Phases[i].At.UTC().Format("15:04:05Z")
		}
		causes = append(causes, fmt.Sprintf("(c) provider did not answer — the overseer stayed %s from %s to the deadline; %d assistant row(s) after the owner echo, none ended",
			last.String(), since, ev.Assistant))
	}

	// (d) not running: the socket's down reason or the daemon's own alarm.
	var downWhy []string
	if last.Down != "" {
		downWhy = append(downWhy, "socket: "+last.Down)
	}
	for _, l := range logs {
		if strings.HasPrefix(l.fields["msg"], "OVERSEER NOT RUNNING") || l.fields["msg"] == "auto-start failed" {
			downWhy = append(downWhy, "daemon: "+ownerStallQuote(l))
		}
	}
	if len(downWhy) > 0 {
		causes = append(causes, "(d) overseer not running — "+strings.Join(downWhy, "; "))
	}

	// (e) answered, but not with the requested token.
	if ev.Ended > 0 {
		causes = append(causes, fmt.Sprintf("(e) overseer answered %d time(s) but not with the requested token; last: %q",
			ev.Ended, trim(ev.LastEnded, 160)))
	}

	if ev.EchoIndex == 0 {
		causes = append(causes, "owner prompt never echoed on the mux")
	}
	if len(causes) == 0 {
		var seen []string
		for _, p := range ev.Phases {
			seen = append(seen, p.String())
		}
		return "unclassified: no busy notice, backend switch, unanswered turn or down overseer in the daemon log or mux; phases seen: [" +
			strings.Join(seen, " → ") + "]"
	}
	return strings.Join(causes, "; ")
}
