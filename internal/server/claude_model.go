// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

// Claude agents do name their model on the wire, so the progress hub learns it
// from the first assistant frame (🎯T287) — but only for as long as that hub
// lives. A daemon restart empties it, and an agent that is idle (or waiting on
// the owner) may not produce another frame for hours; until then its badge had
// nothing to paint, or painted the launch pin the process may have moved off.
//
// Claude Code writes the model of every assistant turn into the session JSONL
// as message.model, so the transcript re-seeds the observation immediately at
// attach — same trick as Grok's billing frames (🎯T293), same shared caching
// machinery (session_model.go). 🎯T311.

import (
	"bytes"
	"encoding/json"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// syntheticModel is what Claude Code stamps on frames it wrote itself (API
// error notices, cancellations). It names no real model and must never reach
// the badge.
const syntheticModel = "<synthetic>"

// newClaudeModelResolver returns a resolver over a Claude projects root
// (~/.claude/projects). An empty dir yields a resolver that always answers "".
func newClaudeModelResolver(projectsDir string) *sessionModelResolver {
	return &sessionModelResolver{
		root:    projectsDir,
		pathFor: claudeTranscriptPath,
		parse:   claudeModelFromTail,
		by:      make(map[string]sessionModelEntry),
	}
}

// claudeTranscriptPath locates the session's JSONL: the deterministic workdir
// bucket first (one stat), then a scan by session id for agents whose workdir
// moved since the session was created.
func claudeTranscriptPath(projectsDir, workDir, sessionID string) string {
	if p := discovery.ClaudeJSONLPathForWorkDir(projectsDir, workDir, sessionID); p != "" {
		return p
	}
	return discovery.ClaudeJSONLPath(projectsDir, sessionID)
}

// claudeModelFromTail returns the model id of the last frame in data that
// names a real one. Pure — the file reading lives in the resolver.
func claudeModelFromTail(data []byte) string {
	model := ""
	for _, line := range bytes.Split(data, []byte("\n")) {
		// Fast reject before the decode: tool results and user turns dominate.
		if !bytes.Contains(line, []byte(`"model"`)) {
			continue
		}
		var l struct {
			Model   string `json:"model"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &l); err != nil {
			continue
		}
		m := l.Message.Model
		if m == "" {
			m = l.Model
		}
		if m == "" || m == syntheticModel {
			continue
		}
		model = m
	}
	return model
}
