// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/marcelocantos/jevons/internal/portown"
	"github.com/marcelocantos/jevons/internal/supervise"
)

// inspectPortOwnership is 🎯T710: shout when a foreign process holds the
// same port, or when localhost and 127.0.0.1 serve different fleets.
//
// It does not change the serving boolean Decide sees. Restarting the
// 127.0.0.1 holder because localhost is an impostor would free our bind
// and can hand the owner URL to the squatter.
func inspectPortOwnership(port int, serving bool) {
	if !serving {
		return
	}
	c, fleet := portown.Inspect(portown.InspectArgs{Port: port})
	text := portown.AlarmText(c, fleet)
	if text == "" {
		return
	}
	logf("port-ownership: %s", text)
	notifyPortConflict(port, text)
}

func notifyPortConflict(port int, text string) {
	blurter, err := exec.LookPath("blurter")
	if err != nil {
		logf("no blurter on PATH; skipping the port-squatter notice")
		return
	}
	cmd := exec.Command(blurter, "send",
		"--app", "jevons-watchdog",
		"--severity", string(supervise.NotifyProblem),
		"--key", fmt.Sprintf("jevonsd-port-squatter-%d", port),
		"--subject", fmt.Sprintf("jevons port conflict on :%d", port),
		"--body", text,
		"--quiet")
	if out, err := cmd.CombinedOutput(); err != nil {
		logf("blurter port-squatter notice failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
}
