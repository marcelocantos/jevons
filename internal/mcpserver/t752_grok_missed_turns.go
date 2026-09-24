// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/spool"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T752 — a Grok Build seat that says GOAL_STATUS: complete in its own
// transcript is heard.
//
// Specimen (2026-09-21): jv-t742-rule-scan-scope, session 01a0bff0. Its
// chat_history.jsonl held several finish-reports and whole GOAL_STATUS:
// complete lines; jevons_agent_report_read listed nothing, and "Continue the
// open objective" kept firing. Two independent faults, and fixing either one
// alone leaves the specimen intact:
//
//  1. THE FILE WAS NEVER PARSED. 🎯T744's scanMissedTurns hand-rolls a
//     Claude-shaped reader — message.content blocks, message.stop_reason, an
//     RFC3339 timestamp. A Grok record has none of those: the text is a
//     top-level content string, there is no stop_reason, and there is no
//     timestamp field at all, so every line was dropped three separate ways
//     and the recovery always found zero turns.
//  2. THE RECOVERY NEVER CLOSED THE GOAL. recoverMissedTurns delivered the
//     turn and stopped; clearSessionGoalIfComplete was reachable only from
//     agentEventSink's terminal-stop branch. So even a working parser would
//     have stored the report and left Continue firing — which is exactly the
//     pair of symptoms the specimen shows.
//
// The parse goes through internal/turnev, the one decoder in this repository
// that knows both providers' shapes (🎯T422 clause 1). A third parser here is
// the defect that package exists to end.

// GrokChatHistoryFile is the Grok Build transcript basename.
//
// 🎯T621 is noted and deliberately not followed here: it prefers updates.jsonl
// because chat_history.jsonl is the model-facing view and is rewritten on
// compact. This target is about the marker the seat wrote into chat_history,
// which is where the specimen's was, so that is the file read. The residue is
// real — a compact between the turn and the recovery loses the turn — and it is
// declared rather than hidden.
const GrokChatHistoryFile = "chat_history.jsonl"

// isGrokChatHistory reports whether path is a Grok Build transcript, which is
// how the recovery picks its parser. Keyed on the basename because that is the
// provider's own convention (~/.grok/sessions/<bucket>/<session>/…), not on a
// registry field an adopted or resumed seat may not carry.
func isGrokChatHistory(path string) bool {
	return filepath.Base(strings.TrimSpace(path)) == GrokChatHistoryFile
}

// scanGrokMissedTurns returns the completed assistant turns in a Grok
// chat_history.jsonl, oldest first.
//
// A Grok turn spans several assistant records — prose, a tool call, more prose
// — and the provider marks the mid-turn ones with a top-level tool_calls list
// rather than a stop_reason. So the turn boundary is "an assistant record that
// asks for no tools", and text accumulates up to it, mirroring agentEventSink.
// Without that segmentation every assistant record in the file concatenates
// into one enormous turn.
//
// A prompt resets the accumulator: a turn interrupted mid-tool-call never
// flushes, and its orphaned prose must not be prepended to the next report.
//
// At is whatever timestamp the record carried, which for Grok is the zero time
// — callers must not window Grok turns by time (see recoverGrokMissedTurns).
func scanGrokMissedTurns(path string) ([]missedTurn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []missedTurn
	var text strings.Builder
	for _, rec := range turnev.DecodeAll(f) {
		switch rec.Kind {
		case turnev.KindUserMessage, turnev.KindQueuedCommand:
			text.Reset()
		case turnev.KindAssistant:
			text.WriteString(rec.Text)
			if rec.HasToolUse {
				continue
			}
			body := strings.TrimSpace(text.String())
			text.Reset()
			if body == "" {
				continue
			}
			out = append(out, missedTurn{At: rec.Timestamp, Text: body})
		}
	}
	return out, nil
}

// recoverGrokMissedTurns is the Grok arm of the read-back: deliver the newest
// completed turn unless it is already this seat's latest stored report.
//
// 🎯T744's Claude arm windows by time, which a Grok record cannot do — it
// carries no timestamp. The dedupe is therefore by report BODY, the same
// question 🎯T747 answers on the delivery path: an identical body is not a new
// turn. That is what makes a re-attach idempotent here.
func (s *Server) recoverGrokMissedTurns(name, path string) int {
	turns, err := scanGrokMissedTurns(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("🎯T752 could not scan Grok chat history for missed turns",
				"agent", name, "path", path, "err", err)
		}
		return 0
	}
	if len(turns) == 0 {
		return 0
	}
	last := turns[len(turns)-1]
	if rec, err := agentreport.Latest(s.agentReportStateDir(), name); err == nil &&
		strings.TrimSpace(rec.Text) == strings.TrimSpace(last.Text) {
		return 0
	}
	slog.Warn("🎯T752 recovering a Grok turn the event stream never carried",
		"agent", name, "turns_in_transcript", len(turns))
	s.logLifecycle(compAgentLifecycle, "missed_turn", "recovered_grok", map[string]any{
		"agent": name, "turns_in_transcript": len(turns),
	})
	s.deliverRecoveredTurn(name, last, 1)
	return 1
}

// seatTranscriptPath is the file the read-back reads for a seat.
//
// A Grok ACP seat reports no JSONL path at all: claudia's grok backend never
// sets StartResult.JSONLPath, and the default computation (SessionJSONLPath) is
// hardcoded to Claude Code's ~/.claude/projects/<encoded-cwd>/<session>.jsonl
// convention. So proc.JSONLPath() is empty for exactly the seats this target is
// about, and the recovery used to skip them on that emptiness alone. Falling
// back to the session store resolves the Grok path by session id.
//
// MUST NOT be called while attachAgentSink holds wireMu — it takes s.mu, and
// that is the lock cycle that function's comment warns about. It is called
// from the recovery goroutine, off both locks.
func (s *Server) seatTranscriptPath(name string, proc *claudia.Agent) string {
	if s != nil && s.registry != nil {
		if def := s.registry.Def(name); def != nil &&
			spool.ResumeFromSpool(string(def.Provider), def.OMP) &&
			spool.SeatHasHistory(spool.Dir(), name) {
			// Sidecar seats have no vendor JSONL (🎯T866.4).
			return ""
		}
	}
	if proc != nil {
		if p := strings.TrimSpace(proc.JSONLPath()); p != "" {
			return p
		}
	}
	dir := s.grokSessionsDir()
	if dir == "" || s.registry == nil {
		return ""
	}
	def := s.registry.Def(name)
	if def == nil || strings.TrimSpace(def.SessionID) == "" {
		return ""
	}
	return discovery.ChatHistoryPath(dir, def.SessionID)
}

// SetGrokSessionsDir wires the Grok session store (~/.grok/sessions) so a seat
// whose process reports no transcript path can still be read back (🎯T752).
func (s *Server) SetGrokSessionsDir(dir string) {
	if s == nil {
		return
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return
	}
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grokSessions = dir
}

// grokSessionsDir reads the configured Grok session store ("" when unwired).
func (s *Server) grokSessionsDir() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.grokSessions
}
