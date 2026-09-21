// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// stubTable installs a fake process table whose signals remove rows, so no
// real process is ever touched.
func stubTable(t *testing.T, rows []claudeProc, immortal map[int]bool) *[]claudeProc {
	t.Helper()
	pt, sig, alive, wait := claudeProcessTable, signalClaudeProc, claudeProcAlive, claudeStopWait
	t.Cleanup(func() { claudeProcessTable, signalClaudeProc, claudeProcAlive, claudeStopWait = pt, sig, alive, wait })
	claudeStopWait = 100 * time.Millisecond
	claudeProcessTable = func() []claudeProc { return rows }
	claudeProcAlive = func(pid int) bool {
		for _, r := range rows {
			if r.PID == pid {
				return true
			}
		}
		return false
	}
	signalClaudeProc = func(pid int, s syscall.Signal) error {
		if immortal[pid] {
			return nil
		}
		var keep []claudeProc
		for _, r := range rows {
			if r.PID != pid {
				keep = append(keep, r)
			}
		}
		rows = keep
		return nil
	}
	return &rows
}

const sidS = "47a1b2c3-0000-4000-8000-000000000001"

func stubRows() []claudeProc {
	return []claudeProc{
		{PID: 80623, Command: "/Users/m/.local/bin/claude --permission-mode bypassPermissions --session-id " + sidS + " --mcp-config {}"},
		{PID: 7001, Command: "/Users/m/.local/bin/claude --permission-mode bypassPermissions --resume other-session --mcp-config {}"},
		{PID: 7002, Command: "/bin/zsh -c echo claude --resume " + sidS},
	}
}

// A live process on session S plus a resume of S leaves exactly one process.
func TestT796LiveClientPlusResumeLeavesOne(t *testing.T) {
	rows := stubTable(t, stubRows(), nil)
	starts := 0
	start := GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		starts++
		// The new client comes up as the only holder of S.
		*rows = append(*rows, claudeProc{PID: 59999, Command: "/x/claude --permission-mode bypassPermissions --resume " + sidS})
		return claudia.StartStub(ctx, cfg, nil)
	})
	a, err := start(context.Background(), claudia.Config{SessionID: sidS, WorkDir: t.TempDir(), TermLogPath: "-"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Stop)
	if starts != 1 {
		t.Fatalf("starts = %d", starts)
	}
	held := claudeHolderPIDs(sidS)
	if len(held) != 1 || held[0] != 59999 {
		t.Fatalf("holders of S = %v, want only the resumed 59999", held)
	}
	if !claudeProcAlive(7001) || !claudeProcAlive(7002) {
		t.Fatal("reaped a process that does not hold S")
	}
}

func TestT796SurvivingHolderRefusesLaunch(t *testing.T) {
	stubTable(t, stubRows(), map[int]bool{80623: true})
	start := GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		t.Fatal("Launch stacked a second client on a live holder")
		return nil, nil
	})
	if _, err := start(context.Background(), claudia.Config{Provider: claudia.ProviderClaude, SessionID: sidS}); err == nil {
		t.Fatal("want refusal")
	}
}

func TestT796MatchesEqualsFormAndIgnoresOtherBinaries(t *testing.T) {
	if !commandHoldsClaudeSession("claude --resume="+sidS, sidS) {
		t.Fatal("= form")
	}
	if commandHoldsClaudeSession("grok --resume "+sidS, sidS) {
		t.Fatal("non-claude matched")
	}
}

// (a) A healthy live client that is ADOPTED is kept: Adopt succeeds, Start is
// never reached, so the guard never runs and nothing is signalled.
func TestT796AdoptedLiveClientIsNotStopped(t *testing.T) {
	rows := stubTable(t, stubRows(), nil)
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	starts := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			starts++
			return claudia.StartStub(ctx, cfg, nil)
		}),
		Adopt: func(cfg claudia.Config) (*claudia.Agent, error) {
			return claudia.StartStub(context.Background(), cfg, nil)
		},
	})
	if err := reg.Register(claudia.AgentDef{
		Name: "po", WorkDir: t.TempDir(), SessionID: sidS,
		Provider: claudia.ProviderClaude, AutoStart: true, TermLogPath: "-",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.AdoptOrLaunch("po"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.StopAll)
	if starts != 0 {
		t.Fatalf("adopt fell through to Launch (%d starts)", starts)
	}
	if !claudeProcAlive(80623) || len(*rows) != 3 {
		t.Fatalf("adopt stopped the live client: table = %v", *rows)
	}
}

// (c) Never the daemon itself, its ancestors, or its process group, even
// when their argv names the session; other sessions' clients are untouched.
func TestT796NeverTargetsSelfAncestorsOrProcessGroup(t *testing.T) {
	self, pgid := os.Getpid(), 5555
	argv := "/x/claude --resume " + sidS
	stubTable(t, []claudeProc{
		{PID: self, PPID: 900, PGID: pgid, Command: argv},
		{PID: 900, PPID: 800, PGID: 900, Command: argv},                             // parent
		{PID: 800, PPID: 1, PGID: 800, Command: argv},                               // grandparent
		{PID: 901, PPID: 1, PGID: pgid, Command: argv},                              // same process group
		{PID: 902, PPID: 1, PGID: 902, Command: argv},                               // the stray
		{PID: 903, PPID: 1, PGID: 903, Command: "/x/claude --resume " + sidS + "0"}, // longer id
	}, nil)
	got := claudeHolderPIDs(sidS)
	if len(got) != 1 || got[0] != 902 {
		t.Fatalf("targets = %v, want only the unrelated stray 902", got)
	}
}

func TestT796StartResultCitesWhatTheGuardDid(t *testing.T) {
	stubTable(t, stubRows(), nil)
	if _, err := reapClaudeSessionHolders(sidS); err != nil {
		t.Fatal(err)
	}
	if c := ClaudeSingleClientCite(sidS); !strings.Contains(c, "80623") {
		t.Fatalf("cite = %q, want the stopped pid", c)
	}
}

// 🎯T796 reopen: the claudia daemon launches a seat itself, so the in-process
// guard never sees that Launch. A stray started earlier outside the daemon
// (the 03:19 grant_held fallback) survived beside the daemon's client. After
// the grant returns, every holder that is not the granted window's client is
// a stray.
func TestT796DaemonGrantedClientKeepsOnlyItsOwnProcess(t *testing.T) {
	rows := stubTable(t, []claudeProc{
		{PID: 75806, PPID: 91440, Command: "/x/claude --resume " + sidS},
		{PID: 500, PPID: 91440, Command: "-zsh"},
		{PID: 28028, PPID: 500, Command: "/x/claude --resume " + sidS},
		{PID: 7001, PPID: 91440, Command: "/x/claude --resume other-session"},
	}, nil)
	old := claudeWindowPanePIDs
	t.Cleanup(func() { claudeWindowPanePIDs = old })
	claudeWindowPanePIDs = func(win string) []int {
		if win != "@164" {
			t.Fatalf("window = %q", win)
		}
		return []int{500}
	}
	stopped, err := reapClaudeStraysExcept(sidS, "@164")
	if err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != 75806 {
		t.Fatalf("stopped = %v, want [75806]", stopped)
	}
	held := claudeHolderPIDs(sidS)
	if len(held) != 1 || held[0] != 28028 {
		t.Fatalf("holders of S = %v, want only the granted 28028 (rows %v)", held, *rows)
	}
	if !claudeProcAlive(7001) {
		t.Fatal("reaped another session's client")
	}
}

// When the granted client cannot be identified nothing is stopped: a guess
// could take the seat itself down.
func TestT796DaemonGrantWithUnknownWindowStopsNothing(t *testing.T) {
	stubTable(t, []claudeProc{
		{PID: 75806, PPID: 91440, Command: "/x/claude --resume " + sidS},
		{PID: 28028, PPID: 500, Command: "/x/claude --resume " + sidS},
	}, nil)
	old := claudeWindowPanePIDs
	t.Cleanup(func() { claudeWindowPanePIDs = old })
	claudeWindowPanePIDs = func(string) []int { return nil }
	stopped, err := reapClaudeStraysExcept(sidS, "@164")
	if err != nil || len(stopped) != 0 {
		t.Fatalf("stopped=%v err=%v", stopped, err)
	}
	if len(claudeHolderPIDs(sidS)) != 2 {
		t.Fatal("touched a holder without knowing which is the seat")
	}
}

func withBroker(t *testing.T, up bool) {
	t.Helper()
	prev := brokerAvailable
	t.Cleanup(func() { brokerAvailable = prev })
	brokerAvailable = func() bool { return up }
}

// 🎯T796 03:19: grant_held made the daemon fall back to an in-process Launch
// and the guard stopped the broker's own client. With a broker reachable a
// holder is refused, never signalled, and nothing launches.
func TestT796BrokerHeldSessionIsRefusedNotStopped(t *testing.T) {
	withBroker(t, true)
	rows := stubTable(t, stubRows(), nil)
	start := GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		t.Fatal("launched a second client beside the broker's")
		return nil, nil
	})
	_, err := start(context.Background(), claudia.Config{Provider: claudia.ProviderClaude, SessionID: sidS})
	if !errors.Is(err, ErrClaudeHeldByBroker) {
		t.Fatalf("err = %v, want ErrClaudeHeldByBroker", err)
	}
	if !claudeProcAlive(80623) || len(*rows) != 3 {
		t.Fatalf("stopped the broker's client: %v", *rows)
	}
}

// A broker is reachable but nobody holds the session: launch as normal.
func TestT796BrokerUpFreeSessionLaunches(t *testing.T) {
	withBroker(t, true)
	stubTable(t, []claudeProc{{PID: 7001, Command: "/x/claude --resume other"}}, nil)
	starts := 0
	start := GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		starts++
		return claudia.StartStub(ctx, cfg, nil)
	})
	a, err := start(context.Background(), claudia.Config{SessionID: sidS, WorkDir: t.TempDir(), TermLogPath: "-"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Stop)
	if starts != 1 {
		t.Fatalf("starts = %d", starts)
	}
}

// 03:45:55: a second SIGHUP cancelled the boot's launch, and the dying daemon
// still stopped the live client at 03:45:56. A cancelled launch stops nothing.
func TestT796CancelledLaunchStopsNothing(t *testing.T) {
	withBroker(t, false)
	rows := stubTable(t, stubRows(), nil)
	start := GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		t.Fatal("launched under a cancelled context")
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := start(ctx, claudia.Config{Provider: claudia.ProviderClaude, SessionID: sidS}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if !claudeProcAlive(80623) || len(*rows) != 3 {
		t.Fatalf("a cancelled launch stopped a client: %v", *rows)
	}
}

// The post-grant stop stands down when a broker is reachable: the other
// holder may be the broker's own.
func TestT796PostGrantStopStandsDownWithBroker(t *testing.T) {
	withBroker(t, true)
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
		t.Fatalf("stopped a holder with a broker up: %v", *rows)
	}
}

// A refused launch is retried, not surfaced as a dead seat.
func TestT796HeldRefusalIsRetriedThenAdopts(t *testing.T) {
	withBroker(t, true)
	pd, pr := heldRetryDelay, heldRetries
	t.Cleanup(func() { heldRetryDelay, heldRetries = pd, pr })
	heldRetryDelay, heldRetries = time.Millisecond, 5
	rows := stubTable(t, stubRows(), nil)
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	tries := 0
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			return claudia.StartStub(ctx, cfg, nil)
		}),
		Adopt: func(cfg claudia.Config) (*claudia.Agent, error) {
			tries++
			if tries < 3 {
				return nil, errors.New("broker protocol: grant_held")
			}
			return claudia.StartStub(context.Background(), cfg, nil)
		},
	})
	if err := reg.Register(claudia.AgentDef{Name: "po", WorkDir: t.TempDir(), SessionID: sidS,
		Provider: claudia.ProviderClaude, AutoStart: true, TermLogPath: "-"}); err != nil {
		t.Fatal(err)
	}
	if _, err := adoptOrLaunchRetryingHeld(context.Background(), reg, "po"); err != nil {
		t.Fatalf("err = %v", err)
	}
	t.Cleanup(reg.StopAll)
	if !claudeProcAlive(80623) || len(*rows) != 3 {
		t.Fatalf("the holder was stopped: %v", *rows)
	}
}
