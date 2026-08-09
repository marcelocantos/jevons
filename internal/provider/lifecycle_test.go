// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// 🎯T27.3 oracles: the registry launches providers, connects providers,
// and reconciles both on config change. These run against fake
// launcher/connector so they are hermetic and fast; the real os/exec
// supervisor is proved separately in supervisor_exec_test.go.

const testPoll = 2 * time.Second

// fakeProcess is a launched provider under test control: it stays alive
// until told to crash or stopped by the supervisor.
type fakeProcess struct {
	pid  int
	mu   sync.Mutex
	done chan struct{}
	err  error
	// stopped records that the supervisor reaped this process.
	stopped bool
}

func newFakeProcess(pid int) *fakeProcess {
	return &fakeProcess{pid: pid, done: make(chan struct{})}
}

func (p *fakeProcess) PID() int { return p.pid }

func (p *fakeProcess) Wait() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *fakeProcess) Stop(context.Context) error {
	p.exit(nil, true)
	return nil
}

// crash simulates the provider dying on its own.
func (p *fakeProcess) crash(err error) { p.exit(err, false) }

func (p *fakeProcess) exit(err error, stopped bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	select {
	case <-p.done:
		return // already exited
	default:
	}
	p.err = err
	p.stopped = stopped
	close(p.done)
}

func (p *fakeProcess) wasStopped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopped
}

// fakeLauncher hands out fakeProcesses and records every start.
type fakeLauncher struct {
	mu      sync.Mutex
	nextPID int
	starts  []config.ProviderDecl
	procs   map[string][]*fakeProcess
	// failWith, when set, makes Start fail instead of spawning.
	failWith error
}

func newFakeLauncher() *fakeLauncher {
	return &fakeLauncher{nextPID: 1000, procs: make(map[string][]*fakeProcess)}
}

func (l *fakeLauncher) Start(_ context.Context, d config.ProviderDecl) (Process, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.starts = append(l.starts, d)
	if l.failWith != nil {
		return nil, l.failWith
	}
	l.nextPID++
	p := newFakeProcess(l.nextPID)
	l.procs[d.ID] = append(l.procs[d.ID], p)
	return p, nil
}

// latest returns the most recent process spawned for id.
func (l *fakeLauncher) latest(id string) *fakeProcess {
	l.mu.Lock()
	defer l.mu.Unlock()
	ps := l.procs[id]
	if len(ps) == 0 {
		return nil
	}
	return ps[len(ps)-1]
}

func (l *fakeLauncher) startCount(id string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, d := range l.starts {
		if d.ID == id {
			n++
		}
	}
	return n
}

// fakeConn is a connect-mode attachment whose probe result is switchable.
type fakeConn struct {
	endpoint string
	mu       sync.Mutex
	probeErr error
	probes   int
	closed   bool
}

func (c *fakeConn) Endpoint() string { return c.endpoint }

func (c *fakeConn) Probe(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.probes++
	return c.probeErr
}

func (c *fakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *fakeConn) setProbeErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.probeErr = err
}

func (c *fakeConn) wasClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

type fakeConnector struct {
	mu       sync.Mutex
	conns    map[string]*fakeConn
	connects int
	failWith error
}

func newFakeConnector() *fakeConnector {
	return &fakeConnector{conns: make(map[string]*fakeConn)}
}

func (c *fakeConnector) Connect(_ context.Context, d config.ProviderDecl) (Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connects++
	if c.failWith != nil {
		return nil, c.failWith
	}
	conn := &fakeConn{endpoint: d.ConnectURL()}
	c.conns[d.ID] = conn
	return conn, nil
}

func (c *fakeConnector) conn(id string) *fakeConn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conns[id]
}

// testLifecycle builds a Lifecycle with fast backoff and fake transports.
func testLifecycle(t *testing.T) (*Lifecycle, *fakeLauncher, *fakeConnector) {
	t.Helper()
	lch := newFakeLauncher()
	con := newFakeConnector()
	l := NewLifecycle(LifecycleArgs{
		Launcher:      lch,
		Connector:     con,
		Backoff:       Backoff{Base: time.Millisecond, Max: 5 * time.Millisecond, Factor: 2},
		ProbeInterval: 2 * time.Millisecond,
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testPoll)
		defer cancel()
		if err := l.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	return l, lch, con
}

// waitForPhase polls health until provider id reaches phase, or fails.
func waitForPhase(t *testing.T, l *Lifecycle, id string, want Phase) Health {
	t.Helper()
	deadline := time.Now().Add(testPoll)
	var last Health
	for time.Now().Before(deadline) {
		for _, h := range l.Health() {
			if h.ID == id {
				last = h
				if h.Phase == want {
					return h
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("provider %q never reached phase %q (last: %+v)", id, want, last)
	return Health{}
}

// waitFor polls cond until true, or fails with msg.
func waitFor(t *testing.T, msg string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testPoll)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

func launchDecl(id string, enable *bool, argv ...string) config.ProviderDecl {
	args := make([]any, len(argv))
	for i, a := range argv {
		args[i] = a
	}
	return config.ProviderDecl{
		ID:        id,
		Transport: config.ProviderTransportLaunch,
		Enable:    enable,
		Params:    map[string]any{"exec": "/usr/bin/true", "argv": args},
	}
}

func connectDecl(id string, enable *bool, url string) config.ProviderDecl {
	return config.ProviderDecl{
		ID:        id,
		Transport: config.ProviderTransportConnect,
		Enable:    enable,
		Params:    map[string]any{"url": url},
	}
}

func boolPtr(b bool) *bool { return &b }

// TestReconcileLaunchesEnabledProvider is the core launch acceptance: an
// enabled launch-mode declaration results in a supervised live process.
func TestReconcileLaunchesEnabledProvider(t *testing.T) {
	l, lch, _ := testLifecycle(t)

	if err := l.Reconcile([]config.ProviderDecl{launchDecl("pulse", nil)}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	h := waitForPhase(t, l, "pulse", PhaseRunning)
	if h.Transport != config.ProviderTransportLaunch {
		t.Errorf("transport = %q, want launch", h.Transport)
	}
	if h.PID == 0 {
		t.Error("running launch provider has no PID in health")
	}
	if got := lch.startCount("pulse"); got != 1 {
		t.Errorf("start count = %d, want 1", got)
	}
}

// TestReconcileConnectsEnabledProvider is the connect-mode half: an
// already-running provider is attached by URL, not spawned.
func TestReconcileConnectsEnabledProvider(t *testing.T) {
	l, lch, con := testLifecycle(t)

	if err := l.Reconcile([]config.ProviderDecl{connectDecl("mnemo", nil, "http://127.0.0.1:8741")}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	h := waitForPhase(t, l, "mnemo", PhaseRunning)
	if h.Endpoint != "http://127.0.0.1:8741" {
		t.Errorf("endpoint = %q, want the declared url", h.Endpoint)
	}
	if h.PID != 0 {
		t.Errorf("connect-mode provider reported PID %d — jevonsd does not own that process", h.PID)
	}
	if lch.startCount("mnemo") != 0 {
		t.Error("connect-mode provider was launched — it must be attached, not spawned")
	}
	if con.connects != 1 {
		t.Errorf("connects = %d, want 1", con.connects)
	}
}

// TestReconcileSkipsDisabledProvider: enable:false is desired-absent.
func TestReconcileSkipsDisabledProvider(t *testing.T) {
	l, lch, _ := testLifecycle(t)

	if err := l.Reconcile([]config.ProviderDecl{launchDecl("pulse", boolPtr(false))}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := len(l.Health()); got != 0 {
		t.Fatalf("health has %d entries, want 0 for a disabled provider", got)
	}
	if lch.startCount("pulse") != 0 {
		t.Error("disabled provider was started")
	}
}

// TestReconcileEnableStartsDisableTearsDown is the acceptance sentence
// itself: "enable starts (launch) or attaches (connect), disable tears down".
func TestReconcileEnableStartsDisableTearsDown(t *testing.T) {
	l, lch, con := testLifecycle(t)

	// Disabled at boot: nothing runs.
	decls := []config.ProviderDecl{
		launchDecl("pulse", boolPtr(false)),
		connectDecl("mnemo", boolPtr(false), "http://127.0.0.1:8741"),
	}
	if err := l.Reconcile(decls); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := len(l.Health()); got != 0 {
		t.Fatalf("health has %d entries before enable, want 0", got)
	}

	// Enable both: launch spawns, connect attaches.
	decls = []config.ProviderDecl{
		launchDecl("pulse", boolPtr(true)),
		connectDecl("mnemo", boolPtr(true), "http://127.0.0.1:8741"),
	}
	if err := l.Reconcile(decls); err != nil {
		t.Fatalf("reconcile enable: %v", err)
	}
	waitForPhase(t, l, "pulse", PhaseRunning)
	waitForPhase(t, l, "mnemo", PhaseRunning)
	proc := lch.latest("pulse")
	conn := con.conn("mnemo")

	// Disable both: the process is reaped and the attachment closed.
	decls = []config.ProviderDecl{
		launchDecl("pulse", boolPtr(false)),
		connectDecl("mnemo", boolPtr(false), "http://127.0.0.1:8741"),
	}
	if err := l.Reconcile(decls); err != nil {
		t.Fatalf("reconcile disable: %v", err)
	}
	if got := len(l.Health()); got != 0 {
		t.Errorf("health has %d entries after disable, want 0", got)
	}
	if !proc.wasStopped() {
		t.Error("disabled launch provider was not reaped")
	}
	waitFor(t, "connect attachment closed", conn.wasClosed)
}

// TestReconcileRemovedProviderTornDown: dropping a provider from config
// is the same as disabling it.
func TestReconcileRemovedProviderTornDown(t *testing.T) {
	l, lch, _ := testLifecycle(t)

	if err := l.Reconcile([]config.ProviderDecl{launchDecl("pulse", nil)}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	waitForPhase(t, l, "pulse", PhaseRunning)
	proc := lch.latest("pulse")

	if err := l.Reconcile(nil); err != nil {
		t.Fatalf("reconcile empty: %v", err)
	}
	if got := len(l.Health()); got != 0 {
		t.Errorf("health has %d entries after removal, want 0", got)
	}
	if !proc.wasStopped() {
		t.Error("removed provider was not reaped")
	}
}

// TestReconcileUnchangedProviderNotRestarted guards against restart churn:
// reloading identical config must not bounce healthy providers.
func TestReconcileUnchangedProviderNotRestarted(t *testing.T) {
	l, lch, _ := testLifecycle(t)

	decls := []config.ProviderDecl{launchDecl("pulse", nil, "--serve")}
	if err := l.Reconcile(decls); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	waitForPhase(t, l, "pulse", PhaseRunning)
	first := lch.latest("pulse")

	// Same declaration, fresh slice and fresh maps — fingerprint must match.
	for range 3 {
		if err := l.Reconcile([]config.ProviderDecl{launchDecl("pulse", nil, "--serve")}); err != nil {
			t.Fatalf("re-reconcile: %v", err)
		}
	}
	if got := lch.startCount("pulse"); got != 1 {
		t.Errorf("start count = %d after identical reconciles, want 1 (no churn)", got)
	}
	if first.wasStopped() {
		t.Error("unchanged provider was torn down by an identical reconcile")
	}
}

// TestReconcileRespecifiedProviderRestarts: changing argv/url replaces
// the running provider rather than leaving the stale one up.
func TestReconcileRespecifiedProviderRestarts(t *testing.T) {
	l, lch, _ := testLifecycle(t)

	if err := l.Reconcile([]config.ProviderDecl{launchDecl("pulse", nil, "--port", "9100")}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	waitForPhase(t, l, "pulse", PhaseRunning)
	old := lch.latest("pulse")

	if err := l.Reconcile([]config.ProviderDecl{launchDecl("pulse", nil, "--port", "9200")}); err != nil {
		t.Fatalf("reconcile respec: %v", err)
	}
	waitForPhase(t, l, "pulse", PhaseRunning)

	if !old.wasStopped() {
		t.Error("respecified provider: old process still running")
	}
	if got := lch.startCount("pulse"); got != 2 {
		t.Errorf("start count = %d after respec, want 2", got)
	}
	newProc := lch.latest("pulse")
	if newProc == old {
		t.Error("respecified provider was not actually restarted")
	}
}

// TestCrashedProviderRestartsWithBackoff proves the restart-on-crash
// half of the acceptance, including that restarts are counted.
func TestCrashedProviderRestartsWithBackoff(t *testing.T) {
	l, lch, _ := testLifecycle(t)

	if err := l.Reconcile([]config.ProviderDecl{launchDecl("pulse", nil)}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	waitForPhase(t, l, "pulse", PhaseRunning)
	first := lch.latest("pulse")

	first.crash(errors.New("boom"))

	waitFor(t, "provider restarted after crash", func() bool {
		return lch.startCount("pulse") >= 2
	})
	h := waitForPhase(t, l, "pulse", PhaseRunning)
	if h.Restarts < 1 {
		t.Errorf("restarts = %d after a crash, want >= 1", h.Restarts)
	}
	if lch.latest("pulse") == first {
		t.Error("crashed process was not replaced")
	}
}

// TestUnrunnableProviderFailsWithoutSpinning: a declaration no restart
// can fix parks in PhaseFailed instead of hammering the launcher.
func TestUnrunnableProviderFailsWithoutSpinning(t *testing.T) {
	lch := newFakeLauncher()
	lch.failWith = fmt.Errorf("%w: no such binary", ErrUnrunnable)
	l := NewLifecycle(LifecycleArgs{
		Launcher: lch,
		Backoff:  Backoff{Base: time.Millisecond, Max: time.Millisecond, Factor: 1},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testPoll)
		defer cancel()
		_ = l.Shutdown(ctx)
	})

	if err := l.Reconcile([]config.ProviderDecl{launchDecl("broken", nil)}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	h := waitForPhase(t, l, "broken", PhaseFailed)
	if h.LastError == "" {
		t.Error("failed provider has no LastError — health is not observable")
	}
	// The runner has given up, so the start count must stay put.
	time.Sleep(20 * time.Millisecond)
	if got := lch.startCount("broken"); got != 1 {
		t.Errorf("start count = %d — unrunnable provider is spinning, not parked", got)
	}
}

// TestConnectProviderBackoffAndRecovery: an unreachable endpoint backs
// off and health recovers when the provider comes back.
func TestConnectProviderBackoffAndRecovery(t *testing.T) {
	l, _, con := testLifecycle(t)

	if err := l.Reconcile([]config.ProviderDecl{connectDecl("mnemo", nil, "http://127.0.0.1:8741")}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	waitForPhase(t, l, "mnemo", PhaseRunning)

	con.conn("mnemo").setProbeErr(errors.New("connection refused"))
	h := waitForPhase(t, l, "mnemo", PhaseBackoff)
	if h.LastError == "" {
		t.Error("unreachable provider has no LastError")
	}

	con.conn("mnemo").setProbeErr(nil)
	h = waitForPhase(t, l, "mnemo", PhaseRunning)
	// Recovery shows in Phase; LastError is history and stays put so the
	// outage remains visible after the fact.
	if h.Restarts == 0 {
		t.Error("recovered provider reports 0 restarts — the outage left no trace")
	}
	if h.LastError == "" {
		t.Error("recovered provider dropped LastError — the outage cause is unrecoverable")
	}
}

// TestShutdownReapsEverything: the daemon exiting takes its providers
// with it, launch and connect alike.
func TestShutdownReapsEverything(t *testing.T) {
	lch := newFakeLauncher()
	con := newFakeConnector()
	l := NewLifecycle(LifecycleArgs{
		Launcher:      lch,
		Connector:     con,
		Backoff:       Backoff{Base: time.Millisecond, Max: 5 * time.Millisecond, Factor: 2},
		ProbeInterval: 2 * time.Millisecond,
	})
	decls := []config.ProviderDecl{
		launchDecl("pulse", nil),
		connectDecl("mnemo", nil, "http://127.0.0.1:8741"),
	}
	if err := l.Reconcile(decls); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	waitForPhase(t, l, "pulse", PhaseRunning)
	waitForPhase(t, l, "mnemo", PhaseRunning)
	proc := lch.latest("pulse")
	conn := con.conn("mnemo")

	ctx, cancel := context.WithTimeout(context.Background(), testPoll)
	defer cancel()
	if err := l.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if !proc.wasStopped() {
		t.Error("shutdown left a launched provider running")
	}
	waitFor(t, "connect attachment closed on shutdown", conn.wasClosed)
	if got := len(l.Health()); got != 0 {
		t.Errorf("health has %d entries after shutdown, want 0", got)
	}
	// Reconcile after shutdown must refuse rather than resurrect providers.
	if err := l.Reconcile(decls); err == nil {
		t.Error("Reconcile after Shutdown succeeded — want refusal")
	}
}

// TestConfigChangeDrivesReconcile is the end-to-end acceptance for
// "reconciles on config change": an owner edits config.yaml, Reload
// runs, and the live provider set converges — no daemon restart, no
// direct Reconcile call in sight.
func TestConfigChangeDrivesReconcile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Boot config: one launch provider enabled, one connect provider off.
	write(`
providers:
  - id: pulse
    transport: launch
    params:
      exec: /usr/bin/true
      argv: ["--port", "9100"]
  - id: mnemo
    transport: connect
    enable: false
    params:
      url: http://127.0.0.1:8741
`)
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	l, lch, con := testLifecycle(t)
	mgr, err := NewConfigManager(ConfigManagerArgs{ConfigPath: cfgPath, Store: store})
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	if err := l.BindConfig(mgr); err != nil {
		t.Fatalf("BindConfig: %v", err)
	}

	waitForPhase(t, l, "pulse", PhaseRunning)
	if got := len(l.Health()); got != 1 {
		t.Fatalf("health has %d entries at boot, want 1 (mnemo is disabled)", got)
	}
	pulseProc := lch.latest("pulse")

	// Owner edits config: disable pulse, enable mnemo. Reload alone must
	// converge both — that is the whole point of the target.
	write(`
providers:
  - id: pulse
    transport: launch
    enable: false
    params:
      exec: /usr/bin/true
      argv: ["--port", "9100"]
  - id: mnemo
    transport: connect
    params:
      url: http://127.0.0.1:8741
`)
	if err := mgr.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	waitForPhase(t, l, "mnemo", PhaseRunning)
	if !pulseProc.wasStopped() {
		t.Error("pulse still running after config disabled it")
	}
	for _, h := range l.Health() {
		if h.ID == "pulse" {
			t.Errorf("pulse still in health after being disabled: %+v", h)
		}
	}
	if con.conn("mnemo") == nil {
		t.Error("mnemo was never attached after config enabled it")
	}

	// A malformed edit must not tear the live fleet down (fail-closed).
	write("providers:\n  - id: broken\n    transport: connect\n    params: {}\n")
	if err := mgr.Reload(); err == nil {
		t.Fatal("malformed config reloaded without error")
	}
	if h := waitForPhase(t, l, "mnemo", PhaseRunning); h.ID != "mnemo" {
		t.Error("a rejected config edit disturbed the running fleet")
	}
}

// TestBackoffDelayGrowsAndCaps pins the restart policy itself.
func TestBackoffDelayGrowsAndCaps(t *testing.T) {
	b := Backoff{Base: 100 * time.Millisecond, Max: time.Second, Factor: 2}
	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		time.Second, // capped
		time.Second,
	}
	for i, w := range want {
		if got := b.Delay(i + 1); got != w {
			t.Errorf("Delay(%d) = %v, want %v", i+1, got, w)
		}
	}
	// A zero policy is usable, not a busy loop.
	if got := (Backoff{}).Delay(1); got != DefaultBackoff.Base {
		t.Errorf("zero Backoff Delay(1) = %v, want %v", got, DefaultBackoff.Base)
	}
}

// TestSpecOfIgnoresEnableAndParamOrder guards the no-churn fingerprint.
func TestSpecOfIgnoresEnableAndParamOrder(t *testing.T) {
	a := config.ProviderDecl{
		ID: "x", Transport: config.ProviderTransportLaunch, Enable: boolPtr(true),
		Params: map[string]any{"exec": "/bin/x", "argv": []any{"-a"}, "extra": "z"},
	}
	b := config.ProviderDecl{
		ID: "x", Transport: config.ProviderTransportLaunch, Enable: boolPtr(false),
		Params: map[string]any{"extra": "z", "argv": []any{"-a"}, "exec": "/bin/x"},
	}
	if specOf(a) != specOf(b) {
		t.Errorf("spec differs on enable/key order:\n a=%q\n b=%q", specOf(a), specOf(b))
	}
	c := a
	c.Params = map[string]any{"exec": "/bin/x", "argv": []any{"-b"}, "extra": "z"}
	if specOf(a) == specOf(c) {
		t.Error("spec identical despite changed argv — respec would not restart")
	}
}
