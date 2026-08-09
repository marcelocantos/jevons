// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// Lifecycle is the provider registry's supervisor half (🎯T27.3): it
// holds the live set of provider runners and reconciles it against the
// desired set from structured config (🎯T27.2).
//
// Reconcile is the whole contract:
//
//	enabled + not live      → start (launch) or attach (connect)
//	disabled / removed      → tear down and reap
//	live but spec changed   → tear down, start again on the new spec
//	live and spec unchanged → left alone (no restart churn on reload)
//
// It is edge-free: Reconcile compares desired against live every time, so
// calling it on a timer, on config reload, or twice in a row all converge
// to the same state. Wire it to ConfigManager.SetOnChange and it tracks
// config.yaml without a daemon restart.
type Lifecycle struct {
	mu      sync.Mutex
	runners map[string]*runner
	closed  bool

	launcher      Launcher
	connector     Connector
	backoff       Backoff
	probeInterval time.Duration
	log           *slog.Logger
	onPhase       func(Health)

	// ctx bounds every runner; cancel is the shutdown signal.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// LifecycleArgs configures a Lifecycle. Every field is optional; the
// zero value yields the production supervisor (real processes, real
// HTTP probes, DefaultBackoff).
type LifecycleArgs struct {
	// Launcher starts launch-mode providers. Nil uses ExecLauncher.
	Launcher Launcher
	// Connector attaches connect-mode providers. Nil uses HTTPConnector.
	Connector Connector
	// Backoff is the restart delay policy. Zero fields take DefaultBackoff.
	Backoff Backoff
	// ProbeInterval is the connect-mode health poll period. Zero uses
	// DefaultProbeInterval.
	ProbeInterval time.Duration
	// Logger receives lifecycle transitions. Nil uses slog.Default.
	Logger *slog.Logger
	// OnPhase observes every phase transition (health snapshot after the
	// change). Nil is fine; it must not block or call back into Lifecycle.
	OnPhase func(Health)
}

// NewLifecycle returns a supervisor with no providers running. Call
// Reconcile to bring the desired set up, and Shutdown to reap it.
func NewLifecycle(args LifecycleArgs) *Lifecycle {
	ctx, cancel := context.WithCancel(context.Background())
	l := &Lifecycle{
		runners:       make(map[string]*runner),
		launcher:      args.Launcher,
		connector:     args.Connector,
		backoff:       args.Backoff.withDefaults(),
		probeInterval: args.ProbeInterval,
		log:           args.Logger,
		onPhase:       args.OnPhase,
		ctx:           ctx,
		cancel:        cancel,
	}
	if l.launcher == nil {
		l.launcher = &ExecLauncher{}
	}
	if l.connector == nil {
		l.connector = &HTTPConnector{}
	}
	if l.probeInterval <= 0 {
		l.probeInterval = DefaultProbeInterval
	}
	if l.log == nil {
		l.log = slog.Default()
	}
	return l
}

// Reconcile converges the live set on the desired declarations. Disabled
// and absent providers are torn down; enabled ones are started or left
// running. It blocks until teardowns are reaped so a caller that
// reconciles then inspects Health sees a settled picture.
//
// Errors from individual providers are logged and surfaced as PhaseFailed
// health rather than aborting the sweep: one unrunnable declaration must
// not stop the rest of the fleet from converging.
func (l *Lifecycle) Reconcile(decls []config.ProviderDecl) error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return errors.New("provider lifecycle: already shut down")
	}

	desired := make(map[string]config.ProviderDecl, len(decls))
	for _, d := range decls {
		if d.Enabled() {
			desired[d.ID] = d
		}
	}

	var stopping []*runner
	for id, r := range l.runners {
		d, want := desired[id]
		if !want {
			stopping = append(stopping, r)
			delete(l.runners, id)
			continue
		}
		if r.spec != specOf(d) {
			// Same id, different declaration — restart on the new spec.
			stopping = append(stopping, r)
			delete(l.runners, id)
			continue
		}
		// Unchanged and already supervised: leave it alone.
		delete(desired, id)
	}

	var starting []config.ProviderDecl
	for _, d := range decls {
		if _, want := desired[d.ID]; want {
			starting = append(starting, d)
		}
	}
	l.mu.Unlock()

	for _, r := range stopping {
		l.log.Info("provider stopping", "provider", r.id, "reason", "disabled or respecified")
		r.stop()
	}

	// Start after teardown so a respecified provider does not race its
	// predecessor for the same port or socket.
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return errors.New("provider lifecycle: already shut down")
	}
	for _, d := range starting {
		r := newRunner(l, d)
		l.runners[d.ID] = r
		l.wg.Go(r.run)
		l.log.Info("provider starting", "provider", d.ID, "transport", d.Transport)
	}
	l.mu.Unlock()
	return nil
}

// BindConfig makes this Lifecycle track structured config (🎯T27.2): the
// current desired set is reconciled now, and every later ConfigManager
// Reload reconciles again. This is the whole daemon wiring — one call at
// boot and provider config becomes live-editable without a restart.
//
// A reconcile failure after a reload is logged, not propagated: config
// reload must not be able to kill the daemon, and the next reload (or a
// manual Reconcile) retries from the same declarative desired state.
func (l *Lifecycle) BindConfig(m *ConfigManager) error {
	if m == nil {
		return errors.New("provider lifecycle: BindConfig needs a ConfigManager")
	}
	m.SetOnChange(func(decls []config.ProviderDecl) {
		if err := l.Reconcile(decls); err != nil {
			l.log.Error("provider reconcile after config reload failed", "err", err)
		}
	})
	return l.Reconcile(m.Desired())
}

// Health returns a snapshot of every supervised provider, sorted by id
// so the owner-visible surface is stable between polls.
func (l *Lifecycle) Health() []Health {
	l.mu.Lock()
	rs := make([]*runner, 0, len(l.runners))
	for _, r := range l.runners {
		rs = append(rs, r)
	}
	l.mu.Unlock()

	out := make([]Health, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.health())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Shutdown tears down every provider and waits for the runners to exit.
// After Shutdown the Lifecycle refuses further Reconcile calls.
func (l *Lifecycle) Shutdown(ctx context.Context) error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	rs := make([]*runner, 0, len(l.runners))
	for _, r := range l.runners {
		rs = append(rs, r)
	}
	l.runners = make(map[string]*runner)
	l.mu.Unlock()

	l.cancel()
	for _, r := range rs {
		r.stop()
	}

	done := make(chan struct{})
	go func() {
		l.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("provider lifecycle: shutdown timed out: %w", ctx.Err())
	}
}

// specOf fingerprints the parts of a declaration that require a restart
// when they change. Enable is excluded: flipping it is handled by the
// desired-set membership test, not by a restart.
func specOf(d config.ProviderDecl) string {
	params := d.Params
	if params == nil {
		params = map[string]any{}
	}
	// json.Marshal sorts map keys, so the fingerprint is order-stable.
	raw, err := json.Marshal(params)
	if err != nil {
		// Unmarshalable params cannot be compared; force a restart by
		// making the fingerprint unique to this reconcile pass.
		raw = fmt.Appendf(nil, "unfingerprintable:%d", time.Now().UnixNano())
	}
	return d.Transport + "\x00" + string(raw)
}
