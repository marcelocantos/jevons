// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package landhold

import (
	"os"
	"strings"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/eventlog"
)

func LogRefusal(r *Refusal, command string) {
	if r == nil {
		return
	}
	stateDir := strings.TrimSpace(os.Getenv("JEVONS_STATE_DIR"))
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
		"seat":    r.Seat,
		"action":  actionName(r.Action),
		"hold":    r.Reason,
		"message": r.Message(),
	}
	if command != "" {
		fields["command"] = command
	}
	eventlog.Log(j, "landhold", "refused", fields)
}

func actionName(a Action) string {
	switch a {
	case ActionIntegrate:
		return "integrate"
	case ActionRestart:
		return "restart-jevonsd"
	default:
		return "unknown"
	}
}
