// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	writeHelper(t, repo, "claudiapin")
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
		"bin/claudiapin",
		repo,
		supervise.LaunchdDefaultPATH,
		"make watchdog-install",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("blocker message missing %q: %s", want, got)
		}
	}
}

// 🎯T606: claudiapin is a hard-fail dependency of the restart script
// (🎯T448 "refusing a silent claudia pin"), same as detach/runlock — a
// machine with no `go` and no bin/claudiapin must be told, not left to
// discover the die() at 03:00.
func TestRestartBlockerNamesAMissingClaudiapinAlone(t *testing.T) {
	repo := t.TempDir()
	writeHelper(t, repo, "detach")
	writeHelper(t, repo, "runlock")
	writeHelper(t, repo, "jevonsd")

	got := supervise.RestartBlocker(repo, fakeLookPath(nil), supervise.LaunchdDefaultPATH)
	if !strings.Contains(got, "bin/claudiapin") {
		t.Errorf("missing claudiapin with no go was not reported: %q", got)
	}
}

func TestRestartBlockerWantsBuildsnapOnlyWithNoBinaryToFallBackOn(t *testing.T) {
	repo := t.TempDir()
	writeHelper(t, repo, "detach")
	writeHelper(t, repo, "runlock")
	writeHelper(t, repo, "claudiapin")

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

// restartHelperExempt are on-demand builds the restart script may run
// that RestartHelpers is allowed not to name. buildsnap is only required
// when there is no bin/jevonsd to fall back on, and RestartBlocker already
// accounts for it separately. buildident's build failure is caught by
// `|| return 0` — a missing `go` degrades that one check rather than
// blocking the restart, which is why it is documented above RestartHelpers
// as excluded on purpose.
var restartHelperExempt = map[string]bool{"buildsnap": true, "buildident": true}

// restartBuildLineRE finds on-demand `go build -o "$X" ./cmd/y` lines in
// the restart script. The helper name is the cmd directory. `$[^"]+`
// (not `\w+`) so `$ROOT/bin/foo` still counts if the script ever writes
// the dest that way.
var restartBuildLineRE = regexp.MustCompile(`go build -o "\$[^"]+" \./cmd/(\w+)`)

// hardRequiredCmdBuilds returns every ./cmd/<name> the script `go build`s
// without a `return 0` escape. Exempt helpers are still returned so a
// liveness check can see them; callers that compare against RestartHelpers
// drop them via unnamedHardRequiredBuilds.
func hardRequiredCmdBuilds(script string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(script, "\n") {
		m := restartBuildLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if strings.Contains(line, "return 0") {
			continue
		}
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func unnamedHardRequiredBuilds(script string, named []string, exempt map[string]bool) []string {
	inList := map[string]bool{}
	for _, h := range named {
		inList[h] = true
	}
	var unnamed []string
	for _, helper := range hardRequiredCmdBuilds(script) {
		if exempt[helper] || inList[helper] {
			continue
		}
		unnamed = append(unnamed, helper)
	}
	return unnamed
}

// 🎯T606: the restart script builds several helpers on demand, and only
// RestartHelpers being a stale list is silent — a helper the script starts
// hard-failing on (die/exit, not the buildident "|| return 0" skip) has to
// show up here too, or a cold machine misses the go build it needs before
// the script gets there. This test reads the shipped script itself, so a
// new hard-required `go build -o "$X" ./cmd/y` line the source list has
// not been told about fails it, instead of only a manual audit finding it.
//
// Liveness is load-bearing: an earlier form of this oracle looped over
// regex matches and did nothing when the pattern matched zero lines, so a
// script-style change would have gone silent. The scanner must observe the
// helpers we already know the script hard-requires.
func TestRestartHelpersNamesEveryHardRequiredBuild(t *testing.T) {
	root, err := findRepoRoot()
	if err != nil {
		t.Fatalf("could not find repo root: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(root, "scripts", "restart-jevonsd.sh"))
	if err != nil {
		t.Fatalf("could not read restart-jevonsd.sh: %v", err)
	}
	script := string(src)

	found := hardRequiredCmdBuilds(script)
	for _, want := range []string{"detach", "runlock", "claudiapin"} {
		if !slices.Contains(found, want) {
			t.Errorf("oracle did not observe bin/%s as a hard-required build in scripts/restart-jevonsd.sh — the scanner is dead, or the script dropped a helper RestartHelpers still names", want)
		}
	}

	for _, helper := range unnamedHardRequiredBuilds(script, supervise.RestartHelpers, restartHelperExempt) {
		t.Errorf("scripts/restart-jevonsd.sh hard-requires bin/%s (no `|| return 0` "+
			"escape) but supervise.RestartHelpers does not name it — "+
			"add it or document why it is exempt", helper)
	}
}

func TestRestartHelpersIncludesClaudiapin(t *testing.T) {
	if !slices.Contains(supervise.RestartHelpers, "claudiapin") {
		t.Fatal("supervise.RestartHelpers does not name claudiapin — a cold machine with no go is not told, and the restart dies later with 'refusing a silent claudia pin'")
	}
}

func TestRestartHelpersDriftOracleFailsOnUnnamedHardBuild(t *testing.T) {
	script := `
(cd "$ROOT" && go build -o "$DETACH" ./cmd/detach) || {
  echo "cannot build detach"; exit 2
}
(cd "$ROOT" && go build -o "$NEWHELPER" ./cmd/newhelper) || {
  echo "cannot build newhelper"; exit 2
}
(cd "$ROOT" && go build -o "$BUILDIDENT" ./cmd/buildident) >/dev/null 2>&1 || return 0
(cd "$ROOT" && go build -o "$BUILDSNAP" ./cmd/buildsnap) || die "cannot build buildsnap"
`
	got := unnamedHardRequiredBuilds(script, []string{"detach"}, restartHelperExempt)
	if len(got) != 1 || got[0] != "newhelper" {
		t.Fatalf("unnamed = %v, want [newhelper] (buildident is skippable, buildsnap is exempt, detach is named)", got)
	}
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}
