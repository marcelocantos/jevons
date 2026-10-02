// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/claudetrust"
)

// Kinds of wait a frontier target can be in. The cockpit paints both as the
// red arrow, but says different things about them: one is a plan that
// cannot take a seat, the other a seat that was minted and died before its
// opening brief landed.
const (
	// SeatWaitKindPlan: a spawn was refused because no plan can take a new
	// seat (🎯T980).
	SeatWaitKindPlan = "plan"
	// SeatWaitKindSeatFailed: a seat was minted for the target and retired
	// before it ever began a turn — unbriefed, stalled on startup, or its
	// launch failed outright (🎯T990). Before this kind existed the cockpit
	// had nothing to show for it: the row bounced from "acknowledged" back
	// to the green play arrow, and the only record of the two seats
	// jv-t989-mobile-webview lost on 2026-10-02 was the event journal.
	SeatWaitKindSeatFailed = "seat_failed"
)

// SeatWait is a frontier target whose worker could not be seated: a spawn
// for it was refused because no plan can take a seat (🎯T980), or the seat
// it got was retired before its opening brief landed (🎯T990).
type SeatWait struct {
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
	// Kind is SeatWaitKindPlan or SeatWaitKindSeatFailed. Empty reads as
	// plan for a client older than the field.
	Kind string `json:"kind,omitempty"`
	// Seat and Parent name the retired seat (seat_failed only).
	Seat   string `json:"seat,omitempty"`
	Parent string `json:"parent,omitempty"`
}

// seatWaits remembers the targets waiting for a seat, so the frontier play
// button can show pause and the reason instead of spinning. Play is only a
// nudge to prioritise a target: it never forces work onto plans that cannot
// take it (owner, 2026-10-01).
type seatWaits struct {
	mu sync.Mutex
	m  map[string]SeatWait
}

func (s *Server) noteSeatWait(target, reason string) {
	s.putSeatWait(target, SeatWait{Reason: strings.TrimSpace(reason), Kind: SeatWaitKindPlan})
}

// noteSeatFailure records that the seat minted for target died before it
// began a turn, so the frontier row says so instead of reverting to a
// green arrow as if nothing had been asked (🎯T990). reason is the
// operator-facing account; the cause, when the daemon has one, is already
// folded in by describeSeatFailure.
func (s *Server) noteSeatFailure(target, seat, parent, reason string) {
	s.putSeatWait(target, SeatWait{
		Reason: strings.TrimSpace(reason),
		Kind:   SeatWaitKindSeatFailed,
		Seat:   strings.TrimSpace(seat),
		Parent: strings.TrimSpace(parent),
	})
}

func (s *Server) putSeatWait(target string, w SeatWait) {
	target = normalizeAgentTargetID(target)
	if s == nil || target == "" {
		return
	}
	w.At = time.Now()
	s.seatWait.mu.Lock()
	defer s.seatWait.mu.Unlock()
	if s.seatWait.m == nil {
		s.seatWait.m = map[string]SeatWait{}
	}
	s.seatWait.m[target] = w
}

// seatFailureCauseMax bounds how much of a launch or delivery error the
// frontier tooltip quotes. The journal keeps the whole thing.
const seatFailureCauseMax = 240

// seatTrustConfigPath is the 🎯T709 config a Claude seat reads its
// workdir-trust answer from. A seam so a test can point it at a fixture
// instead of the owner's real ~/.claude.json.
var seatTrustConfigPath = claudetrust.ConfigPath

// describeSeatFailure renders why a seat for a target is gone, for the
// owner looking at the frontier row: which seat, under which parent, the
// removal reason and detail, the classified cause when the daemon has one,
// and — for a Claude seat whose workdir Claude Code has never been told to
// trust — the one diagnosis the daemon can make on its own. That modal is
// a human keypress no tmux pane can supply; it is what held both
// jv-t989-mobile-webview seats on 2026-10-02, and nothing told the owner.
func describeSeatFailure(def *claudia.AgentDef, name, removalReason, removalDetail string, cause error) string {
	var b strings.Builder
	parent := ""
	if def != nil {
		parent = strings.TrimSpace(def.Parent)
	}
	fmt.Fprintf(&b, "seat %s", name)
	if parent != "" {
		fmt.Fprintf(&b, " (parent %s)", parent)
	}
	fmt.Fprintf(&b, " retired: %s — %s", removalReason, removalDetail)
	if cause != nil {
		text := strings.TrimSpace(cause.Error())
		if len(text) > seatFailureCauseMax {
			text = text[:seatFailureCauseMax] + "…"
		}
		fmt.Fprintf(&b, "; cause (%s): %s", agenterr.ClassifyText(cause.Error()), text)
	}
	if def != nil && def.Provider == claudia.ProviderClaude && strings.TrimSpace(def.WorkDir) != "" {
		if path := seatTrustConfigPath(); path != "" {
			doc, err := os.ReadFile(path)
			if err == nil && !claudetrust.Accepted(doc, def.WorkDir) {
				fmt.Fprintf(&b, "; Claude Code has not trusted workdir %s (no hasTrustDialogAccepted in %s) — "+
					"a seat there opens on the trust dialog, which no automated process can answer; "+
					"accept it once by hand or record the answer in that file", def.WorkDir, path)
			}
		}
	}
	return b.String()
}

// ClearSeatWait forgets target's wait: a worker engaged it, or the owner
// stopped the request.
func (s *Server) ClearSeatWait(target string) {
	target = normalizeAgentTargetID(target)
	if s == nil || target == "" {
		return
	}
	s.seatWait.mu.Lock()
	defer s.seatWait.mu.Unlock()
	delete(s.seatWait.m, target)
}

// SeatWaits is a copy of the targets waiting for a seat.
func (s *Server) SeatWaits() map[string]SeatWait {
	out := map[string]SeatWait{}
	if s == nil {
		return out
	}
	s.seatWait.mu.Lock()
	defer s.seatWait.mu.Unlock()
	for k, v := range s.seatWait.m {
		out[k] = v
	}
	return out
}

// handleSeatWaits serves GET /api/seat-waits.
func (s *Server) handleSeatWaits(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.SeatWaits())
}
