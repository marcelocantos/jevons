// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T828: arming fault-t664-unconfirmed makes a plain stop always refuse
// with the product T664 text; force=true still stops.
func TestT828FaultForcesStopRefusal(t *testing.T) {
	const name = "jv-t828-forced"
	dir := t.TempDir()
	reg := t439Registry(t, name)
	s := &Server{registry: reg}
	s.EnableT664FaultSeam(dir)
	if err := os.WriteFile(filepath.Join(dir, t664UnconfirmedFaultFile), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := s.handleAgentStop(context.Background(), t664Call(name, "jevons", false))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("armed fault must refuse plain stop: %s", toolText(res))
	}
	text := toolText(res)
	for _, want := range []string{"refusing to stop", "delivered_unconfirmed", "T664", "jevons_transcript_read"} {
		if !strings.Contains(text, want) {
			t.Fatalf("refusal missing %q: %s", want, text)
		}
	}

	res, err = s.handleAgentStop(context.Background(), t664Call(name, "jevons", true))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("force=true must stop after fault arm: %s", toolText(res))
	}
}

// 🎯T828 control: with the guard disabled the plain stop goes through, so a
// journey that requires the refusal arm fails when the product guard is gone.
func TestT828ControlDisableGuardAllowsPlainStop(t *testing.T) {
	const name = "jv-t828-control"
	dir := t.TempDir()
	reg := t439Registry(t, name)
	s := &Server{registry: reg}
	s.EnableT664FaultSeam(dir)
	if err := os.WriteFile(filepath.Join(dir, t664UnconfirmedFaultFile), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, t664DisableGuardFile), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := s.handleAgentStop(context.Background(), t664Call(name, "jevons", false))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("disabled guard must allow plain stop (control RED shape for J8b): %s", toolText(res))
	}
}

// Production never looks at the fault files unless the seam is enabled.
func TestT828FaultIgnoredWithoutSeam(t *testing.T) {
	const name = "jv-t828-no-seam"
	dir := t.TempDir()
	reg := t439Registry(t, name)
	s := &Server{registry: reg} // seam not enabled
	if err := os.WriteFile(filepath.Join(dir, t664UnconfirmedFaultFile), []byte(name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := s.handleAgentStop(context.Background(), t664Call(name, "jevons", false))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("without EnableT664FaultSeam a fault file must not refuse: %s", toolText(res))
	}
}
