// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package uidaemon

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/marcelocantos/jevons/internal/config"
)

// ReactPlistXML is the StartInterval React-surface probe. KeepAlive is
// deliberately absent here: the process owner is com.marcelocantos.jevonsd
// (🎯T553.3), not this probe. A second KeepAlive jevonsd would fight
// SIGHUP the same way brew services did (🎯T405).
func ReactPlistXML(spec Spec) string {
	log := filepath.Join(spec.StateDir, "ui-react-probe.log")
	args := []string{spec.Binary, "-ui", "probe"}
	return plistXML(ReactLabel, args, spec.PathEnv, log, ReactProbeInterval, false)
}

// VanillaPlistXML is the KeepAlive UI-only vanilla server. It does not
// open ~/.jevons — no second full daemon on DailyVanillaPort.
func VanillaPlistXML(spec Spec) string {
	up := spec.Upstream
	if up == "" {
		up = defaultUpstream()
	}
	log := filepath.Join(spec.StateDir, "ui-vanilla.log")
	args := []string{
		spec.Binary, "-ui", "vanilla",
		"-port", strconv.Itoa(config.DailyVanillaPort),
		"-upstream", up,
	}
	return plistXML(VanillaLabel, args, spec.PathEnv, log, 0, true)
}

func plistXML(label string, args []string, pathEnv, logPath string, interval int, keepAlive bool) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	b.WriteString("    <key>Label</key>\n    <string>" + esc(label) + "</string>\n")
	b.WriteString("    <key>ProgramArguments</key>\n    <array>\n")
	for _, a := range args {
		b.WriteString("        <string>" + esc(a) + "</string>\n")
	}
	b.WriteString("    </array>\n")
	if pathEnv != "" {
		b.WriteString("    <key>EnvironmentVariables</key>\n    <dict>\n")
		b.WriteString("        <key>PATH</key>\n        <string>" + esc(pathEnv) + "</string>\n")
		b.WriteString("    </dict>\n")
	}
	if interval > 0 {
		fmt.Fprintf(&b, "    <key>StartInterval</key>\n    <integer>%d</integer>\n", interval)
	}
	if keepAlive {
		b.WriteString("    <key>KeepAlive</key>\n    <true/>\n")
	}
	b.WriteString("    <key>RunAtLoad</key>\n    <true/>\n")
	b.WriteString("    <key>StandardOutPath</key>\n    <string>" + esc(logPath) + "</string>\n")
	b.WriteString("    <key>StandardErrorPath</key>\n    <string>" + esc(logPath) + "</string>\n")
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

func writePlist(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("uidaemon: LaunchAgents dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
