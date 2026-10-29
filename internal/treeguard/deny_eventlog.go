// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"os"
	"strings"

	"github.com/marcelocantos/jevons/internal/commitattrib"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/eventlog"
)

const stateDirEnv = "JEVONS_STATE_DIR"

// LogBranchCheckoutDenial appends a durable event when a shared-clone branch
// checkout is refused (🎯T955 acceptance: visible in the eventlog). Best-effort
// only — a denial still returns exit 2 when logging fails.
func LogBranchCheckoutDenial(d Decision) {
	if d.Verdict != Deny || d.Reason != "branch-switch-in-shared-clone" {
		return
	}
	stateDir := strings.TrimSpace(os.Getenv(stateDirEnv))
	if stateDir == "" {
		cfg, err := config.Load(config.DefaultPath())
		if err == nil && strings.TrimSpace(cfg.StateDir) != "" {
			stateDir = cfg.StateDir
		} else {
			stateDir = config.Default().StateDir
		}
	}
	j, err := eventlog.Open(eventlog.DefaultPath(stateDir))
	if err != nil {
		return
	}
	fields := map[string]any{
		"msg":    d.Message,
		"reason": d.Reason,
	}
	if seat := strings.TrimSpace(os.Getenv(commitattrib.ActorEnv)); seat != "" {
		fields["seat"] = seat
	}
	eventlog.Log(j, "treeguard", "branch-checkout-refused", fields)
}
