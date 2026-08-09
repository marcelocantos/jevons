// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agentreport

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
)

// DirName is the report store root under the daemon state dir.
const DirName = "agent-reports"

// DefaultKeepPerAgent bounds how many reports one agent retains. Reports are
// small text; the bound exists so a months-old daemon does not accumulate
// without limit, not to save space. It is deliberately generous — the whole
// point of 🎯T388 is that a report the overseer has not read yet must still be
// there, and an agent produces one per terminal turn, not per token.
const DefaultKeepPerAgent = 200

// Record is one stored agent report.
type Record struct {
	ID    string    `json:"id"`
	Agent string    `json:"agent"`
	At    time.Time `json:"at"`
	Text  string    `json:"text"`
	// Bytes is len(Text), stored so a listing need not load every body.
	Bytes int `json:"bytes"`
}

// Handle returns the retrieval handle for this record.
func (r Record) Handle() Handle {
	return Handle{Agent: r.Agent, ReportID: r.ID}
}

// safeAgentDir maps an agent name to a single path element, refusing anything
// that could escape the store root. Agent names are free-form and reach here
// from the fleet layer, so this is an untrusted-input boundary even though
// today's names are tame (🎯T197 keeps literal dots, which are fine; ".." and
// separators are not).
func safeAgentDir(agent string) (string, error) {
	agent = strings.TrimSpace(agent)
	if agent == "" {
		return "", fmt.Errorf("agentreport: agent name required")
	}
	if agent == "." || agent == ".." ||
		strings.ContainsAny(agent, `/\`) ||
		strings.Contains(agent, "..") ||
		strings.ContainsRune(agent, 0) {
		return "", fmt.Errorf("agentreport: unsafe agent name %q", agent)
	}
	return agent, nil
}

// AgentDir is where one agent's reports live.
func AgentDir(stateDir, agent string) (string, error) {
	elem, err := safeAgentDir(agent)
	if err != nil {
		return "", err
	}
	return filepath.Join(stateDir, DirName, elem), nil
}

// NewID builds a sortable report id: a timestamp prefix so lexical order is
// chronological, plus a content digest so the same report saved twice in one
// second is one record rather than two.
func NewID(now time.Time, text string) string {
	if now.IsZero() {
		now = time.Now()
	}
	sum := sha256.Sum256([]byte(text))
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(sum[:4])
}

// Save stores a report durably and returns its record.
//
// This runs BEFORE delivery and before 🎯T165/T195 auto-deregistration, which
// is the whole point: when jv-t372-auto was asked to resend, it had already
// left the registry and jevons_agent_send answered "agent is not running".
// The text survived only because that worker happened to have committed its
// reasoning to a design doc. Storing first means the product guarantees what
// luck previously provided.
func Save(stateDir, agent, text string, now time.Time) (Record, error) {
	dir, err := AgentDir(stateDir, agent)
	if err != nil {
		return Record{}, err
	}
	if strings.TrimSpace(stateDir) == "" {
		return Record{}, fmt.Errorf("agentreport: state dir required")
	}
	if now.IsZero() {
		now = time.Now()
	}
	rec := Record{
		ID:    NewID(now, text),
		Agent: strings.TrimSpace(agent),
		At:    now.UTC(),
		Text:  text,
		Bytes: len(text),
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Record{}, fmt.Errorf("agentreport: mkdir %s: %w", dir, err)
	}
	if err := writeJSONAtomic(filepath.Join(dir, rec.ID+".json"), rec); err != nil {
		return Record{}, err
	}
	prune(dir, DefaultKeepPerAgent)
	return rec, nil
}

// writeJSONAtomic writes v as JSON via write-and-rename, so a crash or a
// concurrent reader never observes a half-written report.
func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("agentreport: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("agentreport: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("agentreport: rename %s: %w", path, err)
	}
	return nil
}

// prune keeps the newest `keep` records. Ids are timestamp-prefixed, so
// lexical order is chronological.
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

// List returns an agent's stored reports, newest last, without bodies.
// A missing directory is an empty list, not an error: an agent that never
// reported is a normal state, not a fault.
func List(stateDir, agent string) ([]Record, error) {
	dir, err := AgentDir(stateDir, agent)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
		}
	}
	sort.Strings(ids)
	out := make([]Record, 0, len(ids))
	for _, id := range ids {
		rec, err := Load(stateDir, agent, id)
		if err != nil {
			continue
		}
		rec.Text = ""
		out = append(out, rec)
	}
	return out, nil
}

// Load reads one stored report by id.
func Load(stateDir, agent, id string) (Record, error) {
	dir, err := AgentDir(stateDir, agent)
	if err != nil {
		return Record{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return Record{}, fmt.Errorf("agentreport: invalid report id %q", id)
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return Record{}, err
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return Record{}, fmt.Errorf("agentreport: parse %s: %w", id, err)
	}
	return rec, nil
}

// Latest returns an agent's most recent report. It does not consult the
// registry, so it answers for an agent that has already deregistered —
// acceptance 2 of 🎯T388.
func Latest(stateDir, agent string) (Record, error) {
	recs, err := List(stateDir, agent)
	if err != nil {
		return Record{}, err
	}
	if len(recs) == 0 {
		return Record{}, fmt.Errorf("agentreport: no stored reports for %q", agent)
	}
	return Load(stateDir, agent, recs[len(recs)-1].ID)
}
