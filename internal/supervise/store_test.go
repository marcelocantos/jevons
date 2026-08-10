// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package supervise_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/supervise"
)

// TestStateRoundTrip: launchd starts a fresh process every interval, so
// everything the supervisor knows has to survive on disk or it forgets an
// open outage every 30 seconds.
func TestStateRoundTrip(t *testing.T) {
	dir := supervise.Dir(t.TempDir())

	st, err := supervise.LoadState(dir)
	if err != nil {
		t.Fatalf("a missing state file must read as a healthy start: %v", err)
	}
	if st.Down() {
		t.Fatalf("missing state read as an outage: %+v", st)
	}

	want := supervise.State{
		DownSince:   time.Date(2026, 8, 10, 23, 54, 8, 0, time.UTC),
		Attempts:    2,
		LastAttempt: time.Date(2026, 8, 10, 23, 58, 8, 0, time.UTC),
		Notified:    true,
	}
	if err := supervise.SaveState(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := supervise.LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.DownSince.Equal(want.DownSince) || !got.LastAttempt.Equal(want.LastAttempt) ||
		got.Attempts != want.Attempts || got.Notified != want.Notified {
		t.Fatalf("round trip lost state: got %+v want %+v", got, want)
	}
}

// TestMalformedStateIsAnError: silently resetting is how an outage
// becomes invisible — a supervisor that forgets stops escalating.
func TestMalformedStateIsAnError(t *testing.T) {
	dir := supervise.Dir(t.TempDir())
	if err := supervise.SaveState(dir, supervise.State{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := supervise.LoadState(dir); err == nil {
		t.Fatal("malformed state read as a healthy start")
	}
}

// TestOutageLogSurvivesATruncatedLine: the watchdog appends and can be
// killed mid-write. Losing the tail beats losing the history.
func TestOutageLogSurvivesATruncatedLine(t *testing.T) {
	dir := supervise.Dir(t.TempDir())
	o := supervise.Outage{
		ID:          "2026-08-10T23:54:08Z",
		DownSince:   time.Date(2026, 8, 10, 23, 54, 8, 0, time.UTC),
		RecoveredAt: time.Date(2026, 8, 10, 23, 58, 8, 0, time.UTC),
		Attempts:    1,
	}
	if err := supervise.AppendOutage(dir, o); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "outages.jsonl"), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"id":"half-writ`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := supervise.LoadOutages(dir)
	if err != nil {
		t.Fatalf("a torn final line must not be fatal: %v", err)
	}
	if len(got) != 1 || got[0].ID != o.ID {
		t.Fatalf("lost the intact history to a torn line: %+v", got)
	}
	if got[0].Downtime() != 4*time.Minute {
		t.Errorf("downtime=%s want 4m", got[0].Downtime())
	}
	text := got[0].Text()
	for _, want := range []string{"outage", "4m0s", "1 restart", "no owner action"} {
		if !strings.Contains(text, want) {
			t.Errorf("owner-facing text missing %q: %s", want, text)
		}
	}
}

// TestReportPendingTellsTheOwnerOnce is the handover between the two
// processes: the watchdog records a recovery it cannot announce, and the
// daemon announces it when it is back — but only once, however often it
// restarts afterwards.
func TestReportPendingTellsTheOwnerOnce(t *testing.T) {
	dir := supervise.Dir(t.TempDir())
	for _, id := range []string{"a", "b"} {
		if err := supervise.AppendOutage(dir, supervise.Outage{
			ID:          id,
			DownSince:   time.Date(2026, 8, 10, 23, 54, 8, 0, time.UTC),
			RecoveredAt: time.Date(2026, 8, 10, 23, 55, 8, 0, time.UTC),
			Attempts:    1,
		}); err != nil {
			t.Fatal(err)
		}
	}

	var told int
	notify := func(subject, kind, text string) bool {
		if subject != supervise.OutageSubject || kind != supervise.OutageKind {
			t.Errorf("filed as %q/%q", subject, kind)
		}
		told++
		return true
	}
	n, err := supervise.ReportPending(dir, notify)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || told != 2 {
		t.Fatalf("reported %d of 2 outages (told=%d)", n, told)
	}
	if n, err = supervise.ReportPending(dir, notify); err != nil || n != 0 || told != 2 {
		t.Fatalf("re-reported already-told outages: n=%d told=%d err=%v", n, told, err)
	}
}

// TestUndeliveredNoticesStayPending: a notice that did not land must not
// be marked told, or the one outage the owner should hear about is the
// one they never do.
func TestUndeliveredNoticesStayPending(t *testing.T) {
	dir := supervise.Dir(t.TempDir())
	if err := supervise.AppendOutage(dir, supervise.Outage{
		ID:          "a",
		DownSince:   time.Now().Add(-time.Minute),
		RecoveredAt: time.Now(),
		Attempts:    1,
	}); err != nil {
		t.Fatal(err)
	}

	n, err := supervise.ReportPending(dir, func(string, string, string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("counted %d undelivered notices as reported", n)
	}
	n, err = supervise.ReportPending(dir, func(string, string, string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("a later, better-configured run did not retry the notice: n=%d", n)
	}
}

// TestPlistRendersTheJobLaunchdNeeds: the supervisor is only real once
// launchd runs it, so the plist has to name the binary, the port, the
// repo the restart script lives in, and an interval.
func TestPlistRendersTheJobLaunchdNeeds(t *testing.T) {
	spec := supervise.AgentSpec{
		Binary:   "/repo/bin/jevons-watchdog",
		Repo:     "/repo",
		StateDir: "/home/.jevons",
		Port:     13705,
		LogPath:  "/home/.jevons/watchdog/watchdog.log",
	}
	xml := supervise.PlistXML(spec)
	for _, want := range []string{
		supervise.AgentLabel,
		"/repo/bin/jevons-watchdog",
		"<string>13705</string>",
		"<string>/repo</string>",
		"<key>StartInterval</key>",
		"<key>RunAtLoad</key>",
		spec.LogPath,
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("plist missing %q:\n%s", want, xml)
		}
	}

	// A 30s interval against a 90s grace is what bounds an outage at about
	// two minutes; a longer one silently widens that.
	if supervise.AgentInterval > 60 {
		t.Errorf("StartInterval %ds is too coarse to bound an outage", supervise.AgentInterval)
	}

	// Written where launchd looks, and escaped: a path with an ampersand
	// in it would otherwise produce a plist launchd refuses to parse.
	home := t.TempDir()
	path := supervise.AgentPlistPath(home)
	if !strings.HasSuffix(path, filepath.Join("Library", "LaunchAgents", supervise.AgentLabel+".plist")) {
		t.Errorf("plist path %q is not a LaunchAgent", path)
	}
	spec.Repo = "/re&po"
	if err := supervise.WriteAgentPlist(path, spec); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "/re&amp;po") {
		t.Errorf("plist did not escape the path:\n%s", b)
	}
}
