// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// 🎯T661 — a seat whose session carries a record over the broker's 1 MiB
// line limit stays reachable.
//
// On 2026-09-15 jv-t657-steer-ui's session held three tool_result records
// over 1 MiB (screenshot PNGs). jevons_transcript_read died on
// bufio.Scanner: token too long (fixed by transcript.ReadLogical's bounded
// reader), and jevons_agent_send answered a 1.2 KB message with "broker
// protocol: malformed: message exceeds the 1048576-byte line limit" — the
// receiver's session state, not the payload, was what the broker choked
// on. A PO with a confused worker had no tool that worked on it and no
// tool that said why. This file is the why: agent_list marks the seat, and
// a send that dies at the wire names the oversized records and the
// recovery instead of the bare protocol error.

// oversizedEntry caches one session file's census by size and mtime, so a
// fleet list does not rescan a large session on every call.
type oversizedEntry struct {
	size  int64
	mod   time.Time
	lines []transcript.OversizedLine
}

// seatOversized reports the records over transcript.BrokerLineLimit in d's
// current session file under roots. A file smaller than the limit cannot
// hold one, so the common case is a single stat.
func (s *Server) seatOversized(d claudia.AgentDef, roots discovery.Roots) []transcript.OversizedLine {
	path := AgentTranscriptPath(d, roots)
	if path == "" {
		return nil
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() <= transcript.BrokerLineLimit {
		return nil
	}
	if s != nil {
		s.mu.Lock()
		e, ok := s.oversizedSessions[path]
		s.mu.Unlock()
		if ok && e.size == st.Size() && e.mod.Equal(st.ModTime()) {
			return e.lines
		}
	}
	lines, err := transcript.ScanOversized(path)
	if err != nil {
		return nil
	}
	if s != nil {
		s.mu.Lock()
		if s.oversizedSessions == nil {
			s.oversizedSessions = map[string]oversizedEntry{}
		}
		s.oversizedSessions[path] = oversizedEntry{size: st.Size(), mod: st.ModTime(), lines: lines}
		s.mu.Unlock()
	}
	return lines
}

// FormatOversizedSeatLine is the agent_list annotation for a seat whose
// session the broker wire cannot carry.
func FormatOversizedSeatLine(name string, lines []transcript.OversizedLine) string {
	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf("oversized-session: %d record(s) over %d bytes in %q's session (%s) — "+
		"sends to this seat fail at the broker wire until the seat is re-minted: "+
		"jevons_agent_kill then jevons_agent_start with the same name (🎯T661; claudia 🎯T73 bounds the wire)",
		len(lines), transcript.BrokerLineLimit, name, describeOversized(lines))
}

// IsBrokerLineLimitError recognises claudia's broker refusal of a line over
// the wire cap, by text — the pin has no typed error for it.
func IsBrokerLineLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "line limit") ||
		strings.Contains(msg, fmt.Sprintf("%d-byte", transcript.BrokerLineLimit))
}

// OversizedSendAdvice replaces the bare broker error when the receiver's
// session, not the payload, is what exceeded the wire.
func OversizedSendAdvice(name string, payloadBytes int, lines []transcript.OversizedLine, err error) string {
	return fmt.Sprintf("send to %q failed at the broker wire (%v). The %d-byte payload is not the problem: "+
		"%q's session carries %d record(s) over %d bytes (%s) and the broker refuses the line. "+
		"Recovery: re-mint the seat — jevons_agent_kill %q then jevons_agent_start with the same name "+
		"(the overseer's kill discards a held sendq, 🎯T599). Do not retry the send; it cannot land (🎯T661).",
		name, err, payloadBytes, name, len(lines), transcript.BrokerLineLimit, describeOversized(lines), name)
}

func describeOversized(lines []transcript.OversizedLine) string {
	parts := make([]string, 0, len(lines))
	for i, l := range lines {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("and %d more", len(lines)-3))
			break
		}
		parts = append(parts, l.String())
	}
	return strings.Join(parts, ", ")
}
