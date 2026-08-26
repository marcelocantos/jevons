// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// DaemonLabel is the launchd job that KeepAlives daily jevonsd (🎯T553.3).
// This replaces com.marcelocantos.jevons-watchdog as the standing
// supervisor: if the process dies, launchd relaunches the same
// ProgramArguments binary. The fat restart script is not on this path.
const DaemonLabel = "com.marcelocantos.jevonsd"

// DaemonSpec is everything the KeepAlive job needs at install time.
type DaemonSpec struct {
	Binary   string
	Workdir  string
	Port     int
	StateDir string
	PathEnv  string
	LogPath  string
}

// DaemonPlistPath is where the KeepAlive LaunchAgent lives.
func DaemonPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", DaemonLabel+".plist")
}

// DaemonLogPath is the KeepAlive job's stdout/stderr.
func DaemonLogPath(stateDir string) string {
	if stateDir == "" {
		stateDir = filepath.Join(os.Getenv("HOME"), ".jevons")
	}
	return filepath.Join(stateDir, "daily-jevonsd.log")
}

// DaemonPlistXML renders a KeepAlive job for repo bin/jevonsd.
//
// KeepAlive is the point (🎯T553.3). A SIGHUP upgrade-exit is a clean
// process death; launchd then execs the same path, which buildsnap has
// already replaced. The restart script must not also nohup-start a
// second copy — that is the brew-reclaim fight this job exists to end.
func DaemonPlistXML(spec DaemonSpec) string {
	if spec.Port == 0 {
		spec.Port = 13705
	}
	if spec.LogPath == "" {
		spec.LogPath = DaemonLogPath(spec.StateDir)
	}
	args := []string{
		spec.Binary,
		"-port", strconv.Itoa(spec.Port),
		"-vanilla-port", "0",
		"-workdir", spec.Workdir,
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("    <key>Label</key>\n    <string>" + esc(DaemonLabel) + "</string>\n")
	b.WriteString("    <key>ProgramArguments</key>\n    <array>\n")
	for _, a := range args {
		b.WriteString("        <string>" + esc(a) + "</string>\n")
	}
	b.WriteString("    </array>\n")
	if spec.PathEnv != "" {
		b.WriteString("    <key>EnvironmentVariables</key>\n    <dict>\n")
		b.WriteString("        <key>PATH</key>\n        <string>" + esc(spec.PathEnv) + "</string>\n")
		b.WriteString("    </dict>\n")
	}
	b.WriteString("    <key>KeepAlive</key>\n    <true/>\n")
	b.WriteString("    <key>RunAtLoad</key>\n    <true/>\n")
	if spec.Workdir != "" {
		b.WriteString("    <key>WorkingDirectory</key>\n    <string>" + esc(spec.Workdir) + "</string>\n")
	}
	b.WriteString("    <key>StandardOutPath</key>\n    <string>" + esc(spec.LogPath) + "</string>\n")
	b.WriteString("    <key>StandardErrorPath</key>\n    <string>" + esc(spec.LogPath) + "</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// WriteDaemonPlist writes the KeepAlive LaunchAgent.
func WriteDaemonPlist(path string, spec DaemonSpec) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("supervise: LaunchAgents dir: %w", err)
	}
	return writeAtomic(path, []byte(DaemonPlistXML(spec)))
}

// DaemonOwnsProcess reports whether launchd KeepAlive is holding jevonsd.
func DaemonOwnsProcess() bool {
	ok, err := AgentLoaded(DaemonLabel)
	return err == nil && ok
}

// KickstartAgent asks launchd to run the job now (and kill a wedged
// instance first). Missing job is the caller's problem.
func KickstartAgent(label string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("supervise: launchd agents are macOS only")
	}
	out, err := exec.Command("launchctl", "kickstart", "-k", AgentDomain()+"/"+label).CombinedOutput()
	if err != nil {
		return fmt.Errorf("supervise: kickstart %s: %w: %s", label, err, string(out))
	}
	return nil
}

// SkipWatchdogSupervise is true when KeepAlive owns jevonsd, so the
// daemon must not reinstall the probe-that-calls-restart-daily.
func SkipWatchdogSupervise() bool {
	return DaemonOwnsProcess()
}
