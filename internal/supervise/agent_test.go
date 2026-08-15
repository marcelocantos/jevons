// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// The oracle for the other half of 🎯T405: the daemon supervising its own
// supervisor. The gap these tests are written against is a real one — the
// watchdog was installed on 2026-08-10 at 20:48, last probed at 21:02, and
// stayed absent for five days behind a plist that looked perfectly healthy
// on disk. Every case below is that morning's shape or one of the ways a
// naive fix for it makes things worse.
package supervise_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/supervise"
)

func agentCfg() supervise.AgentConfig {
	return supervise.AgentConfig{
		Stale: 2 * time.Minute,
		Retry: 5 * time.Minute,
	}
}

// TestTheGapOfAugustTenth is the outage itself: launchd is not holding the
// job, the plist and the binary are both fine, and the last heartbeat is
// days old. Nothing in the system noticed that for five days, so the one
// thing this must do is act and say so.
func TestTheGapOfAugustTenth(t *testing.T) {
	now := time.Date(2026, 8, 15, 8, 38, 0, 0, time.UTC)
	obs := supervise.AgentObservation{
		Loaded:    false,
		LastProbe: time.Date(2026, 8, 10, 21, 2, 39, 0, time.UTC),
		PlistPath: "/Users/x/Library/LaunchAgents/" + supervise.AgentLabel + ".plist",
		Binary:    "/repo/bin/jevons-watchdog",
	}

	d, st := supervise.ClassifyAgent(agentCfg(), supervise.AgentState{}, obs, now)
	if !d.Reinstate {
		t.Fatalf("an absent supervisor was not reinstated: %+v", d)
	}
	if d.Notify != supervise.NotifyProblem {
		t.Fatalf("an absent supervisor did not reach the owner: %+v", d)
	}
	if st.Attempts != 1 || !st.Notified || st.LastAttempt != now {
		t.Fatalf("the gap was not recorded: %+v", st)
	}
	if d.Silent < 4*24*time.Hour {
		t.Fatalf("silence measured as %s, want the days it really was", d.Silent)
	}
	if text := supervise.AgentNoticeText(d); text == "" {
		t.Fatal("the owner notice is empty")
	}
}

// TestProbingSupervisorIsQuiet: the healthy case must cost nothing and
// leave nothing behind, or the state file accumulates a gap that is not
// there and the next real one is paced against it.
func TestProbingSupervisorIsQuiet(t *testing.T) {
	now := time.Now()
	obs := supervise.AgentObservation{
		Loaded:    true,
		LastProbe: now.Add(-30 * time.Second),
		Binary:    "/repo/bin/jevons-watchdog",
	}

	d, st := supervise.ClassifyAgent(agentCfg(), supervise.AgentState{}, obs, now)
	if d.Reinstate || d.Notify != supervise.NotifyNone {
		t.Fatalf("a probing supervisor was acted on: %+v", d)
	}
	if st != (supervise.AgentState{}) {
		t.Fatalf("a probing supervisor left state behind: %+v", st)
	}
}

// TestLateProbeIsNotAGap is the reason the staleness window is four
// intervals rather than one. launchd defers a StartInterval job under
// load, and a daemon that reinstated the supervisor every time it ran
// late would replace a silent absence with a loud churn — which is worse,
// because the owner learns to ignore it.
func TestLateProbeIsNotAGap(t *testing.T) {
	c := agentCfg()
	now := time.Now()
	obs := supervise.AgentObservation{
		Loaded:    true,
		LastProbe: now.Add(-c.Stale + time.Second),
		Binary:    "/repo/bin/jevons-watchdog",
	}

	d, _ := supervise.ClassifyAgent(c, supervise.AgentState{}, obs, now)
	if d.Reinstate || d.Notify != supervise.NotifyNone {
		t.Fatalf("a merely late probe was treated as a gap: %+v", d)
	}
}

// TestLoadedButNeverProbing is the failure launchctl alone cannot see: a
// job whose binary dies on start is held by launchd forever and runs
// never. Trusting "loaded" would report supervision that does not exist —
// exactly the belief that made the five days possible.
func TestLoadedButNeverProbing(t *testing.T) {
	obs := supervise.AgentObservation{
		Loaded: true,
		Binary: "/repo/bin/jevons-watchdog",
	}

	d, _ := supervise.ClassifyAgent(agentCfg(), supervise.AgentState{}, obs, time.Now())
	if !d.Reinstate {
		t.Fatalf("a loaded job that never probes was accepted as supervision: %+v", d)
	}
}

// TestOneNoticeAndPacedRetries: a gap that persists must not spend a
// notice per tick nor bootstrap per tick. The owner hears once; the
// reinstatement waits out the retry interval and then tries again.
func TestOneNoticeAndPacedRetries(t *testing.T) {
	c := agentCfg()
	t0 := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	obs := supervise.AgentObservation{Binary: "/repo/bin/jevons-watchdog"}

	_, st := supervise.ClassifyAgent(c, supervise.AgentState{}, obs, t0)

	d, st := supervise.ClassifyAgent(c, st, obs, t0.Add(time.Minute))
	if d.Reinstate {
		t.Fatalf("bootstrapped again inside the retry interval: %+v", d)
	}
	if d.Notify != supervise.NotifyNone {
		t.Fatalf("told the owner twice about one gap: %+v", d)
	}

	d, st = supervise.ClassifyAgent(c, st, obs, t0.Add(c.Retry+time.Second))
	if !d.Reinstate {
		t.Fatalf("gave up on a gap that was still open: %+v", d)
	}
	if d.Notify != supervise.NotifyNone {
		t.Fatalf("a paced retry spent a second notice: %+v", d)
	}
	if st.Attempts != 2 {
		t.Fatalf("attempts not counted: %+v", st)
	}
}

// TestRecoveryIsReportedOnlyToWhoeverHeardTheGap. The owner who was told
// supervision was missing is told when it is back; an owner who never
// heard about it gets no news either way.
func TestRecoveryIsReportedOnlyToWhoeverHeardTheGap(t *testing.T) {
	now := time.Now()
	healthy := supervise.AgentObservation{
		Loaded:    true,
		LastProbe: now.Add(-10 * time.Second),
		Binary:    "/repo/bin/jevons-watchdog",
	}

	d, st := supervise.ClassifyAgent(agentCfg(), supervise.AgentState{Notified: true, Attempts: 2}, healthy, now)
	if d.Notify != supervise.NotifyOK {
		t.Fatalf("recovery from a reported gap was silent: %+v", d)
	}
	if st != (supervise.AgentState{}) {
		t.Fatalf("recovery did not clear the gap: %+v", st)
	}

	d, _ = supervise.ClassifyAgent(agentCfg(), supervise.AgentState{}, healthy, now)
	if d.Notify != supervise.NotifyNone {
		t.Fatalf("announced a recovery from a gap nobody heard about: %+v", d)
	}
}

// TestMissingBinaryIsNotLoadable: bootstrapping a job whose program is
// gone succeeds and supervises nothing, so the daemon would report itself
// covered while it is not. Say what is wrong instead.
func TestMissingBinaryIsNotLoadable(t *testing.T) {
	obs := supervise.AgentObservation{} // no binary, not loaded, never probed

	d, st := supervise.ClassifyAgent(agentCfg(), supervise.AgentState{}, obs, time.Now())
	if d.Reinstate {
		t.Fatalf("loaded a job that cannot run: %+v", d)
	}
	if d.Notify != supervise.NotifyProblem || !st.Notified {
		t.Fatalf("a supervisor that cannot be restored did not reach the owner: %+v %+v", d, st)
	}
	if st.Attempts != 0 {
		t.Fatalf("counted an attempt that was never made: %+v", st)
	}
}

// TestHeartbeatCrossesTheProcessBoundary is the wire between the two
// processes: the watchdog writes LastProbe into its own state file and
// the daemon, which shares nothing else with it, reads it back. A field
// that did not survive the round trip would make every check look like a
// job that has never probed.
func TestHeartbeatCrossesTheProcessBoundary(t *testing.T) {
	stateDir := t.TempDir()
	probe := time.Date(2026, 8, 15, 8, 41, 25, 0, time.UTC)

	// The watchdog's side.
	if err := supervise.SaveState(supervise.Dir(stateDir), supervise.State{LastProbe: probe}); err != nil {
		t.Fatalf("saving the watchdog's state: %v", err)
	}

	// The daemon's.
	repo := t.TempDir()
	bin := filepath.Join(repo, "bin", "jevons-watchdog")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	obs := supervise.ObserveAgent(supervise.AgentPaths{StateDir: stateDir, Home: t.TempDir(), Repo: repo, Port: 13705})
	if !obs.LastProbe.Equal(probe) {
		t.Fatalf("heartbeat did not survive the file: got %v, want %v", obs.LastProbe, probe)
	}
	if obs.Binary != bin {
		t.Fatalf("watchdog binary not found: %q", obs.Binary)
	}
}

// TestASupervisorTooOldToHaveAHeartbeatIsStillAlive is the deployment the
// change was made against, not a hypothetical one: the watchdog running
// on this machine was installed before LastProbe existed, and the restart
// script rebuilds only bin/jevonsd, so the daemon meets an older
// supervisor as a matter of course. Judged on the state field alone it
// would be declared absent while it probes every thirty seconds, and the
// owner would be told supervision was missing when it was not.
func TestASupervisorTooOldToHaveAHeartbeatIsStillAlive(t *testing.T) {
	stateDir := t.TempDir()
	dir := supervise.Dir(stateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// What such a watchdog leaves behind: state with no heartbeat in it,
	// and a log launchd has just written a probe line to.
	if err := supervise.SaveState(dir, supervise.State{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(supervise.AgentLogPath(stateDir), []byte("watchdog: port=13705 serving=true action=none: serving\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	obs := supervise.ObserveAgent(supervise.AgentPaths{StateDir: stateDir, Home: t.TempDir(), Repo: t.TempDir()})
	if obs.LastProbe.IsZero() {
		t.Fatal("a supervisor that has just logged a probe was read as never having run")
	}
	obs.Loaded = true // launchd's answer is not this test's subject.

	d, _ := supervise.ClassifyAgent(agentCfg(), supervise.AgentState{}, obs, time.Now())
	if d.Reinstate || d.Notify != supervise.NotifyNone {
		t.Fatalf("a probing supervisor was reinstated for being too old to say so: %+v", d)
	}
}

// TestAStaleLogIsNotAHeartbeat: the fallback must not turn the five-day
// gap into a healthy reading. The log from 2026-08-10 sat there, exactly
// as it does now, while nothing was running.
func TestAStaleLogIsNotAHeartbeat(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.MkdirAll(supervise.Dir(stateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	log := supervise.AgentLogPath(stateDir)
	if err := os.WriteFile(log, []byte("watchdog: port=13705 serving=true action=none: serving\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-5 * 24 * time.Hour)
	if err := os.Chtimes(log, old, old); err != nil {
		t.Fatal(err)
	}

	obs := supervise.ObserveAgent(supervise.AgentPaths{StateDir: stateDir, Home: t.TempDir(), Repo: t.TempDir()})
	obs.Loaded = true
	obs.Binary = "/repo/bin/jevons-watchdog"

	d, _ := supervise.ClassifyAgent(agentCfg(), supervise.AgentState{}, obs, time.Now())
	if !d.Reinstate || d.Notify != supervise.NotifyProblem {
		t.Fatalf("a log five days cold was accepted as a heartbeat: %+v", d)
	}
}

// TestMissingWatchdogBinaryIsSeenAsMissing: an executable that is not
// there, and a plain file that is not executable, are both the one gap
// reloading cannot close, and ObserveAgent has to report them as such.
func TestMissingWatchdogBinaryIsSeenAsMissing(t *testing.T) {
	repo := t.TempDir()
	p := supervise.AgentPaths{StateDir: t.TempDir(), Home: t.TempDir(), Repo: repo, Port: 13705}

	if obs := supervise.ObserveAgent(p); obs.Binary != "" {
		t.Fatalf("found a watchdog that is not there: %q", obs.Binary)
	}

	if err := os.MkdirAll(filepath.Join(repo, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.WatchdogBinary(), []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	if obs := supervise.ObserveAgent(p); obs.Binary != "" {
		t.Fatalf("a non-executable file was accepted as the watchdog: %q", obs.Binary)
	}
}

// TestReinstatementKeepsTheInstalledPATH is the 🎯T434 interaction, and
// the one way this whole mechanism could leave the machine worse than it
// found it. The daemon runs detached with whatever environment the
// restart script had, so recomputing the job's PATH here can legitimately
// find less than the human who ran -install did. A reinstatement that
// rewrote the plist with a poorer PATH would restore supervision and
// silently break the restart it exists to perform.
func TestReinstatementKeepsTheInstalledPATH(t *testing.T) {
	home := t.TempDir()
	installed := "/opt/homebrew/bin:/usr/bin:/bin"
	if err := supervise.WriteAgentPlist(supervise.AgentPlistPath(home), supervise.AgentSpec{
		Binary:   "/repo/bin/jevons-watchdog",
		Repo:     "/repo",
		StateDir: "/state",
		Port:     13705,
		PathEnv:  installed,
	}); err != nil {
		t.Fatalf("writing the installed plist: %v", err)
	}

	spec := supervise.AgentPaths{StateDir: "/state", Home: home, Repo: "/repo", Port: 13705}.Spec()
	if spec.PathEnv != installed {
		t.Fatalf("reinstatement would have rewritten the job's PATH: got %q, want %q", spec.PathEnv, installed)
	}
}

// TestPlistPathEnvSurvivesEscaping: the PATH is read back out of the XML
// the installer wrote, so a directory with an ampersand or an angle
// bracket in it must round-trip. Getting this wrong would hand the
// carried-over PATH back mangled, which is worse than not carrying it.
func TestPlistPathEnvSurvivesEscaping(t *testing.T) {
	weird := `/opt/A&B/bin:/usr/<local>/bin:/Users/x/"q"/bin`
	xml := supervise.PlistXML(supervise.AgentSpec{
		Binary:  "/repo/bin/jevons-watchdog",
		Port:    13705,
		PathEnv: weird,
	})
	if got := supervise.PlistPathEnv(xml); got != weird {
		t.Fatalf("PATH did not round-trip through the plist: got %q, want %q", got, weird)
	}
	if got := supervise.PlistPathEnv(supervise.PlistXML(supervise.AgentSpec{Port: 13705})); got != "" {
		t.Fatalf("read a PATH out of a plist that has none: %q", got)
	}
}

// TestAnUndeliveredNoticeIsNotRecordedAsTold runs the daemon's loop over a
// machine with no watchdog binary — the one gap it cannot close by
// reloading — and takes the notice away from it. Journal writes fail while
// the daemon is still coming up, and a gap marked told on the strength of
// a notice that never landed is a gap the owner never hears about at all,
// which is precisely the five days this target is about.
func TestAnUndeliveredNoticeIsNotRecordedAsTold(t *testing.T) {
	stateDir := t.TempDir()
	dir := supervise.Dir(stateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	told := make(chan string, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go supervise.WatchAgentLoop(ctx,
		supervise.AgentPaths{StateDir: stateDir, Home: t.TempDir(), Repo: t.TempDir()},
		supervise.AgentConfig{Stale: 10 * time.Millisecond, Retry: time.Hour},
		20*time.Millisecond,
		func(subject, kind, text string) bool {
			told <- text
			return false // no journal to write to
		})

	select {
	case text := <-told:
		if !strings.Contains(text, "watchdog gap") {
			t.Fatalf("the owner notice does not say what is wrong: %q", text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon never noticed it had no supervisor")
	}

	// The next check must try again rather than assume silence was delivery.
	select {
	case <-told:
	case <-time.After(5 * time.Second):
		t.Fatal("an undelivered notice was recorded as told")
	}
	cancel()

	// And it kept no claim to have told anyone.
	st, err := supervise.LoadAgentState(dir)
	if err != nil {
		t.Fatalf("loading supervisor state: %v", err)
	}
	if st.Notified {
		t.Fatalf("state claims the owner was told by a notice that failed: %+v", st)
	}
}

// TestSupervisorStateIsNotSilentlyReset: the daemon's memory of an open
// gap paces its retries and holds the fact that the owner was told. A
// malformed file that read as a fresh start would bootstrap every tick
// and re-notify every tick, which is how an alarm stops being read.
func TestSupervisorStateIsNotSilentlyReset(t *testing.T) {
	dir := t.TempDir()
	want := supervise.AgentState{Attempts: 3, Notified: true, LastAttempt: time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)}
	if err := supervise.SaveAgentState(dir, want); err != nil {
		t.Fatalf("saving: %v", err)
	}
	got, err := supervise.LoadAgentState(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if got.Attempts != want.Attempts || got.Notified != want.Notified || !got.LastAttempt.Equal(want.LastAttempt) {
		t.Fatalf("state did not round-trip: got %+v, want %+v", got, want)
	}

	if _, err := supervise.LoadAgentState(t.TempDir()); err != nil {
		t.Fatalf("a missing state file is a healthy start, not an error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "supervisor.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := supervise.LoadAgentState(dir); err == nil {
		t.Fatal("malformed supervisor state was silently reset")
	}
}
