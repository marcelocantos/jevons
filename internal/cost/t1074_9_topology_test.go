// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"

	"github.com/marcelocantos/jevons/internal/brokerfixture"
)

// TestT10749TopologyTwoProcessesOverTheWire is the 🎯T1074.9 topology
// oracle. A real Claudia broker runs in its own process (daemon.New, built
// from the paired Claudia module) and owns claudia-control.db. This process
// owns Jevons' control-replica.db through OpenReplicaLinked, the call
// OpenReplica makes, linked to the broker's control socket through a
// frame-level fault proxy. The test asserts:
//
//   - each database file is open in exactly one process, its owner (lsof),
//     and neither state directory holds the other's file;
//   - usage and receipt facts the broker writes arrive in Jevons' replica
//     over the link, and a desired seat Jevons accepts reaches the broker,
//     whose reconcile answers with a seat status that comes back;
//   - a dropped broker changeset is not lost (the next changeset's sequence
//     gap ends the link and the redial's handshake delivers it), a severed
//     transport is redialled, and a duplicated changeset is neither a
//     second row nor a second usage call;
//   - after the broker restarts, the reconnect loop resumes delivery.
func TestT10749TopologyTwoProcessesOverTheWire(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	broker := brokerfixture.Start(t)
	proxySock := filepath.Join(filepath.Dir(broker.Socket), "proxy.sock.control")
	proxy, err := StartControlFaultProxy(proxySock, broker.ControlSocket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close() })

	jevonsState := t.TempDir()
	src, err := OpenReplicaLinked(ctx, jevonsState, proxySock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	store, err := OpenStore(filepath.Join(jevonsState, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	collector := NewCollector(&CollectorArgs{Store: store, Replica: src})

	replicaPath := filepath.Join(jevonsState, controlplane.JevonsDBFile)
	requireTopology(t, broker, replicaPath)
	if _, err := controlplane.OpenReplica(broker.ControlFile()); !errors.Is(err, controlplane.ErrNotOwner) {
		t.Fatalf("opening the broker's file as a replica = %v, want ErrNotOwner", err)
	}

	// Facts the broker writes reach Jevons' file over the link.
	broker.PublishUsage(topologyFact(1))
	broker.PublishReceipt(topologyReceipt("req-1"))
	waitReplica(t, src, "usage fact 1 and receipt req-1", func(db *sqlpipe.Database) bool {
		return countWhere(t, db, controlplane.UsageTable, "call_id = 'call-1'") == 1 &&
			countWhere(t, db, controlplane.ReceiptTable, "request_id = 'req-1'") == 1
	})
	got, err := collector.ReplicatedReceipt(ctx, topologyQuery("req-1"))
	if err != nil || got.RequestID != "req-1" || got.State != claudia.ReceiptAccepted {
		t.Fatalf("replicated receipt = %+v %v", got, err)
	}
	requireUsageCalls(t, collector, store, 1)

	// Dropped frame: the proxy swallows the changeset carrying fact 2, so
	// fact 2 is not in Jevons' file. The broker's next changeset (fact 3)
	// arrives with a sequence gap; the consumer's peer refuses it, the link
	// ends, the reconnect loop dials again, and the new handshake's diff
	// delivers both facts.
	proxy.DropChangesets(1)
	broker.PublishUsage(topologyFact(2))
	waitFor(t, "the proxy to drop one changeset", func() bool { d, _ := proxy.Faults(); return d == 1 })
	if err := src.Read(func(db *sqlpipe.Database) error {
		if n := countWhere(t, db, controlplane.UsageTable, "call_id = 'call-2'"); n != 0 {
			t.Fatalf("fact 2 arrived although its changeset was dropped (%d rows): the fault did not happen", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	broker.PublishUsage(topologyFact(3))
	waitReplica(t, src, "dropped fact 2 and fact 3 after the gap", func(db *sqlpipe.Database) bool {
		return countWhere(t, db, controlplane.UsageTable, "call_id IN ('call-2', 'call-3')") == 2
	})
	requireUsageCalls(t, collector, store, 3)

	// A severed transport: the proxy closes the connection, the link ends,
	// and the reconnect loop dials again. A fact published on the new link
	// arrives.
	proxy.Sever()
	broker.PublishUsage(topologyFact(7))
	waitReplica(t, src, "fact 7 after the transport was severed", func(db *sqlpipe.Database) bool {
		return countWhere(t, db, controlplane.UsageTable, "call_id = 'call-7'") == 1
	})
	requireUsageCalls(t, collector, store, 4)

	// Duplicated frame: fact 4's changeset arrives twice. It stays one row
	// and one usage call, including across repeated catch-ups.
	proxy.DuplicateChangesets(1)
	broker.PublishUsage(topologyFact(4))
	waitFor(t, "the proxy to duplicate one changeset", func() bool { _, d := proxy.Faults(); return d == 1 })
	waitReplica(t, src, "usage fact 4", func(db *sqlpipe.Database) bool {
		return countWhere(t, db, controlplane.UsageTable, "call_id = 'call-4'") == 1
	})
	requireUsageCalls(t, collector, store, 5)
	requireUsageCalls(t, collector, store, 5)

	// Jevons → broker: a desired seat accepted here (no grant, so nothing
	// launches) reaches the broker's file. The broker reconciles desired
	// rows at startup, so its restart answers with an "ungranted" status,
	// which comes back over the redialled link.
	if err := src.AcceptDesiredSeat(controlplane.DesiredSeat{
		Seat: "seat-topology", Generation: 1, Revision: 1, Provider: "grok", Model: "m",
		WorkDir: t.TempDir(), ConfigJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	// The broker learns of the row only over the link. Give the flush a
	// fact to ride behind so its arrival is observable here, then restart.
	broker.PublishUsage(topologyFact(5))
	waitReplica(t, src, "usage fact 5", func(db *sqlpipe.Database) bool {
		return countWhere(t, db, controlplane.UsageTable, "call_id = 'call-5'") == 1
	})
	firstPid := broker.Pid()
	broker.Restart()
	if broker.Pid() == firstPid {
		t.Fatalf("broker restart kept pid %d", firstPid)
	}
	waitReplica(t, src, "the restarted broker's reconcile status for the desired seat", func(db *sqlpipe.Database) bool {
		statuses, err := controlplane.SeatStatuses(db)
		return err == nil && statuses["seat-topology"].State == "ungranted"
	})
	broker.PublishUsage(topologyFact(6))
	waitReplica(t, src, "usage fact 6 from the restarted broker", func(db *sqlpipe.Database) bool {
		return countWhere(t, db, controlplane.UsageTable, "call_id = 'call-6'") == 1
	})
	requireTopology(t, broker, replicaPath)
	requireUsageCalls(t, collector, store, 7)
	if err := src.Read(func(db *sqlpipe.Database) error {
		if n := countWhere(t, db, controlplane.UsageTable, "1 = 1"); n != 7 {
			t.Fatalf("replica usage rows = %d, want 7", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// requireTopology asserts the two-database shape: the broker's file is open
// in the broker alone and Jevons' file in this process alone, and neither
// directory carries the other's file name.
func requireTopology(t *testing.T, broker *brokerfixture.Broker, replicaPath string) {
	t.Helper()
	for _, want := range []struct {
		path, owner string
		pid         int
	}{
		{broker.ControlFile(), "the broker", broker.Pid()},
		{replicaPath, "this process (jevons)", os.Getpid()},
	} {
		pids, err := brokerfixture.Holders(want.path)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(pids, []int{want.pid}) {
			t.Fatalf("%s is open in pids %v; want %s (pid %d) alone", want.path, pids, want.owner, want.pid)
		}
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(replicaPath), controlplane.ClaudiaDBFile)); !os.IsNotExist(err) {
		t.Fatalf("jevons state holds the broker's file name: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(broker.StateDir, controlplane.JevonsDBFile)); !os.IsNotExist(err) {
		t.Fatalf("broker state holds Jevons' file name: %v", err)
	}
}

func requireUsageCalls(t *testing.T, c *Collector, store *Store, want int64) {
	t.Helper()
	if err := c.CatchUpReplicas(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := store.StructuredUsage(context.Background(), time.Unix(0, 0), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got.Calls != want {
		t.Fatalf("usage calls = %d, want %d (%+v)", got.Calls, want, got)
	}
}

// topologyFact is a completed usage fact. A v0.58.0 broker's exit settle
// rewrote a started fact in place (same seq, cause added), which Claudia's
// transition contract refuses; Claudia's fix/usage-cancel-transition stops
// that rewrite, and TestReplicaCatchUpRecordsRejectedFactAndContinues covers
// catch-up over such a row. Neither is what this oracle measures.
func topologyFact(n int) claudia.UsageFact {
	return claudia.UsageFact{
		Seq: uint64(n), CallID: "call-" + strconv.Itoa(n), Seat: "seat-topology", SessionID: "generation-topology",
		Kind: "model", State: "completed", Provider: "openai", Model: "gpt-5.5",
		Requests: []claudia.UsageRequestRef{}, StartedAt: "2026-10-19T01:00:00Z", FinishedAt: "2026-10-19T01:00:01Z",
		CounterSource: "adapter_normalized_positive", Coverage: "engine_invocation",
	}
}

func topologyReceipt(id string) claudia.RequestReceipt {
	return claudia.RequestReceipt{
		RequestID: id, SessionID: "generation-topology", Seat: "seat-topology",
		State: claudia.ReceiptAccepted, AttemptID: "attempt-" + id, TurnIDs: []string{},
		AcceptedAt: "2026-10-19T01:00:00.000000000Z", UpdatedAt: "2026-10-19T01:00:00.000000000Z",
	}
}

func topologyQuery(id string) claudia.ReceiptQuery {
	return claudia.ReceiptQuery{Seat: "seat-topology", SessionID: "generation-topology", RequestID: id}
}

func countWhere(t *testing.T, db *sqlpipe.Database, table, where string) int {
	t.Helper()
	qr, err := db.Query("SELECT COUNT(*) FROM " + table + " WHERE " + where)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := qr.Rows[0][0].(int64)
	if !ok {
		t.Fatalf("count type %T", qr.Rows[0][0])
	}
	return int(n)
}

// waitReplica polls Jevons' file until cond holds. The test's -timeout is
// the clock; a stuck link fails there with this goroutine's stack.
func waitReplica(t *testing.T, src *ReplicaSource, what string, cond func(*sqlpipe.Database) bool) {
	t.Helper()
	waitFor(t, what, func() bool {
		ok := false
		if err := src.Read(func(db *sqlpipe.Database) error { ok = cond(db); return nil }); err != nil {
			t.Fatal(err)
		}
		return ok
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
