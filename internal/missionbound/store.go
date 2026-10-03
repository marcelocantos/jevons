// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package missionbound bounds target start churn independently of provider quotas.
package missionbound

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultMaxStarts cuts the 2026-10-03 incident short: among ~219 workers,
// T766.2 had 18 starts (~6.2 sigma), the only seat with >=15 starts.
// This is a target-wide rolling count, so renaming a worker cannot reset it.
const DefaultMaxStarts = 15

type Policy struct {
	MaxStarts   int `json:"max_starts"`
	WindowHours int `json:"window_hours"`
}

func DefaultPolicy() Policy { return Policy{DefaultMaxStarts, 24} }
func (p Policy) Validate() error {
	if p.MaxStarts < 1 || p.WindowHours < 1 {
		return fmt.Errorf("mission protection requires positive max_starts and window_hours")
	}
	return nil
}

type Start struct {
	ID             string    `json:"id"`
	At             time.Time `json:"at"`
	Scope          string    `json:"scope"`
	Target         string    `json:"target"`
	Seat           string    `json:"seat"`
	Actor          string    `json:"actor,omitempty"`
	OverrideReason string    `json:"override_reason,omitempty"`
}
type state struct {
	Version  int                  `json:"version"`
	Starts   []Start              `json:"starts"`
	Notified map[string]time.Time `json:"notified"`
}
type Store struct {
	mu     sync.Mutex
	path   string
	policy Policy
	state  state
}

// Open fails on malformed state. The caller seeds history only on the first
// open; subsequent restarts retain reservations and notification claims.
func Open(path string, policy Policy, history io.Reader, scope func(string) string, now time.Time) (*Store, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	s := &Store{path: path, policy: policy, state: state{Version: 1, Notified: map[string]time.Time{}}}
	b, err := os.ReadFile(path)
	if err == nil {
		var loaded state
		if err = json.Unmarshal(b, &loaded); err != nil {
			return nil, fmt.Errorf("mission protection state: %w", err)
		}
		s.state = loaded
		if s.state.Version != 1 || s.state.Notified == nil {
			return nil, fmt.Errorf("invalid mission protection state")
		}
		for _, x := range s.state.Starts {
			if x.ID == "" || x.At.IsZero() || x.Scope == "" || x.Target == "" {
				return nil, fmt.Errorf("invalid mission start record")
			}
		}
		return s, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if history != nil {
		if err = s.seed(history, scope, now); err != nil {
			return nil, err
		}
	}
	if err = s.save(s.state); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Store) seed(r io.Reader, scope func(string) string, now time.Time) error {
	br := bufio.NewReader(r)
	for {
		b, err := br.ReadBytes('\n')
		if err == io.EOF {
			return nil
		} // the writer's last partial record is not committed
		if err != nil {
			return err
		}
		var e struct {
			TS                  time.Time `json:"ts"`
			Component, Decision string
			Fields              map[string]any
		}
		if err = json.Unmarshal(b, &e); err != nil {
			return fmt.Errorf("lifecycle seed: %w", err)
		}
		str := func(k string) string { v, _ := e.Fields[k].(string); return v }
		if e.Component != "agent_lifecycle" || e.Decision != "start" || str("outcome") != "ok" || str("target_id") == "" {
			continue
		}
		if !Worker(str("name"), str("purpose"), str("role")) {
			continue
		}
		if e.TS.IsZero() {
			return fmt.Errorf("lifecycle start lacks timestamp")
		}
		if e.TS.Before(now.Add(-time.Duration(s.policy.WindowHours) * time.Hour)) {
			continue
		}
		wd := str("workdir")
		if wd == "" {
			continue
		}
		if scope != nil {
			wd = scope(wd)
		}
		s.state.Starts = append(s.state.Starts, Start{ID: "history-" + rand.Text(), At: e.TS, Scope: wd, Target: str("target_id"), Seat: str("name"), Actor: str("actor")})
	}
}
func Worker(name, purpose, role string) bool {
	return name != "" && name != "jevons" && name != "po" && !strings.HasSuffix(name, "-po") && (purpose == "" || purpose == "work") && role != "overseer" && role != "product-owner"
}
func key(scope, target string) string { return scope + "\x00" + target }
func (s *Store) prune(now time.Time) state {
	out := state{Version: 1, Notified: map[string]time.Time{}}
	cutoff := now.Add(-time.Duration(s.policy.WindowHours) * time.Hour)
	for _, x := range s.state.Starts {
		if x.At.After(cutoff) {
			out.Starts = append(out.Starts, x)
		}
	}
	for k, t := range s.state.Notified {
		if t.After(cutoff) {
			out.Notified[k] = t
		}
	}
	return out
}

type Notice struct {
	Seat, Target, Scope            string
	Count, Limit, Rank, Population int
	Sigma                          float64
	WindowHours                    int
}

func (n Notice) String() string {
	return fmt.Sprintf("[mission-blowout T998] seat=%s target=%s metric=target_starts count=%d bound=%d window=%dh rank=%d/%d sigma=%.2f. Keep one seat on the remaining mission; force_engage and T753 reopen do not reset this bound. An owner/overseer may authorize one further start with remint_override and override_reason.", n.Seat, n.Target, n.Count, n.Limit, n.WindowHours, n.Rank, n.Population, n.Sigma)
}
func notice(st state, x Start, p Policy) *Notice {
	counts := map[string]int{}
	for _, v := range st.Starts {
		counts[key(v.Scope, v.Target)]++
	}
	n := &Notice{Seat: x.Seat, Target: x.Target, Scope: x.Scope, Count: counts[key(x.Scope, x.Target)], Limit: p.MaxStarts, Population: len(counts), Rank: 1, WindowHours: p.WindowHours}
	var mean, variance float64
	for _, v := range counts {
		mean += float64(v)
		if v > n.Count {
			n.Rank++
		}
	}
	if len(counts) > 0 {
		mean /= float64(len(counts))
		for _, v := range counts {
			variance += math.Pow(float64(v)-mean, 2)
		}
		variance /= float64(len(counts))
		if variance > 0 {
			n.Sigma = (float64(n.Count) - mean) / math.Sqrt(variance)
		}
	}
	return n
}

// Reserve atomically counts a launch before it can race another admission.
// Cancellation on a definite launch failure releases it; a daemon crash keeps
// the reservation conservatively. Override is a one-start, attributed intent.
// authorized is determined by the caller, never inferred from force_engage.
func (s *Store) Reserve(x Start, override, authorized bool, now time.Time) (string, *Notice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x.Scope == "" || x.Target == "" || x.Seat == "" {
		return "", nil, fmt.Errorf("mission start requires scope, target and seat")
	}
	if override && (!authorized || strings.TrimSpace(x.OverrideReason) == "") {
		return "", nil, fmt.Errorf("remint override requires owner/overseer actor and a reason")
	}
	st := s.prune(now)
	n := notice(st, x, s.policy)
	if n.Count >= s.policy.MaxStarts && !override {
		var alert *Notice
		k := key(x.Scope, x.Target)
		if _, sent := st.Notified[k]; !sent {
			st.Notified[k] = now
			alert = n
		}
		if err := s.save(st); err != nil {
			return "", nil, err
		}
		s.state = st
		return "", alert, fmt.Errorf("mission start bound: target %s has %d starts in %dh (limit %d); force_engage cannot override", x.Target, n.Count, s.policy.WindowHours, s.policy.MaxStarts)
	}
	x.ID = rand.Text()
	x.At = now
	if !override {
		x.OverrideReason = ""
	}
	st.Starts = append(st.Starts, x)
	if err := s.save(st); err != nil {
		return "", nil, err
	}
	s.state = st
	return x.ID, nil, nil
}
func (s *Store) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	st.Starts = append([]Start(nil), s.state.Starts...)
	for i, x := range st.Starts {
		if x.ID == id {
			st.Starts = append(st.Starts[:i], st.Starts[i+1:]...)
			break
		}
	}
	if err := s.save(st); err != nil {
		return err
	}
	s.state = st
	return nil
}
func (s *Store) save(st state) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".mission-starts-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
