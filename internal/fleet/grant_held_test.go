// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"testing"
)

func TestGrantHeldDetectsBrokerRefusal(t *testing.T) {
	err := errors.New(`grant_held (name="w"): grant w is owned by another connection`)
	if !GrantHeld(err) {
		t.Fatal("want grant_held detected")
	}
	if GrantHeld(nil) || GrantHeld(errors.New("not_owner")) {
		t.Fatal("want only grant_held")
	}
}

func TestGrantHeldRelaunchPinClearsOnSuccess(t *testing.T) {
	const name = "jv-t953-pin"
	t.Cleanup(func() { ClearGrantHeldRelaunch(name) })
	NoteGrantHeldRelaunch(name)
	if !GrantHeldRelaunchBlocked(name) {
		t.Fatal("want pin after note")
	}
	ClearGrantHeldRelaunch(name)
	if GrantHeldRelaunchBlocked(name) {
		t.Fatal("want pin cleared after success")
	}
}
