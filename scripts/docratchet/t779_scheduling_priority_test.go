// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

func TestT779SupervisorLaunchAgentInteractive(t *testing.T) {
	plist := readRepo(t, "supervisor/homebrew.mxcl.supervisor.plist")
	for _, m := range []string{
		"ProcessType",
		"Interactive",
		"homebrew.mxcl.supervisor",
		"supervisord",
	} {
		if !strings.Contains(plist, m) {
			t.Errorf("supervisor launchagent plist missing 🎯T779 marker %q", m)
		}
	}

	install := readRepo(t, "supervisor/install-launchagent.sh")
	for _, m := range []string{
		"🎯T779",
		"ProcessType",
		"Interactive",
		"homebrew.mxcl.supervisor",
	} {
		if !strings.Contains(install, m) {
			t.Errorf("install-launchagent.sh missing 🎯T779 marker %q", m)
		}
	}

	root := readRepo(t, "supervisor/install.sh")
	if !strings.Contains(root, "install-launchagent.sh") {
		t.Error("supervisor/install.sh must invoke install-launchagent.sh (🎯T779)")
	}
	if !strings.Contains(root, "SUPERVISOR_SKIP_LAUNCHAGENT") {
		t.Error("supervisor/install.sh must document SUPERVISOR_SKIP_LAUNCHAGENT for hermetic installs")
	}
}

func TestT779DoctrineMarkers(t *testing.T) {
	for _, doc := range []struct{ name, body string }{
		{"AGENTS.md", readRepo(t, "AGENTS.md")},
		{"agents-guide.md", readRepo(t, "agents-guide.md")},
		{"internal/config/persona.md", readRepo(t, "internal/config/persona.md")},
	} {
		for _, m := range []string{
			"🎯T779",
			"ProcessType",
			"Interactive",
			"homebrew.mxcl.supervisor",
		} {
			if !strings.Contains(doc.body, m) {
				t.Errorf("%s missing 🎯T779 doctrine marker %q", doc.name, m)
			}
		}
	}
}
