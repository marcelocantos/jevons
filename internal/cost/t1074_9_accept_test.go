// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// OpenReplica opens Jevons' own file and nothing else. The broker's file
// name never appears in the Jevons state directory.
func TestOpenReplicaKeepsOnlyJevonsFile(t *testing.T) {
	dir := t.TempDir()
	src, err := OpenReplicaLinked(context.Background(), dir, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	if _, err := os.Stat(filepath.Join(dir, controlplane.JevonsDBFile)); err != nil {
		t.Fatalf("Jevons' own file: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, controlplane.ClaudiaDBFile)); !os.IsNotExist(err) {
		t.Fatalf("OpenReplica touched the broker's file name: %v", err)
	}
}

// OpenReplica dials the broker's control socket only when the broker is on
// for this process. With CLAUDIA_NO_BROKER set, a hermetic or direct-mode
// daemon must not reach the machine's broker.
func TestOpenReplicaDialsOnlyWhenTheBrokerIsOn(t *testing.T) {
	sockDir, err := os.MkdirTemp("/tmp", "jrc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	brokerSock := filepath.Join(sockDir, "broker.sock")
	ln, err := net.Listen("unix", claudia.ControlSocketPath(brokerSock))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	dials := make(chan struct{}, 64)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
			dials <- struct{}{}
		}
	}()
	t.Setenv("CLAUDIA_BROKER_SOCKET", brokerSock)

	t.Setenv("CLAUDIA_NO_BROKER", "1")
	off, err := OpenReplica(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-dials:
		t.Fatal("OpenReplica dialled the broker with CLAUDIA_NO_BROKER=1")
	case <-time.After(3 * linkUpAfter):
	}
	if err := off.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLAUDIA_NO_BROKER", "0")
	on, err := OpenReplica(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = on.Close() })
	select {
	case <-dials:
	case <-time.After(30 * time.Second):
		t.Fatal("OpenReplica did not dial the broker's control socket with the broker on")
	}
}

// A corrupt status projection delivered into Jevons' file fails catch-up.
// The row is written straight into Jevons' file here, standing in for a
// corrupt delivery.
func TestT10749CorruptStatusFailsCatchUp(t *testing.T) {
	src, err := OpenReplicaLinked(context.Background(), t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	if err := src.replica.Write(func(db *sqlpipe.Database) error {
		return db.Exec(`INSERT INTO claudia_seat_status
			(seat, session_id, generation, revision, runtime_id, state, status_json)
			VALUES ('seat-a', 'sess', 1, 1, 'sess', 'running', 'not-json')`)
	}); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	c := NewCollector(&CollectorArgs{Store: store, Replica: src})
	_, err = c.CatchUpReplicas(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not json") {
		t.Fatalf("catch-up = %v", err)
	}
}
