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
// seat is reconciled against the start attempts the daemon journals and the
// seats the registry holds. Every named seat gets one of: minted, rerouted
// (minted, but on a provider the order did not name), refused (a start was
// attempted and declined — the reason is the journalled error) or
// not_attempted (nothing ever asked the daemon for it). An order is complete
// only when no seat is refused or not_attempted, and [Result.Line] names the
// seats that are, so a cold reader of the panel can tell a finished order
// from a half-finished one without holding the order text.
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

// MaxOrders bounds the store; the oldest orders are dropped first.
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

// Status is a named seat's reconciled outcome.
type Status string

const (
	Minted       Status = "minted"
	Rerouted     Status = "rerouted"
	Refused      Status = "refused"
	NotAttempted Status = "not_attempted"
)

// Attempt is one journalled jevons_agent_start outcome.
type Attempt struct {
	Name     string
	At       time.Time
	OK       bool
	Err      string
	Provider string
}

// SeatResult is one named seat and what became of it.
type SeatResult struct {
	Seat
	Status Status `json:"status"`
	// Reason says why: the start error for refused, the provider actually
	// used for rerouted, and for not_attempted that no start was ever made.
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at,omitzero"`
}

// Result is an order reconciled against the fleet.
type Result struct {
	Order Order        `json:"order"`
	Seats []SeatResult `json:"seats"`
}

// Complete reports whether every named seat was minted (on any provider).
func (r Result) Complete() bool {
	for _, s := range r.Seats {
		if s.Status == Refused || s.Status == NotAttempted {
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

// Reconcile decides each named seat's outcome. attempts are journalled start
// outcomes (any order); present maps registry seat names to their provider.
//
// The latest attempt inside the order's window decides a seat: an OK start is
// minted (or rerouted when the provider differs from the one ordered), an
// error is refused with that error — unless the seat is in the registry now,
// because a later retry may have landed outside the journal tail. A seat with
// no attempt is minted when the registry holds it (it predates the order) and
// not_attempted otherwise: that is the dropped half.
func Reconcile(o Order, attempts []Attempt, present map[string]string) Result {
	from := o.IssuedAt.Add(-DeclareSkew)
	latest := map[string]Attempt{}
	for _, a := range attempts {
		if a.At.Before(from) {
			continue
		}
		if prev, ok := latest[a.Name]; !ok || a.At.After(prev.At) {
			latest[a.Name] = a
		}
	}
	res := Result{Order: o}
	for _, seat := range o.Seats {
		sr := SeatResult{Seat: seat}
		livePro, live := present[seat.Name]
		a, attempted := latest[seat.Name]
		switch {
		case attempted && a.OK:
			sr.At = a.At
			sr.Status, sr.Reason = mintedOn(seat, a.Provider)
		case attempted && live:
			sr.At = a.At
			sr.Status, sr.Reason = mintedOn(seat, livePro)
		case attempted:
			sr.At = a.At
			sr.Status = Refused
			sr.Reason = strings.TrimSpace(a.Err)
			if sr.Reason == "" {
				sr.Reason = "start returned an error with no reason"
			}
		case live:
			sr.Status, sr.Reason = mintedOn(seat, livePro)
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
		out = append(out, Attempt{Name: name, At: at, OK: outcome == "ok", Err: errText, Provider: prov})
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
			seat.Target = strings.TrimSpace(parts[2])
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
type Store struct {
	mu   sync.Mutex
	path string
}

// DefaultPath is the store's path under stateDir.
func DefaultPath(stateDir string) string {
	return filepath.Join(stateDir, FileName)
}

// Open returns a store at path, failing when an existing file is malformed.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
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

func (s *Store) save(orders []Order) error {
	data, err := json.MarshalIndent(fileShape{Orders: orders}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Declare stores a new order, minting an ID and IssuedAt when absent.
func (s *Store) Declare(o Order, now time.Time) (Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
		o.ID = "o-" + o.IssuedAt.Format("20060102T150405")
	}
	for _, prev := range orders {
		if prev.ID == o.ID {
			return Order{}, fmt.Errorf("order id %q already declared", o.ID)
		}
	}
	orders = append(orders, o)
	if len(orders) > MaxOrders {
		orders = orders[len(orders)-MaxOrders:]
	}
	return o, s.save(orders)
}

// Close marks an order finished with.
func (s *Store) Close(id, note string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// Orders returns every stored order, oldest first.
func (s *Store) Orders() ([]Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	orders, err := s.load()
	sort.SliceStable(orders, func(i, j int) bool { return orders[i].IssuedAt.Before(orders[j].IssuedAt) })
	return orders, err
}
