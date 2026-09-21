// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// cursorStoreClearWait is how long a Launch waits for reaped store.db
// writers to actually exit. SIGKILL is async; stacking a second ACP
// client before the leftover releases the inode is 🎯T541.1.
var cursorStoreClearWait = 2 * time.Second

// waitCursorStoreClear is a seam so fail-loud hermetics do not need an
// unkillable leftover.
var waitCursorStoreClear = waitCursorStoreClearImpl

// GuardCursorStart wraps a Registry Start so a Cursor Launch cannot mint
// a second ACP client while a leftover still holds store.db. Non-Cursor
// providers pass through. Inner nil uses [claudia.StartDirectContext].
func GuardCursorStart(start func(context.Context, claudia.Config) (*claudia.Agent, error)) func(context.Context, claudia.Config) (*claudia.Agent, error) {
	if start == nil {
		start = claudia.StartDirectContext
	}
	return func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		if cfg.Provider == claudia.ProviderCursor {
			if err := refuseStackedCursorLaunch(cfg); err != nil {
				return nil, err
			}
		}
		return start(ctx, cfg)
	}
}

// InstallCursorLaunchGuard installs [GuardCursorStart] on reg so every
// in-process Launch (including AdoptOrLaunch fallback) is the T541.1
// fail-loud path. Daemon-held grants are unaffected: they reclaim by
// name and never call this Start.
func InstallCursorLaunchGuard(reg *claudia.Registry) {
	if reg == nil {
		return
	}
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: GuardCursorStart(claudia.StartDirectContext),
	})
}

func refuseStackedCursorLaunch(cfg claudia.Config) error {
	sid := cfg.SessionID
	extra := cfg.ConnectPID
	claudia.ReapCursorACPLeftovers(sid, extra)
	if waitCursorStoreClear(sid, extra) {
		return nil
	}
	pids := cursorLeftoverPIDs(sid)
	return fmt.Errorf("leftover cursor-agent %v still holds store for session %s: %w",
		pids, sid, claudia.ErrCursorResumeDenied)
}

func waitCursorStoreClearImpl(sessionID string, extraPID int) bool {
	_ = extraPID
	deadline := time.Now().Add(cursorStoreClearWait)
	for {
		if !cursorLeftoversAlive(sessionID) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// cursorLeftoversAlive reports whether anything may still hold the store.
// An lsof that could not answer in time counts as yes: the guard exists to
// stop a second ACP client opening a store another still writes, so not
// knowing fails closed.
func cursorLeftoversAlive(sessionID string) bool {
	pids, ok := storeWriterPIDs(claudia.CursorACPStorePath(sessionID))
	if !ok {
		return true
	}
	for _, pid := range pids {
		if pid > 1 && pid != os.Getpid() {
			return true
		}
	}
	return false
}

func cursorLeftoverPIDs(sessionID string) []int {
	self := os.Getpid()
	var out []int
	pids, _ := storeWriterPIDs(claudia.CursorACPStorePath(sessionID))
	for _, pid := range pids {
		if pid <= 1 || pid == self {
			continue
		}
		out = append(out, pid)
	}
	return out
}

// lsofTimeout bounds one holder check. lsof stats the open files of every
// process on the host, and a single stat on a stalled mount can block it
// indefinitely. With no bound, that stall held ReattachFleetContext — and so
// the rest of jevonsd's main, including the cockpit loop that drives the
// fleet pass — for as long as lsof hung: on 2026-09-21 no boot after 14:05
// reached StartCockpitConverge (a goroutine dump put main in this call).
var lsofTimeout = 3 * time.Second

// lsofCommand is the holder probe; a test replaces it.
var lsofCommand = "lsof"

// storeWriterPIDs lists processes holding the store or its -wal/-shm, in one
// lsof call. ok is false when lsof did not answer within lsofTimeout, which
// callers must treat as "may be held", never as "clear". A plain lsof exit
// status of 1 means no process holds the files, which is an answer.
func storeWriterPIDs(path string) ([]int, bool) {
	if path == "" {
		return nil, true
	}
	var args []string
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(p); err == nil {
			args = append(args, p)
		}
	}
	if len(args) == 0 {
		return nil, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), lsofTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, lsofCommand, append([]string{"-t", "--"}, args...)...).Output()
	if ctx.Err() != nil {
		slog.Warn("cursor guard: lsof did not answer in time; treating store as held",
			"path", path, "timeout", lsofTimeout)
		return nil, false
	}
	_ = err // lsof exits 1 when nothing holds the files
	var pids []int
	seen := map[int]struct{}{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		pid, err := strconv.Atoi(strings.TrimSpace(string(line)))
		if err != nil || pid <= 1 {
			continue
		}
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		pids = append(pids, pid)
	}
	return pids, true
}

// hushUnreapedCursorSeats disables AutoStart on Cursor rows whose leftover
// writers survived reap+wait, so PreferAdopt does not Launch a second
// client. Later cockpit Launch still hits [GuardCursorStart].
func hushUnreapedCursorSeats(reg *claudia.Registry) {
	if reg == nil {
		return
	}
	for _, d := range reg.List() {
		if d.Provider != claudia.ProviderCursor {
			continue
		}
		if waitCursorStoreClear(d.SessionID, d.ConnectPID) {
			continue
		}
		slog.Error("cursor leftover survived reap; refusing second ACP client",
			"name", d.Name, "session", d.SessionID,
			"pids", fmt.Sprint(cursorLeftoverPIDs(d.SessionID)))
		d.AutoStart = false
		if err := reg.Register(d); err != nil {
			slog.Error("could not disable AutoStart on unreaped cursor seat",
				"name", d.Name, "err", err)
		}
	}
}
