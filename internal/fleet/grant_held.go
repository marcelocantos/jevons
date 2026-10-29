// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"strings"
	"sync"
)

// GrantHeld reports whether err is a broker grant_held refusal (stale grant
// still owned by another connection).
func GrantHeld(err error) bool {
	if err == nil {
		return false
	}
	return GrantHeldText(err.Error())
}

// GrantHeldText is GrantHeld for an already-formatted error string.
func GrantHeldText(s string) bool {
	return strings.Contains(s, "grant_held")
}

// GrantHeldRelaunchDetail is the stop reason / row text for a relaunch the
// broker refused with grant_held. Prefers the fleet feed's full detail when
// present.
func GrantHeldRelaunchDetail(detail, launchErr string) string {
	if strings.TrimSpace(detail) != "" {
		return strings.TrimSpace(detail)
	}
	return strings.TrimSpace(launchErr)
}

// grantHeldRelaunch pins seats whose last relaunch hit grant_held so broker
// liveness cannot paint them running while this daemon holds no process
// (🎯T953). Cleared on a successful relaunch.
var grantHeldRelaunch sync.Map // name -> struct{}

// NoteGrantHeldRelaunch records that name's relaunch is blocked on a stale
// broker grant until a launch succeeds.
func NoteGrantHeldRelaunch(name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	grantHeldRelaunch.Store(name, struct{}{})
}

// GrantHeldRelaunchBlocked reports whether name must not read as running
// after a grant_held relaunch refusal.
func GrantHeldRelaunchBlocked(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, ok := grantHeldRelaunch.Load(name)
	return ok
}

// ClearGrantHeldRelaunch forgets a grant_held pin after a successful launch.
func ClearGrantHeldRelaunch(name string) {
	if strings.TrimSpace(name) == "" {
		return
	}
	grantHeldRelaunch.Delete(name)
}
