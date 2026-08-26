// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Nothing was responsible for the supervisor being up (🎯T405).
//
// The watchdog was installed on 2026-08-10 at 20:48 and its last probe
// was at 21:02:39. The machine did not reboot — uptime ran unbroken from
// August 1 — and the plist sat on disk the whole time, correct and
// unloaded. launchd simply no longer held the job, and for five days,
// across four daemon bounces, not one line of code anywhere noticed. The
// owner did, by asking.
//
// That is the target's own first clause failing one level up. A
// supervisor whose absence has no alarm is a supervisor only while
// nothing has gone wrong with it, and the whole premise of 🎯T405 is
// that the thing which must not fail silently is exactly the thing that
// does. The daemon is the natural counterweight: it is in a different
// process tree, it is up whenever the watchdog is idle, and it already
// reads this package's state to report outages. So each holds the other
// up — the watchdog restarts a daemon that stops serving, the daemon
// reinstates a watchdog that stops probing — and neither is alone in
// being trusted to be there.
//
// The policy below is pure for the same reason Decide is: the decision
// has to be testable without launchd, a real job, or a clock.

// AgentConfig bounds how patient the daemon is with its supervisor.
type AgentConfig struct {
	// Stale is how long the watchdog's heartbeat may be silent before
	// the daemon treats the supervisor as absent. Four missed intervals
	// rather than one: launchd defers a StartInterval job under load,
	// and reinstating a job that was merely late would trade a silent
	// absence for a noisy churn.
	Stale time.Duration
	// Retry paces reinstatement. A job that cannot be loaded — a broken
	// plist, a missing binary — must not be bootstrapped every tick.
	Retry time.Duration
}

// DefaultAgentConfig is what the daemon runs with.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		Stale: 4 * AgentInterval * time.Second,
		Retry: 5 * time.Minute,
	}
}

// AgentObservation is what the daemon just saw about its supervisor.
//
// Loaded and LastProbe are deliberately separate: launchd holding the
// job proves it is registered, not that it runs. A job whose binary
// crashes on start is loaded forever and probes never.
type AgentObservation struct {
	// Loaded reports whether launchd holds the job.
	Loaded bool
	// LastProbe is the watchdog's own heartbeat, written by every
	// completed cycle. Zero means it has never completed one.
	LastProbe time.Time
	// PlistPath is the LaunchAgent on disk, empty when there is none.
	PlistPath string
	// Binary is the watchdog executable, empty when it is missing —
	// which is the one gap the daemon cannot close by reloading.
	Binary string
}

// AgentState is what the daemon remembers between checks, so a gap costs
// one notice rather than one per tick.
type AgentState struct {
	// LastAttempt paces reinstatement.
	LastAttempt time.Time `json:"last_attempt,omitzero"`
	// Attempts counts reinstatements tried for the current gap.
	Attempts int `json:"attempts,omitempty"`
	// Notified records that the owner has been told about this gap.
	Notified bool `json:"notified,omitempty"`
}

// AgentDecision is one verdict about the supervisor.
type AgentDecision struct {
	// Reinstate asks the caller to write the LaunchAgent and load it.
	Reinstate bool
	// Notify is the owner notice this verdict warrants, if any.
	Notify Notify
	// Reason is the log line, and the only place a human reads why a
	// check did nothing.
	Reason string
	// Silent is how long the heartbeat had been quiet, zero when the
	// supervisor was never seen at all.
	Silent time.Duration
}

// ClassifyAgent decides what the daemon should do about its supervisor:
// what it saw plus what it remembers, in; a verdict and the state to
// persist, out.
func ClassifyAgent(cfg AgentConfig, st AgentState, obs AgentObservation, now time.Time) (AgentDecision, AgentState) {
	silent := time.Duration(0)
	if !obs.LastProbe.IsZero() {
		silent = now.Sub(obs.LastProbe)
	}

	if obs.Loaded && !obs.LastProbe.IsZero() && silent <= cfg.Stale {
		d := AgentDecision{Reason: fmt.Sprintf("supervisor loaded, last probe %s ago", silent.Round(time.Second)), Silent: silent}
		if st.Notified {
			// The owner heard the gap open, so they hear it close. A
			// gap nobody was told about is not news either way.
			d.Notify = NotifyOK
			d.Reason = fmt.Sprintf("supervisor probing again after %d reinstatement attempt(s)", st.Attempts)
		}
		return d, AgentState{}
	}

	var gap string
	switch {
	case !obs.Loaded:
		gap = fmt.Sprintf("launchd is not holding %s", AgentLabel)
	case obs.LastProbe.IsZero():
		gap = "the job is loaded but has never completed a probe"
	default:
		gap = fmt.Sprintf("the job is loaded but its last probe was %s ago", silent.Round(time.Second))
	}

	d := AgentDecision{Silent: silent}

	// The one gap reloading cannot close. Say so rather than
	// bootstrapping a job that is guaranteed not to run, which would
	// report success and leave the daemon unsupervised.
	if obs.Binary == "" {
		d.Reason = gap + ", and the watchdog binary is missing — reinstating it would load a job that cannot run"
		if !st.Notified {
			d.Notify = NotifyProblem
			st.Notified = true
		}
		return d, st
	}

	if !st.LastAttempt.IsZero() && now.Sub(st.LastAttempt) < cfg.Retry {
		d.Reason = fmt.Sprintf("%s; attempt %d was %s ago, next in %s", gap, st.Attempts, now.Sub(st.LastAttempt).Round(time.Second), cfg.Retry)
		return d, st
	}

	st.Attempts++
	st.LastAttempt = now
	d.Reinstate = true
	d.Reason = fmt.Sprintf("%s; reinstating (attempt %d)", gap, st.Attempts)
	if !st.Notified {
		d.Notify = NotifyProblem
		st.Notified = true
	}
	return d, st
}

// SupervisorKind is the notice kind for a gap in supervision itself.
// Distinct from OutageKind because the two say opposite things: an
// outage notice says the machinery caught a failure, this one says the
// machinery was not there to catch anything.
const SupervisorKind = "supervisor-gap"

// SupervisorSubject names what the notice is about.
const SupervisorSubject = "jevons-watchdog"

// AgentNoticeText is what the owner reads.
func AgentNoticeText(d AgentDecision) string {
	if d.Notify == NotifyOK {
		return "watchdog restored: the daemon's own supervisor is loaded and probing again. " + d.Reason
	}
	msg := "watchdog gap: nothing was supervising the daily jevonsd — " + d.Reason + "."
	if d.Silent > 0 {
		msg += fmt.Sprintf(" The last probe was %s ago; while that held, a failed restart would have left the fleet down until you noticed.", d.Silent.Round(time.Second))
	}
	return msg
}

// AgentPaths locates the supervisor from the daemon's point of view.
// The daemon writes the LaunchAgent from its own paths rather than
// trusting whatever spec is on disk, because it knows its repo, state
// dir and port first-hand and a stale plist is one of the ways the job
// stops working.
type AgentPaths struct {
	StateDir string
	Home     string
	Repo     string
	Port     int
}

// WatchdogBinary is where the watchdog lives relative to the repo.
func (p AgentPaths) WatchdogBinary() string {
	return filepath.Join(p.Repo, "bin", "jevons-watchdog")
}

// Spec renders the LaunchAgent the daemon would install.
//
// The PATH (🎯T434) is carried over from whatever is already installed
// in preference to being recomputed. The daemon is started detached by
// the restart script and inherits whatever environment that had, so
// asking exec.LookPath here can legitimately answer with less than the
// human who ran -install saw. Rewriting a working plist with a poorer
// PATH would restore supervision and quietly break the restart it exists
// to perform — a reinstatement must not be able to leave things worse
// than it found them. A computed PATH is the fallback for the case with
// nothing to carry over.
func (p AgentPaths) Spec() AgentSpec {
	spec := AgentSpec{
		Binary:   p.WatchdogBinary(),
		Repo:     p.Repo,
		StateDir: p.StateDir,
		Port:     p.Port,
		LogPath:  AgentLogPath(p.StateDir),
	}
	if b, err := os.ReadFile(AgentPlistPath(p.Home)); err == nil {
		spec.PathEnv = PlistPathEnv(string(b))
	}
	if spec.PathEnv == "" {
		spec.PathEnv, _ = AgentPATH(exec.LookPath, RestartTools)
	}
	return spec
}

// PlistPathEnv reads the PATH out of a rendered LaunchAgent, empty when
// it has none. A targeted scan rather than a plist parser: the only
// question is what PATH the installed job runs with, and the file it
// reads is the one PlistXML wrote.
func PlistPathEnv(plist string) string {
	_, rest, ok := strings.Cut(plist, "<key>PATH</key>")
	if !ok {
		return ""
	}
	if _, rest, ok = strings.Cut(rest, "<string>"); !ok {
		return ""
	}
	value, _, ok := strings.Cut(rest, "</string>")
	if !ok {
		return ""
	}
	return unesc(value)
}

func unesc(s string) string {
	return strings.NewReplacer(
		"&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&#39;", "'", "&#34;", `"`,
		// Last, so a literal "&amp;lt;" survives intact.
		"&amp;", "&",
	).Replace(s)
}

// ObserveAgent gathers what the daemon can see about its supervisor.
//
// A heartbeat that cannot be read counts as no heartbeat. That is the
// conservative direction: the worst it costs is a reinstatement of a
// job that was fine, and the alternative — treating an unreadable state
// file as healthy — is the exact failure this whole file exists to end.
//
// The log's mtime is taken as a heartbeat of last resort, because the
// state field is younger than the supervisor deployed on this machine.
// launchd captures the watchdog's output, so any build of it — including
// the one installed by hand on 2026-08-10, which never heard of
// LastProbe — leaves a timestamp there on every cycle. Without this the
// daemon would meet a perfectly healthy supervisor, find no heartbeat,
// and declare it absent: a false alarm every retry interval, which is
// how an owner learns to stop reading the alarm that matters.
func ObserveAgent(p AgentPaths) AgentObservation {
	obs := AgentObservation{}
	if st, err := LoadState(Dir(p.StateDir)); err == nil {
		obs.LastProbe = st.LastProbe
	}
	if st, err := os.Stat(AgentLogPath(p.StateDir)); err == nil && st.ModTime().After(obs.LastProbe) {
		obs.LastProbe = st.ModTime()
	}
	obs.Loaded, _ = AgentLoaded(AgentLabel)
	if path := AgentPlistPath(p.Home); fileExists(path) {
		obs.PlistPath = path
	}
	if bin := p.WatchdogBinary(); executableFile(bin) {
		obs.Binary = bin
	}
	return obs
}

// ReinstateAgent writes the LaunchAgent and loads it.
func ReinstateAgent(p AgentPaths) error {
	path := AgentPlistPath(p.Home)
	if err := WriteAgentPlist(path, p.Spec()); err != nil {
		return err
	}
	return LoadAgent(path, AgentLabel)
}

// HomeDir is where the LaunchAgent lives. An empty string when the home
// directory cannot be determined, which ObserveAgent reads as "no plist"
// rather than guessing a path and writing a LaunchAgent somewhere wrong.
func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// RepoRoot locates the repo holding bin/jevons-watchdog and the restart
// script, from the running binary rather than the working directory: the
// daemon is started detached with a workdir the caller chose, and the one
// path it can always trust is its own.
func RepoRoot() string {
	if exe, err := os.Executable(); err == nil {
		// The binary lives in <repo>/bin, so the repo is its grandparent.
		if root, err := filepath.Abs(filepath.Join(filepath.Dir(exe), "..")); err == nil {
			return root
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func executableFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

func agentStatePath(dir string) string { return filepath.Join(dir, "supervisor.json") }

// LoadAgentState reads what the daemon remembers about the gap it is
// currently working on. Missing is healthy; malformed is not silently
// reset, for the same reason the watchdog's own state is not.
func LoadAgentState(dir string) (AgentState, error) {
	var st AgentState
	if err := readJSON(agentStatePath(dir), &st); err != nil {
		return AgentState{}, err
	}
	return st, nil
}

// SaveAgentState replaces the remembered state atomically.
func SaveAgentState(dir string, st AgentState) error {
	return writeJSON(agentStatePath(dir), st)
}

// WatchAgentLoop is the daemon's half of the mutual supervision: it
// checks that the watchdog is loaded and probing, reinstates it when it
// is not, and tells the owner once per gap.
//
// The first check waits a full staleness window rather than running at
// startup. A daemon start is very often the tail of a restart that just
// rebuilt both binaries, and in that moment the watchdog has legitimately
// not completed a cycle yet. Judging it there would reinstate a healthy
// job and spend an owner notice on nothing, which is how an alarm stops
// being read. The window is bounded and short; the gap this exists to
// catch lasted five days.
func WatchAgentLoop(ctx context.Context, p AgentPaths, cfg AgentConfig, every time.Duration, notify Notifier) {
	// 🎯T553.3: launchd KeepAlive owns jevonsd. Reinstalling the
	// probe-that-calls-restart-daily would undo the peel.
	if SkipWatchdogSupervise() {
		slog.Info("supervise: KeepAlive owns jevonsd; not reinstating the watchdog", "label", DaemonLabel)
		return
	}
	if every <= 0 {
		every = time.Minute
	}
	if cfg.Stale <= 0 {
		cfg = DefaultAgentConfig()
	}
	dir := Dir(p.StateDir)

	check := func() {
		st, err := LoadAgentState(dir)
		if err != nil {
			slog.Warn("supervise: supervisor state", "err", err)
			return
		}
		obs := ObserveAgent(p)
		d, next := ClassifyAgent(cfg, st, obs, time.Now())

		if d.Reinstate {
			if err := ReinstateAgent(p); err != nil {
				slog.Error("supervise: reinstating the watchdog", "err", err, "reason", d.Reason)
				d.Reason += "; the reinstatement itself failed: " + err.Error()
			} else {
				slog.Warn("supervise: watchdog reinstated", "reason", d.Reason)
			}
		}
		if d.Notify != NotifyNone && notify != nil {
			if !notify(SupervisorSubject, SupervisorKind, AgentNoticeText(d)) && d.Notify == NotifyProblem {
				// No journal to write to. Leave the gap unreported so
				// the next check tells the owner rather than assuming
				// silence was delivery.
				next.Notified = false
			}
		}
		if err := SaveAgentState(dir, next); err != nil {
			slog.Warn("supervise: saving supervisor state", "err", err)
		}
	}

	select {
	case <-ctx.Done():
		return
	case <-time.After(cfg.Stale):
	}
	check()

	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			check()
		}
	}
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("supervise: read %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("supervise: parse %s: %w", path, err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("supervise: marshal %s: %w", path, err)
	}
	return writeAtomic(path, append(b, '\n'))
}
