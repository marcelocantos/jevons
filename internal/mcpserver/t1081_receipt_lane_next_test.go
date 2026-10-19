// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package mcpserver

import (
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/config"
)

// 🎯T1081 through the production replica read: the steer is admitted, and
// the seat pump publishes its receipt only after the send has returned, as
// Claudia's omp reader does. cost.ReplicatedReceipt must report the
// missing row as ErrReceiptAbsent for the await to read it again; a bare
// string error would silently restore the one-shot read.
func TestT1081SteerSettlesThroughReplicaWhenReceiptFollowsTheAck(t *testing.T) {
	f := newBindFixture(t, "T1081")
	receiptJournal(t, f.s, t.TempDir())
	lane := bindReceiptLane(t, f.s, t.TempDir())
	observeSenderFixture(f.s, f.name, f.probe)
	f.s.noteTurnInFlight(f.name)
	const payload = "Overseer here: answer in one word, then carry on."
	f.probe.check = func(_, id string) {
		go func() {
			time.Sleep(t1081ReplicationLag)
			lane.publish(f.name, f.session, id, claudiawire.ReceiptAccepted, claudiawire.ReceiptCauseNone, "", nil)
		}()
	}
	res, handled, err := f.s.escalateIfBusyWithRequestID(f.name, payload, config.EscalationOverseer, "jevons", f.probe, "")
	if err != nil || !handled || res.Status != "steered" {
		t.Fatalf("handled=%v status=%q err=%v message=%q", handled, res.Status, err, res.Message)
	}
	if entries, err := f.s.sendQueue().Snapshot(f.name); err != nil || len(entries) != 0 {
		t.Fatalf("settled steer left the queue: %+v %v", entries, err)
	}
}
