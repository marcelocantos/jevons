// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

// Grok agents never name their model on the wire: claudia forwards only
// message and tool chunks from the ACP session/update stream, so a Grok row
// reached the RHS with a provider and no model at all — the badge painted the
// company icon with no version (🎯T293, owner bug after 🎯T287).
//
// Grok records the model it actually ran in its own session log: every
// turn_completed frame in updates.jsonl carries usage.modelUsage keyed by
// model id ("grok-4.5-build"). Reading that keeps the badge honest — the
// version comes from the harness's own billing frame, never from a default
// we assumed.

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// Only the last turns matter, and a long-lived session file runs to tens of
// megabytes — read the tail, not the log.
const grokModelTailBytes = 256 << 10

// How long a failed path lookup is remembered. Without it, every fleet poll
// rescans the whole sessions root for agents that have never completed a turn.
const grokModelLookupTTL = 30 * time.Second

// grokModelResolver maps (workdir, session) → the model id Grok last billed.
// Safe for concurrent use; results are cached until the session file grows.
type grokModelResolver struct {
	sessionsDir string
	now         func() time.Time

	mu sync.Mutex
	by map[string]grokModelEntry
}

type grokModelEntry struct {
	model   string
	path    string // resolved updates.jsonl, "" when the session has no log yet
	size    int64
	mod     time.Time
	checked time.Time // last path lookup, for the negative-result TTL
}

// newGrokModelResolver returns a resolver over a Grok sessions root
// (~/.grok/sessions). An empty dir yields a resolver that always answers "".
func newGrokModelResolver(sessionsDir string) *grokModelResolver {
	return &grokModelResolver{sessionsDir: sessionsDir, by: make(map[string]grokModelEntry)}
}

// SetGrokSessionsDir attaches the Grok sessions root (~/.grok/sessions) the
// fleet feed reads model ids from (🎯T293). Without it, Grok rows keep the
// pre-T293 behaviour: company icon, no version.
func (s *Server) SetGrokSessionsDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if dir == "" {
		s.grokModels = nil
		return
	}
	s.grokModels = newGrokModelResolver(dir)
}

func (r *grokModelResolver) clock() time.Time {
	if r != nil && r.now != nil {
		return r.now()
	}
	return time.Now()
}

// Model returns the model id the session last billed a turn against, or ""
// when Grok has written no completed turn for it yet — the caller then paints
// the company icon alone rather than inventing a version.
//
// Sticky like AgentProgress.Model: once learned, a later unreadable or
// truncated log never forgets it.
func (r *grokModelResolver) Model(workDir, sessionID string) string {
	if r == nil || r.sessionsDir == "" || sessionID == "" {
		return ""
	}
	key := workDir + "\x00" + sessionID

	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.by[key]
	now := r.clock()

	path := entry.path
	if path == "" {
		if !entry.checked.IsZero() && now.Sub(entry.checked) < grokModelLookupTTL {
			return entry.model
		}
		path = r.updatesPath(workDir, sessionID)
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

	if model := grokModelFromTail(readGrokTail(path, fi.Size())); model != "" {
		entry.model = model
	}
	entry.path, entry.size, entry.mod = path, fi.Size(), fi.ModTime()
	r.by[key] = entry
	return entry.model
}

// updatesPath locates the session's updates.jsonl: the workdir bucket first
// (one stat), then a scan by session id for agents whose workdir moved since
// the session was created.
func (r *grokModelResolver) updatesPath(workDir, sessionID string) string {
	if workDir != "" {
		p := filepath.Join(r.sessionsDir, discovery.EncodeCWDBucket(workDir), sessionID, "updates.jsonl")
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	if dir := discovery.SessionPath(r.sessionsDir, sessionID); dir != "" {
		p := filepath.Join(dir, "updates.jsonl")
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// readGrokTail returns the last grokModelTailBytes of path, trimmed to whole
// lines. Nil when the file cannot be read.
func readGrokTail(path string, size int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	off, n := int64(0), size
	if size > grokModelTailBytes {
		off, n = size-grokModelTailBytes, grokModelTailBytes
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

// grokModelFromTail returns the model id of the last turn_completed frame in
// data that names one. Pure — the file reading lives in the resolver.
//
// A turn that billed several models is vanishingly rare; when it happens the
// lexicographically smallest id wins so the badge does not flicker between
// polls (map order is not stable).
func grokModelFromTail(data []byte) string {
	model := ""
	for _, line := range bytes.Split(data, []byte("\n")) {
		// Fast reject before the decode: chat/tool frames dominate the file.
		if !bytes.Contains(line, []byte(`"turn_completed"`)) || !bytes.Contains(line, []byte(`"modelUsage"`)) {
			continue
		}
		var l struct {
			Params struct {
				Update struct {
					SessionUpdate string `json:"sessionUpdate"`
					Usage         struct {
						ModelUsage map[string]json.RawMessage `json:"modelUsage"`
					} `json:"usage"`
				} `json:"update"`
			} `json:"params"`
		}
		if err := json.Unmarshal(line, &l); err != nil {
			continue
		}
		if l.Params.Update.SessionUpdate != "turn_completed" {
			continue
		}
		best := ""
		for name := range l.Params.Update.Usage.ModelUsage {
			if name == "" {
				continue
			}
			if best == "" || name < best {
				best = name
			}
		}
		if best != "" {
			model = best
		}
	}
	return model
}
