// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1136: replica catch-up ingests only new or rewritten rows after bootstrap.
func TestT1136ReplicaCatchUpIncremental(t *testing.T) {
	state := t.TempDir()
	rig := openRejectedFactRig(t, state)
	t.Cleanup(func() { rig.close(t) })

	facts := []claudia.UsageFact{
		rejectedFactAt(1, "call-A", "completed", 3, 1),
		rejectedFactAt(2, "call-B", "completed", 5, 2),
		rejectedFactAt(3, "call-C", "completed", 7, 3),
	}
	deliverUsageRows(t, rig.src, facts...)

	n, err := rig.collector.CatchUpReplicas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("first pass applied %d rows, want 3", n)
	}
	n, err = rig.collector.CatchUpReplicas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("second pass applied %d rows, want 0", n)
	}

	deliverUsageRows(t, rig.src,
		rejectedFactAt(4, "call-D", "completed", 2, 1),
		rejectedFactAt(5, "call-E", "completed", 4, 1),
	)
	n, err = rig.collector.CatchUpReplicas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("third pass applied %d rows, want 2", n)
	}

	rig.close(t)
	rig = openRejectedFactRig(t, state)
	t.Cleanup(func() { rig.close(t) })
	n, err = rig.collector.CatchUpReplicas(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("after restart applied %d rows, want 0", n)
	}
}

func TestT1136ReplicaCatchUpMissingMetaFails(t *testing.T) {
	state := t.TempDir()
	store, err := OpenStore(filepath.Join(state, "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.db.Exec(`DELETE FROM replica_catchup_meta WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	src, err := OpenReplicaLinked(context.Background(), state, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	c := NewCollector(&CollectorArgs{Store: store, Replica: src})
	_, err = c.CatchUpReplicas(context.Background())
	if err == nil {
		t.Fatal("catch-up with missing meta should fail")
	}
}

// J44's canonical reader is a separate SQLite connection in another
// process. A catch-up that reports success only on the Store's own connection
// while the on-disk usage.db remains empty is not a successful projection.
func TestT1276ReplicaCatchUpVisibleToIndependentCanonicalReader(t *testing.T) {
	state := t.TempDir()
	rig := openRejectedFactRig(t, state)
	defer rig.close(t)
	if _, err := rig.collector.CatchUpReplicas(context.Background()); err != nil {
		t.Fatal(err)
	}
	deliverUsageRows(t, rig.src, rejectedFactAt(1, "call-visible", "completed", 4, 2))
	if _, err := rig.collector.CatchUpReplicas(context.Background()); err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", "file:"+filepath.Join(state, "usage.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var count int
	if err := reader.QueryRow(`SELECT COUNT(*) FROM usage_calls WHERE session_id = ?`, "generation-rej").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("independent canonical reader sees %d calls, want 1", count)
	}
}
