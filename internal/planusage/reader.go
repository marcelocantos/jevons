// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package planusage

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
)

// FetchFunc is the producer seam: one round of readings, one per backend.
// The real implementation is claudia.QueryAllPlanUsage; the oracle supplies
// fixtures. Nothing else in this package knows an endpoint exists.
type FetchFunc func(ctx context.Context) ([]claudia.PlanUsage, error)

// ReaderArgs configures a Reader. Every field has a working default except
// Fetch, which defaults to the real claudia query.
type ReaderArgs struct {
	// Fetch produces one round of readings. Nil uses claudia.QueryAllPlanUsage
	// over SupportedProviders.
	Fetch FetchFunc
	// Load reports registered fleet agents per provider (mcpserver.HarnessLoad).
	// Nil means no backend is known to be in use, which only affects ordering
	// and the tightest-remaining read — never whether a number is shown.
	Load func() map[string]int
	// Refresh is the poll interval. Zero uses DefaultRefresh.
	Refresh time.Duration
	// StaleAfter is how old a reading may be before it is marked stale.
	// Zero uses DefaultStaleAfter; negative disables staleness marking.
	StaleAfter time.Duration
	// Now overrides the clock (tests). Nil uses time.Now.
	Now func() time.Time
	// FetchTimeout bounds one round of provider calls. Zero uses 20s.
	FetchTimeout time.Duration
	// FixturePath, when set, makes Snapshot read that JSON file instead
	// of the last fetch (isolate journeys, 🎯T390.1.5). Env
	// JEVONS_PLAN_USAGE_FIXTURE is used when this is empty.
	FixturePath string
	// OnUpdate runs after every Refresh returns (success or failure),
	// without holding the reader lock. Mux fans the restamped snapshot
	// so the cockpit is not a second poll behind the producer (🎯T631).
	OnUpdate func()
	// History, when set, appends each successful Refresh's published
	// remaining_percent and attaches the current-period series to Snapshot
	// (🎯T634). Nil means no persist — tests and a failed store open.
	// Isolates pass their own state_dir path so they never read development
	// readings.
	History History
	// ForceFetch is the producer used by RefreshNow (cockpit reload,
	// 🎯T653). Nil uses Fetch, or the default LoadPlanUsage with Refresh.
	ForceFetch FetchFunc
	// GrokTokenRefresh rotates the Grok login token after a billing 401
	// (🎯T666). Nil uses DefaultGrokTokenRefresher (a headless grok
	// one-shot); GrokRefreshDisabled turns the hook off entirely.
	GrokTokenRefresh    GrokTokenRefresher
	GrokRefreshDisabled bool
	// GrokRefreshWindow is the least time between two one-shots. Zero uses
	// DefaultGrokRefreshWindow.
	GrokRefreshWindow time.Duration
	// LogEvent, when set, journals the refresh outcome (component plan_usage).
	LogEvent func(component, decision string, fields map[string]any)
}

// Reader keeps the last round of plan-usage readings and re-shapes them on
// demand.
//
// It caches the RAW readings rather than a finished Snapshot on purpose: age
// and staleness are properties of the moment the owner looks, not of the
// moment we fetched. A cached Snapshot would freeze "3 seconds old" into a
// reading that is now forty minutes old, which is the exact dishonesty
// clause 5 of 🎯T390 asks the oracle to rule out.
type Reader struct {
	args ReaderArgs

	mu       sync.Mutex
	readings []claudia.PlanUsage
	fetched  bool
	lastErr  string
	// lastGrokRefresh is when the 🎯T666 one-shot last ran.
	lastGrokRefresh time.Time

	// ready is closed on the first successful Refresh so GET /api/plan-usage
	// can long-poll until the first batch lands instead of returning pending.
	ready     chan struct{}
	readyOnce sync.Once

	refreshing chan struct{}
	lastKick   error
}

// NewReader builds a Reader. It does not fetch — call Refresh or Run.
func NewReader(args ReaderArgs) *Reader {
	usedCustom := args.Fetch != nil
	if args.Fetch == nil {
		args.Fetch = defaultPlanUsageFetch(false)
	}
	if args.ForceFetch == nil {
		if usedCustom {
			args.ForceFetch = args.Fetch
		} else {
			args.ForceFetch = defaultPlanUsageFetch(true)
		}
	}
	if args.Refresh <= 0 {
		args.Refresh = DefaultRefresh
	}
	if args.StaleAfter == 0 {
		args.StaleAfter = DefaultStaleAfter
	}
	if args.Now == nil {
		args.Now = time.Now
	}
	if args.FetchTimeout <= 0 {
		args.FetchTimeout = 20 * time.Second
	}
	return &Reader{args: args, ready: make(chan struct{})}
}

func defaultPlanUsageFetch(refresh bool) FetchFunc {
	return func(ctx context.Context) ([]claudia.PlanUsage, error) {
		tok := strings.TrimSpace(os.Getenv(CursorAPIKeyEnv))
		if tok == "" {
			if t, err := loadCursorAccessToken(""); err == nil {
				tok = t
			}
		}
		// The claudia daemon is the one plan-usage evaluator on the
		// host when it is running; LoadPlanUsage reads its snapshot
		// and falls back to the host-wide filesystem cache (shared
		// with every other claudia consumer) when it is not. Either
		// way jevonsd never races another process to a vendor endpoint.
		// Refresh=true is the cockpit-reload path (🎯T653).
		return claudia.LoadPlanUsage(ctx, &claudia.PlanUsageCacheArgs{
			Refresh: refresh,
			All: &claudia.AllPlanUsageArgs{
				Providers:         SupportedProviders(),
				CursorAccessToken: tok,
			},
		})
	}
}

// Refresh performs one round of readings and replaces the cache.
//
// A whole-query failure keeps the previous readings: a stale number the owner
// can see aged is worth more than a blank panel, and the staleness marking is
// what stops it from lying. Only the never-fetched case reports Pending.
func (r *Reader) Refresh(ctx context.Context) error {
	return r.refresh(ctx, false)
}

// RefreshNow forces a producer poll even when the cache is still fresh
// (🎯T653). Concurrent callers wait for the in-flight round.
func (r *Reader) RefreshNow(ctx context.Context) error {
	return r.refresh(ctx, true)
}

func (r *Reader) refresh(ctx context.Context, force bool) error {
	r.mu.Lock()
	if ch := r.refreshing; ch != nil {
		r.mu.Unlock()
		select {
		case <-ch:
			r.mu.Lock()
			err := r.lastKick
			r.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	done := make(chan struct{})
	r.refreshing = done
	r.mu.Unlock()

	err := r.doRefresh(ctx, force)

	r.mu.Lock()
	r.lastKick = err
	r.refreshing = nil
	close(done)
	r.mu.Unlock()
	return err
}

func (r *Reader) doRefresh(parent context.Context, force bool) error {
	ctx, cancel := context.WithTimeout(parent, r.args.FetchTimeout)
	defer cancel()

	fetch := r.args.Fetch
	if force && r.args.ForceFetch != nil {
		fetch = r.args.ForceFetch
	}
	readings, err := fetch(ctx)
	if err == nil {
		// 🎯T666: a Grok 401 is a stale login token; rotate it and ask again.
		refetch := r.args.ForceFetch
		if refetch == nil {
			refetch = r.args.Fetch
		}
		readings = r.refreshGrokTokenIfNeeded(parent, readings, refetch)
	}
	r.mu.Lock()
	if err != nil {
		r.lastErr = err.Error()
		r.mu.Unlock()
		r.emit()
		return err
	}
	r.readings = readings
	r.fetched = true
	r.lastErr = ""
	r.readyOnce.Do(func() { close(r.ready) })
	r.mu.Unlock()
	r.appendHistory(readings)
	r.emit()
	return nil
}

func (r *Reader) appendHistory(readings []claudia.PlanUsage) {
	if r == nil || r.args.History == nil {
		return
	}
	samples := samplesFromReadings(readings, r.args.Now())
	if len(samples) == 0 {
		return
	}
	if err := r.args.History.Append(samples); err != nil {
		slog.Warn("plan usage history append", "err", err)
	}
}

func (r *Reader) emit() {
	if r == nil || r.args.OnUpdate == nil {
		return
	}
	r.args.OnUpdate()
}

// Snapshot re-shapes the cached readings as of now.
func (r *Reader) Snapshot() Snapshot {
	path := strings.TrimSpace(r.args.FixturePath)
	if path == "" {
		path = fixturePath()
	}
	if path != "" {
		if snap, err := LoadSnapshotFile(path); err == nil {
			if snap.At.IsZero() {
				now := time.Now()
				if r.args.Now != nil {
					now = r.args.Now()
				}
				snap.At = now
			}
			return snap
		}
	}
	now := r.args.Now()

	r.mu.Lock()
	readings := append([]claudia.PlanUsage(nil), r.readings...)
	fetched, lastErr := r.fetched, r.lastErr
	r.mu.Unlock()

	var load map[string]int
	if r.args.Load != nil {
		load = r.args.Load()
	}

	snap := Convert(readings, load, now, r.args.StaleAfter)
	if !fetched {
		snap.Pending = true
	}
	// A failed refresh over readings we still hold is not an error the owner
	// needs to see as one — the readings carry their own age. It is only an
	// error while there is nothing behind it.
	if lastErr != "" && !fetched {
		snap.Error = lastErr
	}
	return AttachHistory(snap, r.args.History)
}

// WaitReady blocks until the first successful Refresh completes, or until
// ctx is done. It is the long-poll seam for GET /api/plan-usage: when the
// snapshot is still Pending, the handler waits here instead of returning an
// empty "waiting for the first reading" payload on every short poll.
//
// Already-ready and fixture-backed snapshots return immediately. A whole-
// query failure does not unblock — only a successful fetch does — so a
// caller that times out still sees Pending and can retry.
func (r *Reader) WaitReady(ctx context.Context) error {
	if !r.Snapshot().Pending {
		return nil
	}
	select {
	case <-r.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Run refreshes immediately and then on the poll interval until ctx ends.
func (r *Reader) Run(ctx context.Context) {
	tick := time.NewTicker(r.args.Refresh)
	defer tick.Stop()
	for {
		if err := r.Refresh(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("plan usage refresh failed", "err", err)
		} else if err == nil {
			slog.Debug("plan usage refreshed", "backends", len(r.Snapshot().Backends))
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// CapacityRemaining is the adapter capacity admission consumes (🎯T390
// clause 4): the tightest published remaining fraction across the backends
// the fleet is actually running on, and what produced it.
//
// nil means no running backend published a number. That is the honest answer
// and it must stay distinguishable from 0, which means exhausted — reading
// one as the other is how a blind subsystem came to report 100% headroom
// while the account was already out of allowance (🎯T406).
func (r *Reader) CapacityRemaining() (*float64, string) {
	snap := r.Snapshot()
	f, source, ok := snap.TightestRemaining()
	if !ok {
		return nil, ""
	}
	// A stale reading still binds — an allowance does not refill because we
	// stopped looking — but say so, since the reason string is what the owner
	// reads when background work parks.
	if b, found := snap.Backend(strings.SplitN(source, " ", 2)[0]); found && b.Stale {
		source += " (stale)"
	}
	return &f, source
}
