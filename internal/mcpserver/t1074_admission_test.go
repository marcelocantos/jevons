// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

func TestT1074IdleNudgeReservesBeforeDispatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	var pushes atomic.Int32
	rep := deliverIdleNudge(admitAgent(), admitSweep(ledger, now, func(string, string, string) error {
		pushes.Add(1)
		reopened, err := OpenIdleNudgeLedger(dir)
		if err != nil {
			t.Errorf("reopen during dispatch: %v", err)
			return err
		}
		count, _ := reopened.Get(admitAgent().Name)
		if count != 1 {
			t.Errorf("dispatch started before the allowance was durable: count=%d", count)
		}
		return errDispatchDied
	}), now, admitReport())
	if pushes.Load() != 1 || rep.Delivered || rep.Error == "" {
		t.Fatalf("ambiguous dispatch pushes=%d rep=%+v", pushes.Load(), rep)
	}
	reopened, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	again := deliverIdleNudge(admitAgent(), admitSweep(reopened, now.Add(time.Second), func(string, string, string) error {
		pushes.Add(1)
		return nil
	}), now.Add(time.Second), admitReport())
	if pushes.Load() != 1 || again.Delivered || again.Reason != string(NudgeBackoff) {
		t.Fatalf("restart repeated an unfinished attempt: pushes=%d rep=%+v", pushes.Load(), again)
	}
	if count, _ := reopened.Get(admitAgent().Name); count != 1 {
		t.Fatalf("ambiguous attempt was refunded: count=%d", count)
	}
}

func TestT1074IdleNudgeReserveFailureDoesNotDispatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	lockLedgerDir(t, dir)
	var pushes atomic.Int32
	rep := deliverIdleNudge(admitAgent(), admitSweep(ledger, time.Unix(1_800_000_000, 0), func(string, string, string) error {
		pushes.Add(1)
		return nil
	}), time.Unix(1_800_000_000, 0), admitReport())
	if pushes.Load() != 0 || rep.Delivered || rep.Error == "" {
		t.Fatalf("failed reserve still dispatched: pushes=%d rep=%+v", pushes.Load(), rep)
	}
	unlockLedgerDir(t, dir)
	reopened, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := reopened.Get(admitAgent().Name); count != 0 {
		t.Fatalf("failed reserve kept an attempt: count=%d", count)
	}
}

func TestT1074IdleNudgeLastAllowanceIsSingleUse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	spendAllowance(t, ledger, admitAgent().Name, DefaultIdleNudgeMax-1, now.Add(-24*time.Hour))
	var pushes atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			deliverIdleNudge(admitAgent(), admitSweep(ledger, now, func(string, string, string) error {
				pushes.Add(1)
				return nil
			}), now, admitReport())
		}()
	}
	close(start)
	wg.Wait()
	if pushes.Load() != 1 {
		t.Fatalf("last allowance dispatched %d times", pushes.Load())
	}
	if count, _ := ledger.Get(admitAgent().Name); count != DefaultIdleNudgeMax {
		t.Fatalf("count=%d want %d", count, DefaultIdleNudgeMax)
	}
}

func TestT1074FleetRecoverReservesBeforeDispatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	var pushes atomic.Int32
	reps := SweepFleetRecover(recoverSweep(t, ledger, now, func(string, bool, string, string) error {
		pushes.Add(1)
		reopened, err := OpenIdleNudgeLedger(dir)
		if err != nil {
			return err
		}
		count, _ := reopened.Get("w1")
		if count != 1 {
			t.Errorf("recover dispatch started before the allowance was durable: count=%d", count)
		}
		return errDispatchDied
	}))
	if pushes.Load() != 1 || !recoverFailed(reps, "w1") {
		t.Fatalf("ambiguous recover pushes=%d reps=%+v", pushes.Load(), reps)
	}
	reopened, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	again := SweepFleetRecover(recoverSweep(t, reopened, now.Add(time.Second), func(string, bool, string, string) error {
		pushes.Add(1)
		return nil
	}))
	if pushes.Load() != 1 || recoverPushed(again, "w1") {
		t.Fatalf("restart repeated an unfinished recover: pushes=%d reps=%+v", pushes.Load(), again)
	}
}

func TestT1074FleetRecoverReserveFailureDoesNotDispatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	lockLedgerDir(t, dir)
	var pushes atomic.Int32
	reps := SweepFleetRecover(recoverSweep(t, ledger, time.Unix(1_800_000_000, 0), func(string, bool, string, string) error {
		pushes.Add(1)
		return nil
	}))
	if pushes.Load() != 0 || recoverPushed(reps, "w1") {
		t.Fatalf("failed reserve still recovered: pushes=%d reps=%+v", pushes.Load(), reps)
	}
	unlockLedgerDir(t, dir)
	reopened, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := reopened.Get("w1"); count != 0 {
		t.Fatalf("failed reserve kept a recover: count=%d", count)
	}
}

func TestT1074FleetRecoverLastAllowanceIsSingleUse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0).UTC()
	spendAllowance(t, ledger, "w1", DefaultFleetRecoverMax-1, now.Add(-24*time.Hour))
	var pushes atomic.Int32
	push := func(string, bool, string, string) error {
		pushes.Add(1)
		return nil
	}
	first := recoverSweep(t, ledger, now, push)
	second := recoverSweep(t, ledger, now, push)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, args := range []FleetRecoverSweepArgs{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			SweepFleetRecover(args)
		}()
	}
	close(start)
	wg.Wait()
	if pushes.Load() != 1 {
		t.Fatalf("last recover allowance dispatched %d times", pushes.Load())
	}
	if count, _ := ledger.Get("w1"); count != DefaultFleetRecoverMax {
		t.Fatalf("count=%d want %d", count, DefaultFleetRecoverMax)
	}
}

func TestT1074RepressureReservesBeforeSend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := admitServer(t, dir)
	err := (&rePressureSink{server: s}).RePressure("jv-t1074-admit", "T1074")
	if err == nil || err.Error() == "repressure refused: "+string(NudgeBackoff) {
		t.Fatalf("first repressure did not reach send after reserving: %v", err)
	}
	reopened, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := reopened.Get("jv-t1074-admit"); count != 1 {
		t.Fatalf("send failure refunded the attempt: count=%d", count)
	}
	s.idleNudgeLedger = reopened
	err = (&rePressureSink{server: s}).RePressure("jv-t1074-admit", "T1074")
	if err == nil || err.Error() != "repressure refused: "+string(NudgeBackoff) {
		t.Fatalf("restart repeated an unfinished repressure: %v", err)
	}
	if count, _ := reopened.Get("jv-t1074-admit"); count != 1 {
		t.Fatalf("second repressure spent another attempt: count=%d", count)
	}
}

func TestT1074RepressureReserveFailureDoesNotSend(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := admitServer(t, dir)
	lockLedgerDir(t, dir)
	err := (&rePressureSink{server: s}).RePressure("jv-t1074-admit", "T1074")
	if err == nil || len(err.Error()) < len("repressure ledger:") || err.Error()[:len("repressure ledger:")] != "repressure ledger:" {
		t.Fatalf("reserve failure was not the refusal: %v", err)
	}
	unlockLedgerDir(t, dir)
	reopened, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := reopened.Get("jv-t1074-admit"); count != 0 {
		t.Fatalf("failed reserve kept a repressure: count=%d", count)
	}
}

func TestT1074RepressureLastAllowanceIsSingleUse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := admitServer(t, dir)
	// The production repressure path stamps the reservation with the real
	// clock, so the prior attempts have to be in that clock's past.
	spendAllowance(t, s.idleNudgeLedger, "jv-t1074-admit", DefaultIdleNudgeMax-1, time.Now().Add(-24*time.Hour))
	var refused atomic.Int32
	var attempted atomic.Int32
	var errsMu sync.Mutex
	var errs []string
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := (&rePressureSink{server: s}).RePressure("jv-t1074-admit", "T1074")
			errsMu.Lock()
			if err == nil {
				errs = append(errs, "<nil>")
			} else {
				errs = append(errs, err.Error())
			}
			errsMu.Unlock()
			if err != nil && err.Error() == "repressure refused: "+string(NudgeExhausted) {
				refused.Add(1)
				return
			}
			attempted.Add(1)
		}()
	}
	close(start)
	wg.Wait()
	if attempted.Load() != 1 || refused.Load() != 1 {
		t.Fatalf("attempted=%d refused=%d errs=%q", attempted.Load(), refused.Load(), errs)
	}
	if count, _ := s.idleNudgeLedger.Get("jv-t1074-admit"); count != DefaultIdleNudgeMax {
		t.Fatalf("count=%d want %d", count, DefaultIdleNudgeMax)
	}
}

var errDispatchDied = os.ErrClosed

func admitAgent() claudia.AgentDef {
	return claudia.AgentDef{Name: "jv-t1074-admit", Purpose: claudia.PurposeWork, TargetID: "T1074"}
}

func admitReport() IdleNudgeReport {
	return IdleNudgeReport{Name: admitAgent().Name, Action: IdleNudgeNudge, Reason: "idle_stuck", Kind: IdleNudgeKindContinue}
}

func admitSweep(ledger *IdleNudgeLedger, now time.Time, push IdleNudgePusher) IdleNudgeSweepArgs {
	return IdleNudgeSweepArgs{Ledger: ledger, Push: push, Now: now}
}

func admitServer(t *testing.T, dir string) *Server {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t1074-admit", Purpose: claudia.PurposeWork, TargetID: "T1074",
		SessionID: "gen-1", Materialized: true, Provider: "grok", AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	ledger, err := OpenIdleNudgeLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg, idleNudgeLedger: ledger}
	// Park the seat so the real send path refuses to launch a process.
	// The reservation still has to precede that refusal.
	st, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(st)
	if err := s.SetAgentIntent("jv-t1074-admit", fleetintent.Parked, "test", "do not launch"); err != nil {
		t.Fatal(err)
	}
	return s
}

func recoverSweep(t *testing.T, ledger *IdleNudgeLedger, now time.Time, push FleetRecoverPusher) FleetRecoverSweepArgs {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "w1", Purpose: claudia.PurposeWork, TargetID: "T1074",
		SessionID: "s1", AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	activity := NewIdleActivityTracker()
	activity.now = func() time.Time { return now }
	activity.NoteTerminalOutcome("w1", "Internal error")
	return FleetRecoverSweepArgs{
		Reg: reg, Activity: activity, Ledger: ledger,
		ProcessRunning: func(string) bool { return true },
		PromptInFlight: func(string) bool { return false },
		Push:           push,
		Now:            now,
		OverseerName:   "jevons",
	}
}

func spendAllowance(t *testing.T, ledger *IdleNudgeLedger, name string, n int, at time.Time) {
	t.Helper()
	for range n {
		if err := ledger.Record(name, at); err != nil {
			t.Fatal(err)
		}
	}
}

func lockLedgerDir(t *testing.T, stateDir string) {
	t.Helper()
	if err := os.Chmod(filepath.Join(stateDir, "fleet"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(stateDir, "fleet"), 0o755) })
}

func unlockLedgerDir(t *testing.T, stateDir string) {
	t.Helper()
	if err := os.Chmod(filepath.Join(stateDir, "fleet"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func recoverFailed(reps []FleetRecoverReport, name string) bool {
	for _, r := range reps {
		if r.Name == name && !r.Delivered && r.Error != "" {
			return true
		}
	}
	return false
}

func recoverPushed(reps []FleetRecoverReport, name string) bool {
	for _, r := range reps {
		if r.Name == name && r.Delivered {
			return true
		}
	}
	return false
}
