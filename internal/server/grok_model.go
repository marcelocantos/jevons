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
// we assumed. The caching/tail machinery is shared (session_model.go).

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// newGrokModelResolver returns a resolver over a Grok sessions root
// (~/.grok/sessions). An empty dir yields a resolver that always answers "".
func newGrokModelResolver(sessionsDir string) *sessionModelResolver {
	return &sessionModelResolver{
		root:    sessionsDir,
		pathFor: grokUpdatesPath,
		parse:   grokModelFromTail,
		by:      make(map[string]sessionModelEntry),
	}
}

// grokUpdatesPath locates the session's updates.jsonl: the workdir bucket
// first (one stat), then a scan by session id for agents whose workdir moved
// since the session was created.
func grokUpdatesPath(sessionsDir, workDir, sessionID string) string {
	if workDir != "" {
		p := filepath.Join(sessionsDir, discovery.EncodeCWDBucket(workDir), sessionID, "updates.jsonl")
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	if dir := discovery.SessionPath(sessionsDir, sessionID); dir != "" {
		p := filepath.Join(dir, "updates.jsonl")
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p
		}
	}
	return ""
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
