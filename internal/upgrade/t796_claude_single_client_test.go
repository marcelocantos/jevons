// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
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
