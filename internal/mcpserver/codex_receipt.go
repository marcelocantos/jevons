// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver, no cgo

	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// Delivery receipts for codex app-server seats (🎯T768).
//
// A drain that submits successfully but sees no session event within the
// confirmation window resolves Uncertain. For a codex seat that is the normal
// case, not the exception: codex queues input behind a running turn, so the
// event arrives only when that turn ends. And a codex seat keeps no transcript
// on disk, so none of the three instruments 🎯T416 calls reliable was
// available to settle it later. On 2026-09-21 jevons-po accumulated four such
// entries in fourteen minutes; every one of them had in fact been delivered.
//
// What codex does keep is its own session log. Each accepted input is logged
// by the session loop as `Submission … op: TurnInput { … UserInput … }` on the
// seat's own thread. That is a receiver-side record of user input — payload
// match at user-message level, the first of 🎯T416's instruments — so an
// Uncertain entry whose exact payload appears there is Confirmed.
//
// Absence proves nothing: the rows are debug-level and codex may prune them.
// An entry with no receipt stays Uncertain, held, and is never resent.

// codexHomeDir mirrors claudia's exclusiveCodexHomeDir (claudia
// mcp_exclusive.go): the durable CODEX_HOME claudia gives each codex thread.
// Duplicated because claudia does not export it (still unexported at the
// v0.52.0 pin, 🎯T1008). If claudia moves the directory this finds nothing
// and entries stay Uncertain, which is the safe failure. The Claudia API
// this waits for is claudia/T194: an exported ExclusiveCodexHomeDir, or
// claudia answering codex delivery-receipt questions itself, typed,
// instead of jevons reading its layout.
func codexHomeDir(sessionID string) string {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return ""
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		state = filepath.Join(os.Getenv("HOME"), ".local", "state")
	}
	return filepath.Join(state, "claudia", "codex-homes", sid)
}

// rustDebugString renders s the way Rust's `{:?}` renders a String's
// contents, which is how codex's session log records the input text.
func rustDebugString(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// codexReceipts confirms Uncertain entries whose exact payload the codex
// seat's own session loop logged as user input on thread, at or after the
// entry was enqueued. Each log row can confirm at most one entry, so two
// held entries with identical text need two submissions. It returns the ids
// it confirmed.
func codexReceipts(q *sendq.Store, name, home, thread string, now time.Time) ([]string, error) {
	entries, err := q.Snapshot(name)
	if err != nil {
		return nil, err
	}
	var held []sendq.Entry
	for _, e := range entries {
		if e.State == sendq.Uncertain {
			held = append(held, e)
		}
	}
	if len(held) == 0 || home == "" || thread == "" {
		return nil, nil
	}
	path := filepath.Join(home, "logs_2.sqlite")
	if _, err := os.Stat(path); err != nil {
		return nil, nil // no log to read is not evidence of anything
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`select id, ts, feedback_log_body from logs
		where thread_id = ? and feedback_log_body like '%op: TurnInput%' order by ts, id`, thread)
	if err != nil {
		return nil, fmt.Errorf("codex receipt: read %s: %w", path, err)
	}
	type submission struct {
		id   int64
		at   time.Time
		body string
	}
	var subs []submission
	for rows.Next() {
		var sub submission
		var ts int64
		if err := rows.Scan(&sub.id, &ts, &sub.body); err != nil {
			rows.Close()
			return nil, err
		}
		sub.at = time.Unix(ts, 0)
		subs = append(subs, sub)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	used := map[int64]bool{}
	var confirmed []string
	for _, e := range held {
		want := rustDebugString(e.Text)
		for _, sub := range subs {
			// Enqueue time truncated to the log's one-second resolution.
			if used[sub.id] || sub.at.Before(e.EnqueuedAt.Truncate(time.Second)) || !strings.Contains(sub.body, want) {
				continue
			}
			evidence := fmt.Sprintf("codex session log %s row %d at %s: TurnInput submission on thread %s carries this exact payload (🎯T768)",
				path, sub.id, sub.at.Format(time.RFC3339), thread)
			if _, err := q.Reconcile(name, e.ID, e.AttemptID, sendq.ReconcileConfirmed, "daemon:codex-receipt", evidence, now); err != nil {
				return confirmed, err
			}
			used[sub.id] = true
			confirmed = append(confirmed, e.ID)
			break
		}
	}
	return confirmed, nil
}

// confirmCodexReceipts settles a codex seat's Uncertain entries from its own
// session log. Codex prunes TurnInput rows after ~30 minutes (🎯T772), so a
// check that waits for the next drain loses the receipt on an idle seat.
// Call sites (no new standing loop):
//   - when an entry first resolves Uncertain (drain path)
//   - at the start of a drain (existing turn boundary)
//   - on the backlog sweep cadence while any remain held (SweepSendBacklogs)
// It returns how many entries it confirmed.
func (s *Server) confirmCodexReceipts(name string) int {
	if s == nil || s.registry == nil {
		return 0
	}
	d := s.registry.Def(name)
	if d == nil || seatprovider.Resolve(d.Provider).Plan != seatprovider.Codex || d.SessionID == "" {
		return 0
	}
	ids, err := codexReceipts(s.sendQueue(), name, codexHomeDir(d.SessionID), d.SessionID, time.Now())
	if err != nil {
		slog.Warn("agent send queue: codex receipt check failed; entries stay held", "name", name, "err", err)
		return 0
	}
	if len(ids) > 0 {
		slog.Info("agent send queue: confirmed held entries from codex session log",
			"name", name, "entries", ids)
		if _, blocked, err := s.sendQueue().BlockedHead(name); err == nil && !blocked {
			s.clearSendqPin(name)
		}
	}
	return len(ids)
}
