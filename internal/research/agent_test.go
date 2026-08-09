// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeFetcher struct {
	mu     sync.Mutex
	bodies map[string]string
	calls  []string
	err    error
}

func (f *fakeFetcher) Fetch(_ context.Context, rawURL string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, rawURL)
	if f.err != nil {
		return nil, f.err
	}
	body, ok := f.bodies[rawURL]
	if !ok {
		return nil, fmt.Errorf("no fixture for %s", rawURL)
	}
	return []byte(body), nil
}

func (f *fakeFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fakeDeliverer struct {
	mu      sync.Mutex
	briefs  []string
	failing bool
}

func (d *fakeDeliverer) DeliverBrief(text string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.failing {
		return fmt.Errorf("overseer unreachable")
	}
	d.briefs = append(d.briefs, text)
	return nil
}

func (d *fakeDeliverer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.briefs)
}

// testAgent builds an agent over a temp state dir and a workdir carrying a
// bullseye ledger, so a context pass has something deterministic to find
// without needing a git repository.
func testAgent(t *testing.T, mutate func(*Args)) (*Agent, string) {
	t.Helper()
	stateDir := t.TempDir()
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "bullseye.yaml"), []byte(bullseyeFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	args := Args{
		StateDir: stateDir,
		Workdir:  work,
		Now:      func() time.Time { return at(0) },
	}
	if mutate != nil {
		mutate(&args)
	}
	agent, err := New(args)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return agent, stateDir
}

func TestRunOnceWritesDurableNoteAndStaysQuietWhenUnchanged(t *testing.T) {
	deliverer := &fakeDeliverer{}
	agent, _ := testAgent(t, func(a *Args) { a.Deliverer = deliverer })

	res, err := agent.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !res.Changed() || len(res.Notes) != 1 {
		t.Fatalf("first pass should write a note: %+v", res)
	}
	if res.Notes[0].Topic != "context/frontier" || res.Notes[0].Revision != 1 {
		t.Fatalf("unexpected note: %+v", res.Notes[0])
	}
	if _, err := os.Stat(res.Notes[0].Path); err != nil {
		t.Fatalf("note path must be a real file (listable output): %v", err)
	}
	if !res.Delivered || deliverer.count() != 1 {
		t.Fatalf("a changed cycle should brief the overseer: delivered=%v briefs=%d", res.Delivered, deliverer.count())
	}
	if !strings.Contains(res.Brief, "context/frontier") {
		t.Fatalf("brief should name the note: %q", res.Brief)
	}

	// Second pass over unchanged context: no revision, no brief.
	again, err := agent.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce repeat: %v", err)
	}
	if again.Changed() {
		t.Fatalf("unchanged context must not manufacture a revision: %+v", again.Notes)
	}
	if again.Delivered || deliverer.count() != 1 {
		t.Fatalf("quiet cycle must not brief: delivered=%v briefs=%d", again.Delivered, deliverer.count())
	}

	st, err := agent.State()
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if st.Runs != 2 || st.BriefsSent != 1 {
		t.Fatalf("run record wrong: %+v", st)
	}
}

func TestRunOnceRespectsDisabledConfig(t *testing.T) {
	agent, stateDir := testAgent(t, nil)
	off := false
	if _, err := PatchConfig(stateDir, ConfigPatch{Enabled: &off, Now: at(0)}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	res, err := agent.RunOnce("schedule")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(res.Notes) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("disabled schedule must skip: %+v", res)
	}
	// A manual MCP call still works while disabled — the dial gates the schedule.
	if manual, err := agent.RunOnce("mcp"); err != nil || !manual.Changed() {
		t.Fatalf("manual cycle should run while disabled: %+v %v", manual, err)
	}
}

// 🎯T356 acceptance #1/#4: the periodic tick runs unattended — no owner turn,
// no MCP call — and leaves a durable note behind.
func TestScheduledTickRunsCycleUnattended(t *testing.T) {
	done := make(chan CycleResult, 4)
	agent, _ := testAgent(t, func(a *Args) {
		a.Interval = 40 * time.Millisecond
		a.OnResult = func(res CycleResult) {
			select {
			case done <- res:
			default:
			}
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		agent.Run(ctx)
	}()
	// The schedule must be stopped before the temp state dir is torn down,
	// otherwise a live cycle races the cleanup.
	defer func() {
		cancel()
		<-stopped
	}()

	select {
	case res := <-done:
		if !res.Changed() {
			t.Fatalf("scheduled tick produced nothing: %+v", res)
		}
		if res.Trigger != "schedule_boot" {
			t.Fatalf("want schedule-driven trigger, got %q", res.Trigger)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled tick never fired")
	}

	notes, err := agent.Store().List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(notes) != 1 || len(notes[0].Revisions) != 1 {
		t.Fatalf("scheduled tick must leave a durable note: %+v", notes)
	}
}

// 🎯T356 acceptance #3: a feed kicks a bounded research cycle with no owner
// turn; an unmoved feed kicks nothing.
func TestFeedTriggeredCycleWritesNoteAndIsBounded(t *testing.T) {
	const feedURL = "https://news.example.com/rss"
	fetcher := &fakeFetcher{bodies: map[string]string{feedURL: rssFixture}}
	deliverer := &fakeDeliverer{}
	agent, stateDir := testAgent(t, func(a *Args) {
		a.Fetcher = fetcher
		a.Deliverer = deliverer
	})
	on := true
	if _, err := PatchConfig(stateDir, ConfigPatch{
		FeedEnabled: &on,
		AddFeed:     &Feed{Name: "model-news", URL: feedURL, Enabled: true},
		Now:         at(0),
	}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}

	res, err := agent.PollFeeds(context.Background(), "schedule")
	if err != nil {
		t.Fatalf("PollFeeds: %v", err)
	}
	if len(res.Cycles) != 1 || res.Polled != 1 {
		t.Fatalf("want one feed-triggered cycle: %+v", res)
	}
	cycle := res.Cycles[0]
	if cycle.Trigger != "feed:model-news" || cycle.FeedItems != 2 {
		t.Fatalf("unexpected cycle: %+v", cycle)
	}
	if !cycle.Delivered || deliverer.count() != 1 {
		t.Fatalf("feed cycle should brief the overseer: %+v", cycle)
	}
	note, err := agent.Store().Get("feed/model-news")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(note.CurrentFindings()) != 3 {
		t.Fatalf("want 2 items + latest headline, got %d", len(note.CurrentFindings()))
	}
	var sawFeedSource bool
	for _, f := range note.CurrentFindings() {
		for _, s := range f.Sources {
			if s.Kind == "feed" {
				sawFeedSource = true
			}
		}
	}
	if !sawFeedSource {
		t.Fatal("feed findings must carry feed provenance")
	}

	// Same feed body again: seen items are remembered, so nothing kicks.
	repeat, err := agent.PollFeeds(context.Background(), "schedule")
	if err != nil {
		t.Fatalf("PollFeeds repeat: %v", err)
	}
	if len(repeat.Cycles) != 0 {
		t.Fatalf("unchanged feed must not kick a cycle: %+v", repeat)
	}
	if deliverer.count() != 1 {
		t.Fatalf("unchanged feed must not brief again, got %d briefs", deliverer.count())
	}

	// A new headline supersedes the rolling latest claim, keeping the old one.
	fetcher.mu.Lock()
	fetcher.bodies[feedURL] = strings.Replace(rssFixture,
		"<guid>news-a</guid>", "<guid>news-c</guid>", 1)
	fetcher.bodies[feedURL] = strings.Replace(fetcher.bodies[feedURL],
		"Frontier model ships tool use", "Successor model ships memory", 1)
	fetcher.mu.Unlock()
	moved, err := agent.PollFeeds(context.Background(), "schedule")
	if err != nil {
		t.Fatalf("PollFeeds moved: %v", err)
	}
	if len(moved.Cycles) != 1 || moved.Cycles[0].Notes[0].Superseded != 1 {
		t.Fatalf("new headline should supersede the latest claim: %+v", moved.Cycles)
	}
	note, err = agent.Store().Get("feed/model-news")
	if err != nil {
		t.Fatalf("Get after move: %v", err)
	}
	var superseded int
	for _, f := range note.Findings {
		if f.Status == StatusSuperseded && f.Key == "feed:latest" {
			superseded++
			if !strings.Contains(f.Claim, "Frontier model ships tool use") {
				t.Fatalf("prior headline must be retained verbatim: %q", f.Claim)
			}
		}
	}
	if superseded != 1 {
		t.Fatalf("want the prior headline retained as superseded, got %d", superseded)
	}
}

func TestPollFeedsRefusesHostsOutsideThePolicy(t *testing.T) {
	fetcher := &fakeFetcher{bodies: map[string]string{}}
	agent, stateDir := testAgent(t, func(a *Args) { a.Fetcher = fetcher })
	on := true
	cfg, err := PatchConfig(stateDir, ConfigPatch{
		FeedEnabled: &on,
		AddFeed:     &Feed{Name: "model-news", URL: "https://news.example.com/rss", Enabled: true},
		Now:         at(0),
	})
	if err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	// Subscribing allowed news.example.com; revoke it and the fetch must not happen.
	empty := []string{}
	if _, err := PatchConfig(stateDir, ConfigPatch{AllowedHosts: &empty, Now: at(0)}); err != nil {
		t.Fatalf("PatchConfig hosts: %v", err)
	}
	if !cfg.HostAllowed("https://news.example.com/rss") {
		t.Fatal("subscribing should have allowed the feed host")
	}
	res, err := agent.PollFeeds(context.Background(), "schedule")
	if err != nil {
		t.Fatalf("PollFeeds: %v", err)
	}
	if len(res.Cycles) != 0 || fetcher.callCount() != 0 {
		t.Fatalf("a host outside the policy must never be fetched: %+v calls=%d", res, fetcher.callCount())
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "allowed_hosts") {
		t.Fatalf("skip reason should name the policy: %+v", res.Skipped)
	}
}

func TestPollFeedsSkipsWhenDisabledOrUnsubscribed(t *testing.T) {
	agent, stateDir := testAgent(t, func(a *Args) { a.Fetcher = &fakeFetcher{} })
	res, err := agent.PollFeeds(context.Background(), "schedule")
	if err != nil {
		t.Fatalf("PollFeeds: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != "feeds disabled" {
		t.Fatalf("feeds are off by default: %+v", res)
	}
	on := true
	if _, err := PatchConfig(stateDir, ConfigPatch{FeedEnabled: &on, Now: at(0)}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	res, err = agent.PollFeeds(context.Background(), "schedule")
	if err != nil {
		t.Fatalf("PollFeeds enabled: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != "no enabled feeds" {
		t.Fatalf("no subscriptions means nothing to poll: %+v", res)
	}
}

func TestPollFeedsRecordsFetchFailureWithoutLosingCursor(t *testing.T) {
	const feedURL = "https://news.example.com/rss"
	fetcher := &fakeFetcher{err: fmt.Errorf("dial tcp: refused")}
	agent, stateDir := testAgent(t, func(a *Args) { a.Fetcher = fetcher })
	on := true
	if _, err := PatchConfig(stateDir, ConfigPatch{
		FeedEnabled: &on,
		AddFeed:     &Feed{Name: "model-news", URL: feedURL, Enabled: true},
		Now:         at(0),
	}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	res, err := agent.PollFeeds(context.Background(), "schedule")
	if err != nil {
		t.Fatalf("a failing feed must not fail the sweep: %v", err)
	}
	if len(res.Cycles) != 0 || len(res.Skipped) != 1 {
		t.Fatalf("want a skip, got %+v", res)
	}
	cur, err := LoadFeedCursor(agent.Store().Dir())
	if err != nil {
		t.Fatalf("LoadFeedCursor: %v", err)
	}
	marker := cur.Feeds["model-news"]
	if marker.Polls != 1 || marker.LastError == "" {
		t.Fatalf("failure should be recorded on the marker: %+v", marker)
	}
}

func TestBriefBudgetBoundsDelivery(t *testing.T) {
	deliverer := &fakeDeliverer{}
	agent, stateDir := testAgent(t, func(a *Args) { a.Deliverer = deliverer })
	one := 1
	if _, err := PatchConfig(stateDir, ConfigPatch{MaxBriefsPerHour: &one, Now: at(0)}); err != nil {
		t.Fatalf("PatchConfig: %v", err)
	}
	if _, err := agent.RunOnce("test"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// Force a second changed cycle by moving the frontier under the agent.
	work := agent.args.Workdir
	moved := strings.Replace(bullseyeFixture, "status: identified\n  T356.1", "status: achieved\n  T356.1", 1)
	if err := os.WriteFile(filepath.Join(work, "bullseye.yaml"), []byte(moved), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := agent.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce 2: %v", err)
	}
	if !res.Changed() {
		t.Fatal("frontier moved; the note should have changed")
	}
	if res.Delivered {
		t.Fatal("brief budget should have blocked the second delivery")
	}
	if deliverer.count() != 1 {
		t.Fatalf("want exactly 1 brief under the budget, got %d", deliverer.count())
	}
	if len(res.Skipped) == 0 || !strings.Contains(res.Skipped[0], "budget") {
		t.Fatalf("skip reason should name the budget: %+v", res.Skipped)
	}
}

func TestDeliveryFailureIsRecordedNotFatal(t *testing.T) {
	deliverer := &fakeDeliverer{failing: true}
	agent, _ := testAgent(t, func(a *Args) { a.Deliverer = deliverer })
	res, err := agent.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if res.Delivered {
		t.Fatal("failed delivery must not report delivered")
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "deliver:") {
		t.Fatalf("failure should be recorded: %+v", res.Skipped)
	}
	if !res.Changed() {
		t.Fatal("the note is still durable even when the brief did not land")
	}
}

func TestDryRunNeverDelivers(t *testing.T) {
	deliverer := &fakeDeliverer{}
	agent, _ := testAgent(t, func(a *Args) {
		a.Deliverer = deliverer
		a.DryRun = true
	})
	res, err := agent.RunOnce("test")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if res.Delivered || deliverer.count() != 0 {
		t.Fatalf("dry run must not deliver: %+v", res)
	}
	if res.Brief == "" {
		t.Fatal("dry run should still render the brief for inspection")
	}
}
