// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/marcelocantos/claudia"
)

// The sentinel repair request and cockpit hook use the same entry point.
// An overlapping request joins the in-flight pass, not a second actuator run.
func TestReconcileConcurrentRequestsJoinOnePass(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	entered := make(chan struct{})
	release := make(chan struct{})
	var passes atomic.Int32
	s.reconcilePassHook = func() { passes.Add(1); close(entered); <-release }
	cockpitDone := make(chan struct{})
	go func() { s.Reconcile(); close(cockpitDone) }()
	<-entered
	joined := make(chan struct{})
	s.reconcileJoinHook = func() { close(joined) }
	sentinelDone := make(chan struct{})
	go func() { s.Reconcile(); close(sentinelDone) }()
	<-joined // The second request has observed the in-flight pass.
	select {
	case <-sentinelDone:
		t.Fatal("sentinel returned before the cockpit pass completed")
	default:
	}
	if got := passes.Load(); got != 1 {
		t.Fatalf("overlapping passes: %d", got)
	}
	close(release)
	<-cockpitDone
	<-sentinelDone
	if got := passes.Load(); got != 1 {
		t.Fatalf("duplicate pass: %d", got)
	}
	// A later tick is still allowed: exclusion is not a permanent throttle.
	s.reconcilePassHook = func() { passes.Add(1) }
	s.Reconcile()
	if got := passes.Load(); got != 2 {
		t.Fatalf("cadence lost after join: %d", got)
	}
}
