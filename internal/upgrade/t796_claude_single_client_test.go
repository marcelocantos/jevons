// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
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
		{80623, "/Users/m/.local/bin/claude --permission-mode bypassPermissions --session-id " + sidS + " --mcp-config {}"},
		{7001, "/Users/m/.local/bin/claude --permission-mode bypassPermissions --resume other-session --mcp-config {}"},
		{7002, "/bin/zsh -c echo claude --resume " + sidS},
	}
}

// A live process on session S plus a resume of S leaves exactly one process.
func TestT796LiveClientPlusResumeLeavesOne(t *testing.T) {
	rows := stubTable(t, stubRows(), nil)
	starts := 0
	start := GuardCursorStart(func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		starts++
		// The new client comes up as the only holder of S.
		*rows = append(*rows, claudeProc{59999, "/x/claude --permission-mode bypassPermissions --resume " + sidS})
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
