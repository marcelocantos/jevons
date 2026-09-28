// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// timedOutBroker models the loaded host of 🎯T796.1: claudia.BrokerAvailable
// returns false because its 2s round trip timed out, while a socket exists.
func timedOutBroker(t *testing.T) {
	t.Helper()
	pa, ps := brokerAvailable, brokerSocketProbe
	t.Cleanup(func() { brokerAvailable, brokerSocketProbe = pa, ps })
	brokerAvailable = func() bool { return false }
	brokerSocketProbe = func() brokerSocketState { return socketMaybe }
}

// A probe timeout with a holder on the session: the launch is refused and no
// signal is sent (the 03:19 mechanism under load).
func TestT796_1ProbeTimeoutRefusesLaunchAndSignalsNothing(t *testing.T) {
	timedOutBroker(t)
	rows := stubTable(t, stubRows(), nil)
	signalled := 0
	prev := signalClaudeProc
	signalClaudeProc = func(pid int, s syscall.Signal) error { signalled++; return prev(pid, s) }
	start := GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		t.Fatal("launched a second client while the broker could not be ruled out")
		return nil, nil
	})
	_, err := start(context.Background(), claudia.Config{Provider: claudia.ProviderClaude, SessionID: sidS})
	if !errors.Is(err, ErrClaudeHeldByBroker) {
		t.Fatalf("err = %v, want ErrClaudeHeldByBroker", err)
	}
	if signalled != 0 || !claudeProcAlive(80623) || len(*rows) != 3 {
		t.Fatalf("signalled=%d rows=%v", signalled, *rows)
	}
}

// 🎯T883: a socket that exists is enough to say a broker may own the seats.
// The broker itself is not asked, so one that accepts and never answers
// costs nothing here; with no socket, an answering broker is still a yes and
// a silent one is the positive-evidence no.
func TestT883SocketAloneMeansBrokerMayOwnSeats(t *testing.T) {
	timedOutBroker(t)
	calls := 0
	brokerAvailable = func() bool { calls++; return false }
	if !brokerMayOwnSeats() {
		t.Fatal("an existing broker socket was ruled out")
	}
	if calls != 0 {
		t.Fatalf("asked the broker %d times though its socket exists", calls)
	}
	brokerSocketProbe = func() brokerSocketState { return socketNone }
	if brokerMayOwnSeats() {
		t.Fatal("no socket and no answer should rule the broker out")
	}
	brokerAvailable = func() bool { return true }
	if !brokerMayOwnSeats() {
		t.Fatal("a broker that answers was ruled out because the socket probe missed it")
	}
}

// Positive evidence of no broker keeps the old behaviour: holders are strays
// and are stopped. Unknown must not wedge a bare dev box.
func TestT796_1NoSocketStillStopsStrays(t *testing.T) {
	timedOutBroker(t)
	brokerSocketProbe = func() brokerSocketState { return socketNone }
	rows := stubTable(t, stubRows(), nil)
	if err := guardClaudeSession(sidS); err != nil {
		t.Fatal(err)
	}
	if claudeProcAlive(80623) || len(*rows) != 2 {
		t.Fatalf("stray survived on a box with no broker: %v", *rows)
	}
}

// The post-grant stop, the upgrade-exit stop-all and the Cursor reap all stand
// down when the broker is unknown.
func TestT796_1EverySignallingGuardStandsDownWhenUnknown(t *testing.T) {
	timedOutBroker(t)
	rows := stubTable(t, []claudeProc{
		{PID: 75806, PPID: 91440, Command: "/x/claude --resume " + sidS},
		{PID: 28028, PPID: 500, Command: "/x/claude --resume " + sidS},
	}, nil)
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		return claudia.StartStub(ctx, cfg, nil)
	}})
	if err := reg.Register(claudia.AgentDef{Name: "po", WorkDir: t.TempDir(), SessionID: sidS,
		Provider: claudia.ProviderClaude, AutoStart: true, TermLogPath: "-"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AdoptOrLaunch("po"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.StopAll)
	old := claudeWindowPanePIDs
	t.Cleanup(func() { claudeWindowPanePIDs = old })
	claudeWindowPanePIDs = func(string) []int { return []int{500} }
	ReapClaudeStraysAfterGrant(reg, "po")
	if len(*rows) != 2 {
		t.Fatalf("post-grant stop signalled with an unknown broker: %v", *rows)
	}
	if n := StopNonAdoptable(reg); n != 0 {
		t.Fatalf("StopNonAdoptable stopped %d seats with an unknown broker", n)
	}
	if ModeNormal.StopAgents() {
		t.Fatal("exit would StopAll seats a broker may own")
	}
}

// Bounded, then loud: with the grant held and the broker unknown, adoption
// retries heldRetries times, the seat stays down, the holder is untouched and
// the owner is told exactly once.
func TestT796_1RefusalIsBoundedThenLoudNeverSignals(t *testing.T) {
	timedOutBroker(t)
	pd, pr := heldRetryDelay, heldRetries
	t.Cleanup(func() { heldRetryDelay, heldRetries = pd, pr })
	heldRetryDelay, heldRetries = time.Millisecond, 3
	pn := LaunchRefusedNotifier
	t.Cleanup(func() { LaunchRefusedNotifier = pn })
	var told []string
	LaunchRefusedNotifier = func(agent string, err error) { told = append(told, agent+": "+err.Error()) }
	rows := stubTable(t, stubRows(), nil)
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	attempts := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			t.Fatal("launched beside a holder")
			return nil, nil
		}),
		Adopt: func(cfg claudia.Config) (*claudia.Agent, error) {
			attempts++
			return nil, errors.New("broker protocol: grant_held")
		},
	})
	if err := reg.Register(claudia.AgentDef{Name: "po", WorkDir: t.TempDir(), SessionID: sidS,
		Provider: claudia.ProviderClaude, AutoStart: true, TermLogPath: "-"}); err != nil {
		t.Fatal(err)
	}
	ReattachSeatsContext(context.Background(), reg, nil, 1)
	if attempts != heldRetries+1 {
		t.Fatalf("attempts = %d, want %d (bounded)", attempts, heldRetries+1)
	}
	if len(told) != 1 {
		t.Fatalf("owner notices = %v, want exactly one", told)
	}
	if !claudeProcAlive(80623) || len(*rows) != 3 {
		t.Fatalf("a holder was stopped: %v", *rows)
	}
}

func shortSockDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "b")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// The real socket probe against real sockets: hung listener = maybe (the
// timeout case), stale file with no listener = none, no file = none, and the
// disabled switch = none.
func TestT796_1SocketProbeClassifiesRealSockets(t *testing.T) {
	t.Setenv("CLAUDIA_NO_BROKER", "0")
	dir := shortSockDir(t)
	sock := filepath.Join(dir, "s")
	t.Setenv("CLAUDIA_BROKER_SOCKET", sock)
	if got := probeBrokerSocket(); got != socketNone {
		t.Fatalf("no file: %v", got)
	}
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	// Listening but never accepting or answering: a hung broker.
	if got := probeBrokerSocket(); got != socketMaybe {
		t.Fatalf("hung listener: %v", got)
	}
	_ = l.Close()
	if got := probeBrokerSocket(); got != socketNone {
		t.Fatalf("stale socket file: %v", got)
	}
	t.Setenv("CLAUDIA_NO_BROKER", "1")
	if got := probeBrokerSocket(); got != socketNone {
		t.Fatalf("disabled: %v", got)
	}
}

// Path resolution mirrors claudia's (its broker package is internal): the
// override wins, then an absolute XDG_STATE_HOME, then ~/.local/state.
func TestT796_1SocketPathResolution(t *testing.T) {
	t.Setenv("CLAUDIA_BROKER_SOCKET", "")
	t.Setenv("XDG_STATE_HOME", "/x/state")
	if got := brokerSocketPath(); got != "/x/state/claudia/broker.sock" {
		t.Fatalf("xdg: %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "relative")
	home, _ := os.UserHomeDir()
	if got, want := brokerSocketPath(), filepath.Join(home, ".local/state/claudia/broker.sock"); got != want {
		t.Fatalf("relative xdg ignored: %q, want %q", got, want)
	}
	t.Setenv("CLAUDIA_BROKER_SOCKET", "/y/b.sock")
	if got := brokerSocketPath(); got != "/y/b.sock" {
		t.Fatalf("override: %q", got)
	}
}
