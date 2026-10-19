// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

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
	broker.PublishUsage(completedFact("seat-e", "call-e", 1))
	waitUntil(t, "call-e after the Jevons restart", func() bool { return replicaHas(t, restarted, "call-e") })
	requireCalls(t, ctx2, restarted, 5)
	requireHeldBy(t, filepath.Join(dir, controlplane.JevonsDBFile), os.Getpid())
	requireHeldBy(t, broker.ControlFile(), broker.Pid())
}

// TestT10749BrokerWriteDuringHandshakeKeepsFlowing is the regression for
// the link defects reported with 🎯T1074.9 and fixed in sqlpipe v0.33.0
// (Claudia v0.59.0). It writes on the broker while the guard's fresh link
// may still be in its handshake, with no wait for the link.
//
// Defect 1: Claudia delivered a row written during the handshake twice, in
// the handshake diff and again as changeset seq 1; the consumer's apply
// aborted on the conflict and the link ended and redialled. The test
// requires that no link ended.
//
// Defect 2: after that redial, the broker's next write (call-b) was
// committed in its own file but never sent as a changeset on the live
// link. The proxy counts the changeset the broker sends for call-b (and
// drops it, which the next changeset's gap would recover); one must arrive
// within the delivery budget of a live link, not a retry.
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
