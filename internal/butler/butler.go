// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package butler is the CEO orchestrator: it owns thread lifecycle on
// the owner's behalf — adopting existing sessions observe-only, and
// (in later increments) spawning, directing, and garbage-collecting the
// disposable processes behind durable threads. It composes the durable
// thread store with the non-invasive session scanner and the transcript
// reader; it holds no long-lived process state of its own beyond what
// the store persists.
package butler

import (
	"fmt"
	"time"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// defaultTailEntries is how many trailing transcript lines the butler
// reads to derive a thread's status. Enough to span a full turn's worth
// of assistant/tool/user lines without loading whole transcripts.
const defaultTailEntries = 40

// Butler orchestrates threads. It is safe for concurrent use (the Store
// it wraps is concurrency-safe; the scanner and reader are read-only).
type Butler struct {
	store   *thread.Store
	scanner *discovery.Scanner
	reader  *transcript.Reader

	// processUp reports whether the butler owns a live process for the
	// given thread ID. Wired to the claudia registry in production; nil
	// (always-false) until the spawn path lands.
	processUp func(id string) bool

	now           func() time.Time
	idleThreshold time.Duration
	tailN         int
}

// Config parameterises New. Store, Scanner, and Reader are required.
type Config struct {
	Store     *thread.Store
	Scanner   *discovery.Scanner
	Reader    *transcript.Reader
	ProcessUp func(id string) bool
	// Now and IdleThreshold are injected for deterministic tests; both
	// default sensibly when zero.
	Now           func() time.Time
	IdleThreshold time.Duration
}

// New constructs a Butler.
func New(cfg Config) *Butler {
	b := &Butler{
		store:         cfg.Store,
		scanner:       cfg.Scanner,
		reader:        cfg.Reader,
		processUp:     cfg.ProcessUp,
		now:           cfg.Now,
		idleThreshold: cfg.IdleThreshold,
		tailN:         defaultTailEntries,
	}
	if b.now == nil {
		b.now = time.Now
	}
	return b
}

// AdoptArgs parameterises Adopt. WorkDir and Description are optional;
// WorkDir is resolved from the session's own transcript when omitted.
type AdoptArgs struct {
	SessionID   string // Claude Code session UUID
	ID          string // butler-level handle; defaults to SessionID
	WorkDir     string
	Description string
}

// Adopt registers an existing Claude Code session as an observe-only
// thread. It is non-invasive by construction: it only reads the
// session's transcript (to validate it exists and resolve its workdir)
// and never launches, resumes, or otherwise touches the process.
func (b *Butler) Adopt(args AdoptArgs) (*thread.Thread, error) {
	if !discovery.IsUUID(args.SessionID) {
		return nil, fmt.Errorf("adopt: %q is not a valid session id", args.SessionID)
	}

	// Observe the session non-invasively to confirm it exists and learn
	// its workdir. A session with no transcript on disk cannot be tailed
	// for status, so adopting it would be meaningless.
	info, err := b.scanner.Get(args.SessionID)
	if err != nil {
		return nil, fmt.Errorf("adopt: observe session %q: %w", args.SessionID, err)
	}
	if info == nil {
		return nil, fmt.Errorf("adopt: no transcript found for session %q — cannot observe it", args.SessionID)
	}

	workdir := args.WorkDir
	if workdir == "" {
		workdir = info.WorkDir
	}

	return b.store.Adopt(thread.AdoptArgs{
		ID:          args.ID,
		WorkDir:     workdir,
		SessionID:   args.SessionID,
		Description: args.Description,
		Now:         b.now(),
	})
}

// ThreadStatus pairs a persisted thread record with its derived live
// status.
type ThreadStatus struct {
	Thread *thread.Thread `json:"thread"`
	Status thread.Status  `json:"status"`
}

// List returns every thread with its status derived on demand.
func (b *Butler) List() []ThreadStatus {
	threads := b.store.List()
	out := make([]ThreadStatus, 0, len(threads))
	for _, t := range threads {
		out = append(out, ThreadStatus{Thread: t, Status: b.deriveStatus(t)})
	}
	return out
}

// Status returns the status of a single thread by ID.
func (b *Butler) Status(id string) (ThreadStatus, error) {
	t, ok := b.store.Get(id)
	if !ok {
		return ThreadStatus{}, fmt.Errorf("status: no thread %q", id)
	}
	return ThreadStatus{Thread: t, Status: b.deriveStatus(t)}, nil
}

// deriveStatus tails the thread's transcript and folds in liveness
// signals. A missing/unreadable transcript yields an empty tail, which
// DeriveStatus reports as idle — never an error, since status is a
// best-effort observation.
func (b *Butler) deriveStatus(t *thread.Thread) thread.Status {
	var entries []transcript.Entry
	if e, err := b.reader.Tail(t.SessionID, b.tailN); err == nil {
		entries = e
	}

	externallyActive := false
	if b.scanner != nil {
		externallyActive = b.scanner.IsActive(t.SessionID)
	}
	processUp := false
	if b.processUp != nil {
		processUp = b.processUp(t.ID)
	}

	return thread.DeriveStatus(thread.StatusInput{
		Entries:          entries,
		Now:              b.now(),
		ExternallyActive: externallyActive,
		ProcessUp:        processUp,
		IdleThreshold:    b.idleThreshold,
	})
}
