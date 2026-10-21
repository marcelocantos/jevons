// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"testing"

	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"

	"github.com/marcelocantos/jevons/internal/brokerfixture"
)

// TestT10749DisconnectedAcceptSurvivesRestarts accepts a desired seat and a
// stale stop on Jevons' own file while the broker is down, restarts Jevons
// (closes and reopens the replica), then brings the broker up. The reconnect
// loop's handshake delivers both rows to the broker's file; the broker's
// next start reconciles them (no grant, so nothing launches) and its status
// and the stale outcome come back over the link. A further broker restart
// on unchanged desired state adds no outcome and no second status row.
func TestT10749DisconnectedAcceptSurvivesRestarts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	broker := brokerfixture.Start(t)
	broker.Stop()
	dir := t.TempDir()

	src, err := OpenReplicaLinked(ctx, dir, broker.ControlSocket())
	if err != nil {
		t.Fatal(err)
	}
	if err := src.AcceptDesiredSeat(controlplane.DesiredSeat{
		Seat: "seat-a", Generation: 1, Revision: 1, Provider: "grok", Model: "m",
		WorkDir: t.TempDir(), ConfigJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := src.AcceptControlIntent(controlplane.ControlIntent{
		OperationID: "op-stale", Seat: "seat-a", Generation: 2, Kind: "stop",
	}); err != nil {
		t.Fatal(err)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}

	src, err = OpenReplicaLinked(ctx, dir, broker.ControlSocket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	if err := src.Read(func(db *sqlpipe.Database) error {
		if n := countWhere(t, db, controlplane.DesiredSeatTable, "seat = 'seat-a'"); n != 1 {
			t.Fatalf("desired seats after the Jevons restart = %d", n)
		}
		if n := countWhere(t, db, controlplane.IntentTable, "operation_id = 'op-stale'"); n != 1 {
			t.Fatalf("intents after the Jevons restart = %d", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	broker.Restart()
	// A broker fact arriving here means the handshake has run, and the
	// handshake is what carries Jevons' rows the other way.
	broker.PublishUsage(topologyFact(1))
	waitReplica(t, src, "the broker's first fact", func(db *sqlpipe.Database) bool {
		return countWhere(t, db, controlplane.UsageTable, "call_id = 'call-1'") == 1
	})
	broker.Restart()
	var status controlplane.SeatStatus
	var outcome controlplane.ControlOutcome
	waitReplica(t, src, "the reconciled status and the stale outcome", func(db *sqlpipe.Database) bool {
		statuses, err := controlplane.SeatStatuses(db)
		if err != nil {
			t.Fatal(err)
		}
		outcomes, err := controlplane.ControlOutcomes(db)
		if err != nil {
			t.Fatal(err)
		}
		status, outcome = statuses["seat-a"], outcomes["op-stale"]
		return status.State != "" && outcome.Outcome != ""
	})
	if status.State != "ungranted" || status.Generation != 1 {
		t.Fatalf("status = %+v", status)
	}
	if outcome.Outcome != "stale" || outcome.Reason != "generation" || outcome.Generation != 2 {
		t.Fatalf("outcome = %+v", outcome)
	}

	broker.Restart()
	broker.PublishUsage(topologyFact(2))
	waitReplica(t, src, "the third broker's fact", func(db *sqlpipe.Database) bool {
		return countWhere(t, db, controlplane.UsageTable, "call_id = 'call-2'") == 1
	})
	if err := src.Read(func(db *sqlpipe.Database) error {
		if n := countWhere(t, db, controlplane.OutcomeTable, "operation_id = 'op-stale'"); n != 1 {
			t.Fatalf("outcomes for op-stale = %d", n)
		}
		if n := countWhere(t, db, controlplane.StatusTable, "seat = 'seat-a'"); n != 1 {
			t.Fatalf("status rows for seat-a = %d", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
