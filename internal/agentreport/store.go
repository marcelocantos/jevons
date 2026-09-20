// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agentreport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// NewID proposes the base report id: a timestamp prefix so lexical order is
// chronological, plus a content digest so a given text has a stable, greppable
// name.
//
// 🎯T746: the digest is NOT a uniqueness guarantee and must never be read as
// one. It carries no per-report entropy when the text repeats — every
// "No response requested." ack ever stored is <second>-85caba2f, 649 of them
// in this machine's store — so the base id collides whenever one agent stores
// the same text twice inside one second. Uniqueness is settled by Save, when
// it claims the file; this function only proposes a name.
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
	rec, err = claimRecordFile(dir, rec)
	if err != nil {
		return Record{}, err
	}
	prune(dir, DefaultKeepPerAgent)
	return rec, nil
}

// maxIDSiblings bounds the search for a free name in one id family. One agent
// would have to store the same text a thousand times inside a single second to
// reach it; the bound exists so a filesystem that reports every name as taken
// fails loudly instead of spinning.
const maxIDSiblings = 1000

// claimRecordFile writes rec under the first free name in its id family and
// returns the record carrying the id it actually got.
//
// 🎯T746. The path this replaces was write-temp-then-rename onto <id>.json,
// and rename overwrites: two reports with the same text in the same second
// resolved to one filename, so the second one deleted the first. For a 22-byte
// harness ack that was invisible, which is why it survived from 🎯T388's
// landing until now; for two real reports it silently lost one, which is the
// single failure 🎯T388 exists to prevent.
//
// The base id is left exactly as NewID proposes it, so every id already on
// disk still resolves and the non-colliding save — which is nearly all of them
// — mints precisely what it minted before. Only a genuine collision produces a
// sibling, and a sibling could not have existed under the old scheme anyway.
// Siblings are "<base>-2", "-3", …, which sort immediately after the base
// (a string sorts before its own extensions), so List and prune keep reading
// lexical order as chronological and same-second siblings read in arrival
// order.
//
// Note the record is re-marshalled per attempt: rec.ID is stored INSIDE the
// file, so a body written for the base name cannot be published under a
// sibling name without lying about its own id.
func claimRecordFile(dir string, rec Record) (Record, error) {
	base := rec.ID
	for n := 1; n <= maxIDSiblings; n++ {
		if n > 1 {
			rec.ID = fmt.Sprintf("%s-%d", base, n)
		}
		err := linkJSONExclusive(filepath.Join(dir, rec.ID+".json"), rec)
		if err == nil {
			return rec, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return Record{}, err
		}
	}
	return Record{}, fmt.Errorf(
		"agentreport: no free report id for %s after %d siblings", base, maxIDSiblings)
}

// linkJSONExclusive publishes v as JSON at path, and reports fs.ErrExist
// rather than overwriting when that name is already taken.
//
// os.Link is the whole mechanism, and it is doing two jobs at once: the name
// appears only if it does not already exist, and it appears already carrying
// the complete file. An O_EXCL reservation would give exclusivity a moment
// before the content, and a concurrent List reading that window would find an
// empty placeholder where a report should be.
//
// The temp name comes from os.CreateTemp rather than path+".tmp", which was
// the same defect one layer down: two saves racing for one id shared one temp
// file and could interleave their bytes.
func linkJSONExclusive(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("agentreport: marshal: %w", err)
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".report-*.tmp")
	if err != nil {
		return fmt.Errorf("agentreport: temp file in %s: %w", dir, err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("agentreport: write %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("agentreport: close %s: %w", tmp, err)
	}
	// CreateTemp makes 0600; stored reports are world-readable like the rest
	// of the state dir so an operator can read one without the daemon.
	if err := os.Chmod(tmp, 0o644); err != nil {
		return fmt.Errorf("agentreport: chmod %s: %w", tmp, err)
	}
	if err := os.Link(tmp, path); err != nil {
		return fmt.Errorf("agentreport: claim %s: %w", path, err)
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

// DecodeBody returns the report text to scan (🎯T468). Stored reports are
// JSON Record envelopes; scanning the file bytes matches escaped `\n` and
// disagrees with the daemon. Plain-text input is returned unchanged.
func DecodeBody(raw []byte) string {
	var rec Record
	if err := json.Unmarshal(raw, &rec); err == nil && rec.Text != "" {
		return rec.Text
	}
	return string(raw)
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
