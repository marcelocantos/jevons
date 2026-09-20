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

func cursorLeftoversAlive(sessionID string) bool {
	return len(cursorLeftoverPIDs(sessionID)) > 0
}

func cursorLeftoverPIDs(sessionID string) []int {
	self := os.Getpid()
	var out []int
	for _, pid := range listStoreWriterPIDs(claudia.CursorACPStorePath(sessionID)) {
		if pid <= 1 || pid == self {
			continue
		}
		out = append(out, pid)
	}
	return out
}

func listStoreWriterPIDs(path string) []int {
	if path == "" {
		return nil
	}
	var pids []int
	seen := map[int]struct{}{}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		out, err := exec.Command("lsof", "-t", "--", p).Output()
		if err != nil {
			continue
		}
		for _, line := range bytes.Split(out, []byte("\n")) {
			s := strings.TrimSpace(string(line))
			if s == "" {
				continue
			}
			pid, err := strconv.Atoi(s)
			if err != nil || pid <= 1 {
				continue
			}
			if _, ok := seen[pid]; ok {
				continue
			}
			seen[pid] = struct{}{}
			pids = append(pids, pid)
		}
	}
	return pids
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
