// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"

	"github.com/marcelocantos/jevons/internal/brokerfixture"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/mcpserver"
	"github.com/marcelocantos/jevons/internal/seatpair"
	"github.com/marcelocantos/jevons/internal/server"
)

// Exercise the composition called by main, through both accounting early
// returns. The broker runs in another process with no provider or auth work.
func TestT10749ControlServicesOutliveAccounting(t *testing.T) {
	broker := brokerfixture.Start(t)
	t.Setenv("CLAUDIA_NO_BROKER", "0")
	t.Setenv("CLAUDIA_BROKER_SOCKET", broker.Socket)
	for _, mode := range []string{"disabled", "usage-db-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			dir := t.TempDir()
			usagePath := filepath.Join(dir, "usage.db")
			if mode == "disabled" {
				if err := os.WriteFile(filepath.Join(dir, "budget.json"), []byte(`{"disabled":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(usagePath, 0700); err != nil {
				t.Fatal(err)
			}
			// Pending per-seat work must not start executing merely because
			// the shared observation service is now independent of accounting.
			root := filepath.Join(dir, seatpair.DirName)
			caller, err := seatpair.OpenCaller(root, "pending-seat")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := caller.Commit(ctx, &seatpair.CommitArgs{Generation: seatpair.InitialGeneration, Tool: seatpair.ToolPing, ArgsJSON: `{}`}); err != nil {
				t.Fatal(err)
			}
			if err := seatpair.PublishGeneration(root, "pending-seat", seatpair.InitialGeneration); err != nil {
				t.Fatal(err)
			}
			reg, err := claudia.NewRegistry(filepath.Join(dir, "registry.json"))
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{StateDir: dir, SessionsDir: dir, OverseerName: "jevons"}
			srv, mcp := server.New("test", dir), mcpserver.New(dir, nil, nil)
			replica, guard := startControlServices(ctx, cfg, config.NewWatcher(nil), reg, nil, srv, mcp)
			if replica == nil || guard != nil {
				t.Fatalf("startup replica=%v guard=%v; want observation without accounting", replica, guard)
			}
			t.Cleanup(func() {
				if err := replica.Close(); err != nil {
					t.Error(err)
				}
			})
			q := claudia.ReceiptQuery{Seat: "seat", SessionID: "generation", RequestID: mode}
			broker.PublishReceipt(claudia.RequestReceipt{
				RequestID: q.RequestID, SessionID: q.SessionID, Seat: q.Seat,
				State: claudia.ReceiptAccepted, AttemptID: "attempt", TurnIDs: []string{},
				AcceptedAt: "2026-10-16T01:00:00Z", UpdatedAt: "2026-10-16T01:00:00Z",
			})
			waitUntil(t, "receipt with accounting unavailable", func() bool {
				r, err := mcp.LookupHeldReceipt(ctx, q)
				return err == nil && r.RequestID == q.RequestID && r.State == claudia.ReceiptAccepted
			})
			r, err := srv.LookupSendReceipt(ctx, q)
			if err != nil || r.RequestID != q.RequestID || r.State != claudia.ReceiptAccepted {
				t.Fatalf("owner receipt = %+v, %v", r, err)
			}
			if err := replica.Read(func(db *sqlpipe.Database) error {
				for _, table := range []string{controlplane.DesiredSeatTable, controlplane.GrantTable, controlplane.IntentTable} {
					rows, err := db.Query("SELECT COUNT(*) FROM " + table)
					if err != nil {
						return err
					}
					if rows.Rows[0][0].(int64) != 0 {
						t.Errorf("startup published lifecycle work into %s", table)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if len(reg.List()) != 0 {
				t.Fatal("startup registered an agent")
			}
			if _, err := os.Stat(filepath.Join(dir, seatpair.EffectLog)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("startup executed a pending seat tool: %v", err)
			}
			if mode == "disabled" {
				if _, err := os.Stat(usagePath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("disabled accounting created usage.db: %v", err)
				}
			}
			ownFile := filepath.Join(dir, controlplane.JevonsDBFile)
			requireHeldBy(t, ownFile, os.Getpid())
			requireHeldBy(t, broker.ControlFile(), broker.Pid())
			if err := replica.Close(); err != nil {
				t.Fatal(err)
			}
			holders, err := brokerfixture.Holders(ownFile)
			if err != nil || len(holders) != 0 {
				t.Fatalf("closed replica holders=%v, error=%v", holders, err)
			}
			if _, err := srv.LookupSendReceipt(ctx, q); err == nil {
				t.Fatal("closed receipt reader returned success")
			}
		})
	}
}

func TestT10749DisabledAccountingRespectsNoBroker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "budget.json"), []byte(`{"disabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	sockDir, err := os.MkdirTemp("/tmp", "jcs-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	socket := filepath.Join(sockDir, "broker.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: claudia.ControlSocketPath(socket), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	t.Setenv("CLAUDIA_BROKER_SOCKET", socket)
	replica, guard := startControlServices(t.Context(), config.Config{StateDir: dir}, config.NewWatcher(nil), nil, nil, server.New("test", dir), mcpserver.New(dir, nil, nil))
	if replica == nil || guard != nil {
		t.Fatalf("replica=%v guard=%v", replica, guard)
	}
	defer replica.Close()
	if err := listener.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	conn, err := listener.AcceptUnix()
	if conn != nil {
		conn.Close()
		t.Fatal("disabled-broker startup dialled the broker")
	}
	var timeout net.Error
	if !errors.As(err, &timeout) || !timeout.Timeout() {
		t.Fatalf("accept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, controlplane.ClaudiaDBFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup opened the broker's database: %v", err)
	}
}
