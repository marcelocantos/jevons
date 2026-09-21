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
	"sync"
	"syscall"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"
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
	return stopClaudeHolders(sessionID, claudeHolderPIDs(sessionID))
}

// claudeWindowPanePIDs returns the pane pids of a tmux window on claudia's
// socket; a test replaces it. Nil when the window or server is gone.
var claudeWindowPanePIDs = tmuxWindowPanePIDs

func tmuxWindowPanePIDs(windowID string) []int {
	if strings.TrimSpace(windowID) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "-S", cli.AgentTmuxSocket(),
		"list-panes", "-t", windowID, "-F", "#{pane_pid}").Output()
	if err != nil {
		return nil
	}
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if pid, e := strconv.Atoi(f); e == nil && pid > 1 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// reapClaudeStraysExcept stops every claude bound to sessionID that is not
// the client running in windowID. It is the post-grant half of the guard
// (🎯T796): the claudia daemon launches seats itself, so the in-process
// Start guard never sees that Launch, and a client started earlier outside
// the daemon (the grant_held fallback) would stay beside it. When the
// window's own client cannot be identified nothing is stopped — a guess
// could take the seat down.
func reapClaudeStraysExcept(sessionID, windowID string) ([]int, error) {
	holders := claudeHolderPIDs(sessionID)
	if len(holders) < 2 {
		return nil, nil
	}
	panes := claudeWindowPanePIDs(windowID)
	byPID := map[int]claudeProc{}
	for _, p := range claudeProcessTable() {
		byPID[p.PID] = p
	}
	inWindow := func(pid int) bool {
		for hops := 0; pid > 1 && hops < 64; hops++ {
			for _, pane := range panes {
				if pid == pane {
					return true
				}
			}
			pid = byPID[pid].PPID
		}
		return false
	}
	var keep, strays []int
	for _, pid := range holders {
		if inWindow(pid) {
			keep = append(keep, pid)
		} else {
			strays = append(strays, pid)
		}
	}
	if len(keep) == 0 {
		slog.Warn("claude guard: granted client not identifiable; leaving all holders",
			"session", sessionID, "window", windowID, "holders", fmt.Sprint(holders))
		return nil, nil
	}
	return stopClaudeHolders(sessionID, strays)
}

func stopClaudeHolders(sessionID string, pids []int) ([]int, error) {
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
	recordClaudeReap(sessionID, pids)
	if len(pids) > 0 {
		slog.Warn("stopped stray claude clients before launch: one client per session",
			"session", sessionID, "pids", fmt.Sprint(pids))
	}
	return pids, nil
}

var (
	claudeReapMu   sync.Mutex
	claudeReapNote = map[string]string{}
)

// ClaudeSingleClientCite is the jevons_agent_start fragment saying what the
// one-client-per-session guard did for sessionID's last Launch (🎯T796): which
// stray pids it stopped, or that it found none. Empty when no Launch of that
// session ran the guard (an adopted seat), which is itself the "kept" case.
func ClaudeSingleClientCite(sessionID string) string {
	claudeReapMu.Lock()
	defer claudeReapMu.Unlock()
	return claudeReapNote[sessionID]
}

func recordClaudeReap(sessionID string, pids []int) {
	note := "claude_single_client: no other client held the session"
	if len(pids) > 0 {
		note = fmt.Sprintf("claude_single_client: stopped stray claude pid(s) %v before launch", pids)
	}
	claudeReapMu.Lock()
	claudeReapNote[sessionID] = note
	claudeReapMu.Unlock()
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

// ReapClaudeStraysAfterGrant runs the post-grant one-client check for a Claude
// seat the Registry just started or adopted (🎯T796).
func ReapClaudeStraysAfterGrant(reg *claudia.Registry, name string) {
	def := reg.Def(name)
	proc := reg.Get(name)
	if def == nil || proc == nil || !isClaudeProvider(def.Provider) || def.SessionID == "" {
		return
	}
	if _, err := reapClaudeStraysExcept(def.SessionID, proc.WindowID()); err != nil {
		slog.Error("claude guard: stray client survived", "agent", name, "err", err)
	}
}
