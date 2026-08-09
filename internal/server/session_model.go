// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

// The fleet badge names the model an agent is RUNNING (🎯T311). Live wire
// frames are the freshest evidence of that, but they only exist while the
// daemon that saw them is alive: after a restart the progress hub is empty,
// and an idle agent may not speak again for hours — the badge stayed blank
// for exactly that long, or worse, fell back to a launch pin the process had
// already moved off.
//
// Both harnesses write the model they actually ran into their own session
// log, so the log is the seed the hub lacks. Grok has needed this since
// 🎯T293 (its ACP frames name no model at all); T311 generalizes the same
// machinery to Claude, whose JSONL carries message.model on every assistant
// turn. The two differ only in where the log lives and how a model id is
// spelled inside it, which is what pathFor and parse supply.

import (
	"bytes"
	"io"
	"os"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// Only the last turns matter, and a long-lived session file runs to tens of
// megabytes — read the tail, not the log.
const sessionModelTailBytes = 256 << 10

// How long a failed path lookup is remembered. Without it, every fleet poll
// rescans the whole sessions root for agents that have never written a turn.
const sessionModelLookupTTL = 30 * time.Second

// sessionModelResolver maps (workdir, session) → the model id a harness's own
// session log last named. Safe for concurrent use; results are cached until
// the log grows.
type sessionModelResolver struct {
	root    string
	pathFor func(root, workDir, sessionID string) string
	parse   func(tail []byte) string
	now     func() time.Time

	mu sync.Mutex
	by map[string]sessionModelEntry
}

type sessionModelEntry struct {
	model   string
	path    string // resolved session log, "" when there is none yet
	size    int64
	mod     time.Time
	checked time.Time // last path lookup, for the negative-result TTL
}

func (r *sessionModelResolver) clock() time.Time {
	if r != nil && r.now != nil {
		return r.now()
	}
	return time.Now()
}

// Model returns the model id the session log last named, or "" when the
// harness has written none yet — the caller then falls back rather than
// inventing a version.
//
// Sticky like AgentProgress.Model: once learned, a later unreadable or
// truncated log never forgets it.
func (r *sessionModelResolver) Model(workDir, sessionID string) string {
	if r == nil || r.root == "" || sessionID == "" {
		return ""
	}
	key := workDir + "\x00" + sessionID

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.by == nil {
		r.by = make(map[string]sessionModelEntry)
	}
	entry := r.by[key]
	now := r.clock()

	path := entry.path
	if path == "" {
		if !entry.checked.IsZero() && now.Sub(entry.checked) < sessionModelLookupTTL {
			return entry.model
		}
		path = r.pathFor(r.root, workDir, sessionID)
		entry.checked = now
		if path == "" {
			r.by[key] = entry
			return entry.model
		}
	}

	fi, err := os.Stat(path)
	if err != nil {
		// Session log vanished (pruned, workdir moved) — re-resolve next tick.
		entry.path = ""
		r.by[key] = entry
		return entry.model
	}
	if entry.path == path && fi.Size() == entry.size && fi.ModTime().Equal(entry.mod) {
		return entry.model
	}

	if model := r.parse(readSessionTail(path, fi.Size())); model != "" {
		entry.model = model
	}
	entry.path, entry.size, entry.mod = path, fi.Size(), fi.ModTime()
	r.by[key] = entry
	return entry.model
}

// readSessionTail returns the last sessionModelTailBytes of path, trimmed to
// whole lines. Nil when the file cannot be read.
func readSessionTail(path string, size int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	off, n := int64(0), size
	if size > sessionModelTailBytes {
		off, n = size-sessionModelTailBytes, sessionModelTailBytes
	}
	if n <= 0 {
		return nil
	}
	buf := make([]byte, n)
	read, err := f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil
	}
	buf = buf[:read]
	if off > 0 {
		// The window opened mid-line; drop the fragment.
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return nil
		}
		buf = buf[i+1:]
	}
	return buf
}

// fleetModelResolver answers "what is this agent running?" for any provider by
// reading that provider's own session log (🎯T311). It is the seed the
// progress hub cannot provide across a daemon restart, and the only source at
// all for a provider that names no model on the wire.
type fleetModelResolver struct {
	grok   *sessionModelResolver
	claude *sessionModelResolver
}

// newFleetModelResolver builds a resolver over the daemon's session roots.
// Either root may be empty; the corresponding provider then answers "".
func newFleetModelResolver(roots discovery.Roots) *fleetModelResolver {
	return &fleetModelResolver{
		grok:   newGrokModelResolver(roots.GrokSessions),
		claude: newClaudeModelResolver(roots.ClaudeProjects),
	}
}

// Model returns the model the provider's session log says the agent ran, or
// "" when that provider is unknown or has written nothing yet.
func (f *fleetModelResolver) Model(provider claudia.Provider, workDir, sessionID string) string {
	if f == nil {
		return ""
	}
	switch provider {
	case claudia.ProviderGrok:
		return f.grok.Model(workDir, sessionID)
	case claudia.ProviderClaude:
		return f.claude.Model(workDir, sessionID)
	default:
		return ""
	}
}

// SetModelSessionRoots attaches the session roots the fleet badge reads
// running models from (🎯T293 Grok, 🎯T311 Claude). Without it, rows fall back
// to the live hub alone and go blank across a daemon restart.
func (s *Server) SetModelSessionRoots(roots discovery.Roots) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if roots.GrokSessions == "" && roots.ClaudeProjects == "" {
		s.fleetModels = nil
		return
	}
	s.fleetModels = newFleetModelResolver(roots)
}
