// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"errors"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/provider"
)

func TestT607PlanCockpitWaitsDuringAuthBackoff(t *testing.T) {
	o := cockpitObs{Registered: true, ProcAlive: false, AuthLaunchBlocked: true}
	if got := planCockpit(o, 0, 8, 0); got != cockpitAuthWait {
		t.Fatalf("got phase %d, want auth wait", got)
	}
	o.AuthLaunchBlocked = false
	if got := planCockpit(o, 0, 8, 0); got != cockpitLaunch {
		t.Fatalf("without auth block got %d, want launch", got)
	}
}

func TestT607AuthLaunchFailureArmsBackoff(t *testing.T) {
	st := &cockpitState{}
	now := time.Now()
	d := st.noteAuthLaunchFailure(now, errAuthFixture())
	if d < provider.AuthLaunchBackoff.Base {
		t.Fatalf("delay %v too short", d)
	}
	if !st.authLaunchBlocked(now.Add(d / 2)) {
		t.Fatal("should block mid-backoff")
	}
	if st.authLaunchBlocked(now.Add(d + time.Second)) {
		t.Fatal("should be open after backoff")
	}
	d2 := st.noteAuthLaunchFailure(now.Add(d+time.Second), errAuthFixture())
	if d2 <= d {
		t.Fatalf("second delay %v should grow from %v", d2, d)
	}
}

func errAuthFixture() error {
	return errors.New("acp session/new: Authentication required")
}
