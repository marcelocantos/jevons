// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

func TestProjectionMigratesLegacyOnceAndPolicyAges(t *testing.T) {
	state := t.TempDir()
	store, err := OpenReadingStore(DefaultReadingsPath(state))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	reset := now.Add(time.Hour)
	if err := store.Append([]Reading{{Provider: "claude", Window: "weekly", FetchedAt: now.Add(-time.Hour), Remaining: 80, ResetsAt: &reset}}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	old := NewOverrideStore(filepath.Join(state, OverrideFile))
	if err := old.Set("claude", Override{Band: BandOK, Reason: "keep", SetAt: now}); err != nil {
		t.Fatal(err)
	}
	replica, err := controlplane.OpenReplica(filepath.Join(state, controlplane.JevonsDBFile))
	if err != nil {
		t.Fatal(err)
	}
	defer replica.Close()
	overrides, err := OpenReplicaOverrides(replica, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := overrides.Set("claude", Override{Band: BandUnder, Reason: "new", SetAt: now}); err != nil {
		t.Fatal(err)
	}
	// A second open must never reread obsolete files, even now malformed.
	if err := os.WriteFile(filepath.Join(state, OverrideFile), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReplicaOverrides(replica, state); err != nil {
		t.Fatal(err)
	}
	got, err := overrides.Load()
	if err != nil || got["claude"].Band != BandUnder {
		t.Fatalf("override migration reverted: %v %v", got, err)
	}
	if err := replica.Read(func(db *sqlpipe.Database) error {
		qr, err := db.Query("SELECT count(*) FROM jv_plan_history_legacy")
		if err == nil && qr.Rows[0][0] != int64(1) {
			t.Fatalf("duplicate import: %v", qr.Rows)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	deadline := now.Add(-time.Second)
	raw, _ := json.Marshal(Snapshot{Backends: []Backend{{Provider: "claude", Status: StatusAvailable, FetchedAt: now.Add(-time.Hour), StaleAt: &deadline, FleetAgents: 1, Windows: []Window{{Name: "weekly", RemainingPercent: floatPtr(80)}}}}})
	p := &Projection{payload: raw}
	if !p.Snapshot().Backends[0].Stale {
		t.Fatal("silent transport kept policy fresh")
	}
	if f, _ := p.CapacityRemaining(map[string]int{"claude": 1}); f != nil {
		t.Fatalf("stale data became capacity: %v", *f)
	}
	if string(p.JSON()) != string(raw) {
		t.Fatal("policy aging changed subscribed bytes")
	}
}

// Opt-in measurement uses only a scratch copy of legacy percentage history.
// No real provider, credentials or live daemon is involved.
func TestProjectionHistoryScale(t *testing.T) {
	state := os.Getenv("JEVONS_PLAN_HISTORY_BENCHMARK")
	if state == "" {
		t.Skip("requires scratch history copy")
	}
	replica, err := controlplane.OpenReplica(filepath.Join(t.TempDir(), controlplane.JevonsDBFile))
	if err != nil {
		t.Fatal(err)
	}
	defer replica.Close()
	if _, err := OpenReplicaOverrides(replica, state); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := replica.Write(func(db *sqlpipe.Database) error {
		qr, err := db.Query("SELECT count(*) FROM jv_plan_history_legacy")
		if err != nil {
			return err
		}
		count = qr.Rows[0][0].(int64)
		// Source-derived latest periods: measurement does not choose a small corpus.
		return db.Exec(`INSERT INTO claudia_plan_usage(provider,status,reason,attempted_at,success_at,next_attempt_at,reading_json)
   SELECT provider,'available','',max(fetched_at),max(fetched_at),max(fetched_at),
    json_object('provider',provider,'windows',json_group_array(json_object('name',window,'remaining_percent',remaining,'resets_at',nullif(resets_key,''),'band','ok')))
   FROM (SELECT provider,window,resets_key,fetched_at,remaining,row_number() OVER(PARTITION BY provider,window ORDER BY fetched_at DESC,id DESC) AS n FROM jv_plan_history_legacy)
   WHERE n=1 GROUP BY provider`)
	}); err != nil {
		t.Fatal(err)
	}
	const runs = 20
	var total, maximum time.Duration
	var bytes int
	for i := 0; i < runs; i++ {
		start := time.Now()
		err := replica.Read(func(db *sqlpipe.Database) error {
			qr, err := db.Query(ProjectionQuery)
			if err != nil {
				return err
			}
			p := &Projection{}
			if err := p.accept(qr); err != nil {
				return err
			}
			bytes = len(p.JSON())
			return nil
		})
		elapsed := time.Since(start)
		total += elapsed
		if elapsed > maximum {
			maximum = elapsed
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("source-derived history rows=%d; query runs=%d mean=%s max=%s payload=%d bytes", count, runs, total/runs, maximum, bytes)
}
