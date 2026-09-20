// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"fmt"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// idleObs is the last live-stream activity the idle sweep may treat as a
// transcript tail. Timestamp is when this daemon observed the event, not
// a replayed provider clock.
type idleObs struct {
	at    time.Time
	entry transcript.Entry
}

func idleUserEntry(at time.Time) transcript.Entry {
	return transcript.Entry{
		Type:       "user",
		Role:       "user",
		Text:       "send",
		IsUserTurn: true,
		Timestamp:  at,
	}
}

func idleEntryFromEvent(ev claudia.Event, at time.Time) transcript.Entry {
	e := transcript.Entry{
		Type:       ev.Type,
		Text:       ev.Text,
		StopReason: ev.StopReason,
		Timestamp:  at,
	}
	switch {
	case ev.Type == "user":
		e.Role = "user"
		e.IsUserTurn = ev.Text != ""
	case ev.Type == "assistant", ev.Type == "tool_call", ev.Type == "tool_call_update", ev.ProgressType == "tool_use":
		e.Type = "assistant"
		e.Role = "assistant"
		if ev.StopReason == "tool_use" || ev.ProgressType == "tool_use" || ev.Type == "tool_call" ||
			ev.ToolStatus == "pending" || ev.ToolStatus == "in_progress" {
			e.HasToolUse = true
		}
	default:
		if e.Type == "" {
			e.Type = "progress"
		}
		e.Role = "assistant"
		if e.Text == "" {
			e.Text = ev.ProgressType
		}
		if e.Text == "" {
			e.Text = ev.Type
		}
	}
	return e
}

func liveStreamIdleEntries(obs idleObs) ([]transcript.Entry, error) {
	if obs.entry.Timestamp.IsZero() && obs.at.IsZero() {
		return nil, fmt.Errorf("idle transcript: no observed live-stream activity")
	}
	e := obs.entry
	if e.Timestamp.IsZero() {
		e.Timestamp = obs.at
	}
	return []transcript.Entry{e}, nil
}

func matchingLiveSession(tSession, liveSession, registeredSession string) error {
	if tSession == "" || liveSession != tSession || registeredSession != tSession {
		return fmt.Errorf("idle transcript: no matching live session")
	}
	return nil
}

func (f *Claudia) noteIdleUser(id string) {
	if f == nil || id == "" {
		return
	}
	at := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.idleObs == nil {
		f.idleObs = map[string]idleObs{}
	}
	f.idleObs[id] = idleObs{at: at, entry: idleUserEntry(at)}
}

func (f *Claudia) noteIdleEvent(id string, ev claudia.Event) {
	if f == nil || id == "" {
		return
	}
	at := time.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.idleObs == nil {
		f.idleObs = map[string]idleObs{}
	}
	f.idleObs[id] = idleObs{at: at, entry: idleEntryFromEvent(ev, at)}
}

func (f *Claudia) ensureIdleWatch(id string, ag *claudia.Agent) {
	if f == nil || ag == nil || id == "" {
		return
	}
	f.mu.Lock()
	if f.idleWatch == nil {
		f.idleWatch = map[string]bool{}
	}
	if f.idleWatch[id] {
		f.mu.Unlock()
		return
	}
	f.idleWatch[id] = true
	f.mu.Unlock()
	ag.SubscribeEvents(func(ev claudia.Event) {
		f.noteIdleEvent(id, ev)
	})
}

func (f *Claudia) clearIdleWatch(id string) {
	if f == nil || id == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.idleObs, id)
	delete(f.idleWatch, id)
}
