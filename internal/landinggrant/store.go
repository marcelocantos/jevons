// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package landinggrant stores exact, single-use integration grants. A grant
// is a workflow safeguard; OS confinement of worker writes to the shared Git
// directory is separately required before it is a security boundary.
package landinggrant

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/marcelocantos/jevons/internal/worktree"
)

type EpochAuthority interface {
	// WithCurrent holds the epoch lock through action. It refuses a stale or
	// held target; action must not call epoch methods or execute git operations.
	WithCurrent(repo, target string, epoch uint64, action func() error) error
}

type Grant struct {
	ID      string               `json:"id"`
	Review  worktree.BatchReview `json:"review"`
	Epoch   uint64               `json:"epoch"`
	Expires time.Time            `json:"expires"`
	POEvent string               `json:"po_event"`
	Spent   bool                 `json:"spent"`
}
type Audit struct {
	At      time.Time            `json:"at"`
	GrantID string               `json:"grant_id"`
	Actor   string               `json:"actor"`
	Command string               `json:"command"`
	POEvent string               `json:"po_event,omitempty"`
	Review  worktree.BatchReview `json:"review"`
	Epoch   uint64               `json:"epoch"`
	Outcome string               `json:"outcome"`
}

// ProvenanceVerifier checks a completed event against the current registered
// PO PROCESS (not the MCP actor string or an event ID supplied by a caller).
// The daemon must provide this; absent verifier always refuses mint.
type ProvenanceVerifier func(poEvent string, review worktree.BatchReview) error

type Store struct {
	VerifyPOEvent  ProvenanceVerifier
	SnapshotEpoch  func(repo, target string) (epoch uint64, held bool, err error)
	Dir            string
	Epoch          EpochAuthority
	Actor, Command string
	Now            func() time.Time
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Issue is deliberately NOT a worker-facing API: the daemon must only call it
// after verifying a completed event from the current registered PO process.
// poEvent is audit evidence, NOT proof of identity by itself. Missing epoch
// authority or provenance is refused.
func (s *Store) Issue(g Grant) error {
	if s == nil || s.Epoch == nil || s.VerifyPOEvent == nil || g.POEvent == "" || g.ID == "" || !validReview(g.Review) || !g.Expires.After(s.now()) {
		return errors.New("grant: missing epoch authority, PO event, exact review or future expiry")
	}
	if err := s.VerifyPOEvent(g.POEvent, g.Review); err != nil {
		return fmt.Errorf("grant: PO process provenance refused: %w", err)
	}
	return s.Epoch.WithCurrent(g.Review.Repo, g.Review.Target, g.Epoch, func() error {
		return s.locked(func(path string, rows *map[string]Grant) error {
			if _, ok := (*rows)[g.ID]; ok {
				return errors.New("grant: duplicate ID")
			}
			(*rows)[g.ID] = g
			if err := save(path, *rows); err != nil {
				return err
			}
			return s.audit(Audit{At: s.now(), GrantID: g.ID, Actor: "current-po-process", POEvent: g.POEvent, Review: g.Review, Epoch: g.Epoch, Outcome: "issued"})
		})
	})
}

// RedeemAndLand implements worktree.Redeemer. The integrator MUST hold
// its landing lock before calling. The epoch callback serializes durable spend and the bounded final Git ref
// advance against hold acceptance. A hold cannot return accepted between
// spending and mutation. No sendq lock is held.
func (s *Store) RedeemAndLand(id string, review worktree.BatchReview, land func() error) error {
	if s == nil || s.Epoch == nil {
		return worktree.ErrNoAuthority
	}
	if land == nil {
		return errors.New("grant: landing action required")
	}
	if !validReview(review) {
		return errors.New("grant: incomplete review")
	}
	// Read the epoch from the durable grant under grant lock, release it, then
	// acquire epoch -> grant in the same order used by Issue. A hold in between
	// is detected by WithCurrent, not silently accepted.
	var g Grant
	err := s.locked(func(_ string, rows *map[string]Grant) error {
		var ok bool
		g, ok = (*rows)[id]
		if !ok {
			return errors.New("grant: unknown ID")
		}
		return nil
	})
	if err != nil {
		return s.refuse(id, review, 0, err)
	}
	if g.Spent || g.POEvent == "" || !reflect.DeepEqual(g.Review, review) || !g.Expires.After(s.now()) {
		return s.refuse(id, review, g.Epoch, errors.New("grant: spent, expired, or reviewed tuple changed"))
	}
	err = s.Epoch.WithCurrent(review.Repo, review.Target, g.Epoch, func() error {
		if err := s.locked(func(path string, rows *map[string]Grant) error {
			latest, ok := (*rows)[id]
			if !ok || latest.Spent || latest.POEvent == "" || !reflect.DeepEqual(latest.Review, review) || !latest.Expires.After(s.now()) {
				return errors.New("grant: spent, expired, or reviewed tuple changed")
			}
			latest.Spent = true
			(*rows)[id] = latest
			if e := save(path, *rows); e != nil {
				return e
			}
			// Audit failure after spend is uncertain and cannot unspend.
			return s.audit(Audit{At: s.now(), GrantID: id, Actor: s.Actor, Command: s.Command, POEvent: latest.POEvent, Review: review, Epoch: latest.Epoch, Outcome: "spent-before-landing"})
		}); err != nil {
			return err
		}
		// Keep the epoch authority locked during mutation: a newer hold can
		// only be accepted after this callback returns. Git --ff-only is
		// bounded by the integrator; uncertain failure consumes the grant.
		if land == nil {
			return errors.New("grant: missing landing action after spend")
		}
		err := land()
		outcome := "landed"
		if err != nil {
			outcome = "landing-failed: " + err.Error()
		}
		if auditErr := s.audit(Audit{At: s.now(), GrantID: id, Actor: s.Actor, Command: s.Command, POEvent: g.POEvent, Review: review, Epoch: g.Epoch, Outcome: outcome}); auditErr != nil {
			return fmt.Errorf("grant: landing audit failed: %w (landing: %v)", auditErr, err)
		}
		return err
	})
	if err != nil {
		return s.refuse(id, review, g.Epoch, err)
	}
	return nil
}
func (s *Store) refuse(id string, review worktree.BatchReview, epoch uint64, err error) error {
	if e := s.audit(Audit{At: s.now(), GrantID: id, Actor: s.Actor, Command: s.Command, Review: review, Epoch: epoch, Outcome: "refused: " + err.Error()}); e != nil {
		return fmt.Errorf("grant: refusal audit unavailable: %w (original: %v)", e, err)
	}
	return err
}
func validReview(r worktree.BatchReview) bool {
	if r.Repo == "" || r.Target == "" || !strings.HasPrefix(r.BaseRef, "refs/heads/") || r.BaseSHA == "" || len(r.Workers) == 0 {
		return false
	}
	for _, w := range r.Workers {
		if w.Name == "" || !strings.HasPrefix(w.Ref, "refs/heads/") || w.SHA == "" {
			return false
		}
	}
	return true
}
func (s *Store) locked(fn func(string, *map[string]Grant) error) error {
	if s.Dir == "" {
		return errors.New("grant: no durable store")
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, "grants.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	path := filepath.Join(s.Dir, "grants.json")
	rows := map[string]Grant{}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(b) > 0 {
		if err = json.Unmarshal(b, &rows); err != nil {
			return err
		}
	}
	return fn(path, &rows)
}
func save(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path+".tmp", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
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
	if err = os.Rename(path+".tmp", path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (s *Store) audit(a Audit) error {
	if s == nil || s.Dir == "" {
		return errors.New("grant: audit directory unavailable")
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, "audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		return err
	}
	return f.Sync()
}
