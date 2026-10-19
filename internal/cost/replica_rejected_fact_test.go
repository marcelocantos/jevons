// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package cost

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// rejectedFactAt builds a provider usage fact for one seat generation.
// Terminal facts carry known input and output counters.
func rejectedFactAt(seq uint64, call, state string, in, out int64) claudia.UsageFact {
	f := claudia.UsageFact{
		Seq: seq, CallID: call, Seat: "seat-rej", SessionID: "generation-rej",
		Kind: "model", State: state, Provider: "openai", Model: "gpt-5.5",
		Requests: []claudia.UsageRequestRef{}, StartedAt: "2026-10-19T01:00:00Z",
		CounterSource: "adapter_normalized_positive", Coverage: "engine_invocation",
	}
	if state != "started" {
		f.FinishedAt = "2026-10-19T01:00:01Z"
		f.Counters.InputTokens, f.Counters.OutputTokens = &in, &out
	}
	return f
}

// deliverUsageRows writes rows into Jevons' file as the link delivers them:
// a row at an existing (seat, session, seq) is replaced. Raw SQL stands in
// for the broker's delivery, as TestT10749CorruptStatusFailsCatchUp does.
func deliverUsageRows(t *testing.T, src *ReplicaSource, facts ...claudia.UsageFact) {
	t.Helper()
	if err := src.replica.Write(func(db *sqlpipe.Database) error {
		for _, f := range facts {
			body, err := json.Marshal(f)
			if err != nil {
				return err
			}
			if err := db.Exec(`INSERT INTO claudia_usage_fact (seat, session_id, seq, call_id, kind, fact_json)
				VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT(seat, session_id, seq) DO UPDATE SET call_id = excluded.call_id, kind = excluded.kind, fact_json = excluded.fact_json`,
				f.Seat, f.SessionID, int64(f.Seq), f.CallID, f.Kind, string(body)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type rejectedFactRig struct {
	src       *ReplicaSource
	store     *Store
	collector *Collector
}

func openRejectedFactRig(t *testing.T, state string) *rejectedFactRig {
	t.Helper()
	src, err := OpenReplicaLinked(context.Background(), state, "")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(state, "usage.db"))
	if err != nil {
		_ = src.Close()
		t.Fatal(err)
	}
	return &rejectedFactRig{src: src, store: store, collector: NewCollector(&CollectorArgs{Store: store, Replica: src})}
}

func (r *rejectedFactRig) close(t *testing.T) {
	t.Helper()
	if err := r.store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.src.Close(); err != nil {
		t.Fatal(err)
	}
}

// catchUpRejecting runs one catch-up pass and checks the monitor's view of
// the window holding the facts, then that the pass reported the rejected
// fact rather than passing clean.
func (r *rejectedFactRig) catchUpRejecting(t *testing.T, calls, incomplete, in, out int64) {
	t.Helper()
	err := r.collector.CatchUpReplicas(context.Background())
	now := time.Date(2026, 10, 19, 1, 2, 0, 0, time.UTC)
	mon := NewMonitor(&MonitorArgs{Store: r.store, Config: DefaultBudgetConfig,
		CollectorHealth: r.collector.Health, Now: func() time.Time { return now }})
	snap, serr := mon.Snapshot()
	if serr != nil {
		t.Fatal(serr)
	}
	u := snap.StructuredUsage
	if u.Calls != calls || u.IncompleteCalls != incomplete || u.Known.Input != in || u.Known.Output != out || u.ObservationFailures != 1 {
		t.Fatalf("monitor usage = calls %d incomplete %d in %d out %d failures %d; want calls %d incomplete %d in %d out %d failures 1 (catch-up: %v)",
			u.Calls, u.IncompleteCalls, u.Known.Input, u.Known.Output, u.ObservationFailures, calls, incomplete, in, out, err)
	}
	if err == nil || !strings.Contains(err.Error(), "seq 1") {
		t.Fatalf("catch-up error does not report the rejected fact: %v", err)
	}
	if snap.Collector == nil || !strings.Contains(snap.Collector.LastHistoryErr, "rejected") {
		t.Fatalf("monitor snapshot hides the rejection: %+v", snap.Collector)
	}
}

// TestReplicaCatchUpRecordsRejectedFactAndContinues reproduces the stall a
// v0.58.0 broker caused. Its exit settle rewrote a started usage fact at
// the same seq, as cancelled with a cause, which Claudia's transition
// contract refuses. Catch-up then stopped at that row, so every later fact
// of the generation went unseen. Now the rejected fact is an observation
// failure, visible on the monitor and the collector health, never a zero;
// later valid facts are ingested; nothing is counted twice across passes
// or a restart; and the abandoned call is not skipped for good.
func TestReplicaCatchUpRecordsRejectedFactAndContinues(t *testing.T) {
	state := t.TempDir()
	rig := openRejectedFactRig(t, state)
	deliverUsageRows(t, rig.src,
		rejectedFactAt(1, "call-A", "started", 0, 0),
		rejectedFactAt(2, "call-B", "started", 0, 0))
	if err := rig.collector.CatchUpReplicas(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The v0.58.0 rewrite of seq 1, then the journal's later facts.
	rewritten := rejectedFactAt(1, "call-A", "started", 0, 0)
	rewritten.State, rewritten.Cause, rewritten.FinishedAt = "cancelled", "sidecar exited", "2026-10-19T01:00:01Z"
	deliverUsageRows(t, rig.src,
		rewritten,
		rejectedFactAt(3, "call-B", "completed", 11, 3),
		rejectedFactAt(4, "call-C", "completed", 5, 2))

	// call-A stays started: incomplete, unknown, not a cancelled zero.
	rig.catchUpRejecting(t, 3, 1, 16, 5)
	rig.catchUpRejecting(t, 3, 1, 16, 5)

	rig.close(t)
	rig = openRejectedFactRig(t, state)
	t.Cleanup(func() { rig.close(t) })
	rig.catchUpRejecting(t, 3, 1, 16, 5)

	// A valid terminal fact for the abandoned call still lands.
	deliverUsageRows(t, rig.src, rejectedFactAt(5, "call-A", "completed", 7, 1))
	rig.catchUpRejecting(t, 3, 0, 23, 6)
}
