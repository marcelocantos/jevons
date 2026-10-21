// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

func TestT10749ReconcileDoesNotResetAllowance(t *testing.T) {
	dir := t.TempDir()
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ledger.Reserve("seat-a", time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC), 3, nil)
	if err != nil || got != NudgeReserved {
		t.Fatalf("reserve = %s %v", got, err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		return claudia.StartStub(ctx, cfg, nil)
	}})
	t.Cleanup(func() { reg.StopAll() })
	// The reconcile is Claudia's, run here on a scratch database standing in
	// for the broker's file: no process but the broker opens that file.
	db, err := sqlpipe.OpenDatabase(filepath.Join(dir, "reconcile-input.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := controlplane.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := controlplane.PublishDesiredSeat(db, controlplane.DesiredSeat{
		Seat: "seat-a", Generation: 1, Revision: 1, Provider: "grok", Model: "m",
		WorkDir: work, ConfigJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := controlplane.PublishGrant(db, controlplane.Grant{
		Seat: "seat-a", GrantID: "g", Revision: 1, Scope: `{"name":"read"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := controlplane.Reconcile(context.Background(), db, reg); err != nil {
		t.Fatal(err)
	}
	if n, _ := ledger.Get("seat-a"); n != 1 {
		t.Fatalf("allowance in memory = %d", n)
	}
	reopened, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := reopened.Get("seat-a"); n != 1 {
		t.Fatalf("allowance on disk = %d", n)
	}
	if reg.Def("seat-a") == nil || reg.Def("seat-a").SessionID == "" {
		t.Fatal("reconcile did not reach the registry")
	}
}
