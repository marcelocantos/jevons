// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Directive is explicit control-plane intent, never inferred from message prose.
// Family is an opaque, caller-chosen operation scope (for example a target id).
// Only a later hold supersedes earlier pending authorizations in the same
// recipient/family. A hold before a new authorization does not veto the new one.
type Directive struct {
	Family string `json:"family,omitempty"`
	Kind   string `json:"kind,omitempty"` // authorization | hold
}

func (d Directive) Validate() error {
	if strings.TrimSpace(d.Family) == "" || d.Family != strings.TrimSpace(d.Family) || strings.ContainsAny(d.Family, "\n\r") || (d.Kind != "authorization" && d.Kind != "hold") {
		return fmt.Errorf("sendq: directive requires a nonempty single-line family and kind authorization|hold")
	}
	return nil
}

type Supersession struct {
	Agent   string    `json:"agent"`
	At      time.Time `json:"at"`
	HoldID  string    `json:"hold_id"`
	Removed []Entry   `json:"removed"`
	// State is prepared until the queue save succeeds. A prepared archive is
	// evidence of an interrupted transaction, NOT evidence of supersession.
	State string `json:"state"`
}

// ApplyDirective serializes a directive against ClaimFront and ClaimDigest.
// queued=true appends the new directive; otherwise the caller delivers it
// directly after the queue transaction. An active matching attempt cannot be
// recalled: refuse the hold instead of falsely claiming it won. Orphaned
// attempts from a previous daemon are also refused pending reconciliation.
// The archive is committed before removing entries; failure leaves the queue
// untouched. The hold id names the archive even for direct deliveries.
func (s *Store) ApplyDirective(agent, text string, d Directive, queued bool, at time.Time) (Entry, int, []Entry, error) {
	if err := d.Validate(); err != nil {
		return Entry{}, 0, nil, err
	}
	if s == nil {
		return Entry{}, 0, nil, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return Entry{}, 0, nil, err
	}
	e := Entry{ID: NewID(), Text: text, EnqueuedAt: at.UTC(), Directive: &d}
	var kept, removed []Entry
	for _, old := range f.Entries {
		if d.Kind == "hold" && old.Directive != nil && old.Directive.Kind == "authorization" && old.Directive.Family == d.Family {
			if old.State != Pending {
				return Entry{}, 0, nil, fmt.Errorf("sendq: authorization %s has unresolved delivery attempt; reconcile before holding %q", old.ID, d.Family)
			}
			removed = append(removed, old)
			continue
		}
		kept = append(kept, old)
	}
	if len(removed) > 0 {
		if err := s.archiveSupersession(Supersession{Agent: agent, At: at.UTC(), HoldID: e.ID, Removed: removed, State: "prepared"}); err != nil {
			return Entry{}, 0, nil, err
		}
	}
	if queued {
		kept = append(kept, e)
	}
	f.Entries = kept
	if queued || len(removed) > 0 {
		if err := s.save(f); err != nil {
			return Entry{}, 0, nil, err // archive remains prepared, never a claimed disposition
		}
	}
	if len(removed) > 0 {
		if err := s.archiveSupersession(Supersession{Agent: agent, At: at.UTC(), HoldID: e.ID, Removed: removed, State: "committed"}); err != nil {
			return Entry{}, 0, nil, fmt.Errorf("sendq: queue updated but supersession audit %s remains prepared: %w (do not blindly retry)", e.ID, err)
		}
	}
	return e, len(f.Entries), removed, nil
}

func (s *Store) archiveSupersession(a Supersession) error {
	if !s.Durable() {
		if s.memSupersessions == nil {
			s.memSupersessions = map[string]Supersession{}
		}
		s.memSupersessions[a.HoldID] = a
		return nil
	}
	dir := filepath.Join(s.dir, "supersessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, a.HoldID+".json")
	if err := os.WriteFile(path+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// ReadSupersession returns the persisted disposition and original payloads.
func (s *Store) ReadSupersession(holdID string) (Supersession, error) {
	if s == nil {
		return Supersession{}, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Durable() {
		a, ok := s.memSupersessions[holdID]
		if !ok {
			return Supersession{}, os.ErrNotExist
		}
		if a.State != "committed" {
			return a, fmt.Errorf("sendq: supersession %s is prepared, not committed", holdID)
		}
		return a, nil
	}
	if holdID == "" || strings.ContainsAny(holdID, `/\\`) {
		return Supersession{}, fmt.Errorf("sendq: invalid hold id")
	}
	b, err := os.ReadFile(filepath.Join(s.dir, "supersessions", holdID+".json"))
	if err != nil {
		return Supersession{}, err
	}
	var a Supersession
	err = json.Unmarshal(b, &a)
	if err == nil && a.State != "committed" {
		err = fmt.Errorf("sendq: supersession %s is prepared, not committed", holdID)
	}
	return a, err
}
