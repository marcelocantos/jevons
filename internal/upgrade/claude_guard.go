// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/marcelocantos/claudia"
)

// claudeProc is one row of the process table.
type claudeProc struct {
	PID, PPID, PGID int
	Command         string
}

// claudeProcessTable lists every process on the host; a test replaces it with
// a stub table so no real seat is ever touched.
var claudeProcessTable = psProcessTable

// signalClaudeProc and claudeProcAlive are the kill seams.
var (
	signalClaudeProc = func(pid int, sig syscall.Signal) error { return syscall.Kill(pid, sig) }
	claudeProcAlive  = func(pid int) bool { return syscall.Kill(pid, 0) == nil }
)

// claudeStopWait is how long a holder gets to exit after SIGTERM before
// SIGKILL, and again after SIGKILL before the Launch is refused.
var claudeStopWait = 3 * time.Second

// claudeHolderPIDs returns live claude processes started with --session-id or
// --resume of sessionID, excluding this process, its ancestors and every
// process in its process group: a guard that could stop the caller's own
// tree would take the seat that asked for the launch down with it. This is what bound two
// clients to one session JSONL on 2026-09-22 (jevons-po: --session-id pid
// 80623 plus --resume pid 59999).
func claudeHolderPIDs(sessionID string) []int {
	if sessionID == "" {
		return nil
	}
	self := os.Getpid()
	table := claudeProcessTable()
	byPID := map[int]claudeProc{}
	for _, p := range table {
		byPID[p.PID] = p
	}
	protected := map[int]bool{self: true}
	for pid, hops := byPID[self].PPID, 0; pid > 1 && hops < 64; hops++ {
		protected[pid] = true
		pid = byPID[pid].PPID
	}
	selfPGID := byPID[self].PGID
	if selfPGID == 0 {
		selfPGID = syscall.Getpgrp()
	}
	var out []int
	for _, p := range table {
		if p.PID <= 1 || protected[p.PID] || (selfPGID > 1 && p.PGID == selfPGID) {
			continue
		}
		if commandHoldsClaudeSession(p.Command, sessionID) {
			out = append(out, p.PID)
		}
	}
	return out
}

// commandHoldsClaudeSession reports whether command is a claude binary bound
// to sessionID by --session-id or --resume (spaced or =-joined).
func commandHoldsClaudeSession(command, sessionID string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 || filepath.Base(fields[0]) != "claude" {
		return false
	}
	for i, f := range fields[1:] {
		switch f {
		case "--session-id", "--resume":
			if i+2 < len(fields) && fields[i+2] == sessionID {
				return true
			}
		case "--session-id=" + sessionID, "--resume=" + sessionID:
			return true
		}
	}
	return false
}

// reapClaudeSessionHolders stops every claude process already bound to
// sessionID so the Launch about to run is the only one. Adoption of a live
// seat never reaches Start, so anything found here is a stray. It fails closed
// when a holder survives SIGKILL. The stopped pids are returned for the log.
func reapClaudeSessionHolders(sessionID string) ([]int, error) {
	pids := claudeHolderPIDs(sessionID)
	for _, pid := range pids {
		_ = signalClaudeProc(pid, syscall.SIGTERM)
	}
	if !waitClaudeGone(pids) {
		for _, pid := range pids {
			if claudeProcAlive(pid) {
				_ = signalClaudeProc(pid, syscall.SIGKILL)
			}
		}
		if !waitClaudeGone(pids) {
			return pids, fmt.Errorf("claude %v still holds session %s after SIGKILL; refusing a second client", pids, sessionID)
		}
	}
	if len(pids) > 0 {
		slog.Warn("stopped stray claude clients before launch: one client per session",
			"session", sessionID, "pids", fmt.Sprint(pids))
	}
	return pids, nil
}

func waitClaudeGone(pids []int) bool {
	deadline := time.Now().Add(claudeStopWait)
	for {
		alive := false
		for _, pid := range pids {
			if claudeProcAlive(pid) {
				alive = true
			}
		}
		if !alive {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func psProcessTable() []claudeProc {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,pgid=,command=").Output()
	if err != nil && len(out) == 0 {
		slog.Warn("claude guard: ps failed", "err", err)
		return nil
	}
	var rows []claudeProc
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		pgid, e3 := strconv.Atoi(f[2])
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		cmd := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, f[0])), f[1])), f[2]))
		rows = append(rows, claudeProc{PID: pid, PPID: ppid, PGID: pgid, Command: cmd})
	}
	return rows
}

func isClaudeProvider(p claudia.Provider) bool {
	return p == "" || p == claudia.ProviderClaude
}
