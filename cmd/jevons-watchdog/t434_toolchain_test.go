// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// The 🎯T434 oracle: what the watchdog does when the PATH it was handed
// cannot reach the toolchain its restart is built on.
//
// The watchdog runs under launchd, and a LaunchAgent whose plist declares
// no environment runs on /usr/bin:/bin:/usr/sbin:/sbin — no Homebrew, no
// Go, no blurter. The restart script builds bin/detach and bin/runlock on
// demand and fails closed when it cannot, so on that PATH the supervisor
// could not restart anything the moment bin/ was clean, and could not say
// so either: its only channel to the owner during an outage is on the
// same unreachable PATH. It was latent only because both helpers happened
// to be on disk.
//
// Both tests here run the SHIPPED scripts/restart-daily-jevonsd.sh and the
// SHIPPED sources of the helpers it re-execs through, out of a repo whose
// bin/ is empty — the cold start where this stops being latent. They are
// each other's control:
//
//	scrubbed PATH → fails closed AND the owner is told why, by name
//	the PATH the installer computes → restarts a throwaway daemon
//
// The second is the guarantee 🎯T434 chose; the first is the failure it
// accepts, kept audible.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/supervise"
)

// newColdRepo is a repo the restart script can be run out of with nothing
// built: the shipped script, the real sources of the helpers it re-execs
// through, and a bin/ holding only a daemon to start.
//
// A copy rather than the shared clone because the case under test is an
// EMPTY bin/, and this repo's bin/ is full — of binaries other workers are
// using while this test runs. The script and the helper sources are byte
// copies of the shipped files, not restatements of them: what runs here is
// the same code that runs at 03:00 under launchd.
func newColdRepo(t *testing.T, r *rig) string {
	t.Helper()
	root := t.TempDir()
	// The restart this test provokes detaches its daemon, so the throwaway
	// jevonsd outlives the cycle that started it and is reparented to init.
	// Nothing here used to reap it: one was found alive on the owner's Mac
	// two hours after the run, and the same omission in the 🎯T218 harness
	// stranded seven. Registered after TempDir so it runs before the removal.
	t.Cleanup(func() { reapUnder(t, root) })
	for _, dir := range []string{"scripts", "bin"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	copyFile(t, r.script, filepath.Join(root, "scripts", "restart-daily-jevonsd.sh"), 0o755)

	// detach and runlock import nothing from this module, which is what
	// lets them be built out of a scratch module with no go.sum and no
	// network — and is also why the script can build them when the rest of
	// the tree does not compile.
	for _, helper := range supervise.RestartHelpers {
		dir := filepath.Join(root, "cmd", helper)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		copyFile(t, filepath.Join(r.root, "cmd", helper, "main.go"),
			filepath.Join(dir, "main.go"), 0o644)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module coldjevons\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Something for the restart to start. The watchdog skips the rebuild
	// when a binary exists, which is the case the outage happens in:
	// restoring service beats freshness.
	copyFile(t, r.stub, filepath.Join(root, "bin", "jevonsd"), 0o755)
	return root
}

// reapUnder kills every live process whose executable lives under dir, and
// fails the test if any survives. Matching on the executable path is what
// makes the sweep total — a detached daemon has no parent left to walk down
// from, and is not necessarily holding a port to be found by — while keeping
// the owner's real jevonsd, which lives outside any TempDir, unreachable.
func reapUnder(t *testing.T, dir string) {
	t.Helper()
	prefix := dir + string(os.PathSeparator)
	live := func() []int {
		out, err := exec.Command("ps", "-Ao", "pid=,comm=").Output()
		if err != nil {
			return nil
		}
		var pids []int
		for _, line := range strings.Split(string(out), "\n") {
			// Split on the first space only: paths may contain spaces.
			line = strings.TrimSpace(line)
			sp := strings.IndexByte(line, ' ')
			if sp < 0 || !strings.HasPrefix(strings.TrimSpace(line[sp+1:]), prefix) {
				continue
			}
			if pid, err := strconv.Atoi(line[:sp]); err == nil && pid != os.Getpid() {
				pids = append(pids, pid)
			}
		}
		return pids
	}
	for range 10 {
		pids := live()
		if len(pids) == 0 {
			return
		}
		for _, pid := range pids {
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if left := live(); len(left) > 0 {
		t.Errorf("leaked %d process(es) from %s that outlived the test: %v", len(left), dir, left)
	}
}

func copyFile(t *testing.T, src, dst string, mode os.FileMode) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read %s: %v", src, err)
	}
	if err := os.WriteFile(dst, b, mode); err != nil {
		t.Fatalf("write %s: %v", dst, err)
	}
}

// cycleIn runs one watchdog invocation — what launchd does, once — against
// the given repo and with the given PATH, and returns its log. Unlike
// rig.cycle it does not inherit this test process's PATH, because the PATH
// is the subject.
func (r *rig) cycleIn(t *testing.T, repo, path string) string {
	t.Helper()
	cmd := exec.Command(r.watchdog,
		"-port", strconv.Itoa(r.port),
		"-repo", repo,
		"-state", r.dir,
		"-grace", "0")
	cmd.Dir = repo
	// Last value wins for a duplicate key, so this PATH replaces the rig's.
	cmd.Env = append(r.env("JEVONS_RESTART_BIN="+filepath.Join(repo, "bin", "jevonsd")),
		"PATH="+path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// A failed restart is not a failed watchdog: it stays up and keeps
		// supervising. A non-zero exit here means the supervisor itself
		// broke, which is a different bug.
		t.Fatalf("watchdog cycle exited %v:\n%s", err, out)
	}
	t.Logf("watchdog: %s", strings.TrimSpace(string(out)))
	return string(out)
}

// TestScrubbedPathRestartFailsClosedAndTheOwnerHearsWhy is the failure
// 🎯T434 accepts, with the silence removed.
//
// launchd's own PATH, an empty bin/, and a daemon that is down: the script
// cannot build the helper it re-execs through, so it refuses rather than
// restart in a session its caller's death can sweep. That refusal is
// correct. What was wrong was that nobody heard it — the watchdog logged
// "the restart script failed" into a file under ~/.jevons that the owner
// has no reason to read while the cockpit is down.
func TestScrubbedPathRestartFailsClosedAndTheOwnerHearsWhy(t *testing.T) {
	r := newRig(t)
	cold := newColdRepo(t, r)

	// The PATH a LaunchAgent gets with no EnvironmentVariables block, plus
	// the blurter shim — the notice channel the fixed plist does reach, so
	// that what this test measures is the toolchain being gone and not the
	// notifier being gone.
	path := filepath.Join(r.dir, "bin") + ":" + supervise.LaunchdDefaultPATH
	if _, err := lookPathIn(path, "go"); err == nil {
		t.Fatalf("launchd's default PATH reached a toolchain on this machine; the test proves nothing")
	}

	if r.serving() {
		t.Fatalf("something is already listening on scratch port %d", r.port)
	}
	// One unserved observation is not an outage; the second cycle acts.
	r.cycleIn(t, cold, path)
	if out := r.cycleIn(t, cold, path); !strings.Contains(out, "action=restart") {
		t.Fatalf("the watchdog never tried to restart:\n%s", out)
	}

	// Failed closed: nothing was started. A restart that ran anyway would
	// be running unserialised and in a sweepable session, which is the
	// failure 🎯T392.5 and 🎯T405 removed.
	if r.waitServing(true, 5*time.Second) {
		t.Fatal("the restart ran without the helpers it refuses to run without")
	}

	blurts := strings.Join(r.blurts(), "\n")
	if blurts == "" {
		t.Fatal("a restart that can never succeed was retried in silence — the 🎯T434 outage")
	}
	// The owner reads this on their phone with the cockpit down. It has to
	// carry what is missing, where, why the restart stopped, and the fix.
	for _, want := range []string{
		"refusing to restart",   // the script's own refusal, verbatim
		"bin/detach",            // what is missing
		cold,                    // where
		"no `go` on PATH",       // why it could not be built
		"make watchdog-install", // what to do about it
		"--severity problem",    // and it arrives as a problem, not a log line
	} {
		if !strings.Contains(blurts, want) {
			t.Errorf("the out-of-band notice does not mention %q:\n%s", want, blurts)
		}
	}
}

// TestTheInstalledAgentPATHRestartsFromAColdBin is the guarantee.
//
// Same cold repo, same empty bin/, same launchd-shaped environment — but
// the PATH the installer computes and writes into the plist, and nothing
// else. No login shell, no inherited environment: if this is enough, the
// installed job is enough. The script builds both helpers on demand and
// the throwaway daemon comes back.
func TestTheInstalledAgentPATHRestartsFromAColdBin(t *testing.T) {
	r := newRig(t)
	cold := newColdRepo(t, r)

	agentPath, missing := supervise.AgentPATH(exec.LookPath, []string{"go"})
	if len(missing) != 0 {
		t.Skipf("no %v on this machine's PATH, so there is no installed PATH to test", missing)
	}
	path := filepath.Join(r.dir, "bin") + ":" + agentPath

	if r.serving() {
		t.Fatalf("something is already listening on scratch port %d", r.port)
	}
	r.cycleIn(t, cold, path)
	if out := r.cycleIn(t, cold, path); !strings.Contains(out, "action=restart") {
		t.Fatalf("the watchdog never tried to restart:\n%s", out)
	}

	if !r.waitServing(true, 90*time.Second) {
		t.Fatalf(":%d never came up on the PATH the installer writes\nrestart log:\n%s",
			r.port, t405Tail(t405ReadFile(r.restartLog()), 40))
	}
	// Built on demand, out of the cold repo, by a process that could see
	// only the plist's PATH — the property the whole target turns on.
	for _, helper := range supervise.RestartHelpers {
		if _, err := os.Stat(filepath.Join(cold, "bin", helper)); err != nil {
			t.Errorf("bin/%s was never built: %v", helper, err)
		}
	}
	if blurts := strings.Join(r.blurts(), "\n"); strings.Contains(blurts, "refusing to restart") {
		t.Errorf("a restart that worked reported a refusal:\n%s", blurts)
	}

	// And the outage closes, which is the whole point of restarting it.
	if out := r.cycleIn(t, cold, path); !strings.Contains(out, "action=recovered") {
		t.Errorf("the recovered daemon did not close the outage:\n%s", out)
	}
}

// lookPathIn answers exec.LookPath's question against a PATH other than
// this process's own, so a test can assert what a child will and will not
// find.
func lookPathIn(path, tool string) (string, error) {
	saved, had := os.LookupEnv("PATH")
	os.Setenv("PATH", path)
	defer func() {
		if had {
			os.Setenv("PATH", saved)
			return
		}
		os.Unsetenv("PATH")
	}()
	return exec.LookPath(tool)
}
