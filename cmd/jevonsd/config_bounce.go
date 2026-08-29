// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/marcelocantos/jevons/internal/butler"
	"github.com/marcelocantos/jevons/internal/server"
)

// configBounceEnv disables the forced restart (isolates, tests, and an owner
// who wants to batch several structural edits before one bounce).
const configBounceEnv = "JEVONS_CONFIG_BOUNCE"

// configEventComponent is the eventlog component for config reload notices.
const configEventComponent = "config"

var bounceOnce sync.Once

// configBounceArmed is set by main once the boot config is known: only a
// supervised daemon (launchd KeepAlive re-raises it — the development
// daemon on DailyPort with the default state dir) may bounce. An isolate
// or journey daemon has no supervisor to come back under, so it only
// notifies.
var configBounceArmed bool

// bounceForConfig is the 🎯T574 escape hatch for fields that a running
// process cannot rejig — listen address, identity, state paths, the cost
// guard's `disabled` — so an edit never sits silently ignored. It tells the
// owner and the overseer which fields forced it, then takes the 🎯T392.5
// upgrade exit (self-SIGHUP: in-flight agent turns preserved, launchd
// KeepAlive re-raises). It fires at most once per process: the successor
// reads the new file at boot.
//
// With JEVONS_CONFIG_BOUNCE=0 (isolates, journeys, batching edits) it only
// notifies; the fields are listed so the owner knows a restart is owed.
func bounceForConfig(srv *server.Server, path string, fields []string) {
	msg := "config reload: " + filepath.Base(path) + " changed restart-only fields [" +
		strings.Join(fields, ", ") + "]"
	disabled := os.Getenv(configBounceEnv) == "0" || !configBounceArmed
	if disabled {
		msg += " — restart the daemon to apply"
	} else {
		msg += " — bouncing the daemon to apply"
	}
	slog.Warn(msg, "path", path, "fields", fields, "bounce", !disabled)
	if srv != nil {
		srv.Broadcast(map[string]any{"type": "capacity_notice", "message": msg})
		srv.LogEvent(configEventComponent, "restart_only_change", map[string]any{"path": path, "fields": fields, "bounce": !disabled})
		if err := srv.DeliverToOverseerAs(butler.FormatEventPush(configEventComponent, msg), "agent"); err != nil {
			slog.Warn("config reload: overseer notice deliver failed", "err", err)
		}
	}
	if disabled {
		return
	}
	bounceOnce.Do(func() {
		// 🎯T392.5 upgrade exit: SIGHUP to ourselves skips StopAll, writes
		// the reattach handles so in-flight agent turns survive, and frees
		// the port; launchd KeepAlive (🎯T553.3) re-raises the same binary,
		// which reads the new file at boot.
		slog.Warn("config reload: requesting upgrade exit (SIGHUP self)", "fields", fields)
		if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
			slog.Error("config reload: self-SIGHUP failed", "err", err)
		}
	})
}
