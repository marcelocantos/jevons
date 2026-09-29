// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// The refusal the broker returned for jv-t928-mcp-attach on every relaunch
// from 00:09:56 to 00:20:16 on 2026-09-30, verbatim.
var t935SpoolRefusal = errors.New(`broker protocol: agent_failed: session fe8f1413-8636-466c-be11-d98e57124921: ` +
	`existing conversation required but no spool records for seat "jv-t928-mcp-attach" under ` +
	`/Users/marcelo/.jevons/spool — refusing to mint a replacement session`)

// 🎯T935: a resume refused because the seat has no history anywhere is
// reminted on a fresh session, whatever the provider. A resume refused on a
// store that does exist still fails closed.
func TestT935AbsentHistoryRemints(t *testing.T) {
	sidecar := &claudia.AgentDef{Name: "jv-t928-mcp-attach", Provider: "anthropic", Materialized: true}
	if !remintAfterResumeError(sidecar, t935SpoolRefusal) {
		t.Fatal("the incident's no-spool refusal does not remint: the seat stays down forever")
	}
	claude := &claudia.AgentDef{Name: "w", Provider: claudia.ProviderClaude, Materialized: true}
	jsonl := errors.New("session abc: existing conversation required but JSONL not found at /x/abc.jsonl — refusing to mint a replacement session")
	if !remintAfterResumeError(claude, jsonl) {
		t.Fatal("a missing Claude transcript reported by the broker does not remint")
	}

	for _, tc := range []struct {
		def *claudia.AgentDef
		err error
	}{
		{&claudia.AgentDef{Provider: claudia.ProviderGrok}, errors.New("acp session/load s1: boom — existing conversation; refusing to mint a replacement session")},
		{&claudia.AgentDef{Provider: claudia.ProviderCodex}, errors.New("session s1: existing Codex thread required but thread/resume failed: eof — refusing to mint a replacement session")},
		{&claudia.AgentDef{Provider: "anthropic"}, errors.New("omp: keychain was not read at startup")},
		{sidecar, errors.New("broker protocol: grant_held (name=\"x\"): grant x is owned by another connection")},
	} {
		if remintAfterResumeError(tc.def, tc.err) {
			t.Errorf("%s: %q reminted — a store that exists must fail closed", tc.def.Provider, tc.err)
		}
	}
	if HistoryAbsent(nil) {
		t.Fatal("nil error reads as absent history")
	}
}

type t935Reg struct {
	defs      []claudia.AgentDef
	alive     map[string]bool
	relaunch  func(name string) (*LostSession, error)
	relaunchs int
}

func (r *t935Reg) List() []claudia.AgentDef { return r.defs }
func (r *t935Reg) Alive(name string) bool   { return r.alive[name] }
func (r *t935Reg) Adopt(name string) error {
	return fmt.Errorf("broker protocol: agent_failed: no live tmux window for session: %s", name)
}
func (r *t935Reg) Relaunch(name string) (*LostSession, error) {
	r.relaunchs++
	fresh, err := r.relaunch(name)
	if err == nil {
		r.alive[name] = true
	}
	return fresh, err
}

func t935Cleanup(t *testing.T, names ...string) {
	t.Cleanup(func() {
		for _, n := range names {
			brokerReattachTried.Delete(n)
			brokerLostMu.Lock()
			delete(brokerLostOutages, n)
			brokerLostMu.Unlock()
		}
	})
}

// 🎯T935: a seat lost to a broker restart that cannot be relaunched is
// reported once, within brokerLostFlagAfter, naming the refusal and its
// parent — not retried in silence. After the report the loop keeps trying,
// at the slow cooldown, and a seat that comes back ends the outage; a later
// loss is a new outage with its own bound.
func TestT935SeatStillDownIsFlaggedOnceWithinTheBound(t *testing.T) {
	const name = "jv-t935-stuck"
	t935Cleanup(t, name)
	reg := &t935Reg{
		defs:  []claudia.AgentDef{{Name: name, Parent: "jevons-po", Purpose: claudia.PurposeWork}},
		alive: map[string]bool{},
		relaunch: func(string) (*LostSession, error) {
			return nil, errors.New("broker protocol: agent_failed: context deadline exceeded")
		},
	}
	lost := func(n string) bool { return n == name }
	start := time.Date(2026, 9, 30, 0, 9, 56, 0, time.UTC)

	// The product loop ticks every 30s or so; drive it at 10s so the retry
	// cadence is what spaces the attempts, not the tick.
	var stuck []BrokerLostStuck
	var flaggedAt time.Time
	now := start
	for ; now.Before(start.Add(20 * time.Minute)); now = now.Add(10 * time.Second) {
		res := reattachWith(reg, fleetintent.Snapshot{}, now, lost)
		if len(res.Back) != 0 || len(res.Fresh) != 0 {
			t.Fatalf("a seat whose relaunch fails came back: %+v", res)
		}
		if len(res.Stuck) > 0 && flaggedAt.IsZero() {
			flaggedAt = now
		}
		stuck = append(stuck, res.Stuck...)
	}
	if len(stuck) != 1 {
		t.Fatalf("stuck reports = %d, want exactly one per outage: %+v", len(stuck), stuck)
	}
	st := stuck[0]
	if d := flaggedAt.Sub(start); d < brokerLostFlagAfter || d > brokerLostFlagAfter+brokerLostRetry {
		t.Fatalf("flagged %s after the first failure, want within [%s, %s]", d, brokerLostFlagAfter, brokerLostFlagAfter+brokerLostRetry)
	}
	if st.Name != name || st.Parent != "jevons-po" || !st.Since.Equal(start) || st.Attempts < 10 || st.Err == "" {
		t.Fatalf("stuck report = %+v", st)
	}
	// Before the flag: every 20s. After it: every 2m, not forever at 20s.
	fast := int(brokerLostFlagAfter/brokerLostRetry) + 1
	slow := int((20*time.Minute - brokerLostFlagAfter) / brokerReattachCooldown)
	if reg.relaunchs > fast+slow+1 {
		t.Fatalf("relaunch attempts = %d, want at most %d: the loop did not back off after flagging", reg.relaunchs, fast+slow+1)
	}
	if reg.relaunchs <= fast {
		t.Fatalf("relaunch attempts = %d: the loop stopped trying after flagging", reg.relaunchs)
	}

	// The broker recovers: the seat comes back and the outage ends.
	reg.relaunch = func(string) (*LostSession, error) { return nil, nil }
	now = now.Add(brokerReattachCooldown)
	if res := reattachWith(reg, fleetintent.Snapshot{}, now, lost); len(res.Back) != 1 {
		t.Fatalf("recovered broker did not bring the seat back: %+v", res)
	}
	// Lost again later: a new outage, flagged on its own clock, not at once.
	reg.alive[name] = false
	reg.relaunch = func(string) (*LostSession, error) { return nil, errors.New("still down") }
	again := now.Add(time.Hour)
	if res := reattachWith(reg, fleetintent.Snapshot{}, again, lost); len(res.Stuck) != 0 {
		t.Fatalf("a new outage was flagged on the old outage's clock: %+v", res.Stuck)
	}
}

// 🎯T935: a seat relaunched on a fresh session is reported as such, so its
// parent re-sends the brief the new session does not have.
func TestT935FreshRelaunchIsReported(t *testing.T) {
	const name = "jv-t935-fresh"
	t935Cleanup(t, name)
	reg := &t935Reg{
		defs:  []claudia.AgentDef{{Name: name, Parent: "jevons-po", Purpose: claudia.PurposeWork}},
		alive: map[string]bool{},
		relaunch: func(n string) (*LostSession, error) {
			return &LostSession{Name: n, Parent: "jevons-po", OldSession: "old", NewSession: "new"}, nil
		},
	}
	res := reattachWith(reg, fleetintent.Snapshot{}, time.Now(), func(n string) bool { return n == name })
	if len(res.Back) != 1 || len(res.Fresh) != 1 || res.Fresh[0].NewSession != "new" || res.Fresh[0].Parent != "jevons-po" {
		t.Fatalf("fresh relaunch not reported: %+v", res)
	}
}

// A seat stopped on purpose mid-outage ends the outage: nothing flags it.
func TestT935DeliberateStopEndsTheOutage(t *testing.T) {
	const name = "jv-t935-stopped"
	t935Cleanup(t, name)
	reg := &t935Reg{
		defs:     []claudia.AgentDef{{Name: name, Purpose: claudia.PurposeWork}},
		alive:    map[string]bool{},
		relaunch: func(string) (*LostSession, error) { return nil, errors.New("down") },
	}
	isLost := true
	lost := func(n string) bool { return isLost && n == name }
	start := time.Date(2026, 9, 30, 1, 0, 0, 0, time.UTC)
	reattachWith(reg, fleetintent.Snapshot{}, start, lost)
	isLost = false // the owner stopped it: the stop record is no longer the broker's
	reattachWith(reg, fleetintent.Snapshot{}, start.Add(time.Minute), lost)
	isLost = true
	if res := reattachWith(reg, fleetintent.Snapshot{}, start.Add(brokerLostFlagAfter+time.Minute), lost); len(res.Stuck) != 0 {
		t.Fatalf("an outage that ended was flagged: %+v", res.Stuck)
	}
}
