// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// jevons-watchdog is the supervisor for the daily jevonsd (🎯T405).
//
// launchd runs it on an interval, which is the point: it lives outside
// every process tree that a daemon restart tears down, so it is still
// there when the restart that killed the daemon has killed itself. One
// invocation is one probe and at most one decision — no loop, no state
// in memory, nothing to wedge. Everything it remembers is on disk.
//
// It restarts the daily daemon and it tells the owner. It does not
// diagnose, does not build, and does not decide whether the outage was
// deserved. Its whole job is that the fleet is never down because nobody
// was looking.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/supervise"
)

const (
	defaultPort = 13705
	// A restart may rebuild from a HEAD worktree and then wait 90s for
	// health, so the wait has to exceed that comfortably. It exists to
	// stop a wedged restart holding a launchd slot forever, not to
	// bound a normal one.
	restartTimeout = 8 * time.Minute
	probeTimeout   = 5 * time.Second
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		port    = flag.Int("port", defaultPort, "daily jevonsd port to supervise")
		repo    = flag.String("repo", defaultRepo(), "repo root holding scripts/restart-daily-jevonsd.sh")
		state   = flag.String("state", defaultState(), "jevonsd state dir (supervision state lives under watchdog/)")
		grace   = flag.Duration("grace", supervise.DefaultConfig().Grace, "how long the port may be unserved before this is an outage")
		install = flag.Bool("install", false, "write and load the launchd agent for the current user, then exit")
		remove  = flag.Bool("uninstall", false, "unload and remove the launchd agent, then exit")
		status  = flag.Bool("status", false, "print current supervision state as JSON and exit")
		dry     = flag.Bool("dry-run", false, "decide and log, but do not restart or notify")
	)
	flag.Parse()

	switch {
	case *install:
		return installAgent(*repo, *port, *state)
	case *remove:
		return uninstallAgent()
	case *status:
		return printStatus(*state, *port)
	}

	cfg := supervise.DefaultConfig()
	cfg.Grace = *grace
	dir := supervise.Dir(*state)

	st, err := supervise.LoadState(dir)
	if err != nil {
		// Malformed state is a hard error, never a silent reset: a
		// supervisor that forgets an open outage stops escalating it.
		logf("ERROR %v", err)
		return 1
	}

	serving := probe(*port)
	now := time.Now()
	d, next := supervise.Decide(cfg, st, serving, now)
	logf("port=%d serving=%v action=%s: %s", *port, serving, d.Action, d.Reason)

	if *dry {
		return 0
	}

	switch d.Action {
	case supervise.ActionRestart:
		detail := restart(*repo, *port)
		if detail != "" {
			logf("restart reported: %s", detail)
		}
		notify(d, *port, detail)
	case supervise.ActionRecovered:
		rec := supervise.Outage{
			ID:          st.DownSince.Format(time.RFC3339Nano),
			DownSince:   st.DownSince,
			RecoveredAt: now,
			Attempts:    st.Attempts,
		}
		if st.Attempts == 0 {
			rec.Detail = "It came back without the watchdog having to restart it."
		}
		if err := supervise.AppendOutage(dir, rec); err != nil {
			logf("ERROR recording outage: %v", err)
		}
		notify(d, *port, "")
	}

	if err := supervise.SaveState(dir, next); err != nil {
		logf("ERROR saving state: %v", err)
		return 1
	}
	return 0
}

// probe asks the only question that matters: is the daily port serving?
func probe(port int) bool {
	c := &http.Client{Timeout: probeTimeout}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// restart invokes the restart script and returns a human-readable
// detail when it did not work, empty when it did.
//
// --force on purpose. The thrash policy (🎯T218) waits out a debounce
// window meant to coalesce healthy workers landing the same build;
// during an outage that window is three more minutes of downtime for no
// benefit, since nothing is serving to protect.
//
// SKIP_MAKE when a binary already exists, also on purpose. Restoring
// service beats freshness: if the daemon died because HEAD does not
// build, rebuilding from HEAD fails and the outage stands, whereas the
// binary that was serving five minutes ago will serve now. A worker who
// wants their newer build activated calls the restart script itself.
func restart(repo string, port int) string {
	script := filepath.Join(repo, "scripts", "restart-daily-jevonsd.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Sprintf("no restart script at %s (%v)", script, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), restartTimeout)
	defer cancel()

	args := []string{"--force"}
	cmd := exec.CommandContext(ctx, script, args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), fmt.Sprintf("JEVONS_RESTART_PORT=%d", port))
	if bin := filepath.Join(repo, "bin", "jevonsd"); executable(bin) {
		cmd.Env = append(cmd.Env, "JEVONS_RESTART_SKIP_MAKE=1")
	}
	out, err := cmd.CombinedOutput()
	logf("restart-daily-jevonsd --force exited: %v", errText(err))
	for _, line := range lastLines(string(out), 20) {
		logf("  | %s", line)
	}
	if err != nil {
		return fmt.Sprintf("The restart script failed (%s); the watchdog will keep trying.", errText(err))
	}
	if !probe(port) {
		return "The restart script succeeded but the port is still not serving."
	}
	return ""
}

// notify sends an out-of-band notice through blurter, which is a
// separate launchd daemon and therefore still alive when jevonsd is not.
//
// The cockpit is where the owner looks, and during an outage the cockpit
// is exactly what is missing — so the notice that an outage is happening
// cannot go there. The notice that one HAPPENED does: the recovery is
// recorded to disk and the daemon reports it into the owner's own chat
// journal as soon as it is back (internal/supervise.ReportPending).
//
// Best-effort by design. A missing blurter must not stop a restart, and
// the durable half of the story does not depend on it.
func notify(d supervise.Decision, port int, detail string) {
	if d.Notify == supervise.NotifyNone {
		return
	}
	blurter, err := exec.LookPath("blurter")
	if err != nil {
		logf("no blurter on PATH; skipping the %s notice", d.Notify)
		return
	}
	subject := fmt.Sprintf("jevons daemon down on :%d", port)
	body := fmt.Sprintf("The daily jevonsd stopped serving. The watchdog is restarting it. %s", d.Reason)
	switch {
	case d.Notify == supervise.NotifyOK:
		subject = fmt.Sprintf("jevons daemon back on :%d", port)
		body = fmt.Sprintf("Serving again after %s.", d.Downtime.Round(time.Second))
	case d.Escalated:
		subject = fmt.Sprintf("jevons daemon still down on :%d", port)
		body = fmt.Sprintf("Restarts are not taking. %s", d.Reason)
	}
	if detail != "" {
		body += " " + detail
	}
	cmd := exec.Command(blurter, "send",
		"--app", "jevons-watchdog",
		"--severity", string(d.Notify),
		"--key", fmt.Sprintf("jevonsd-down-%d", port),
		"--subject", subject,
		"--body", body,
		"--quiet")
	if out, err := cmd.CombinedOutput(); err != nil {
		logf("blurter notice failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
}

func printStatus(state string, port int) int {
	dir := supervise.Dir(state)
	st, err := supervise.LoadState(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	outages, err := supervise.LoadOutages(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	b, err := json.MarshalIndent(map[string]any{
		"port":    port,
		"serving": probe(port),
		"state":   st,
		"outages": outages,
		"dir":     dir,
	}, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(string(b))
	return 0
}

func executable(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

func errText(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}

func lastLines(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func logf(format string, args ...any) {
	fmt.Printf("%s watchdog: %s\n", time.Now().Format("2006-01-02T15:04:05-0700"), fmt.Sprintf(format, args...))
}

func defaultRepo() string {
	if v := os.Getenv("JEVONS_WATCHDOG_REPO"); v != "" {
		return v
	}
	if exe, err := os.Executable(); err == nil {
		// The binary lives in <repo>/bin, so the repo is its grandparent.
		if root, err := filepath.Abs(filepath.Join(filepath.Dir(exe), "..")); err == nil {
			return root
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func defaultState() string {
	if v := os.Getenv("JEVONS_STATE_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".jevons"
	}
	return filepath.Join(home, ".jevons")
}
