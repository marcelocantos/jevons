// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package uidaemon

import (
	"runtime"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/launchdfake"
	"github.com/marcelocantos/jevons/internal/supervise"
)

// 🎯T1097: Install and Uninstall drive launchd through the supervise seam,
// so a test answers for launchd and sees exactly what was asked of it —
// and the owner's gui/<uid> domain hears nothing.
func TestT1097InstallAndUninstallGoThroughTheFake(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("launchd agents are macOS only")
	}
	launchdfake.Use(t, func(args ...string) ([]byte, error) {
		if args[0] == "bootstrap" || args[0] == "bootout" {
			return nil, nil
		}
		return launchdfake.Empty(args...)
	})
	before := len(launchdfake.Calls())
	home := t.TempDir()
	spec := Spec{Binary: "/nonexistent/jevonsd", Home: home, StateDir: t.TempDir()}
	if err := Install(spec); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	var got []string
	for _, c := range launchdfake.Calls()[before:] {
		got = append(got, strings.Join(c, " "))
	}
	want := []string{
		"bootstrap " + supervise.AgentDomain() + " " + reactPlistPath(home),
		"bootout " + supervise.AgentDomain() + "/" + ReactLabel,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("launchctl calls:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}
