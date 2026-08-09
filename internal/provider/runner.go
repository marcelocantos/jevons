// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
)

// runner supervises exactly one provider for its whole life: spawn or
// dial, watch, restart with backoff, and reap on teardown. One runner
// owns one goroutine, so a wedged provider cannot stall the reconciler
// or its siblings.
type runner struct {
	l    *Lifecycle
	id   string
	decl config.ProviderDecl
	spec string

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu   sync.Mutex
	h    Health
	proc Process // launch mode: the process currently being supervised
}

// stopTimeout bounds how long teardown waits for one provider to die
// before the supervisor stops caring and moves on.
const stopTimeout = StopGrace + 2*time.Second

func newRunner(l *Lifecycle, d config.ProviderDecl) *runner {
	ctx, cancel := context.WithCancel(l.ctx)
	return &runner{
		l:      l,
		id:     d.ID,
		decl:   d,
		spec:   specOf(d),
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		h: Health{
			ID:        d.ID,
			Transport: d.Transport,
			Phase:     PhaseStarting,
			Since:     time.Now(),
		},
	}
}

func (r *runner) run() {
	defer close(r.done)
	switch r.decl.Transport {
	case config.ProviderTransportLaunch:
		r.runLaunch()
	case config.ProviderTransportConnect:
		r.runConnect()
	default:
		// Config validation rejects this, so reaching it means a
		// declaration bypassed ValidateProviders — fail loudly, visibly.
		r.setPhase(PhaseFailed, errors.New("unsupported transport "+r.decl.Transport))
	}
}

// runLaunch spawns the provider and restarts it on every crash, backing
// off between attempts. It exits only on teardown or an unrunnable
// declaration — nothing a restart could fix.
func (r *runner) runLaunch() {
	attempt := 0
	for {
		r.setPhase(PhaseStarting, nil)
		proc, err := r.l.launcher.Start(r.ctx, r.decl)
		if err != nil {
			if errors.Is(err, ErrUnrunnable) {
				r.setPhase(PhaseFailed, err)
				return
			}
			attempt++
			if !r.backoffWait(attempt, err) {
				return
			}
			continue
		}

		r.mu.Lock()
		r.proc = proc
		r.mu.Unlock()
		// A teardown that landed during Start would otherwise be missed:
		// the runner never sees ctx.Done() again until Wait returns.
		if r.ctx.Err() != nil {
			r.stopProc(proc)
			r.setPhase(PhaseStopped, nil)
			return
		}

		r.setRunning(proc.PID(), "")
		r.l.log.Info("provider running", "provider", r.id, "transport", r.decl.Transport, "pid", proc.PID())

		waitErr := proc.Wait()

		r.mu.Lock()
		r.proc = nil
		r.mu.Unlock()

		if r.ctx.Err() != nil {
			r.setPhase(PhaseStopped, nil)
			return
		}
		attempt++
		r.l.log.Warn("provider exited — restarting", "provider", r.id, "attempt", attempt, "err", waitErr)
		if !r.backoffWait(attempt, waitErr) {
			return
		}
	}
}

// runConnect attaches to an already-running provider and polls it. The
// provider's own lifecycle is not ours: a failed probe means "not
// reachable right now", so the runner keeps retrying on backoff rather
// than giving up or trying to start anything.
func (r *runner) runConnect() {
	r.setPhase(PhaseStarting, nil)
	conn, err := r.l.connector.Connect(r.ctx, r.decl)
	if err != nil {
		r.setPhase(PhaseFailed, err)
		return
	}
	defer conn.Close()

	attempt := 0
	for {
		probeErr := conn.Probe(r.ctx)
		if r.ctx.Err() != nil {
			r.setPhase(PhaseStopped, nil)
			return
		}
		var wait time.Duration
		if probeErr == nil {
			if attempt > 0 {
				r.l.log.Info("provider reachable again", "provider", r.id, "endpoint", conn.Endpoint())
			}
			attempt = 0
			r.setRunning(0, conn.Endpoint())
			wait = r.l.probeInterval
		} else {
			attempt++
			r.setPhase(PhaseBackoff, probeErr)
			r.l.log.Warn("provider unreachable", "provider", r.id, "endpoint", conn.Endpoint(), "attempt", attempt, "err", probeErr)
			wait = r.l.backoff.Delay(attempt)
		}
		if !r.sleep(wait) {
			r.setPhase(PhaseStopped, nil)
			return
		}
	}
}

// backoffWait parks in PhaseBackoff for the attempt's delay. It reports
// false when teardown interrupted the wait, having already recorded the
// stopped phase.
func (r *runner) backoffWait(attempt int, cause error) bool {
	r.setPhase(PhaseBackoff, cause)
	if !r.sleep(r.l.backoff.Delay(attempt)) {
		r.setPhase(PhaseStopped, nil)
		return false
	}
	return true
}

// sleep waits d, reporting false if teardown arrived first.
func (r *runner) sleep(d time.Duration) bool {
	if d <= 0 {
		return r.ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-r.ctx.Done():
		return false
	}
}

// stop tears the provider down and waits for the runner goroutine to
// finish, so the caller knows the process is reaped when stop returns.
func (r *runner) stop() {
	r.cancel()

	r.mu.Lock()
	proc := r.proc
	r.mu.Unlock()
	if proc != nil {
		r.stopProc(proc)
	}

	select {
	case <-r.done:
	case <-time.After(stopTimeout):
		r.l.log.Error("provider teardown timed out", "provider", r.id, "timeout", stopTimeout)
	}
}

func (r *runner) stopProc(proc Process) {
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	if err := proc.Stop(ctx); err != nil {
		r.l.log.Warn("provider stop failed", "provider", r.id, "err", err)
	}
}

// setPhase records a transition and notifies the observer. The observer
// runs outside the runner lock so a slow hook cannot wedge supervision.
func (r *runner) setPhase(p Phase, cause error) {
	r.mu.Lock()
	if p == PhaseBackoff {
		r.h.Restarts++
	}
	r.h.Phase = p
	r.h.Since = time.Now()
	if p != PhaseRunning {
		r.h.PID = 0
	}
	if cause != nil {
		r.h.LastError = cause.Error()
	}
	snap := r.h
	r.mu.Unlock()
	r.notify(snap)
}

// setRunning marks the provider healthy. LastError deliberately survives:
// it is the last failure this provider suffered, not a claim about the
// present. A crash-looping provider is briefly Running between deaths, and
// clearing the cause there would hide the very thing the owner needs to
// see. Current health is Phase; Restarts says how rocky the ride was.
func (r *runner) setRunning(pid int, endpoint string) {
	r.mu.Lock()
	r.h.Phase = PhaseRunning
	r.h.Since = time.Now()
	r.h.PID = pid
	r.h.Endpoint = endpoint
	snap := r.h
	r.mu.Unlock()
	r.notify(snap)
}

func (r *runner) notify(snap Health) {
	if r.l.onPhase != nil {
		r.l.onPhase(snap)
	}
}

func (r *runner) health() Health {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.h
}
