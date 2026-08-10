// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ctxcap

import (
	"os"
	"path/filepath"
	"time"

	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/discovery"
)

// Observation of live context (🎯T392.1).
//
// Deliberately self-contained rather than reading internal/cost's
// usage.db: that store is gated by budget.json, which the owner disabled
// on 2026-08-03 because its dollar figures were meaningless under a
// subscription. It had six rows when the fleet burned a billion tokens.
// A ceiling whose measurement can be switched off by an unrelated
// accounting decision is not a ceiling.
//
// The read is a bounded tail, not a full parse: only the last usage frame
// matters, session logs reach hundreds of megabytes, and this runs on a
// timer for every agent.

// tailBytes is how much of the end of a session log to read. Usage frames
// are a few KiB apart at most; 256 KiB spans many turns even when tool
// results are large, and a miss is reported as "no context observed"
// rather than guessed at.
const tailBytes = 256 * 1024

// Observer resolves an agent's live context from provider session logs.
type Observer struct {
	Roots discovery.Roots
}

// AgentRef is what the daemon knows about an agent when it asks.
type AgentRef struct {
	Name      string
	Provider  string
	WorkDir   string
	SessionID string
	Exempt    bool
}

// Observe reads the most recent model call's context for one agent.
// HasContext is false — never a zero context — when nothing can be read.
func (o Observer) Observe(a AgentRef) Observation {
	obs := Observation{Agent: a.Name, Exempt: a.Exempt}
	path := o.usagePath(a)
	if path == "" {
		return obs
	}
	line := lastUsageLine(path)
	if line == nil {
		return obs
	}
	ev := cost.ParseLine(line, a.SessionID, timeZero())
	if ev == nil || ev.ModelCalls <= 0 {
		return obs
	}
	obs.Context = contextTokens(a.Provider, ev.Usage) / ev.ModelCalls
	obs.HasContext = true
	return obs
}

// contextTokens is the conversation a call actually carried, which is not
// the same field on both providers. Grok's inputTokens is INCLUSIVE of
// cachedReadTokens; Claude reports input_tokens fresh-only with cache
// reads alongside — typically tens of tokens against hundreds of
// thousands served from cache.
//
// Reading Usage.Input directly is therefore correct for Grok and
// catastrophic for Claude: every agent looks near-empty, never crosses
// the ceiling, and is never compacted. The ceiling shipped that way and
// was inert on Claude until the 🎯T392 post-change measurement showed
// mean context RISING to 205k under a 100k cap.
func contextTokens(provider string, u cost.Usage) int64 {
	switch provider {
	case "grok", "xai":
		return u.Input
	default:
		return u.Input + u.CacheRead + u.CacheCreate
	}
}

// usagePath locates the file carrying the session's usage frames. Grok
// records them in updates.jsonl beside the chat history; Claude records
// them inline in the session transcript itself.
func (o Observer) usagePath(a AgentRef) string {
	switch a.Provider {
	case "grok", "xai":
		if o.Roots.GrokSessions == "" {
			return ""
		}
		if a.WorkDir != "" {
			p := filepath.Join(o.Roots.GrokSessions,
				discovery.EncodeCWDBucket(a.WorkDir), a.SessionID, "updates.jsonl")
			if isFile(p) {
				return p
			}
		}
		if dir := discovery.SessionPath(o.Roots.GrokSessions, a.SessionID); dir != "" {
			if p := filepath.Join(dir, "updates.jsonl"); isFile(p) {
				return p
			}
		}
	default: // claude and anything else claudia drives through Claude Code
		if o.Roots.ClaudeProjects == "" {
			return ""
		}
		if a.WorkDir != "" {
			p := filepath.Join(o.Roots.ClaudeProjects,
				discovery.EncodeClaudeProject(a.WorkDir), a.SessionID+".jsonl")
			if isFile(p) {
				return p
			}
		}
	}
	return ""
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// lastUsageLine returns the final line in the file's tail that carries
// token usage, or nil. Scanning backwards costs one read of a bounded
// window regardless of how large the log has grown.
func lastUsageLine(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	size := fi.Size()
	start := size - tailBytes
	if start < 0 {
		start = 0
	}
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && len(buf) == 0 {
		return nil
	}

	// Walk lines back to front and take the first that parses with usage.
	// A partial first line (the window cut it) simply fails to parse.
	end := len(buf)
	for end > 0 {
		nl := lastIndexByte(buf[:end], '\n')
		line := buf[nl+1 : end]
		if len(line) > 0 {
			if ev := cost.ParseLine(line, "", timeZero()); ev != nil && ev.ModelCalls > 0 {
				return append([]byte(nil), line...)
			}
		}
		if nl < 0 {
			break
		}
		end = nl
	}
	return nil
}

func lastIndexByte(b []byte, c byte) int {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// timeZero is the "now" handed to cost.ParseLine. Observation only needs
// the token counts, never the timestamp, and passing a fixed value keeps
// the read pure enough to test.
func timeZero() time.Time { return time.Time{} }
