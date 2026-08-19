// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package delivery is the durable record that a message was confirmed
// delivered (🎯T417).
//
// WHY A SEPARATE STORE. The receiving agent's transcript used to be the
// only delivery oracle. Compaction / session rotation rewrites that
// transcript down to a handover seed, so a message that was confirmed
// landed earlier becomes unprovable afterwards — the exact failure that
// lost jv-t416-send-turn-begin's endorsement after its post-handover
// transcript collapsed. This store lives under the daemon state dir and
// is never rewritten by compaction, so "was this delivered" stays
// answerable later.
//
// WHAT IS STORED. Agent name, session id (when known), a payload needle
// (same tail-hash the confirm instrument uses), when confirmation
// happened, and the operator-facing evidence detail. The full payload is
// deliberately NOT stored: needles identify the message cheaply, and the
// payload may be large or sensitive. Lookup is by agent + needle.
//
// A MALFORMED RECORD IS AN ERROR on read of that id, never a silent
// "not delivered": quiet false negatives are the defect this package
// exists to end.
package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/turnev"
)

// DirName is the store root under the daemon state dir.
const DirName = "delivery-evidence"

// DefaultKeepPerAgent bounds retained confirmations per agent. Generous:
// confirmation is rare relative to tokens, and the whole point is that
// an old confirmation still answers after later compaction.
const DefaultKeepPerAgent = 500

// Evidence is one confirmed delivery.
type Evidence struct {
	ID        string    `json:"id"`
	Agent     string    `json:"agent"`
	SessionID string    `json:"session_id,omitempty"`
	Needle    string    `json:"needle"`
	At        time.Time `json:"at"`
	Detail    string    `json:"detail,omitempty"`
	Outcome   string    `json:"outcome"`
}

// safeAgentDir maps an agent name to a single path element.
func safeAgentDir(agent string) (string, error) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "", fmt.Errorf("delivery: agent name required")
	}
	if agent == "." || agent == ".." ||
		strings.ContainsAny(agent, `/\`) ||
		strings.Contains(agent, "..") ||
		strings.ContainsRune(agent, 0) {
		return "", fmt.Errorf("delivery: unsafe agent name %q", agent)
	}
	return agent, nil
}

// AgentDir is where one agent's delivery evidence lives.
func AgentDir(stateDir, agent string) (string, error) {
	elem, err := safeAgentDir(agent)
	if err != nil {
		return "", err
	}
	return filepath.Join(stateDir, DirName, elem), nil
}

// Needle reduces a payload the same way the confirm instrument does, so
// a later lookup matches what was recorded at confirmation time.
func Needle(payload string) string { return turnev.Needle(payload) }

// newID builds a sortable evidence id.
func newID(now time.Time, agent, needle string) string {
	if now.IsZero() {
		now = time.Now()
	}
	sum := sha256.Sum256([]byte(agent + "\n" + needle))
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(sum[:4])
}

// Record stores a confirmed delivery. Empty needle (payload too short to
// identify) is refused rather than recording an ambiguous hit.
func Record(stateDir string, e Evidence, now time.Time) (Evidence, error) {
	if strings.TrimSpace(stateDir) == "" {
		return Evidence{}, fmt.Errorf("delivery: state dir required")
	}
	agent := strings.TrimSpace(e.Agent)
	needle := strings.TrimSpace(e.Needle)
	if needle == "" {
		return Evidence{}, fmt.Errorf("delivery: needle required (payload too short to identify)")
	}
	dir, err := AgentDir(stateDir, agent)
	if err != nil {
		return Evidence{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	rec := Evidence{
		ID:        newID(now, agent, needle),
		Agent:     agent,
		SessionID: strings.TrimSpace(e.SessionID),
		Needle:    needle,
		At:        now.UTC(),
		Detail:    strings.TrimSpace(e.Detail),
		Outcome:   strings.TrimSpace(e.Outcome),
	}
	if rec.Outcome == "" {
		rec.Outcome = "begun"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Evidence{}, fmt.Errorf("delivery: mkdir %s: %w", dir, err)
	}
	if err := writeJSONAtomic(filepath.Join(dir, rec.ID+".json"), rec); err != nil {
		return Evidence{}, err
	}
	prune(dir, DefaultKeepPerAgent)
	return rec, nil
}

// RecordPayload stores confirmation for a full payload (needle derived).
func RecordPayload(stateDir, agent, sessionID, payload, detail, outcome string, now time.Time) (Evidence, error) {
	return Record(stateDir, Evidence{
		Agent:     agent,
		SessionID: sessionID,
		Needle:    Needle(payload),
		Detail:    detail,
		Outcome:   outcome,
	}, now)
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("delivery: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("delivery: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("delivery: rename %s: %w", path, err)
	}
	return nil
}

func prune(dir string, keep int) {
	if keep <= 0 {
		keep = DefaultKeepPerAgent
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	if len(names) <= keep {
		return
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// Lookup returns the newest confirmation for agent+needle. ok=false means
// none — which is a normal answer, not an error. A malformed matching
// file is skipped with the next-newest tried; total absence of readable
// matches is ok=false.
func Lookup(stateDir, agent, needle string) (Evidence, bool, error) {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return Evidence{}, false, nil
	}
	dir, err := AgentDir(stateDir, agent)
	if err != nil {
		return Evidence{}, false, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Evidence{}, false, nil
		}
		return Evidence{}, false, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	sort.Strings(ids)
	// Newest last — walk reverse so the first hit is the latest.
	for i := len(ids) - 1; i >= 0; i-- {
		rec, err := load(dir, ids[i])
		if err != nil {
			continue
		}
		if rec.Needle == needle {
			return rec, true, nil
		}
	}
	return Evidence{}, false, nil
}

// LookupPayload looks up by full payload (needle derived).
func LookupPayload(stateDir, agent, payload string) (Evidence, bool, error) {
	return Lookup(stateDir, agent, Needle(payload))
}

// WasDelivered reports whether a confirmation exists for this payload.
func WasDelivered(stateDir, agent, payload string) (bool, error) {
	_, ok, err := LookupPayload(stateDir, agent, payload)
	return ok, err
}

func load(dir, id string) (Evidence, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return Evidence{}, fmt.Errorf("delivery: invalid id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return Evidence{}, err
	}
	var rec Evidence
	if err := json.Unmarshal(data, &rec); err != nil {
		return Evidence{}, fmt.Errorf("delivery: parse %s: %w", id, err)
	}
	return rec, nil
}
