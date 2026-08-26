// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/supervise"
)

func TestDaemonPlistIsKeepAliveOnJevonsd(t *testing.T) {
	xml := supervise.DaemonPlistXML(supervise.DaemonSpec{
		Binary:   "/repo/bin/jevonsd",
		Workdir:  "/repo",
		Port:     13705,
		StateDir: "/tmp/state",
		PathEnv:  "/opt/homebrew/bin:/usr/bin:/bin",
		LogPath:  "/tmp/state/daily-jevonsd.log",
	})
	for _, want := range []string{
		supervise.DaemonLabel,
		"<key>KeepAlive</key>",
		"<true/>",
		"/repo/bin/jevonsd",
		"-port",
		"13705",
		"-vanilla-port",
		"0",
		"-workdir",
		"/repo",
		"<key>WorkingDirectory</key>",
		"/opt/homebrew/bin",
		"daily-jevonsd.log",
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("DaemonPlistXML missing %q\n%s", want, xml)
		}
	}
	if strings.Contains(xml, "restart-daily-jevonsd") {
		t.Error("KeepAlive plist must not invoke the fat restart script")
	}
	if strings.Contains(xml, supervise.AgentLabel) {
		t.Error("daemon KeepAlive must not be the watchdog job")
	}
}

func TestDaemonPlistPath(t *testing.T) {
	got := supervise.DaemonPlistPath("/Users/x")
	want := filepath.Join("/Users/x", "Library", "LaunchAgents", supervise.DaemonLabel+".plist")
	if got != want {
		t.Fatalf("DaemonPlistPath=%q want %q", got, want)
	}
}

func TestWriteDaemonPlist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "LaunchAgents", supervise.DaemonLabel+".plist")
	err := supervise.WriteDaemonPlist(path, supervise.DaemonSpec{
		Binary:  "/repo/bin/jevonsd",
		Workdir: "/repo",
		Port:    13705,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "<key>KeepAlive</key>") {
		t.Fatalf("written plist missing KeepAlive:\n%s", b)
	}
}
