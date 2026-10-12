// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 🎯T828 — isolate journeys force the T664 stop-refusal precondition.
//
// J8b must prove the refusal arm runs. A real send is sometimes already
// decided by the time the journey stops the worker, so the soft "if plain
// stop is refused…" branch went green without ever reading the T664 text.
// The seam lets a journey put a named seat into delivered_unconfirmed on
// purpose, the same way T811 arms a broker not_owner refusal: a state-dir
// file, only when main wires the seam under JEVONS_TEST_FAULTS=1 off the
// development port. A second file disables the guard entirely so a control
// run fails at the refusal assertion when the product path is skipped.

const (
	t664UnconfirmedFaultFile = "fault-t664-unconfirmed"
	t664DisableGuardFile     = "fault-t664-disable-guard"
)

var t664FaultMu sync.Mutex

// EnableT664FaultSeam arms the file-driven T664 journey seam under dir.
// Production daemons never call this.
func (s *Server) EnableT664FaultSeam(dir string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.t664FaultDir = strings.TrimSpace(dir)
	s.mu.Unlock()
}

// applyT664Faults runs under stop / kill before the product guard. An armed
// unconfirmed file names seats that must look undecided; a disable file
// short-circuits the guard so the control journey cannot go green.
func (s *Server) applyT664Faults(name string) (skipGuard bool) {
	if s == nil {
		return false
	}
	s.mu.Lock()
	dir := s.t664FaultDir
	s.mu.Unlock()
	if dir == "" || strings.TrimSpace(name) == "" {
		return false
	}

	t664FaultMu.Lock()
	disabledRaw, _ := os.ReadFile(filepath.Join(dir, t664DisableGuardFile))
	disabled := len(strings.TrimSpace(string(disabledRaw))) > 0
	unconfirmedRaw, unconfirmedErr := os.ReadFile(filepath.Join(dir, t664UnconfirmedFaultFile))
	t664FaultMu.Unlock()

	if disabled {
		return true
	}
	if unconfirmedErr != nil {
		return false
	}
	for _, line := range strings.Split(string(unconfirmedRaw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line != name {
			continue
		}
		// Inject through the product ledger so StopGuard / FormatStopRefusal
		// take the real path — not a parallel refusal string.
		s.noteUnconfirmedSendAt(name, "t828 fault-armed undecided delivery", time.Now())
		return false
	}
	return false
}
