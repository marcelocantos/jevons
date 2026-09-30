// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T970: a row registered minutes before its process exists is starting,
// not stopped. On 2026-09-30 four workers waited 90–116 s on startMu behind
// other launches and read "unknown: process exited and no reason was
// recorded" the whole time. A start that fails says so as its reason.
func TestT970StartingCoversTheWaitOnStartMu(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, nil, nil)
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	s.SetRegistry(reg)
	release := make(chan struct{})
	s.launchAgentFn = func(context.Context, string) (*claudia.Agent, error) {
		<-release
		return nil, errors.New("provider refused the launch")
	}
	const name = "jv-t970-queued"

	// Another launch holds startMu, as it did for the four workers.
	s.startMu.Lock()
	done := make(chan error, 1)
	go func() { done <- s.spawnFrontierWorker(name, t.TempDir(), "jevons-po", "T970", "brief") }()
	for reg.Def(name) == nil {
		time.Sleep(5 * time.Millisecond) // `go test -timeout` is the clock
	}
	if !s.AgentStarting(name) {
		t.Fatal("a registered row waiting on startMu does not read as starting")
	}
	if reason, _, ok := s.SeatStopShown(name); ok {
		t.Fatalf("a starting seat has a stop reason: %q", reason)
	}

	s.startMu.Unlock()
	close(release)
	if err := <-done; err == nil {
		t.Fatal("the refused launch reported success")
	}
	if s.AgentStarting(name) {
		t.Fatal("the seat still reads as starting after its start returned")
	}
	reason, _, ok := s.SeatStopShown(name)
	if !ok || !strings.HasPrefix(reason, "start failed: ") || !strings.Contains(reason, "provider refused the launch") {
		t.Fatalf("stop reason after a failed start = %q, %v; want the failure", reason, ok)
	}
}
