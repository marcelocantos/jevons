// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"

	"github.com/marcelocantos/jevons/internal/brokerfixture"
	"github.com/marcelocantos/jevons/internal/cost"
)

// TestT10749LostFrameRecoversAndDuplicateIngestsOnce drives startCostGuard's
// replica through a frame-level fault proxy on the wire to a real broker
// process. The guard dials the proxy as if it were the broker
// (CLAUDIA_BROKER_SOCKET names the proxy's directory). A dropped broker
// changeset is recovered when the next changeset's gap ends the link and the
// redial's handshake delivers it; a duplicated changeset is one usage call.
// Restarting the guard (a Jevons restart) replays every row in its file and
// still adds no usage.
func TestT10749LostFrameRecoversAndDuplicateIngestsOnce(t *testing.T) {
	broker := brokerfixture.Start(t)
	proxyDir := filepath.Dir(broker.Socket)
	proxyBroker := filepath.Join(proxyDir, "proxy.sock")
	proxy, err := cost.StartControlFaultProxy(claudia.ControlSocketPath(proxyBroker), broker.ControlSocket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close() })
	t.Setenv("CLAUDIA_NO_BROKER", "0")
	t.Setenv("CLAUDIA_BROKER_SOCKET", proxyBroker)
	dir := t.TempDir()

	ctx, stop := context.WithCancel(context.Background())
	guard, _, _ := startTestCostGuard(t, ctx, dir)
	epoch := awaitLinkEstablished(t, guard.replica, 0)
	broker.PublishUsage(completedFact("seat-a", "call-a", 1))
	waitUntil(t, "call-a", func() bool { return replicaHas(t, guard, "call-a") })
	requireCalls(t, ctx, guard, 1)

	proxy.DropChangesets(1)
	broker.PublishUsage(completedFact("seat-b", "call-b", 1))
	waitUntil(t, "the proxy to drop a changeset", func() bool { d, _ := proxy.Faults(); return d == 1 })
	if replicaHas(t, guard, "call-b") {
		t.Fatal("call-b arrived although its changeset was dropped")
	}
	broker.PublishUsage(completedFact("seat-c", "call-c", 1))
	waitUntil(t, "dropped call-b and call-c", func() bool {
		return replicaHas(t, guard, "call-b") && replicaHas(t, guard, "call-c")
	})
	requireCalls(t, ctx, guard, 3)
	awaitLinkEstablished(t, guard.replica, epoch) // the redial the gap forced

	proxy.DuplicateChangesets(1)
	broker.PublishUsage(completedFact("seat-d", "call-d", 1))
	waitUntil(t, "the proxy to duplicate a changeset", func() bool { _, d := proxy.Faults(); return d == 1 })
	waitUntil(t, "call-d", func() bool { return replicaHas(t, guard, "call-d") })
	requireCalls(t, ctx, guard, 4)
	requireCalls(t, ctx, guard, 4)

	// Jevons restart: the guard's context ends, its replica and store close,
	// and a new guard opens the same files and ingests every row again.
	stop()
	if err := guard.replica.Close(); err != nil {
		t.Fatal(err)
	}
	if err := guard.store.Close(); err != nil {
		t.Fatal(err)
	}
	ctx2, stop2 := context.WithCancel(context.Background())
	defer stop2()
	restarted, _, _ := startTestCostGuard(t, ctx2, dir)
	t.Cleanup(func() { _ = restarted.replica.Close() })
	requireCalls(t, ctx2, restarted, 4)
	awaitLinkEstablished(t, restarted.replica, 0)
	broker.PublishUsage(completedFact("seat-e", "call-e", 1))
	waitUntil(t, "call-e after the Jevons restart", func() bool { return replicaHas(t, restarted, "call-e") })
	requireCalls(t, ctx2, restarted, 5)
	requireHeldBy(t, filepath.Join(dir, controlplane.JevonsDBFile), os.Getpid())
	requireHeldBy(t, broker.ControlFile(), broker.Pid())
}

// TestT10749BrokerWriteDuringHandshakeKeepsFlowing reproduces the Claudia
// link defects reported with 🎯T1074.9, with no link-established
// precondition. It is expected RED until the Claudia link is fixed; it is
// the reproduction the Claudia fix is checked against.
//
// Defect 1 (every run observed): the broker writes call-a while the guard's
// fresh link is still in its handshake. Claudia delivers that row twice, in
// the handshake diff and again as changeset seq 1; the consumer's apply
// aborts on the conflict ("changeset_apply: not an error") and the link
// ends and redials. The test reports that ended link and carries on.
//
// Defect 2 (timing-dependent; seen on every run of an earlier shape of
// TestT10749LostFrameRecoversAndDuplicateIngestsOnce): after that redial,
// whose diff re-carries call-a, the broker's next write (call-b) is
// committed in its own file but never sent as a changeset on the live
// link; the broker's _sqlpipe_meta master_seq stays 1, and a Jevons write
// that gives the broker a frame to answer draws only an Ack. The proxy
// counts the changeset the broker sends for call-b (and drops it, which the
// next changeset's gap would recover); none arrives. The bound is the
// delivery budget of a live link, not a retry.
func TestT10749BrokerWriteDuringHandshakeKeepsFlowing(t *testing.T) {
	broker := brokerfixture.Start(t)
	proxyBroker := filepath.Join(filepath.Dir(broker.Socket), "proxy.sock")
	proxy, err := cost.StartControlFaultProxy(claudia.ControlSocketPath(proxyBroker), broker.ControlSocket())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proxy.Close() })
	t.Setenv("CLAUDIA_NO_BROKER", "0")
	t.Setenv("CLAUDIA_BROKER_SOCKET", proxyBroker)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	guard, _, _ := startTestCostGuard(t, ctx, t.TempDir())

	broker.PublishUsage(completedFact("seat-a", "call-a", 1))
	waitUntil(t, "call-a", func() bool { return replicaHas(t, guard, "call-a") })
	requireCalls(t, ctx, guard, 1)
	if st := guard.replica.LinkState(); st.Ended != 0 {
		t.Errorf("defect 1: a broker write during the handshake ended the link %d time(s) (last: %s)", st.Ended, st.DownReason)
	}

	// The proxy is told to drop the next broker changeset, so the test sees
	// whether the broker sends one at all for call-b.
	proxy.DropChangesets(1)
	broker.PublishUsage(completedFact("seat-b", "call-b", 1))
	deadline := time.Now().Add(liveLinkDeliveryBudget)
	for {
		if dropped, _ := proxy.Faults(); dropped == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("defect 2: the broker sent no changeset for call-b on the live link within %s (link state %+v)",
				liveLinkDeliveryBudget, guard.replica.LinkState())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// liveLinkDeliveryBudget bounds how long a live link may take to deliver a
// committed broker write. Delivery is one changeset frame on a local socket.
const liveLinkDeliveryBudget = 10 * time.Second

// awaitLinkEstablished is brokerfixture.AwaitLinkEstablished for a
// replica; see there for why these tests wait for it.
func awaitLinkEstablished(t *testing.T, src *cost.ReplicaSource, after uint64) uint64 {
	t.Helper()
	return brokerfixture.AwaitLinkEstablished(t, func() (bool, uint64) {
		st := src.LinkState()
		return st.Linked, st.Epoch
	}, after)
}
