// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/supervise"
)

// 🎯T434 — the pure half of "the supervisor's restart does not depend on a
// toolchain launchd cannot reach". The end-to-end half, which runs the
// shipped restart script under launchd's own PATH, lives in
// cmd/jevons-watchdog/t434_toolchain_test.go.

// fakeLookPath answers from a table instead of the machine's PATH, so
// these tests say the same thing on a host with no Homebrew.
func fakeLookPath(found map[string]string) supervise.LookPath {
	return func(tool string) (string, error) {
		if p, ok := found[tool]; ok {
			return p, nil
		}
		return "", errors.New("executable file not found in $PATH")
	}
}

func TestAgentPATHReachesTheToolsLaunchdCannotSee(t *testing.T) {
	path, missing := supervise.AgentPATH(fakeLookPath(map[string]string{
		"go":      "/opt/homebrew/bin/go",
		"blurter": "/Users/someone/.local/bin/blurter",
	}), supervise.RestartTools)

	if len(missing) != 0 {
		t.Fatalf("reported %v missing when both were found", missing)
	}
	if !strings.HasPrefix(path, supervise.LaunchdDefaultPATH) {
		t.Errorf("agent PATH dropped launchd's own entries: %s", path)
	}
	for _, want := range []string{"/opt/homebrew/bin", "/Users/someone/.local/bin"} {
		if !strings.Contains(path, want) {
			t.Errorf("agent PATH %q does not reach %s", path, want)
		}
	}
}

func TestAgentPATHNamesWhatItCouldNotFind(t *testing.T) {
	// The install path turns a missing `go` into a refusal and a missing
	// blurter into a warning, so it has to be told which is which.
	path, missing := supervise.AgentPATH(fakeLookPath(map[string]string{
		"blurter": "/usr/local/bin/blurter",
	}), supervise.RestartTools)

	if len(missing) != 1 || missing[0] != "go" {
		t.Fatalf("missing = %v, want exactly [go]", missing)
	}
	if strings.Contains(path, "homebrew") {
		t.Errorf("agent PATH invented a toolchain directory: %s", path)
	}
}

func TestAgentPATHDoesNotRepeatADirectory(t *testing.T) {
	// Both tools in /usr/bin, which launchd already provides: the PATH
	// must not grow a duplicate entry per tool.
	path, _ := supervise.AgentPATH(fakeLookPath(map[string]string{
		"go":      "/usr/bin/go",
		"blurter": "/usr/bin/blurter",
	}), supervise.RestartTools)

	if path != supervise.LaunchdDefaultPATH {
		t.Errorf("agent PATH = %q, want launchd's default unchanged", path)
	}
}

func TestPlistCarriesTheAgentPATH(t *testing.T) {
	xml := supervise.PlistXML(supervise.AgentSpec{
		Binary:   "/repo/bin/jevons-watchdog",
		Repo:     "/repo",
		StateDir: "/state",
		Port:     1234,
		LogPath:  "/state/watchdog.log",
		PathEnv:  "/usr/bin:/bin:/opt/homebrew/bin",
	})

	// The plist this replaced had ProgramArguments and log paths and no
	// environment at all, which is how the watchdog came to run without a
	// toolchain it needs and without the blurter that would have said so.
	if !strings.Contains(xml, "<key>EnvironmentVariables</key>") {
		t.Fatalf("plist declares no environment:\n%s", xml)
	}
	if !strings.Contains(xml, "<string>/usr/bin:/bin:/opt/homebrew/bin</string>") {
		t.Errorf("plist does not carry the computed PATH:\n%s", xml)
	}
}

func TestPlistWithoutAPathStaysSilent(t *testing.T) {
	// An empty PathEnv means "say nothing", which is launchd's default —
	// worth keeping distinct from writing an empty PATH, which would be
	// worse than the bug.
	xml := supervise.PlistXML(supervise.AgentSpec{Binary: "/repo/bin/jevons-watchdog"})
	if strings.Contains(xml, "EnvironmentVariables") {
		t.Errorf("plist invented an environment block:\n%s", xml)
	}
}

func TestRestartBlockerIsSilentWhenTheHelpersAreThere(t *testing.T) {
	repo := t.TempDir()
	writeHelper(t, repo, "detach")
	writeHelper(t, repo, "runlock")
	writeHelper(t, repo, "jevonsd")

	// No `go` anywhere, and it does not matter: nothing needs building.
	if got := supervise.RestartBlocker(repo, fakeLookPath(nil), supervise.LaunchdDefaultPATH); got != "" {
		t.Errorf("blocked a restart that can succeed: %s", got)
	}
}

func TestRestartBlockerIsSilentWhenGoCanBuildTheMissingHelper(t *testing.T) {
	repo := t.TempDir()
	writeHelper(t, repo, "jevonsd")

	got := supervise.RestartBlocker(repo, fakeLookPath(map[string]string{
		"go": "/opt/homebrew/bin/go",
	}), "/usr/bin:/opt/homebrew/bin")
	if got != "" {
		t.Errorf("blocked a restart that only needed a build: %s", got)
	}
}

func TestRestartBlockerNamesTheMissingHelperAndTheFix(t *testing.T) {
	repo := t.TempDir()
	writeHelper(t, repo, "jevonsd")

	got := supervise.RestartBlocker(repo, fakeLookPath(nil), supervise.LaunchdDefaultPATH)
	if got == "" {
		t.Fatal("a restart that cannot possibly succeed was reported as fine")
	}
	// The owner reads this during an outage, with the cockpit down. It has
	// to name what is missing, where, and what to do about it.
	for _, want := range []string{
		"bin/detach",
		"bin/runlock",
		repo,
		supervise.LaunchdDefaultPATH,
		"make watchdog-install",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("blocker message missing %q: %s", want, got)
		}
	}
}

func TestRestartBlockerWantsBuildsnapOnlyWithNoBinaryToFallBackOn(t *testing.T) {
	repo := t.TempDir()
	writeHelper(t, repo, "detach")
	writeHelper(t, repo, "runlock")

	// No bin/jevonsd, so the restart has to rebuild one, which needs
	// buildsnap, which needs go.
	got := supervise.RestartBlocker(repo, fakeLookPath(nil), supervise.LaunchdDefaultPATH)
	if !strings.Contains(got, "bin/buildsnap") {
		t.Errorf("a restart with nothing to start and no way to build it was not blocked: %q", got)
	}

	// With a binary present the watchdog skips the rebuild, so buildsnap
	// is not in the way.
	writeHelper(t, repo, "jevonsd")
	if got := supervise.RestartBlocker(repo, fakeLookPath(nil), supervise.LaunchdDefaultPATH); got != "" {
		t.Errorf("buildsnap blocked a restart that would not have run it: %s", got)
	}
}

func writeHelper(t *testing.T, repo, name string) {
	t.Helper()
	dir := filepath.Join(repo, "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}
