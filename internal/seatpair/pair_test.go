// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatpair

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/sqlpipe/go/sqlpipe"
)

func TestT107410CommitWaitsAndDoesNotReplay(t *testing.T) {
	root := t.TempDir()
	caller, err := OpenCaller(root, "seat-a")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	id, err := caller.Commit(ctx, &CommitArgs{Generation: 1, Tool: ToolPing, ArgsJSON: `{}`})
	if err != nil || id == "" {
		t.Fatal(err)
	}
	if _, err := os.Stat(caller.jevonsPath()); !os.IsNotExist(err) {
		t.Fatal("commit created the jevons file")
	}
	if _, err := caller.Wait(ctx, id); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait without executor = %v", err)
	}
	if _, err := os.Stat(caller.mcpPath()); err != nil {
		t.Fatal(err)
	}

	var calls int
	if err := os.WriteFile(filepath.Join(root, "seat-a", GenerationFile), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	effect := func(context.Context, Intent) Result {
		calls++
		return Result{Outcome: OutcomeAccepted, Reason: "pong"}
	}
	if err := CatchUpDir(context.Background(), root, effect); err != nil {
		t.Fatal(err)
	}
	if err := CatchUpDir(context.Background(), root, effect); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("effect calls = %d", calls)
	}
	waitCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	got, err := caller.Wait(waitCtx, id)
	if err != nil || got.Outcome != OutcomeAccepted || got.Reason != "pong" {
		t.Fatalf("wait = %+v %v", got, err)
	}
}

func TestT107410StaleUncertainAndIsolation(t *testing.T) {
	root := t.TempDir()
	var calls int
	effect := func(context.Context, Intent) Result {
		calls++
		return Result{Outcome: OutcomeUncertain, Reason: "external effect"}
	}
	a, err := OpenCaller(root, "seat-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenCaller(root, "seat-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCaller(root, "../seat-c"); err == nil {
		t.Fatal("parent seat name was accepted")
	}
	if _, err := a.Commit(context.Background(), &CommitArgs{Generation: 1, Tool: "secret", ArgsJSON: `{"token":"x"}`}); err == nil {
		t.Fatal("token args were committed")
	}
	staleID, err := a.Commit(context.Background(), &CommitArgs{Generation: 2, Tool: "external", ArgsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seat-a", GenerationFile), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	otherID, err := b.Commit(context.Background(), &CommitArgs{Generation: 1, Tool: "external", ArgsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seat-b", GenerationFile), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CatchUpDir(context.Background(), root, effect); err != nil {
		t.Fatal(err)
	}
	if err := CatchUpDir(context.Background(), root, effect); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("effect calls = %d, want the matching seat only", calls)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stale, err := a.Wait(ctx, staleID)
	if err != nil || stale.Outcome != OutcomeStale {
		t.Fatalf("stale = %+v %v", stale, err)
	}
	other, err := b.Wait(ctx, otherID)
	if err != nil || other.Outcome != OutcomeUncertain || other.Reason != "external effect" {
		t.Fatalf("other = %+v %v", other, err)
	}
	if intentsContain(t, a.mcpPath(), otherID) || intentsContain(t, b.mcpPath(), staleID) {
		t.Fatal("seat files share operation ids")
	}
	dbs := 0
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() == MCPFile || info.Name() == JevonsFile {
			dbs++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if dbs != 4 {
		t.Fatalf("db files = %d", dbs)
	}
}

func TestT107410NilEffectAndEmptyDir(t *testing.T) {
	root := t.TempDir()
	if err := CatchUpDir(context.Background(), filepath.Join(root, "missing"), nil); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(root, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CatchUpDir(context.Background(), empty, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(empty)
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty dir grew: %+v %v", entries, err)
	}
	caller, err := OpenCaller(root, "seat-a")
	if err != nil {
		t.Fatal(err)
	}
	id, err := caller.Commit(context.Background(), &CommitArgs{Generation: 1, Tool: ToolPing, ArgsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seat-a", GenerationFile), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CatchUpDir(context.Background(), root, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := caller.Wait(ctx, id)
	if err != nil || got.Outcome != OutcomeUncertain || got.Reason != "no effect" {
		t.Fatalf("nil effect = %+v %v", got, err)
	}
}

func TestT107410OwnershipRejectsOutcomeClaim(t *testing.T) {
	if approveMCPIntent(nil) || approveMCPIntent(map[string]bool{}) || approveMCPIntent(map[string]bool{OutcomeTable: true}) {
		t.Fatal("non-intent claim was approved")
	}
	dir := t.TempDir()
	client, err := sqlpipe.OpenDatabase(filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := sqlpipe.OpenDatabase(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := client.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if err := server.Exec(schema); err != nil {
		t.Fatal(err)
	}
	cp, err := sqlpipe.NewPeer(client, sqlpipe.PeerConfig{OwnedTables: map[string]bool{OutcomeTable: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	sp, err := sqlpipe.NewPeer(server, sqlpipe.PeerConfig{ApproveOwnership: approveMCPIntent})
	if err != nil {
		t.Fatal(err)
	}
	defer sp.Close()
	err = sqlpipe.SyncPeerHandshake(cp, sp)
	if cp.State() == sqlpipe.PeerLive || sp.State() == sqlpipe.PeerLive {
		t.Fatalf("outcome claim reached live: err=%v client=%d server=%d", err, cp.State(), sp.State())
	}
}

func TestT107410ProcessRestartWaitsOnTheSameInvocation(t *testing.T) {
	if os.Getenv("T1074_10_CHILD") == "1" {
		t107410Child(t)
		return
	}
	root := t.TempDir()
	caller, err := OpenCaller(root, "seat-a")
	if err != nil {
		t.Fatal(err)
	}
	id, err := caller.Commit(context.Background(), &CommitArgs{Generation: 1, Tool: ToolPing, ArgsJSON: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seat-a", GenerationFile), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "effect.log")
	runChild := func(mode string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestT107410ProcessRestartWaitsOnTheSameInvocation$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			"T1074_10_CHILD=1",
			"T1074_10_MODE="+mode,
			"T1074_10_ROOT="+root,
			"T1074_10_SEAT=seat-a",
			"T1074_10_OP="+id,
			"T1074_10_LOG="+logPath,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child %s: %v\n%s", mode, err, out)
		}
	}
	runChild("catchup")
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != id {
		t.Fatalf("effect log = %q", body)
	}
	runChild("wait")
	body, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != id {
		t.Fatalf("wait committed another effect: %q", body)
	}
}

func t107410Child(t *testing.T) {
	root := os.Getenv("T1074_10_ROOT")
	seat := os.Getenv("T1074_10_SEAT")
	id := os.Getenv("T1074_10_OP")
	logPath := os.Getenv("T1074_10_LOG")
	switch os.Getenv("T1074_10_MODE") {
	case "catchup":
		var once sync.Mutex
		err := CatchUpDir(context.Background(), root, func(_ context.Context, in Intent) Result {
			once.Lock()
			defer once.Unlock()
			f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return Result{Outcome: OutcomeUncertain, Reason: err.Error()}
			}
			_, _ = fmt.Fprintln(f, in.OperationID)
			_ = f.Close()
			return Result{Outcome: OutcomeAccepted, Reason: "pong"}
		})
		if err != nil {
			t.Fatal(err)
		}
	case "wait":
		caller, err := OpenCaller(root, seat)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		got, err := caller.Wait(ctx, id)
		if err != nil || got.Outcome != OutcomeAccepted || got.Reason != "pong" {
			t.Fatalf("restart wait = %+v %v", got, err)
		}
	default:
		t.Fatalf("mode %q", os.Getenv("T1074_10_MODE"))
	}
}

func intentsContain(t *testing.T, path, id string) bool {
	t.Helper()
	db, err := sqlpipe.OpenDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	qr, err := db.Query("SELECT operation_id FROM "+IntentTable+" WHERE operation_id = ?", id)
	if err != nil {
		t.Fatal(err)
	}
	return len(qr.Rows) != 0
}
