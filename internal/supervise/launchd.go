// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentLabel is the launchd job label for the daily-daemon watchdog.
const AgentLabel = "com.marcelocantos.jevons-watchdog"

// AgentInterval is how often launchd runs one probe, in seconds. With
// the default 90s grace this puts a ceiling of about two minutes on how
// long a dead daemon can stay dead before something acts.
const AgentInterval = 30

// AgentSpec is everything the launchd job needs to know.
type AgentSpec struct {
	// Binary is the watchdog, normally <repo>/bin/jevons-watchdog.
	Binary string
	// Repo holds scripts/restart-daily-jevonsd.sh.
	Repo string
	// StateDir is the daemon's state dir.
	StateDir string
	// Port is the daily port.
	Port int
	// LogPath is where launchd sends the job's output.
	LogPath string
	// PathEnv is the PATH the job runs with (🎯T434). Empty leaves the
	// plist silent, which means launchd's default — and a restart that
	// cannot find the Go toolchain it builds its helpers with. Install
	// fills this from AgentPATH.
	PathEnv string
}

// AgentPlistPath is where the LaunchAgent lives for the current user.
func AgentPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", AgentLabel+".plist")
}

// AgentLogPath is the watchdog's own log.
func AgentLogPath(stateDir string) string {
	return filepath.Join(Dir(stateDir), "watchdog.log")
}

// PlistXML renders the LaunchAgent.
//
// launchd rather than a loop inside jevonsd, or a wrapper around it,
// because the supervisor has to survive exactly the events that kill
// everything else here: the daemon exiting, the agent that asked for the
// restart being stopped by that exit, and the whole process group going
// with it. launchd is the one thing on this machine that outlives all of
// those, and brew services already uses it for the installed daemon.
func PlistXML(spec AgentSpec) string {
	args := []string{
		spec.Binary,
		"-port", fmt.Sprint(spec.Port),
		"-repo", spec.Repo,
		"-state", spec.StateDir,
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	b.WriteString("    <key>Label</key>\n    <string>" + esc(AgentLabel) + "</string>\n")
	b.WriteString("    <key>ProgramArguments</key>\n    <array>\n")
	for _, a := range args {
		b.WriteString("        <string>" + esc(a) + "</string>\n")
	}
	b.WriteString("    </array>\n")
	// 🎯T434: without this the job runs on LaunchdDefaultPATH, where there
	// is no `go` to build the restart script's helpers with and no blurter
	// to tell the owner that the restart therefore refused.
	if spec.PathEnv != "" {
		b.WriteString("    <key>EnvironmentVariables</key>\n    <dict>\n")
		b.WriteString("        <key>PATH</key>\n        <string>" + esc(spec.PathEnv) + "</string>\n")
		b.WriteString("    </dict>\n")
	}
	fmt.Fprintf(&b, "    <key>StartInterval</key>\n    <integer>%d</integer>\n", AgentInterval)
	// RunAtLoad so a login, or an install, checks immediately instead of
	// leaving a dead daemon dead for the first interval.
	b.WriteString("    <key>RunAtLoad</key>\n    <true/>\n")
	b.WriteString("    <key>StandardOutPath</key>\n    <string>" + esc(spec.LogPath) + "</string>\n")
	b.WriteString("    <key>StandardErrorPath</key>\n    <string>" + esc(spec.LogPath) + "</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

func esc(s string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return s
	}
	return buf.String()
}

// WriteAgentPlist writes the LaunchAgent, creating its directory.
func WriteAgentPlist(path string, spec AgentSpec) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("supervise: LaunchAgents dir: %w", err)
	}
	return writeAtomic(path, []byte(PlistXML(spec)))
}
