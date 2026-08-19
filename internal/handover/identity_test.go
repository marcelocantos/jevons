// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package handover_test

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/handover"
)

func TestT474HasMintIdentity(t *testing.T) {
	ok := handover.Pending{
		Agent: "jv-t444-phase-remint", Purpose: "work", WorkDir: "/work/jevons",
		NewSessionID: "prepared-session",
	}
	if !ok.HasMintIdentity() {
		t.Fatal("work identity with prepared session must be recoverable")
	}
	if ok.Delivered = true; ok.HasMintIdentity() {
		t.Fatal("delivered handover must not rebuild a mint")
	}
	thin := handover.Pending{Agent: "x", NewSessionID: "s"} // pre-T474 shape
	if thin.HasMintIdentity() {
		t.Fatal("pre-T474 record without purpose/workdir is not recoverable")
	}
	noSession := handover.Pending{Agent: "x", Purpose: "work", WorkDir: "/w"}
	if noSession.HasMintIdentity() {
		t.Fatal("identity without prepared session id is not recoverable")
	}
}

func TestT474FindBlockingRotation(t *testing.T) {
	pending := []handover.Pending{
		{Agent: "other", NewSessionID: "a"},
		{Agent: "jv-worker", Purpose: "work", NewSessionID: "b"},
		{Agent: "done", Purpose: "work", NewSessionID: "c", Delivered: true},
	}
	got, ok := handover.FindBlockingRotation(pending, "jv-worker")
	if !ok || got.NewSessionID != "b" {
		t.Fatalf("want undelivered jv-worker, got ok=%v %+v", ok, got)
	}
	if _, ok := handover.FindBlockingRotation(pending, "done"); ok {
		t.Fatal("delivered seed must not block reap")
	}
	if _, ok := handover.FindBlockingRotation(pending, "missing"); ok {
		t.Fatal("absent name must not block")
	}
}
