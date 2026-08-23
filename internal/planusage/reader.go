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

// GrokUsageEnv is claudia's opt-in for the undocumented Grok billing surface.
// The daemon reads it here as well as passing it down, because a detached
// daemon does not always inherit the shell that set it, and an opt-in that
// silently fails to apply looks identical to a backend that publishes nothing.
const GrokUsageEnv = "CLAUDIA_GROK_USAGE"

// CursorUsageEnv is claudia's opt-in for the undocumented Cursor dashboard
// usage surface (CLAUDIA_CURSOR_USAGE). Same detach/inherit rationale as
// GrokUsageEnv.
const CursorUsageEnv = "CLAUDIA_CURSOR_USAGE"

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
	// GrokUnstableUsage opts into claudia's undocumented Grok billing read.
	GrokUnstableUsage bool
	// CursorUnstableUsage opts into claudia's undocumented Cursor usage read.
	CursorUnstableUsage bool
	// FetchTimeout bounds one round of provider calls. Zero uses 20s.
	FetchTimeout time.Duration
	// FixturePath, when set, makes Snapshot read that JSON file instead
	// of the last fetch (isolate journeys, 🎯T390.1.5). Env
	// JEVONS_PLAN_USAGE_FIXTURE is used when this is empty.
	FixturePath string
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

	// ready is closed on the first successful Refresh so GET /api/plan-usage
	// can long-poll until the first batch lands instead of returning pending.
	ready     chan struct{}
	readyOnce sync.Once
}

// NewReader builds a Reader. It does not fetch — call Refresh or Run.
func NewReader(args ReaderArgs) *Reader {
	if args.Fetch == nil {
		grok := args.GrokUnstableUsage || os.Getenv(GrokUsageEnv) == "1"
		cursor := args.CursorUnstableUsage || os.Getenv(CursorUsageEnv) == "1"
		args.Fetch = func(ctx context.Context) ([]claudia.PlanUsage, error) {
			tok := strings.TrimSpace(os.Getenv(CursorAPIKeyEnv))
			if tok == "" {
				if t, err := loadCursorAccessToken(""); err == nil {
					tok = t
				}
			}
			return claudia.QueryAllPlanUsage(ctx, &claudia.AllPlanUsageArgs{
				Providers:           SupportedProviders(),
				GrokUnstableUsage:   grok,
				CursorUnstableUsage: cursor,
				CursorAccessToken:   tok,
			})
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

// Refresh performs one round of readings and replaces the cache.
//
// A whole-query failure keeps the previous readings: a stale number the owner
// can see aged is worth more than a blank panel, and the staleness marking is
// what stops it from lying. Only the never-fetched case reports Pending.
func (r *Reader) Refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.args.FetchTimeout)
	defer cancel()

	readings, err := r.args.Fetch(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.lastErr = err.Error()
		return err
	}
	r.readings = readings
	r.fetched = true
	r.lastErr = ""
	r.readyOnce.Do(func() { close(r.ready) })
	return nil
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
	return snap
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
