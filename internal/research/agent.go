// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// EventSource labels research briefs on the wire to the overseer.
const EventSource = "research"

// StateFileName records what the standing cycle last did.
const StateFileName = "state.json"

// firstDelay keeps boot cheap without postponing the first pass by a whole
// interval when the daemon restarts.
const firstDelay = 3 * time.Minute

// BriefDeliverer posts a research brief to the overseer (fire-and-forget).
type BriefDeliverer interface {
	DeliverBrief(text string) error
}

// Args configures the standing research agent (🎯T356).
type Args struct {
	// StateDir holds config, notes, cursor, and run state (required).
	StateDir string
	// Workdir is the primary repository explored each pass.
	Workdir string
	// RelatedRoots are org roots scanned for sibling repositories.
	RelatedRoots []string
	// EventLogPath and SessionsDir are the fleet surfaces. Empty skips.
	EventLogPath string
	SessionsDir  string
	// Deliverer posts briefs to the overseer. Nil means notes-only.
	Deliverer BriefDeliverer
	// Capacity admits or defers scheduled passes against the fleet's
	// remaining budget and load (🎯T359). Nil runs every scheduled pass, as
	// before. Owner-invoked cycles (MCP, tests) are never gated.
	Capacity capacity.Gate
	// Fetcher retrieves feed bodies (default: bounded HTTP fetcher).
	Fetcher Fetcher
	// Interval overrides the config cadence when non-zero.
	Interval time.Duration
	// DryRun never delivers.
	DryRun bool
	// Now optional clock.
	Now func() time.Time
	// OnResult optional hook after each cycle.
	OnResult func(CycleResult)
}

// NoteChange reports one note's outcome in a cycle.
type NoteChange struct {
	ID         string `json:"id"`
	Topic      string `json:"topic"`
	Path       string `json:"path"`
	Revision   int    `json:"revision"`
	Added      int    `json:"added"`
	Confirmed  int    `json:"confirmed"`
	Superseded int    `json:"superseded"`
	Changed    bool   `json:"changed"`
}

// CycleResult is one bounded research cycle.
type CycleResult struct {
	Trigger   string       `json:"trigger"`
	Notes     []NoteChange `json:"notes"`
	Stats     ScanStats    `json:"stats"`
	FeedItems int          `json:"feed_items"`
	Brief     string       `json:"brief,omitempty"`
	Delivered bool         `json:"delivered"`
	Skipped   []string     `json:"skipped,omitempty"`
}

// Changed reports whether the cycle moved any note.
func (r CycleResult) Changed() bool {
	for _, n := range r.Notes {
		if n.Changed {
			return true
		}
	}
	return false
}

// State is the durable record of the standing cycle's activity.
type State struct {
	Runs           int    `json:"runs"`
	LastRunAt      string `json:"last_run_at,omitempty"`
	LastTrigger    string `json:"last_trigger,omitempty"`
	LastChanged    int    `json:"last_changed,omitempty"`
	FeedRuns       int    `json:"feed_runs,omitempty"`
	LastFeedRunAt  string `json:"last_feed_run_at,omitempty"`
	LastFeedItems  int    `json:"last_feed_items,omitempty"`
	BriefsSent     int    `json:"briefs_sent,omitempty"`
	LastBriefAt    string `json:"last_brief_at,omitempty"`
	LastSkipReason string `json:"last_skip_reason,omitempty"`
}

// Agent is the standing research staff cycle: periodic context refresh plus an
// async feed trigger, both writing durable versioned notes.
type Agent struct {
	args       Args
	store      *Store
	mu         sync.Mutex
	briefs     []time.Time
	feedCycles []time.Time
}

// New validates args and opens the note store.
func New(args Args) (*Agent, error) {
	if strings.TrimSpace(args.StateDir) == "" {
		return nil, fmt.Errorf("research: StateDir required")
	}
	if args.Now == nil {
		args.Now = func() time.Time { return time.Now().UTC() }
	}
	if args.Fetcher == nil {
		args.Fetcher = NewHTTPFetcher()
	}
	store, err := Open(args.StateDir)
	if err != nil {
		return nil, err
	}
	return &Agent{args: args, store: store}, nil
}

// Store exposes the durable note ledger (listing surfaces).
func (a *Agent) Store() *Store {
	if a == nil {
		return nil
	}
	return a.store
}

// StateDir returns the configured state directory.
func (a *Agent) StateDir() string {
	if a == nil {
		return ""
	}
	return a.args.StateDir
}

// Config reloads the durable config.
func (a *Agent) Config() (Config, error) {
	if a == nil {
		return Config{}, fmt.Errorf("research: nil agent")
	}
	return LoadConfig(a.args.StateDir)
}

// PatchConfig applies an overseer retune and persists it.
func (a *Agent) PatchConfig(patch ConfigPatch) (Config, error) {
	if a == nil {
		return Config{}, fmt.Errorf("research: nil agent")
	}
	if patch.Now.IsZero() {
		patch.Now = a.args.Now()
	}
	return PatchConfig(a.args.StateDir, patch)
}

// State returns the durable run record.
func (a *Agent) State() (State, error) {
	if a == nil {
		return State{}, fmt.Errorf("research: nil agent")
	}
	return LoadState(a.store.Dir())
}

// LoadState reads the durable run record from a store directory.
func LoadState(dir string) (State, error) {
	path := filepath.Join(dir, StateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("research state: read %s: %w", path, err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("research state: parse %s: %w", path, err)
	}
	return st, nil
}

// SaveState persists the run record atomically.
func SaveState(dir string, st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("research state: marshal: %w", err)
	}
	return writeFileAtomic(filepath.Join(dir, StateFileName), append(data, '\n'), 0o644)
}

// Run starts the standing schedules until ctx is done: context refresh on one
// cadence, feed polling on another.
func (a *Agent) Run(ctx context.Context) {
	if a == nil {
		return
	}
	go a.runFeedSchedule(ctx)
	cfg, err := LoadConfig(a.args.StateDir)
	if err != nil {
		slog.Warn("research schedule disabled", "err", err)
		<-ctx.Done()
		return
	}
	interval := cfg.IntervalDuration()
	if a.args.Interval != 0 {
		interval = a.args.Interval
	}
	if interval <= 0 {
		<-ctx.Done()
		return
	}
	delay := min(firstDelay, interval)
	first := time.NewTimer(delay)
	defer first.Stop()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			a.runContextSafe("schedule_boot")
		case <-ticker.C:
			// Bi-directional retune: reload cadence, restart ticker if changed.
			next, err := LoadConfig(a.args.StateDir)
			if err != nil {
				continue
			}
			if !next.Enabled {
				continue
			}
			if d := next.IntervalDuration(); d > 0 && d != interval && a.args.Interval == 0 {
				ticker.Stop()
				interval = d
				ticker = time.NewTicker(interval)
			}
			a.runContextSafe("schedule")
		}
	}
}

func (a *Agent) runFeedSchedule(ctx context.Context) {
	cfg, err := LoadConfig(a.args.StateDir)
	if err != nil {
		<-ctx.Done()
		return
	}
	interval := cfg.FeedIntervalDuration()
	if interval <= 0 {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := LoadConfig(a.args.StateDir)
			if err != nil {
				continue
			}
			if !next.Enabled || !next.FeedEnabled {
				continue
			}
			if d := next.FeedIntervalDuration(); d > 0 && d != interval {
				ticker.Stop()
				interval = d
				ticker = time.NewTicker(interval)
			}
			// The feed trigger competes for the same ambient capacity as the
			// context pass (🎯T359) — one research cycle at a time, and none
			// while the owner or Build work needs the room.
			verdict, release := capacity.Ask(a.args.Capacity, capacity.ClassResearch, "feed_schedule")
			if !verdict.Admitted() {
				release()
				a.noteCapacitySkip("feed_schedule", verdict)
				continue
			}
			_, err = a.PollFeeds(ctx, "schedule")
			release()
			if err != nil {
				slog.Warn("research feed poll failed", "err", err)
			}
		}
	}
}

// runContextSafe runs one scheduled pass, subject to capacity admission
// (🎯T359): when the fleet is short of budget or busy with owner work, this
// tick is skipped (and the skip recorded) rather than queued behind everything
// else. An elevated-pressure pass still runs, at a reduced scope.
func (a *Agent) runContextSafe(reason string) {
	verdict, release := capacity.Ask(a.args.Capacity, capacity.ClassResearch, reason)
	defer release()
	if !verdict.Admitted() {
		a.noteCapacitySkip(reason, verdict)
		return
	}
	res, err := a.runOnce(reason, verdict.Tier)
	if err != nil {
		slog.Warn("research cycle failed", "reason", reason, "err", err)
		return
	}
	if res.Changed() {
		slog.Info("research cycle",
			"reason", reason,
			"notes", len(res.Notes),
			"repos", res.Stats.Repos,
			"commits", res.Stats.Commits,
			"delivered", res.Delivered,
		)
	}
}

// RunOnce executes one context-refresh cycle now (schedule, MCP, or tests) at
// full scope. Scheduled passes go through runContextSafe, which may run this
// at a reduced scope when capacity is elevated (🎯T359).
func (a *Agent) RunOnce(reason string) (CycleResult, error) {
	return a.runOnce(reason, capacity.TierFull)
}

// noteCapacitySkip records a deferred tick durably, so "why did research go
// quiet?" is answerable from state.json and the log rather than guessed at.
func (a *Agent) noteCapacitySkip(reason string, d capacity.Decision) {
	slog.Info("research cycle deferred by capacity",
		"reason", reason, "verdict", d.Verdict, "cause", d.Reason,
		"pressure", d.Pressure.String(), "detail", d.Detail)
	st, _ := LoadState(a.store.Dir())
	st.LastSkipReason = fmt.Sprintf("capacity %s: %s (%s)", d.Verdict, d.Reason, d.Detail)
	if err := SaveState(a.store.Dir(), st); err != nil {
		slog.Warn("research state save failed", "err", err)
	}
}

// reducedFactor shrinks a bound for a degraded pass: half the lookback and
// half the mining budget still refreshes the notes, at roughly half the cost.
func reducedFactor(n int) int {
	if n <= 1 {
		return n
	}
	return n / 2
}

func (a *Agent) runOnce(reason, tier string) (CycleResult, error) {
	if a == nil {
		return CycleResult{}, fmt.Errorf("research: nil agent")
	}
	cfg, err := LoadConfig(a.args.StateDir)
	if err != nil {
		return CycleResult{}, err
	}
	manual := reason == "mcp" || reason == "test"
	if !cfg.Enabled && !manual {
		return CycleResult{Trigger: reason, Skipped: []string{"disabled"}}, nil
	}
	now := a.args.Now()
	workdir := firstNonEmpty(cfg.Workdir, a.args.Workdir)
	roots := cfg.RelatedRoots
	if len(roots) == 0 {
		roots = a.args.RelatedRoots
	}
	lookback, maxCommits, maxRepos := cfg.Lookback(), cfg.EffectiveMaxCommits(), cfg.EffectiveMaxRepos()
	if tier == capacity.TierReduced {
		lookback, maxCommits, maxRepos = lookback/2, reducedFactor(maxCommits), reducedFactor(maxRepos)
	}
	scan, err := Scan(ScanArgs{
		Workdir:      workdir,
		RelatedRoots: roots,
		EventLogPath: a.args.EventLogPath,
		SessionsDir:  a.args.SessionsDir,
		Since:        now.Add(-lookback),
		MaxCommits:   maxCommits,
		MaxRepos:     maxRepos,
		Now:          now,
	})
	if err != nil {
		return CycleResult{}, err
	}
	res := CycleResult{Trigger: reason, Stats: scan.Stats}
	for i := range scan.Inputs {
		scan.Inputs[i].Trigger = reason
		if err := a.applyInput(&res, scan.Inputs[i]); err != nil {
			return res, err
		}
	}
	a.finish(&res, cfg, now, false)
	return res, nil
}

// FeedPollResult reports one feed-trigger sweep.
type FeedPollResult struct {
	Cycles  []CycleResult `json:"cycles"`
	Polled  int           `json:"polled"`
	Skipped []string      `json:"skipped,omitempty"`
}

// PollFeeds fetches every enabled feed and runs a bounded research cycle per
// feed that produced new items. This is the async trigger path: it kicks
// without waiting for an owner turn, and stays quiet when the feed has not
// moved (🎯T356 acceptance #3).
func (a *Agent) PollFeeds(ctx context.Context, reason string) (FeedPollResult, error) {
	if a == nil {
		return FeedPollResult{}, fmt.Errorf("research: nil agent")
	}
	cfg, err := LoadConfig(a.args.StateDir)
	if err != nil {
		return FeedPollResult{}, err
	}
	manual := reason == "mcp" || reason == "test"
	var out FeedPollResult
	if !cfg.FeedEnabled && !manual {
		out.Skipped = append(out.Skipped, "feeds disabled")
		return out, nil
	}
	feeds := cfg.EnabledFeeds()
	if len(feeds) == 0 {
		out.Skipped = append(out.Skipped, "no enabled feeds")
		return out, nil
	}
	now := a.args.Now()
	cur, err := LoadFeedCursor(a.store.Dir())
	if err != nil {
		return out, err
	}
	for _, feed := range feeds {
		if !a.allowFeedCycle(cfg, now) {
			out.Skipped = append(out.Skipped, "feed cycle budget exhausted")
			break
		}
		if !cfg.HostAllowed(feed.URL) {
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s: host not in allowed_hosts", feed.Name))
			continue
		}
		marker := cur.Feeds[feed.Name]
		marker.Polls++
		marker.LastPolledAt = stamp(now)
		out.Polled++

		body, err := a.args.Fetcher.Fetch(ctx, feed.URL)
		if err != nil {
			marker.LastError = err.Error()
			cur.Feeds[feed.Name] = marker
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s: fetch: %v", feed.Name, err))
			continue
		}
		items, err := ParseFeed(body)
		if err != nil {
			marker.LastError = err.Error()
			cur.Feeds[feed.Name] = marker
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s: parse: %v", feed.Name, err))
			continue
		}
		marker.LastError = ""
		fresh, marker := NewItems(marker, items, cfg.EffectiveMaxFeedItems())
		cur.Feeds[feed.Name] = marker
		if len(fresh) == 0 {
			out.Skipped = append(out.Skipped, fmt.Sprintf("%s: no new items", feed.Name))
			continue
		}
		trigger := "feed:" + feed.Name
		res := CycleResult{Trigger: trigger, FeedItems: len(fresh)}
		err = a.applyInput(&res, RevisionInput{
			Topic:    feed.NoteTopic(),
			Title:    "Feed: " + feed.Name,
			Trigger:  trigger,
			Summary:  fmt.Sprintf("%d new items from %s", len(fresh), feed.URL),
			At:       now,
			Findings: FeedFindings(feed, fresh),
			Sources:  []Source{{Kind: "feed", Ref: feed.URL, At: stamp(now)}},
		})
		if err != nil {
			return out, err
		}
		a.recordFeedCycle(now)
		a.finish(&res, cfg, now, true)
		out.Cycles = append(out.Cycles, res)
	}
	if err := SaveFeedCursor(a.store.Dir(), cur); err != nil {
		return out, err
	}
	return out, nil
}

// applyInput folds one proposed revision into its note and records the outcome.
func (a *Agent) applyInput(res *CycleResult, in RevisionInput) error {
	note, delta, err := a.store.Apply(in)
	if err != nil {
		return err
	}
	res.Notes = append(res.Notes, NoteChange{
		ID:         note.ID,
		Topic:      note.Topic,
		Path:       a.store.NotePath(note.ID),
		Revision:   delta.Revision,
		Added:      len(delta.Added),
		Confirmed:  len(delta.Confirmed),
		Superseded: len(delta.Superseded),
		Changed:    delta.Changed,
	})
	return nil
}

// finish delivers a brief when the cycle actually moved something, then
// persists the run record. A quiet cycle stays silent.
func (a *Agent) finish(res *CycleResult, cfg Config, now time.Time, feed bool) {
	if res.Changed() && cfg.Deliver && !a.args.DryRun && a.args.Deliverer != nil {
		if a.allowBrief(cfg, now) {
			res.Brief = FormatBrief(*res, a.store)
			if err := a.args.Deliverer.DeliverBrief(res.Brief); err != nil {
				slog.Warn("research brief delivery failed", "err", err)
				res.Skipped = append(res.Skipped, "deliver: "+err.Error())
			} else {
				res.Delivered = true
				a.recordBrief(now)
			}
		} else {
			res.Skipped = append(res.Skipped, "brief budget exhausted")
		}
	} else if res.Changed() && res.Brief == "" {
		res.Brief = FormatBrief(*res, a.store)
	}

	st, _ := LoadState(a.store.Dir())
	changed := 0
	for _, n := range res.Notes {
		if n.Changed {
			changed++
		}
	}
	if feed {
		st.FeedRuns++
		st.LastFeedRunAt = stamp(now)
		st.LastFeedItems = res.FeedItems
	} else {
		st.Runs++
		st.LastRunAt = stamp(now)
	}
	st.LastTrigger = res.Trigger
	st.LastChanged = changed
	if res.Delivered {
		st.BriefsSent++
		st.LastBriefAt = stamp(now)
	}
	if len(res.Skipped) > 0 {
		st.LastSkipReason = res.Skipped[len(res.Skipped)-1]
	}
	if err := SaveState(a.store.Dir(), st); err != nil {
		slog.Warn("research state save failed", "err", err)
	}
	if a.args.OnResult != nil {
		a.args.OnResult(*res)
	}
}

// FormatBrief renders the overseer-facing digest of one cycle.
func FormatBrief(res CycleResult, store *Store) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Research cycle (🎯T356) trigger=%s\n", res.Trigger)
	if res.FeedItems > 0 {
		fmt.Fprintf(&b, "  feed items: %d\n", res.FeedItems)
	}
	if res.Stats.Repos > 0 {
		fmt.Fprintf(&b, "  scanned: %d repos, %d commits, %d event rows, %d session turns\n",
			res.Stats.Repos, res.Stats.Commits, res.Stats.EventRows, res.Stats.SessionTurns)
	}
	for _, n := range res.Notes {
		if !n.Changed {
			continue
		}
		fmt.Fprintf(&b, "  %s rev %d: +%d new, %d superseded, %d confirmed → %s\n",
			n.Topic, n.Revision, n.Added, n.Superseded, n.Confirmed, n.Path)
		if store == nil {
			continue
		}
		note, err := store.Get(n.ID)
		if err != nil {
			continue
		}
		rev := note.LatestRevision()
		for _, s := range rev.Superseded {
			fmt.Fprintf(&b, "    superseded %s: %q → %q\n", s.Key, s.PriorClaim, s.NewClaim)
		}
		for _, key := range rev.Added {
			for _, f := range note.CurrentFindings() {
				if f.Key == key {
					fmt.Fprintf(&b, "    new %s: %s\n", f.Key, f.Claim)
					break
				}
			}
		}
	}
	b.WriteString("Notes are durable and versioned; read them with jevons_research_read.\n")
	return b.String()
}

func (a *Agent) allowBrief(cfg Config, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.briefs = pruneWindow(a.briefs, now, time.Hour)
	return len(a.briefs) < cfg.EffectiveMaxBriefsPerHour()
}

func (a *Agent) recordBrief(now time.Time) {
	a.mu.Lock()
	a.briefs = append(pruneWindow(a.briefs, now, time.Hour), now)
	a.mu.Unlock()
}

func (a *Agent) allowFeedCycle(cfg Config, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.feedCycles = pruneWindow(a.feedCycles, now, time.Hour)
	return len(a.feedCycles) < cfg.EffectiveMaxFeedCyclesPerHour()
}

func (a *Agent) recordFeedCycle(now time.Time) {
	a.mu.Lock()
	a.feedCycles = append(pruneWindow(a.feedCycles, now, time.Hour), now)
	a.mu.Unlock()
}

func pruneWindow(stamps []time.Time, now time.Time, window time.Duration) []time.Time {
	floor := now.Add(-window)
	out := stamps[:0]
	for _, t := range stamps {
		if t.After(floor) {
			out = append(out, t)
		}
	}
	return out
}
