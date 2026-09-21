// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package spawnorder records a spawn order as the list of seats it named, so
// a seat that was ordered and never minted is visible (🎯T762).
//
// On 2026-09-21 two consecutive orders to jevons-po each named claude seats
// and grok seats. Both times the claude half minted and the grok half never
// reached jevons_agent_start at all: the eventlog holds no start attempt, not
// even a refused one, for T759 / T760 / T755 / T743 in either window. A start
// the daemon never sees cannot be logged as refused, so recording refusals is
// not enough. The only record that can name the dropped half is one that
// holds what was ordered, independently of what was attempted.
//
// So an order is declared (by whoever issues it) as its named seats, and each
// seat is reconciled against the start attempts the daemon journals. Every
// named seat gets one of: minted, rerouted (minted, but on a provider the
// order did not name), refused (a start was attempted and declined — the
// reason is the journalled error), not_attempted (the journal covers the
// order's window and holds no start for it) or unknown (the evidence cannot
// decide: the journal was unreadable or does not reach back far enough, or
// the only seat under that name is an older incarnation). Absence of evidence
// is never reported as evidence of absence. An order is complete only when
// every seat is minted or rerouted, and [Result.Line] names the rest, so a
// cold reader of the panel can tell a finished order from a half-finished one
// without holding the order text.
//
// An outcome, once observed, is written back to the order ([Store.Record]),
// so it outlives the journal tail it was read from.
package spawnorder

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
)

// FileName is the store's file under the daemon state dir.
const FileName = "spawn-orders.json"

// MaxOrders bounds the store. Closed orders are evicted oldest first; when
// every stored order is still open, a new declaration is refused rather than
// dropping an unresolved obligation.
const MaxOrders = 200

// DeclareSkew is how far before an order's IssuedAt a start attempt still
// counts for it. The issuer typically declares right after sending, and a
// fast PO can mint before the declaration lands.
const DeclareSkew = 10 * time.Minute

// Seat is one seat an order names.
type Seat struct {
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
	Target   string `json:"target,omitempty"`
	// Observed is the latest start outcome seen for this seat inside the
	// order's window, persisted so it survives the journal tail.
	Observed *Observation `json:"observed,omitempty"`
}

// Observation is one start outcome attributed to a seat of an order.
type Observation struct {
	At       time.Time `json:"at"`
	OK       bool      `json:"ok"`
	Err      string    `json:"err,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Target   string    `json:"target,omitempty"`
	Session  string    `json:"session,omitempty"`
}

// Order is one declared spawn order.
type Order struct {
	ID       string    `json:"id"`
	Parent   string    `json:"parent"`
	By       string    `json:"by,omitempty"`
	Note     string    `json:"note,omitempty"`
	IssuedAt time.Time `json:"issued_at"`
	Seats    []Seat    `json:"seats"`
	// Closed marks an order the issuer has finished with (the dropped seats
	// were re-ordered, or deliberately abandoned). A closed order no longer
	// decorates the panel, but its reconciliation is still readable.
	Closed     bool      `json:"closed,omitempty"`
	ClosedAt   time.Time `json:"closed_at,omitzero"`
	ClosedNote string    `json:"closed_note,omitempty"`
}

// windowStart is the earliest start attempt that counts for the order.
func (o Order) windowStart() time.Time {
	return o.IssuedAt.Add(-DeclareSkew)
}

// Status is a named seat's reconciled outcome.
type Status string

const (
	Minted       Status = "minted"
	Rerouted     Status = "rerouted"
	Refused      Status = "refused"
	NotAttempted Status = "not_attempted"
	Unknown      Status = "unknown"
)

// Attempt is one journalled jevons_agent_start outcome.
type Attempt struct {
	Name     string
	At       time.Time
	OK       bool
	Err      string
	Provider string
	Target   string
	Session  string
}

// Incarnation is the registry's current seat under a name.
type Incarnation struct {
	Provider string
	Target   string
	Session  string
}

// Evidence is everything a reconciliation reads.
type Evidence struct {
	// Attempts are journalled start outcomes, any order.
	Attempts []Attempt
	// ReadErr is set when the journal could not be read at all; every seat
	// without a persisted observation is then unknown.
	ReadErr string
	// CoveredSince is the oldest instant the attempts are complete from:
	// zero means the whole journal was read. A seat whose order window
	// starts before it has no evidence of absence.
	CoveredSince time.Time
	// Present is the registry's seats by name.
	Present map[string]Incarnation
}

// SeatResult is one named seat and what became of it.
type SeatResult struct {
	Seat
	Status Status `json:"status"`
	// Reason says why: the start error for refused, the provider actually
	// used for rerouted, what the evidence lacked for unknown, and for
	// not_attempted that no start was ever made.
	Reason string `json:"reason,omitempty"`
}

// Result is an order reconciled against the fleet.
type Result struct {
	Order Order        `json:"order"`
	Seats []SeatResult `json:"seats"`
}

// Complete reports whether every named seat was minted (on any provider).
func (r Result) Complete() bool {
	for _, s := range r.Seats {
		if s.Status != Minted && s.Status != Rerouted {
			return false
		}
	}
	return true
}

// Minted counts the seats that exist, rerouted included.
func (r Result) Minted() int {
	n := 0
	for _, s := range r.Seats {
		if s.Status == Minted || s.Status == Rerouted {
			n++
		}
	}
	return n
}

// Line is the one-line panel reading of the order: how many named seats were
// minted, and every seat that was not (or went to another provider), by name
// and reason.
func (r Result) Line() string {
	var b strings.Builder
	fmt.Fprintf(&b, "order %s: %d/%d minted", r.Order.ID, r.Minted(), len(r.Seats))
	if r.Complete() {
		b.WriteString(" (complete)")
	} else {
		b.WriteString(" (INCOMPLETE)")
	}
	var notes []string
	for _, s := range r.Seats {
		if s.Status == Minted {
			continue
		}
		prov := s.Provider
		if prov == "" {
			prov = "any provider"
		}
		notes = append(notes, fmt.Sprintf("%s (%s) %s: %s", s.Name, prov, s.Status, s.Reason))
	}
	if len(notes) > 0 {
		b.WriteString("; ")
		b.WriteString(strings.Join(notes, "; "))
	}
	return b.String()
}

// Reconcile decides each named seat's outcome from ev and the observations
// already persisted on the order.
//
// An attempt counts for a seat when its name matches, it falls inside the
// order's window, and — when both carry one — its target matches the seat's:
// a same-name start on another mission is another incarnation, not this
// order's seat. The latest counting outcome (journal or persisted) decides:
// OK is minted or rerouted, an error is refused. With no counting outcome the
// seat is not_attempted only when the journal was read and covers the whole
// window and no seat of that name exists; otherwise it is unknown, naming
// which evidence was missing.
func Reconcile(o Order, ev Evidence) Result {
	from := o.windowStart()
	res := Result{Order: o}
	for _, seat := range o.Seats {
		sr := SeatResult{Seat: seat}
		obs := seat.Observed
		for _, a := range ev.Attempts {
			if a.Name != seat.Name || a.At.Before(from) || !sameTarget(seat.Target, a.Target) {
				continue
			}
			if obs == nil || a.At.After(obs.At) {
				obs = &Observation{At: a.At, OK: a.OK, Err: a.Err, Provider: a.Provider, Target: a.Target, Session: a.Session}
			}
		}
		sr.Observed = obs
		inc, live := ev.Present[seat.Name]
		switch {
		case obs != nil && obs.OK:
			sr.Status, sr.Reason = mintedOn(seat, obs.Provider)
		case obs != nil:
			sr.Status = Refused
			sr.Reason = strings.TrimSpace(obs.Err)
			if sr.Reason == "" {
				sr.Reason = "start returned an error with no reason"
			}
		case ev.ReadErr != "":
			sr.Status = Unknown
			sr.Reason = "start journal unreadable: " + ev.ReadErr
		case !ev.CoveredSince.IsZero() && ev.CoveredSince.After(from):
			sr.Status = Unknown
			sr.Reason = "start journal read reaches back only to " + ev.CoveredSince.UTC().Format(time.RFC3339) + ", after this order's window opened"
		case live:
			sr.Status = Unknown
			sr.Reason = fmt.Sprintf("a seat of this name exists (target %q, session %q) but no start for it fell in this order's window: an earlier incarnation is not evidence for this order", inc.Target, inc.Session)
		default:
			sr.Status = NotAttempted
			sr.Reason = "no jevons_agent_start was made for this seat"
		}
		res.Seats = append(res.Seats, sr)
	}
	return res
}

func mintedOn(seat Seat, actual string) (Status, string) {
	want := normProvider(seat.Provider)
	got := normProvider(actual)
	if want != "" && got != "" && want != got {
		return Rerouted, "minted on " + got + ", ordered " + want
	}
	return Minted, ""
}

func normProvider(p string) string {
	return strings.ToLower(strings.TrimSpace(p))
}

func normTarget(t string) string {
	return strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(t)), "🎯")
}

// sameTarget is true unless both sides name a target and they differ.
func sameTarget(a, b string) bool {
	a, b = normTarget(a), normTarget(b)
	return a == "" || b == "" || a == b
}

// AttemptsFromEvents projects journalled agent_lifecycle.start events.
func AttemptsFromEvents(events []eventlog.Event) []Attempt {
	var out []Attempt
	for _, ev := range events {
		if ev.Component != "agent_lifecycle" || ev.Decision != "start" {
			continue
		}
		name, _ := ev.Fields["name"].(string)
		if name == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, ev.TS)
		if err != nil {
			continue
		}
		outcome, _ := ev.Fields["outcome"].(string)
		errText, _ := ev.Fields["err"].(string)
		prov, _ := ev.Fields["provider"].(string)
		if prov == "" {
			prov, _ = ev.Fields["dest"].(string)
		}
		target, _ := ev.Fields["target_id"].(string)
		session, _ := ev.Fields["session_id"].(string)
		out = append(out, Attempt{Name: name, At: at, OK: outcome == "ok", Err: errText, Provider: prov, Target: target, Session: session})
	}
	return out
}

// ParseSeats reads "name:provider[:target]" entries separated by commas or
// newlines. Provider and target may be empty ("name", "name::T12").
func ParseSeats(s string) ([]Seat, error) {
	var seats []Seat
	seen := map[string]bool{}
	for _, raw := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == ';' }) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, ":", 3)
		seat := Seat{Name: strings.TrimSpace(parts[0])}
		if len(parts) > 1 {
			seat.Provider = normProvider(parts[1])
		}
		if len(parts) > 2 {
			seat.Target = normTarget(parts[2])
		}
		if seat.Name == "" {
			return nil, fmt.Errorf("seat %q has no name", raw)
		}
		if seen[seat.Name] {
			return nil, fmt.Errorf("seat %q named twice", seat.Name)
		}
		seen[seat.Name] = true
		seats = append(seats, seat)
	}
	if len(seats) == 0 {
		return nil, errors.New("an order names at least one seat")
	}
	return seats, nil
}

// Store is the durable order list. Writes are atomic write-and-rename; a
// malformed file is a hard error, never a silent reset.
//
// Every Store opened on the same path shares one lock, because the daemon
// opens a Store per request: a mutex per instance would let two handlers
// read, modify and write the file concurrently and lose one side's order.
type Store struct {
	path string
}

// pathLocks holds one mutex per cleaned store path.
var pathLocks sync.Map

func (s *Store) lock() func() {
	v, _ := pathLocks.LoadOrStore(s.path, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// DefaultPath is the store's path under stateDir.
func DefaultPath(stateDir string) string {
	return filepath.Join(stateDir, FileName)
}

// Open returns a store at path, failing when an existing file is malformed.
func Open(path string) (*Store, error) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	s := &Store{path: filepath.Clean(path)}
	defer s.lock()()
	if _, err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

type fileShape struct {
	Orders []Order `json:"orders"`
}

func (s *Store) load() ([]Order, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("spawnorder: read %s: %w", s.path, err)
	}
	var f fileShape
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("spawnorder: %s is malformed: %w", s.path, err)
	}
	return f.Orders, nil
}

// save writes through a uniquely named temp file, so two writers can never
// interleave into one .tmp, then renames it over the store.
func (s *Store) save(orders []Order) error {
	data, err := json.MarshalIndent(fileShape{Orders: orders}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Declare stores a new order, minting an ID and IssuedAt when absent. At
// MaxOrders the oldest closed orders are evicted; if none is closed the
// declaration is refused, because dropping an open order would silently
// discard an unresolved obligation.
func (s *Store) Declare(o Order, now time.Time) (Order, error) {
	defer s.lock()()
	orders, err := s.load()
	if err != nil {
		return Order{}, err
	}
	o.Parent = strings.TrimSpace(o.Parent)
	if o.Parent == "" {
		return Order{}, errors.New("an order names the parent it was given to")
	}
	if len(o.Seats) == 0 {
		return Order{}, errors.New("an order names at least one seat")
	}
	if o.IssuedAt.IsZero() {
		o.IssuedAt = now
	}
	o.IssuedAt = o.IssuedAt.UTC()
	if o.ID == "" {
		o.ID = "o-" + o.IssuedAt.Format("20060102T150405.000")
	}
	for _, prev := range orders {
		if prev.ID == o.ID {
			return Order{}, fmt.Errorf("order id %q already declared", o.ID)
		}
	}
	for len(orders) >= MaxOrders {
		evict := -1
		for i, prev := range orders {
			if prev.Closed && (evict < 0 || prev.IssuedAt.Before(orders[evict].IssuedAt)) {
				evict = i
			}
		}
		if evict < 0 {
			return Order{}, fmt.Errorf("spawn order store at capacity: %d open orders, none closed; close resolved orders (action=close) before declaring more", len(orders))
		}
		orders = append(orders[:evict], orders[evict+1:]...)
	}
	orders = append(orders, o)
	return o, s.save(orders)
}

// Close marks an order finished with.
func (s *Store) Close(id, note string, now time.Time) error {
	defer s.lock()()
	orders, err := s.load()
	if err != nil {
		return err
	}
	for i := range orders {
		if orders[i].ID == id {
			orders[i].Closed = true
			orders[i].ClosedAt = now.UTC()
			orders[i].ClosedNote = strings.TrimSpace(note)
			return s.save(orders)
		}
	}
	return fmt.Errorf("no order %q", id)
}

// Record writes each result's observed outcomes back onto its stored order
// when newer than what is stored, so an outcome outlives the journal tail it
// was read from. It writes only when something changed.
func (s *Store) Record(results []Result) error {
	defer s.lock()()
	orders, err := s.load()
	if err != nil {
		return err
	}
	byID := map[string]Result{}
	for _, r := range results {
		byID[r.Order.ID] = r
	}
	changed := false
	for i := range orders {
		r, ok := byID[orders[i].ID]
		if !ok {
			continue
		}
		for j := range orders[i].Seats {
			seat := &orders[i].Seats[j]
			for _, sr := range r.Seats {
				if sr.Name != seat.Name || sr.Observed == nil {
					continue
				}
				if seat.Observed == nil || sr.Observed.At.After(seat.Observed.At) {
					o := *sr.Observed
					seat.Observed = &o
					changed = true
				}
			}
		}
	}
	if !changed {
		return nil
	}
	return s.save(orders)
}

// Orders returns every stored order, oldest first.
func (s *Store) Orders() ([]Order, error) {
	defer s.lock()()
	orders, err := s.load()
	sort.SliceStable(orders, func(i, j int) bool { return orders[i].IssuedAt.Before(orders[j].IssuedAt) })
	return orders, err
}
