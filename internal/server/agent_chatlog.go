// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/chatlog"
)

// 🎯T367: sidebar conversations persist through the SAME code as main view
// messages.
//
// Main chat has been durable since 🎯T30.1 because jevons owns an append-only
// journal (internal/chatlog): every owner turn is fsynced there BEFORE the
// provider sees it, every broadcast line is appended after, and the UI
// rehydrates from that journal — never from the provider's private store. A
// dead provider process, a daemon restart, or a rotated session cannot blank
// it. "No conversation is ever lost."
//
// The RHS sidebar (fleet agents + asides) had none of that. Its history came
// only from transcript.Reader over the provider's session JSONL, and the
// owner's just-typed message existed nowhere but the browser's optimistic
// bubble (index.html _agentInspectLines / asideWireTurnsCache). Hard-reload or
// a daemon bounce before the provider flushed its session — or an agent whose
// session never materialised — and the message was gone.
//
// This file gives every agent conversation the identical mechanism: the same
// chatlog.Log type, the same wire-frame line format, the same sealed replay
// (ReplayTailSealed) and the same wire→turn projection (overseerTurnsFromWire)
// the overseer's chatlog path already uses. One journal per agent, under
// {stateDir}/agent-chatlogs/{name}.jsonl.
//
// Write points (both unconditional — not gated on a sidebar being open):
//   - sendToNamedAgentAs: the owner/agent turn, journaled before delivery, the
//     mirror of sendToOverseerAsOwner's BroadcastChat(chatUserEcho(text)).
//   - DeliverInspectLive: every agent event, journaled before the subscriber
//     fan-out, the mirror of BroadcastChat's journal append.
//
// Read point: buildAgentTranscriptPayload merges journal turns over session
// turns (mergeAgentTurns), so rehydrate needs neither a live process nor a
// readable provider store.

const (
	// agentChatLogDirName holds one journal per agent under the state dir.
	agentChatLogDirName = "agent-chatlogs"
	// agentJournalMaxTurns caps how many recent turns one history payload
	// materialises — the same bound the overseer path uses.
	agentJournalMaxTurns = overseerTranscriptMaxTurns
	// conversationSourceAgentJournal marks turns whose base is the
	// jevons-owned per-agent journal rather than the provider session.
	conversationSourceAgentJournal = "agent_journal"
)

// agentJournals owns the lazily-opened per-agent journals. Its own mutex keeps
// journal IO off the Server's hot lock: appends happen on the agent event hook,
// which fires for every ACP delta on every agent.
type agentJournals struct {
	mu   sync.Mutex
	dir  string
	logs map[string]*chatlog.Log
	// lastUser is the most recent user text appended per agent, so the ACP
	// echo of a turn we just journaled on the send path is not recorded twice
	// (the overseer wire drops its echo via userTurnPrefix; fleet agents carry
	// no such marker, so the dedupe lives here).
	lastUser map[string]string
	// failed remembers agents whose journal could not be opened, so a broken
	// path logs once instead of on every event.
	failed map[string]bool
}

func newAgentJournals(dir string) *agentJournals {
	return &agentJournals{
		dir:      dir,
		logs:     make(map[string]*chatlog.Log),
		lastUser: make(map[string]string),
		failed:   make(map[string]bool),
	}
}

// agentJournalFileName maps a free-form agent name to a safe file name.
// Fleet names keep literal dots (🎯T197: jv-t27.2-config), so '.' is allowed;
// anything that could escape the directory or confuse the filesystem is not.
func agentJournalFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	safe := b.String()
	// "." / ".." (and any all-dot name) would resolve to a directory.
	if strings.Trim(safe, ".") == "" {
		return ""
	}
	return safe + ".jsonl"
}

// path returns the journal path for name, or "" when the name is unusable.
func (j *agentJournals) path(name string) string {
	file := agentJournalFileName(name)
	if file == "" || j == nil || j.dir == "" {
		return ""
	}
	return filepath.Join(j.dir, agentChatLogDirName, file)
}

// logFor opens (once) and returns the journal for name. nil when unavailable;
// callers treat a nil journal as "no durability beyond the provider store"
// rather than failing the send.
func (j *agentJournals) logFor(name string) *chatlog.Log {
	if j == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	path := j.path(name)
	if path == "" {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if l, ok := j.logs[name]; ok {
		return l
	}
	if j.failed[name] {
		return nil
	}
	l, err := chatlog.Open(path)
	if err != nil {
		j.failed[name] = true
		slog.Warn("agent_chatlog_open_failed",
			"component", "agent_chatlog",
			"name", name,
			"path", path,
			"err", err.Error(),
		)
		return nil
	}
	j.logs[name] = l
	return l
}

// appendUser journals a user-role turn, skipping a repeat of the text last
// journaled for this agent (the provider's ACP echo of our own send).
func (j *agentJournals) appendUser(name, text string) {
	j.appendUserAs(name, text, sendOriginOwner)
}

// appendUserAs journals the turn with its provenance (🎯T381), so a report an
// agent sent into this conversation replays as a document rather than as the
// owner's literal keystrokes.
func (j *agentJournals) appendUserAs(name, text, origin string) {
	text = strings.TrimSpace(text)
	if j == nil || text == "" {
		return
	}
	l := j.logFor(name)
	if l == nil {
		return
	}
	j.mu.Lock()
	if j.lastUser[name] == text {
		j.mu.Unlock()
		return
	}
	j.lastUser[name] = text
	j.mu.Unlock()
	if err := l.Append(chatUserEchoAs(text, origin)); err != nil {
		slog.Warn("agent_chatlog_append_failed",
			"component", "agent_chatlog",
			"name", name,
			"role", "user",
			"err", err.Error(),
		)
	}
}

// appendLine journals a pre-built wire line (assistant frames).
func (j *agentJournals) appendLine(name, line string) {
	if j == nil || strings.TrimSpace(line) == "" {
		return
	}
	l := j.logFor(name)
	if l == nil {
		return
	}
	if err := l.Append(line); err != nil {
		slog.Warn("agent_chatlog_append_failed",
			"component", "agent_chatlog",
			"name", name,
			"role", "assistant",
			"err", err.Error(),
		)
	}
}

// turns replays the journal tail and projects it into the family's turn shape.
// Uses ReplayTailSealed + overseerTurnsFromWire — the exact path the overseer's
// chatlog transcript takes, so both sources agree on shape and on how streamed
// assistant deltas coalesce (🎯T142).
func (j *agentJournals) turns(name string) ([]map[string]any, error) {
	if j == nil {
		return nil, nil
	}
	// Read without opening a write handle: an agent may have a journal on disk
	// from a previous daemon life while nothing has been appended this boot.
	path := j.path(name)
	if path == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	l := j.logFor(name)
	if l == nil {
		return nil, nil
	}
	var lines []string
	if _, _, err := l.ReplayTailSealed(agentJournalMaxTurns, func(line string) error {
		lines = append(lines, line)
		return nil
	}); err != nil {
		return nil, err
	}
	return overseerTurnsFromWire(lines), nil
}

// closeAll closes every open journal (daemon shutdown / test cleanup).
func (j *agentJournals) closeAll() {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for name, l := range j.logs {
		_ = l.Close()
		delete(j.logs, name)
	}
}

// --- Server seams -----------------------------------------------------------

// agentJournalsFor returns the per-agent journal store, creating it on first
// use so a Server built without New (hermetic fixtures) still journals.
func (s *Server) agentJournalsFor() *agentJournals {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentJournals == nil {
		if s.stateDir == "" {
			return nil
		}
		s.agentJournals = newAgentJournals(s.stateDir)
	}
	return s.agentJournals
}

// journalAgentUserTurn records a turn addressed TO an agent before it is
// delivered, so the message is durable even if delivery, the provider, or the
// daemon dies immediately after. Mirrors the owner wire's journal-then-send.
func (s *Server) journalAgentUserTurn(name, text string) {
	s.journalAgentUserTurnAs(name, text, sendOriginOwner)
}

// journalAgentUserTurnAs records the turn together with who spoke it, so the
// sidebar transcript can tell the owner's words from an agent's report the
// same way main chat does (🎯T381). A turn addressed to a fleet agent carries
// no userTurnPrefix marker, so provenance has to travel on the line itself.
func (s *Server) journalAgentUserTurnAs(name, text, origin string) {
	if s.isOverseerAgent(name) {
		// The overseer's durable record is the owner chat journal; a second
		// copy here would double-paint main chat's own history.
		return
	}
	s.agentJournalsFor().appendUserAs(name, text, origin)
}

// journalAgentEvent records one agent event in that agent's durable journal.
// Called from DeliverInspectLive ahead of the subscriber check, so history is
// captured whether or not a sidebar happens to be watching.
func (s *Server) journalAgentEvent(name string, ev claudia.Event) {
	if s == nil || strings.TrimSpace(name) == "" || s.isOverseerAgent(name) {
		return
	}
	event, ok := inspectLiveEvent(ev)
	if !ok {
		return
	}
	if ev.Type == "user" {
		// Route through appendUser so the ACP echo of our own send is deduped.
		s.agentJournalsFor().appendUser(name, ev.Text)
		return
	}
	line, err := json.Marshal(event)
	if err != nil {
		return
	}
	s.agentJournalsFor().appendLine(name, string(line))
}

// agentJournalTurns returns the durable journal turns for name.
func (s *Server) agentJournalTurns(name string) ([]map[string]any, error) {
	return s.agentJournalsFor().turns(name)
}

// CloseAgentJournals closes every open per-agent journal (daemon shutdown).
func (s *Server) CloseAgentJournals() {
	if s == nil {
		return
	}
	s.mu.RLock()
	j := s.agentJournals
	s.mu.RUnlock()
	j.closeAll()
}

// --- Pure merge -------------------------------------------------------------

// turnKey identifies a turn by what the owner actually sees, so the same
// exchange recorded by both the provider session and the jevons journal
// collapses to one row.
func turnKey(t map[string]any) string {
	role, _ := t["role"].(string)
	text, _ := t["text"].(string)
	return role + "\x00" + strings.TrimSpace(text)
}

// renumberTurns rewrites turn_number so the merged list numbers turns from 1
// at each user boundary, matching what both sources produce alone.
func renumberTurns(turns []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(turns))
	n := 0
	for _, t := range turns {
		if t == nil {
			continue
		}
		if role, _ := t["role"].(string); role == "user" {
			n++
		}
		if n == 0 {
			n = 1
		}
		cp := make(map[string]any, len(t)+1)
		for k, v := range t {
			cp[k] = v
		}
		cp["turn_number"] = n
		out = append(out, cp)
	}
	return out
}

// mergeAgentTurns overlays the jevons-owned journal onto the provider session
// transcript so the sidebar shows the union, never the intersection.
//
// The journal is append-only and opened at a point in time, so its content can
// only ever overlap the session's TAIL — it never holds turns older than its
// first entry. Three cases follow from that:
//
//	session empty            → the journal is the whole record (provider store
//	                           missing, rotated, or never flushed).
//	journal ⊇ session        → the journal is the fuller record; use it whole
//	                           (a rotated session that kept only recent turns).
//	otherwise                → session first, then the journal turns that come
//	                           after its last overlap with the session (the
//	                           un-flushed tail — typically the owner's message
//	                           sent seconds before a reload or daemon bounce).
//
// Returns the merged turns and whether the journal contributed anything.
func mergeAgentTurns(sessionTurns, journalTurns []map[string]any) (merged []map[string]any, journalUsed bool) {
	if len(journalTurns) == 0 {
		return sessionTurns, false
	}
	if len(sessionTurns) == 0 {
		return renumberTurns(journalTurns), true
	}

	inSession := make(map[string]bool, len(sessionTurns))
	for _, t := range sessionTurns {
		inSession[turnKey(t)] = true
	}

	// Journal a superset of the session → it is the better base.
	superset := len(journalTurns) >= len(sessionTurns)
	if superset {
		inJournal := make(map[string]bool, len(journalTurns))
		for _, t := range journalTurns {
			inJournal[turnKey(t)] = true
		}
		for _, t := range sessionTurns {
			if !inJournal[turnKey(t)] {
				superset = false
				break
			}
		}
	}
	if superset {
		return renumberTurns(journalTurns), true
	}

	// Append only what follows the journal's last overlap with the session.
	lastOverlap := -1
	for i, t := range journalTurns {
		if inSession[turnKey(t)] {
			lastOverlap = i
		}
	}
	tail := journalTurns[lastOverlap+1:]
	if len(tail) == 0 {
		return sessionTurns, false
	}
	out := make([]map[string]any, 0, len(sessionTurns)+len(tail))
	out = append(out, sessionTurns...)
	out = append(out, tail...)
	return renumberTurns(out), true
}
