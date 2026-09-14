// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// stubBin plants an executable jevonsd at path. Only its existence and
// location matter: the cases below resolve, they never execute.
func stubBin(t *testing.T, path, name string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "#!/bin/sh\necho RAN:" + name + "\nexit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// runJevonsd asks supervisor/run-jevonsd.sh which binary it would use.
func runJevonsd(t *testing.T, env ...string) (string, error) {
	t.Helper()
	dir := supervisorDir(t)
	// --print-bin resolves and stops. Executing the winner would start a
	// real daemon and bind :13705.
	cmd := exec.Command("/bin/sh", filepath.Join(dir, "run-jevonsd.sh"), "--print-bin")
	cmd.Env = append([]string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin"}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// 🎯T594: the default is the Homebrew release; a machine that develops
// jevons overrides that with its own build.
func TestDevRepoBuildWinsOverTheRelease(t *testing.T) {
	repo := t.TempDir()
	stubBin(t, filepath.Join(repo, "bin", "jevonsd"), "dev-repo")
	brewDir := t.TempDir()
	stubBin(t, filepath.Join(brewDir, "jevonsd"), "brew-release")

	out, err := runJevonsd(t, "JEVONS_DEV_REPO="+repo, "PATH="+brewDir+":/usr/bin:/bin")
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, filepath.Join(repo, "bin", "jevonsd")) {
		t.Fatalf("dev repo build did not win:\n%s", out)
	}
	if strings.Contains(out, brewDir) {
		t.Fatalf("the release was chosen on a dev machine:\n%s", out)
	}
}

func TestReleaseRunsWhenNoDevRepoIsConfigured(t *testing.T) {
	brewDir := t.TempDir()
	stubBin(t, filepath.Join(brewDir, "jevonsd"), "brew-release")

	out, err := runJevonsd(t, "PATH="+brewDir+":/usr/bin:/bin")
	if err != nil {
		if strings.Contains(out, "no jevonsd on PATH and no Homebrew install found") {
			t.Skip("no Homebrew jevonsd on this machine; the default release path is the macOS install")
		}
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	// With no dev repo the script searches its own hardcoded PATH (🎯T434),
	// so the winner is this machine's real Homebrew install — which is the
	// documented default, and is what a machine without this repo gets.
	resolved := strings.TrimSpace(out)
	if !strings.HasSuffix(resolved, "jevonsd") {
		t.Fatalf("default did not resolve to a jevonsd: %q", resolved)
	}
	if strings.Contains(resolved, brewDir) {
		t.Fatalf("resolution honoured the caller's PATH; it must use its own (🎯T434): %q", resolved)
	}
}

// The case the whole design turns on. An unbuilt dev repo must stop the
// program, not quietly serve an older Cellar build — a daemon running code
// nobody is looking at is the stale-binary failure 🎯T552 / 🎯T553 exist to
// catch, and it would be invisible precisely on the machine where the repo
// is supposed to be the truth.
func TestUnbuiltDevRepoRefusesRatherThanServingTheRelease(t *testing.T) {
	repo := t.TempDir() // no bin/jevonsd in it
	brewDir := t.TempDir()
	stubBin(t, filepath.Join(brewDir, "jevonsd"), "brew-release")

	out, err := runJevonsd(t, "JEVONS_DEV_REPO="+repo, "PATH="+brewDir+":/usr/bin:/bin")
	if err == nil {
		t.Fatalf("unbuilt dev repo started anyway:\n%s", out)
	}
	if strings.Contains(out, brewDir) || strings.Contains(out, "/opt/homebrew") {
		t.Fatalf("fell back to the release instead of refusing:\n%s", out)
	}
	if !strings.Contains(out, "make jevonsd") {
		t.Fatalf("refusal must say how to fix it:\n%s", out)
	}
}

func TestExplicitBinaryPinOutranksBoth(t *testing.T) {
	pinDir := t.TempDir()
	pin := stubBin(t, filepath.Join(pinDir, "pinned"), "pinned")
	repo := t.TempDir()
	stubBin(t, filepath.Join(repo, "bin", "jevonsd"), "dev-repo")

	out, err := runJevonsd(t, "JEVONS_BIN="+pin, "JEVONS_DEV_REPO="+repo)
	if err != nil {
		t.Fatalf("run failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, pin) {
		t.Fatalf("explicit pin lost:\n%s", out)
	}
}

// The rendered program is what makes a machine a development one, so the
// marker has to survive rendering with the repo path expanded.
func TestRenderedProgramCarriesTheDevRepoMarker(t *testing.T) {
	dir := supervisorDir(t)
	conf := t.TempDir()
	cmd := exec.Command("/bin/sh", filepath.Join(dir, "install.sh"))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"SUPERVISOR_CONF_DIR="+conf,
		"SUPERVISOR_SKIP_CTL=1",
		"HOME="+t.TempDir(),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	body, err := os.ReadFile(filepath.Join(conf, "jevonsd.ini"))
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Clean(filepath.Join(dir, ".."))
	if !strings.Contains(string(body), `JEVONS_DEV_REPO="`+repo+`"`) {
		t.Fatalf("rendered program does not mark this repo as the dev source:\n%s", body)
	}
}
