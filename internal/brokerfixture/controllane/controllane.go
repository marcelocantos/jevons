// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package controllane is the receipt lane fleet and mcpserver tests read
// through: a real broker process (brokerfixture) publishes receipts into its
// own file, and Jevons' replica, opened as startCostGuard opens it, receives
// them over the control socket. The collector's ReplicatedReceipt is the
// query those tests install. Only tests import this package.
package controllane

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/brokerfixture"
	"github.com/marcelocantos/jevons/internal/cost"
)

// arrivalTimeout bounds how long a published receipt may take to reach
// Jevons' file over the link.
const arrivalTimeout = 30 * time.Second

// Lane is one broker and the Jevons replica linked to it.
type Lane struct {
	t      testing.TB
	ctx    context.Context
	dir    string
	store  *cost.Store
	Broker *brokerfixture.Broker

	Replica   *cost.ReplicaSource
	Collector *cost.Collector
}

// Start runs a broker and opens Jevons' replica in dir, linked to it, with a
// collector over store. It does not wait for the link: a broker write that
// lands before or during the handshake still reaches the replica.
func Start(t testing.TB, dir string, store *cost.Store) *Lane {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	l := &Lane{t: t, ctx: ctx, dir: dir, store: store, Broker: brokerfixture.Start(t)}
	l.open()
	t.Cleanup(func() {
		if l.Replica != nil {
			_ = l.Replica.Close()
		}
	})
	return l
}

func (l *Lane) open() {
	l.t.Helper()
	replica, err := cost.OpenReplicaLinked(l.ctx, l.dir, l.Broker.ControlSocket())
	if err != nil {
		l.t.Fatal(err)
	}
	l.Replica = replica
	l.Collector = cost.NewCollector(&cost.CollectorArgs{Store: l.store, Replica: replica})
	if l.Collector.UsesSocketHistory() {
		l.t.Fatal("receipt proof polled the usage socket")
	}
}

// Reopen is a Jevons restart: the replica closes and opens again on the
// same file, with a new collector. The broker keeps running.
func (l *Lane) Reopen() *cost.Collector {
	l.t.Helper()
	if err := l.Replica.Close(); err != nil {
		l.t.Fatal(err)
	}
	l.open()
	return l.Collector
}

// PublishReceipt has the broker record receipt in its own file and returns
// once Jevons' replica holds exactly that receipt. The seat pump publishes
// before a send returns; waiting here keeps that order for the reader.
func (l *Lane) PublishReceipt(receipt claudia.RequestReceipt) {
	l.t.Helper()
	l.Broker.PublishReceipt(receipt)
	q := claudia.ReceiptQuery{Seat: receipt.Seat, SessionID: receipt.SessionID, RequestID: receipt.RequestID}
	deadline := time.Now().Add(arrivalTimeout)
	for {
		got, err := l.Collector.ReplicatedReceipt(l.ctx, q)
		if err == nil && reflect.DeepEqual(normal(*got), normal(receipt)) {
			return
		}
		if time.Now().After(deadline) {
			l.t.Fatalf("receipt %s did not reach Jevons' replica: last %+v %v", receipt.RequestID, got, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// normal treats a nil and an empty turn list alike: JSON carries both as [].
func normal(r claudia.RequestReceipt) claudia.RequestReceipt {
	if r.TurnIDs == nil {
		r.TurnIDs = []string{}
	}
	return r
}
