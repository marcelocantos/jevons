// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

func TestT10749AcceptDesiredSeatWhilePlaneClosed(t *testing.T) {
	dir := t.TempDir()
	src, err := OpenReplica(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	if err := src.plane.Close(); err != nil {
		t.Fatal(err)
	}
	seat := controlplane.DesiredSeat{
		Seat: "seat-a", Generation: 1, Revision: 1, Provider: "grok", Model: "m",
		WorkDir: t.TempDir(), ConfigJSON: `{"model":"m"}`,
	}
	if err := src.AcceptDesiredSeat(seat); err != nil {
		t.Fatal(err)
	}
	if n := countTable(t, src.replica, controlplane.DesiredSeatTable); n != 1 {
		t.Fatalf("replica rows = %d", n)
	}
	plane, err := controlplane.Open(filepath.Join(dir, controlplane.ClaudiaDBFile))
	if err != nil {
		t.Fatal(err)
	}
	src.plane = plane
	if err := src.sync(); err != nil {
		t.Fatal(err)
	}
	if err := src.sync(); err != nil {
		t.Fatal(err)
	}
	var got []controlplane.DesiredSeat
	if err := plane.WithDB(func(db *sqlpipe.Database) error {
		rows, err := controlplane.DesiredSeats(db)
		got = rows
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Seat != "seat-a" || got[0].ConfigJSON != `{"model":"m"}` {
		t.Fatalf("delivered = %+v", got)
	}
}

func TestT10749CorruptStatusFailsCatchUp(t *testing.T) {
	dir := t.TempDir()
	src, err := OpenReplica(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = src.Close() })
	if err := src.plane.WithDB(func(db *sqlpipe.Database) error {
		return db.Exec(`INSERT INTO claudia_seat_status
			(seat, session_id, generation, revision, runtime_id, state, status_json)
			VALUES ('seat-a', 'sess', 1, 1, 'sess', 'running', 'not-json')`)
	}); err != nil {
		t.Fatal(err)
	}
	c := NewCollector(&CollectorArgs{Replica: src})
	err = c.CatchUpReplicas(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not json") {
		t.Fatalf("catch-up = %v", err)
	}
}

func countTable(t *testing.T, db *sqlpipe.Database, table string) int {
	t.Helper()
	qr, err := db.Query("SELECT COUNT(*) FROM " + table)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := qr.Rows[0][0].(int64)
	if !ok {
		t.Fatalf("count type %T", qr.Rows[0][0])
	}
	return int(n)
}
