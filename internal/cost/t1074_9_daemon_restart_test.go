// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build jevons_claudia_next

package cost

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/controlplane"
	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

// TestT10749DisconnectedAcceptSurvivesDaemonRestart writes the desired seat,
// grant, and a stale stop through the Jevons replica while the Claudia file
// is closed. CatchUpReplicas, the collector method the cost guard runs, is
// what delivers those rows. Each child is a new process. It loads grants.json,
// the registry file daemon.New opens, then calls ReconcileControlFile and
// OpenControlPublisher, the two control-plane calls inside daemon.New.
// The second child is the restart. The stale stop names the next generation
// and must not change the session.
func TestT10749DisconnectedAcceptSurvivesDaemonRestart(t *testing.T) {
	if os.Getenv("T1074_9_DAEMON") == "1" {
		runT10749DaemonChild(t)
		return
	}
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := OpenReplica(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := src.plane.Close(); err != nil {
		t.Fatal(err)
	}
	src.plane = nil
	seat := controlplane.DesiredSeat{
		Seat: "seat-a", Generation: 1, Revision: 1, Provider: "grok", Model: "m",
		WorkDir: work, ConfigJSON: `{}`,
	}
	if err := src.AcceptDesiredSeat(seat); err != nil {
		t.Fatal(err)
	}
	if err := src.AcceptGrant(controlplane.Grant{
		Seat: "seat-a", GrantID: "g", Revision: 1, Scope: `{"name":"read"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := src.AcceptControlIntent(controlplane.ControlIntent{
		OperationID: "op-stale", Seat: "seat-a", Generation: 2, Kind: "stop",
	}); err != nil {
		t.Fatal(err)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	if n := countDesired(t, filepath.Join(dir, controlplane.ClaudiaDBFile)); n != 0 {
		t.Fatalf("closed plane delivered %d desired seats", n)
	}
	if n := countDesired(t, filepath.Join(dir, controlplane.JevonsDBFile)); n != 1 {
		t.Fatalf("replica desired seats = %d", n)
	}

	src, err = OpenReplica(dir)
	if err != nil {
		t.Fatal(err)
	}
	catchUp := NewCollector(&CollectorArgs{Replica: src})
	if err := catchUp.CatchUpReplicas(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := catchUp.CatchUpReplicas(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := countDesired(t, filepath.Join(dir, controlplane.ClaudiaDBFile)); n != 1 {
		t.Fatalf("delivered desired seats = %d", n)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}

	runDaemon := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestT10749DisconnectedAcceptSurvivesDaemonRestart$", "-test.count=1")
		cmd.Env = append(os.Environ(), "T1074_9_DAEMON=1", "T1074_9_DIR="+dir, "T1074_9_SEAT=seat-a")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %v\n%s", err, out)
		}
	}
	runDaemon()
	runDaemon()

	body, err := os.ReadFile(filepath.Join(dir, "sessions.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.FieldsFunc(string(body), func(r rune) bool { return r == '\n' })
	if len(lines) != 2 {
		t.Fatalf("session lines = %q", lines)
	}
	first := strings.Fields(lines[0])
	second := strings.Fields(lines[1])
	if len(first) != 2 || len(second) != 2 || first[0] == "" || first[0] != second[0] {
		t.Fatalf("sessions = %q", lines)
	}
	if first[1] != "false" || second[1] != "true" {
		t.Fatalf("resume flags = %q", lines)
	}

	src, err = OpenReplica(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	if err := NewCollector(&CollectorArgs{Replica: src}).CatchUpReplicas(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status controlplane.SeatStatus
	var outcome controlplane.ControlOutcome
	if err := src.plane.WithDB(func(db *sqlpipe.Database) error {
		statuses, err := controlplane.SeatStatuses(db)
		if err != nil {
			return err
		}
		status = statuses["seat-a"]
		outcomes, err := controlplane.ControlOutcomes(db)
		outcome = outcomes["op-stale"]
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if status.State != "running" || status.Generation != 1 || status.SessionID != first[0] || status.RuntimeID != first[0] {
		t.Fatalf("status = %+v", status)
	}
	if outcome.Outcome != "stale" || outcome.Reason != "generation" {
		t.Fatalf("outcome = %+v", outcome)
	}
	var replicaStatus controlplane.SeatStatus
	var replicaOutcome controlplane.ControlOutcome
	statuses, err := controlplane.SeatStatuses(src.replica)
	if err != nil {
		t.Fatal(err)
	}
	replicaStatus = statuses["seat-a"]
	outcomes, err := controlplane.ControlOutcomes(src.replica)
	if err != nil {
		t.Fatal(err)
	}
	replicaOutcome = outcomes["op-stale"]
	if replicaStatus.SessionID != status.SessionID || replicaStatus.State != status.State || replicaOutcome != outcome {
		t.Fatalf("replica status=%+v outcome=%+v", replicaStatus, replicaOutcome)
	}
}

func runT10749DaemonChild(t *testing.T) {
	dir := os.Getenv("T1074_9_DIR")
	seat := os.Getenv("T1074_9_SEAT")
	if dir == "" || seat == "" {
		t.Fatal("child dir")
	}
	logPath := filepath.Join(dir, "sessions.txt")
	reg, err := claudia.NewRegistry(filepath.Join(dir, "grants.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.StopAll()
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		_, werr := fmt.Fprintf(f, "%s %v\n", cfg.SessionID, cfg.RequireResume)
		cerr := f.Close()
		if werr != nil {
			return nil, werr
		}
		if cerr != nil {
			return nil, cerr
		}
		return claudia.StartStub(ctx, cfg, nil)
	}})
	controlPath := filepath.Join(dir, claudia.ClaudiaDBFile)
	if err := reg.ReconcileControlFile(context.Background(), controlPath); err != nil {
		t.Fatal(err)
	}
	closePlane, err := claudia.OpenControlPublisher(controlPath)
	if err != nil {
		t.Fatal(err)
	}
	if closePlane != nil {
		defer closePlane()
	}
	def := reg.Def(seat)
	if def == nil || def.SessionID == "" {
		t.Fatalf("def = %+v", def)
	}
}

func countDesired(t *testing.T, path string) int {
	t.Helper()
	db, err := sqlpipe.OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := controlplane.DesiredSeats(db)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}
