// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe/transport"
)

// This is a connected consumer test: a real Unix socket and sqlpipe peer
// deliver fixture vendor observations through the production ReplicaSource,
// subscription query, opaque payload and policy decode. It does not claim to
// test provider authentication or Claudia's scheduler.
func TestPlanProjectionConnectedRecovery(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "jv-plan-link-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	db, err := sqlpipe.OpenDatabase(filepath.Join(dir, "fixture-producer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := controlplane.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	flush := make(chan struct{}, 1)
	now := time.Now().UTC().Truncate(time.Second)
	observed := now.Add(-time.Minute)
	publish := func(status string, remaining int, at time.Time) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		var err error
		if status == "available" {
			raw, _ := json.Marshal(map[string]any{"provider": "claude", "status": status, "fetched_at": at.Format(time.RFC3339), "windows": []any{map[string]any{"name": "weekly", "remaining_percent": remaining, "used_percent": 100 - remaining, "limit_window": int64(7 * 24 * time.Hour), "resets_at": now.Add(24 * time.Hour).Format(time.RFC3339), "band": "ok"}}})
			tx, e := db.Begin()
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback()
			err = tx.Exec(`INSERT INTO claudia_plan_usage(provider,status,reason,attempted_at,success_at,next_attempt_at,reading_json)
    VALUES ('claude','available','',?,?,?,?) ON CONFLICT(provider) DO UPDATE SET status=excluded.status,reason='',attempted_at=excluded.attempted_at,success_at=excluded.success_at,next_attempt_at=excluded.next_attempt_at,reading_json=excluded.reading_json`, at.Format(time.RFC3339), at.Format(time.RFC3339), at.Add(5*time.Minute).Format(time.RFC3339), string(raw))
			if err == nil {
				err = tx.Exec("INSERT INTO claudia_plan_usage_history(provider,observed_at,reading_json) VALUES ('claude',?,?)", at.Format(time.RFC3339), string(raw))
			}
			if err == nil {
				err = tx.Commit()
			}
		} else {
			err = db.Exec("UPDATE claudia_plan_usage SET status='unavailable',reason='fixture429',attempted_at=?", at.Format(time.RFC3339))
		}
		if err != nil {
			t.Fatal(err)
		}
		select {
		case flush <- struct{}{}:
		default:
		}
	}
	publish("available", 41, observed)
	socket := filepath.Join(dir, "source.sock")
	start := func() func() {
		t.Helper()
		ln, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				mu.Lock()
				peer, err := sqlpipe.NewPeer(db, sqlpipe.PeerConfig{OwnedTables: controlplane.ClaudiaTables()})
				mu.Unlock()
				if err != nil {
					t.Errorf("producer peer: %v", err)
					conn.Close()
					return
				}
				link := transport.NewLink(&planSocket{Conn: conn})
				link.OwnerLock = &mu
				_ = link.RunPeer(ctx, peer, true, flush, nil)
				mu.Lock()
				peer.Close()
				mu.Unlock()
				conn.Close()
			}
		}()
		return func() { cancel(); ln.Close(); <-done }
	}
	stop := start()
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	state := filepath.Join(dir, "jevons")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	replica, err := cost.OpenReplicaLinked(t.Context(), state, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if replica != nil {
			replica.Close()
		}
	}()
	overrides, err := planusage.OpenReplicaOverrides(replica, state)
	if err != nil {
		t.Fatal(err)
	}
	updates := make(chan struct{}, 64)
	projection, err := planusage.SubscribeProjection(t.Context(), replica, func() { updates <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	await := func(status string, rem float64, band string) {
		t.Helper()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for {
			snap := projection.Snapshot()
			be, ok := snap.Backend("claude")
			if ok && be.Status == status && len(be.Windows) == 1 && *be.Windows[0].RemainingPercent == rem && be.Windows[0].Band == band {
				return
			}
			select {
			case <-updates:
			case <-timer.C:
				t.Fatalf("missing subscribed state %s %v %s: %s", status, rem, band, projection.JSON())
			}
		}
	}
	await("available", 41, "ok")
	publish("unavailable", 0, now)
	await("unavailable", 41, "")
	be := projection.Snapshot().Backends[0]
	if !be.Stale || !be.FetchedAt.Equal(observed) || len(be.Windows[0].History) != 1 {
		t.Fatalf("failure invented observation: %+v", be)
	}
	stop()
	stop = nil
	before := string(projection.JSON())
	publish("available", 33, now.Add(time.Second))
	if string(projection.JSON()) != before {
		t.Fatal("offline consumer read producer file")
	}
	stop = start()
	await("available", 33, "ok")
	if err := overrides.Set("claude", planusage.Override{Band: planusage.BandUnder, Reason: "fixture owner", SetAt: now}); err != nil {
		t.Fatal(err)
	}
	await("available", 33, "under")
	stop()
	stop = nil
	if err := replica.Close(); err != nil {
		t.Fatal(err)
	}
	replica = nil
	// Restart disconnected: the initial subscription reads the persisted replica.
	replica, err = cost.OpenReplicaLinked(t.Context(), state, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planusage.OpenReplicaOverrides(replica, state); err != nil {
		t.Fatal(err)
	}
	projection, err = planusage.SubscribeProjection(t.Context(), replica, nil)
	if err != nil {
		t.Fatal(err)
	}
	await("available", 33, "under")
	if len(projection.Snapshot().Backends[0].Windows[0].History) != 2 {
		t.Fatal("restart lost history")
	}
}

type planSocket struct{ net.Conn }

func (s *planSocket) Send(ctx context.Context, b []byte) error {
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(b)))
	_, err := s.Write(append(h[:], b...))
	return err
}
func (s *planSocket) SendDatagram(b []byte) error { return s.Send(context.Background(), b) }
func (s *planSocket) Recv(ctx context.Context) ([]byte, error) {
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	var h [4]byte
	if _, err := io.ReadFull(s, h[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(h[:])
	if n == 0 || n > 64<<20 {
		return nil, fmt.Errorf("invalid frame %d", n)
	}
	b := make([]byte, n)
	_, err := io.ReadFull(s, b)
	return b, err
}
func (*planSocket) RecvDatagram(ctx context.Context) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
