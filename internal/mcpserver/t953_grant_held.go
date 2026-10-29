// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T953 — a dead seat whose feed or sweep relaunch fails on grant_held must
// read stopped (never running: true) and its parent must hear promptly.

// FormatGrantHeldRelaunchNotice is the parent-facing line when a worker died
// and could not be relaunched because the broker still holds its grant.
func FormatGrantHeldRelaunchNotice(worker, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "broker protocol: grant_held"
	}
	return fmt.Sprintf("Seat %s was found dead and could not be relaunched (🎯T953): %s. "+
		"The fleet row is stopped until the grant clears or you remint: jevons_agent_kill name=%s force=true, then jevons_agent_start name=%s with its brief.",
		worker, detail, worker, worker)
}

func (s *Server) noticeGrantHeldRelaunch(parent, worker, occurrence, detail string) {
	if s == nil {
		return
	}
	text := FormatGrantHeldRelaunchNotice(worker, detail)
	s.logLifecycle(compAgentLifecycle, "grant_held_relaunch_notice", "ok", map[string]any{
		"parent": parent, "worker": worker, "occurrence": occurrence,
	})
	parent = strings.TrimSpace(parent)
	if parent != "" && parent != s.overseerName() {
		if _, err := s.deliverByName(parent, "[Fleet health] "+text, OriginAgent, false); err != nil {
			slog.Warn("grant_held relaunch notice to parent undelivered", "parent", parent, "worker", worker, "err", err)
		}
	}
	s.notifyFleetHealth(noticeAct, occurrence, text)
}

// recordGrantHeldRelaunchOutcome pins the row, records the refusal for
// rehydrate health, and tells the parent once per occurrence.
func (s *Server) recordGrantHeldRelaunchOutcome(name, detail, launchErr string) {
	if s == nil || (!fleet.GrantHeldText(detail) && !fleet.GrantHeldText(launchErr)) {
		return
	}
	fleet.NoteGrantHeldRelaunch(name)
	line := fleet.GrantHeldRelaunchDetail(detail, launchErr)
	if launchErr != "" || detail != "" {
		fleet.RecordRehydrateFailure(name, fmt.Errorf("%s", line))
	}
	parent := ""
	if s.registry != nil {
		if d := s.registry.Def(name); d != nil {
			parent = strings.TrimSpace(d.Parent)
		}
	}
	occurrence := "t953-grant-held:" + name + ":" + strings.TrimSpace(line)
	if s.noticeAlreadySubmitted(occurrence) {
		return
	}
	s.noticeGrantHeldRelaunch(parent, name, occurrence, line)
	s.markNoticeOutcome(occurrence, true, nil, time.Now())
}
