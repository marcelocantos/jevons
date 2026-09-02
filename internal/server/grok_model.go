// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

// The fleet badge names the model a Grok session billed or declared (🎯T619).
// Exclusive-MCP seats write under process GROK_HOME (temp claudia-mcp-grok-*),
// not ~/.grok/sessions — both roots are searched. Evidence order: summary.json
// current_model_id, then last turn_completed modelUsage, then _meta.modelId.
// Live ACP frames carry update._meta.modelId on user_message_chunk (T293's
// "Grok names no model on the wire" is stale). Caching/tail machinery is
// shared (session_model.go).

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// newGrokModelResolver returns a resolver over cfg.SessionsDir plus any
// exclusive GROK_HOME session trees. An empty set of roots always answers "".
func newGrokModelResolver(sessionsDir string, extraSessionRoots ...string) *sessionModelResolver {
	roots := make([]string, 0, 1+len(extraSessionRoots))
	if sessionsDir != "" {
		roots = append(roots, sessionsDir)
	}
	for _, e := range extraSessionRoots {
		if e != "" {
			roots = append(roots, e)
		}
	}
	primary := ""
	if len(roots) > 0 {
		primary = roots[0]
	}
	return &sessionModelResolver{
		root: primary,
		pathFor: func(_, workDir, sessionID string) string {
			return grokEvidencePath(roots, workDir, sessionID)
		},
		parse: grokModelFromEvidence,
		by:    make(map[string]sessionModelEntry),
	}
}

// grokEvidencePath locates the session's evidence file across roots: the
// workdir bucket first (one stat), then a scan by session id. Prefers
// summary.json when it names current_model_id; otherwise updates.jsonl.
// When the same session exists in more than one root, the newest mtime wins
// (a remint can leave a leftover exclusive home).
func grokEvidencePath(roots []string, workDir, sessionID string) string {
	var best string
	var bestMod time.Time
	for _, root := range roots {
		dir := grokSessionDir(root, workDir, sessionID)
		if dir == "" {
			continue
		}
		path, mod, ok := grokEvidenceInDir(dir)
		if !ok {
			continue
		}
		if best == "" || mod.After(bestMod) {
			best, bestMod = path, mod
		}
	}
	return best
}

func grokSessionDir(sessionsDir, workDir, sessionID string) string {
	if sessionsDir == "" || sessionID == "" {
		return ""
	}
	if workDir != "" {
		p := filepath.Join(sessionsDir, discovery.EncodeCWDBucket(workDir), sessionID)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}
	return discovery.SessionPath(sessionsDir, sessionID)
}

func grokEvidenceInDir(dir string) (string, time.Time, bool) {
	summary := filepath.Join(dir, "summary.json")
	if fi, err := os.Stat(summary); err == nil && fi.Mode().IsRegular() {
		if current, err := os.ReadFile(summary); err == nil {
			if m := grokCurrentModelID(current); m != "" {
				return summary, fi.ModTime(), true
			}
		}
	}
	updates := filepath.Join(dir, "updates.jsonl")
	if fi, err := os.Stat(updates); err == nil && fi.Mode().IsRegular() {
		return updates, fi.ModTime(), true
	}
	return "", time.Time{}, false
}

type grokMeta struct {
	ModelID string `json:"modelId"`
}

type grokUpdate struct {
	SessionUpdate string `json:"sessionUpdate"`
	Usage         struct {
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	} `json:"usage"`
	Meta *grokMeta `json:"_meta"`
}

// grokUpdateLine covers both updates.jsonl (method/params wrapper) and the
// ACP params blob claudia puts on Event.Raw.
type grokUpdateLine struct {
	Params *struct {
		Update grokUpdate `json:"update"`
		Meta   *grokMeta  `json:"_meta"`
	} `json:"params"`
	Update grokUpdate `json:"update"`
	Meta   *grokMeta  `json:"_meta"`
}

func (l grokUpdateLine) update() grokUpdate {
	if l.Params != nil {
		return l.Params.Update
	}
	return l.Update
}

func grokCurrentModelID(data []byte) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ""
	}
	var sum struct {
		CurrentModelID string `json:"current_model_id"`
	}
	if err := json.Unmarshal(trimmed, &sum); err != nil {
		return ""
	}
	return strings.TrimSpace(sum.CurrentModelID)
}

// grokModelFromEvidence returns current_model_id when data is a summary.json
// object, otherwise the last billed/declared id in an updates.jsonl tail.
func grokModelFromEvidence(data []byte) string {
	if m := grokCurrentModelID(data); m != "" {
		return m
	}
	return grokModelFromTail(data)
}

// grokModelFromTail returns the model id of the last turn_completed
// modelUsage, else the last _meta.modelId. Pure — file reading lives in the
// resolver. Several billed models on one turn: lexicographically smallest
// id so the badge does not flicker (map order is not stable).
func grokModelFromTail(data []byte) string {
	billed, declared := "", ""
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		hasUsage := bytes.Contains(line, []byte(`"turn_completed"`)) && bytes.Contains(line, []byte(`"modelUsage"`))
		hasID := bytes.Contains(line, []byte(`"modelId"`))
		if !hasUsage && !hasID {
			continue
		}
		var l grokUpdateLine
		if err := json.Unmarshal(line, &l); err != nil {
			continue
		}
		if hasUsage {
			if best := grokBilledModel(l); best != "" {
				billed = best
			}
		}
		if id := grokDeclaredModel(l); id != "" {
			declared = id
		}
	}
	if billed != "" {
		return billed
	}
	return declared
}

func grokBilledModel(l grokUpdateLine) string {
	u := l.update()
	if u.SessionUpdate != "turn_completed" {
		return ""
	}
	best := ""
	for name := range u.Usage.ModelUsage {
		if name == "" {
			continue
		}
		if best == "" || name < best {
			best = name
		}
	}
	return best
}

func grokDeclaredModel(l grokUpdateLine) string {
	u := l.update()
	if u.Meta != nil {
		if m := strings.TrimSpace(u.Meta.ModelID); m != "" {
			return m
		}
	}
	if l.Params != nil && l.Params.Meta != nil {
		if m := strings.TrimSpace(l.Params.Meta.ModelID); m != "" {
			return m
		}
	}
	if l.Meta != nil {
		return strings.TrimSpace(l.Meta.ModelID)
	}
	return ""
}
