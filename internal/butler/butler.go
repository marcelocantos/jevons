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
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/thread"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// defaultTailEntries is how many trailing transcript lines the butler
// reads to derive a thread's status. Enough to span a full turn's worth
// of assistant/tool/user lines without loading whole transcripts.
const defaultTailEntries = 40

// Fleet manages the disposable processes behind spawned threads. It is
// the mechanism seam the butler drives; production is backed by
// claudia.Registry, tests by a fake. The butler owns the *policy*
// (when to rehydrate, when to reap); the Fleet owns the *mechanism*
// (launch/resume a claude process, deliver a turn, stop it).
type Fleet interface {
	// Launch ensures a live process exists for the thread, resuming its
	// session (--resume) when one already exists. Idempotent when the
	// process is already alive. It blocks until the process is ready to
	// accept input or returns an actionable error.
	Launch(t *thread.Thread) error
	// Send delivers a directed turn to the thread's live process and
	// returns its reply. It requires a live process (Launch first).
	Send(id, text string) (string, error)
	// Alive reports whether a live process currently exists for the thread.
	Alive(id string) bool
	// Stop stops the process resumably — the thread and its session
	// persist, and a later Launch rehydrates it (process-as-cache).
	Stop(id string)
	// Remove stops the process and deregisters the agent entirely, so it
	// does not auto-restart. Used when a thread is deleted.
	Remove(id string)
}

// Participants is the optional secondary lookup for fleet agents that are
// not butler threads (🎯T114 / 🎯T111.2). One deliver path addresses any
// participant by name: thread store first, then this registry.
type Participants interface {
	// Exists reports whether a registered agent is known by id/name.
	Exists(id string) bool
	// Deliver rehydrates if needed, sends text, and returns a reply
	// (or a typed error — never a silent drop).
	Deliver(id, text string) (string, error)
}

// Butler orchestrates threads. It is safe for concurrent use (the Store
// it wraps is concurrency-safe; the scanner and reader are read-only).
type Butler struct {
	store   *thread.Store
	scanner *discovery.Scanner
	reader  *transcript.Reader
	fleet   Fleet

	// participants resolves fleet agents that are not in the thread store
	// so Deliver/PushEvent share one name space with work agents (🎯T114).
	participants Participants

	// externallyActive reports whether a foreign process is currently
	// driving the given session (the two-writer signal). Wired to the
	// scanner in production; injectable for tests.
	externallyActive func(sessionID string) bool

	// spawnGuard gates new spawns; resumeGuard gates (re)launching a
	// thread's process. Both default to permissive. They are the budget
	// clamp-down's hooks into thread lifecycle.
	spawnGuard  func() error
	resumeGuard func(id string, auto bool) error

	now           func() time.Time
	idleThreshold time.Duration
	tailN         int
}

// Config parameterises New. Store, Scanner, and Reader are required.
// Fleet is required for the spawn/direct/GC/take-over paths but may be
// nil for an observe-only butler (adopt/list/status still work).
// Participants is optional; when set, Deliver/PushEvent resolve agents
// that exist only in the fleet registry (not threads.json).
type Config struct {
	Store        *thread.Store
	Scanner      *discovery.Scanner
	Reader       *transcript.Reader
	Fleet        Fleet
	Participants Participants
	// Now, IdleThreshold, and ExternallyActive are injected for
	// deterministic tests; all default sensibly when nil/zero
	// (ExternallyActive falls back to the scanner).
	Now              func() time.Time
	IdleThreshold    time.Duration
	ExternallyActive func(sessionID string) bool
	// SpawnGuard, if set, gates new spawns (the budget hard ceiling); a
	// non-nil error refuses the spawn. ResumeGuard gates (re)launching a
	// thread's process; auto marks unattended rehydrates. Both default
	// to permissive.
	SpawnGuard  func() error
	ResumeGuard func(id string, auto bool) error
}

// New constructs a Butler.
func New(cfg Config) *Butler {
	b := &Butler{
		store:            cfg.Store,
		scanner:          cfg.Scanner,
		reader:           cfg.Reader,
		fleet:            cfg.Fleet,
		participants:     cfg.Participants,
		externallyActive: cfg.ExternallyActive,
		spawnGuard:       cfg.SpawnGuard,
		resumeGuard:      cfg.ResumeGuard,
		now:              cfg.Now,
		idleThreshold:    cfg.IdleThreshold,
		tailN:            defaultTailEntries,
	}
	if b.now == nil {
		b.now = time.Now
	}
	if b.spawnGuard == nil {
		b.spawnGuard = func() error { return nil }
	}
	if b.resumeGuard == nil {
		b.resumeGuard = func(string, bool) error { return nil }
	}
	if b.externallyActive == nil {
		if b.scanner != nil {
			b.externallyActive = func(sessionID string) bool { return b.scanner.IsActive(sessionID) }
		} else {
			b.externallyActive = func(string) bool { return false }
		}
	}
	return b
}

// AdoptArgs parameterises Adopt. Only SessionID is required: the name is
// derived from the session's repo, and adoption takes the session over by
// default (ObserveOnly opts out). ID, WorkDir, and Description are
// optional overrides.
type AdoptArgs struct {
	SessionID   string // Grok session id
	ID          string // optional handle; defaults to the repo/dir name
	WorkDir     string // optional; resolved from the transcript when empty
	Description string // optional; defaults to the repo/dir name
	ObserveOnly bool   // opt out of the default take-over
}

// Adopt registers an existing Grok session as a thread and, by default,
// takes it over so it is immediately directable. Observing is
// non-invasive (transcript only); take-over enforces the two-writer guard.
func (b *Butler) Adopt(args AdoptArgs) (*thread.Thread, error) {
	if !discovery.IsSessionID(args.SessionID) {
		return nil, fmt.Errorf("adopt: %q is not a valid session id", args.SessionID)
	}
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

	// Idempotent per session: re-adopting returns the existing thread
	// rather than creating a duplicate under a second name.
	t, existed := b.store.FindBySession(args.SessionID)
	if !existed {
		id := args.ID
		if id == "" {
			id = b.uniqueName(deriveName(workdir), args.SessionID)
		}
		desc := args.Description
		if desc == "" {
			desc = deriveName(workdir)
		}
		t, err = b.store.Adopt(thread.AdoptArgs{
			ID:          id,
			WorkDir:     workdir,
			SessionID:   args.SessionID,
			Description: desc,
			Now:         b.now(),
		})
		if err != nil {
			return nil, err
		}
	}

	// Take over by default so the thread is directable and registers as an
	// owned agent (which is what makes it appear in the UI panel). On the
	// two-writer refusal the observe-only record persists and the error
	// tells the caller to stop driving the session first.
	if !args.ObserveOnly && t.Kind == thread.KindAdopted {
		return b.TakeOver(t.ID)
	}
	return t, nil
}

// Remove deletes a thread: it stops and deregisters any owned process
// (leaving the underlying Claude session on disk intact) and drops the
// record. Used to clean up threads without leaving duplicates behind.
func (b *Butler) Remove(id string) error {
	if _, ok := b.store.Get(id); !ok {
		return fmt.Errorf("remove: no thread %q", id)
	}
	if b.fleet != nil {
		b.fleet.Remove(id)
	}
	return b.store.Remove(id)
}

// deriveName picks a meaningful default handle from a session's workdir —
// the repo/directory basename (e.g. ".../marcelocantos/sawmill" → "sawmill").
func deriveName(workdir string) string {
	base := filepath.Base(strings.TrimRight(workdir, "/"))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "session"
	}
	return base
}

// uniqueName returns id if it's free (or already this session's), else a
// numeric-suffixed variant, so two different sessions in same-named repos
// don't collide.
func (b *Butler) uniqueName(id, sessionID string) string {
	for i := 0; ; i++ {
		cand := id
		if i > 0 {
			cand = fmt.Sprintf("%s-%d", id, i+1)
		}
		existing, ok := b.store.Get(cand)
		if !ok || existing.SessionID == sessionID {
			return cand
		}
	}
}

// SpawnArgs parameterises Spawn.
type SpawnArgs struct {
	ID          string // stable thread handle (required)
	WorkDir     string // required
	Description string
	Model       string
	// Provider is the claudia backend id for the thread's process (🎯T148).
	// Empty → fleet default (config/env/grok).
	Provider string
	// Parent is the fleet lineage parent recorded on the agent registry
	// when Launch mints the process (🎯T111.3). Empty means unknown.
	Parent string
	// Purpose is the unified fleet role (work | aside | overseer).
	// Thread spawn defaults to aside (side-chat participant) (🎯T114).
	Purpose string
}

// Spawn creates a new thread the butler owns end-to-end and launches its
// process. The thread record is persisted before the process starts, so
// even a launch that dies leaves a durable, rehydratable thread.
// Spawned threads are side-chat participants by default (Purpose aside);
// work agents use jevons_agent_start (🎯T114).
func (b *Butler) Spawn(args SpawnArgs) (*thread.Thread, error) {
	if b.fleet == nil {
		return nil, fmt.Errorf("spawn: no fleet configured")
	}
	if args.ID == "" || args.WorkDir == "" {
		return nil, fmt.Errorf("spawn: id and workdir are required")
	}
	if _, exists := b.store.Get(args.ID); exists {
		return nil, fmt.Errorf("spawn: thread %q already exists", args.ID)
	}
	if err := b.spawnGuard(); err != nil {
		return nil, fmt.Errorf("spawn %q: %w", args.ID, err)
	}

	purpose := strings.TrimSpace(args.Purpose)
	if purpose == "" {
		purpose = thread.PurposeAside
	}

	t := &thread.Thread{
		ID:          args.ID,
		Kind:        thread.KindSpawned,
		WorkDir:     args.WorkDir,
		Description: args.Description,
		Model:       args.Model,
		Provider:    strings.TrimSpace(args.Provider),
		Parent:      strings.TrimSpace(args.Parent),
		Purpose:     purpose,
		CreatedAt:   b.now(),
	}
	if err := b.store.Put(t); err != nil {
		return nil, err
	}

	if err := b.fleet.Launch(t); err != nil {
		return nil, fmt.Errorf("spawn %q: launch failed: %w", args.ID, err)
	}
	// Launch populates t.SessionID with the session the fleet minted, so
	// the thread can be rehydrated later. Persist the updated record.
	if err := b.store.Put(t); err != nil {
		return nil, err
	}
	return t, nil
}

// Direct delivers a turn to a thread and returns its reply. It upholds
// the two load-bearing guarantees:
//
//   - NEVER LOSE A THREAD / PROCESS-AS-CACHE: if the thread's process
//     has been stopped or aged out, Direct transparently rehydrates it
//     (--resume) before sending.
//   - NO SILENT-FAIL: a thread whose process cannot be (re)started
//     returns a distinct, actionable error — never a silent timeout.
//
// Adopted (observe-only) threads are refused until taken over, since a
// second writer must not drive a session the owner still holds.
//
// Prefer Deliver when the target may be a fleet agent rather than a
// butler thread (🎯T114). Direct remains the thread-only path.
func (b *Butler) Direct(id, text string) (string, error) {
	if b.fleet == nil {
		return "", fmt.Errorf("direct: no fleet configured")
	}
	t, ok := b.store.Get(id)
	if !ok {
		return "", fmt.Errorf("direct: no thread %q", id)
	}
	if t.Kind == thread.KindAdopted {
		return "", fmt.Errorf("direct: thread %q is observe-only; take it over before directing it", id)
	}

	if !b.fleet.Alive(id) {
		// Process aged out or was GC'd — transparently rehydrate rather
		// than fail. This is the T24 wedge's fix at the butler layer. The
		// resume is owner-driven (they are directing), so it is attended,
		// not an unattended auto-resume — but the budget clamp can still
		// refuse it if this worker is pause/kill-clamped for overspend.
		if err := b.resumeGuard(id, false); err != nil {
			return "", fmt.Errorf("direct %q: %w", id, err)
		}
		if err := b.fleet.Launch(t); err != nil {
			return "", fmt.Errorf("direct %q: could not rehydrate its process: %w", id, err)
		}
	}

	reply, err := b.fleet.Send(id, text)
	if err != nil {
		return "", fmt.Errorf("direct %q: %w", id, err)
	}
	return reply, nil
}

// Deliver is the unified talk path (🎯T114 / 🎯T111.2): resolve id as a
// butler thread first, else as a fleet agent participant. Same no-silent-
// fail / rehydrate guarantees on both arms. Missing targets report
// "no participant", never "no thread" when an agent exists.
func (b *Butler) Deliver(id, text string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("deliver: target id is required")
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("deliver: text is required")
	}
	if _, ok := b.store.Get(id); ok {
		return b.Direct(id, text)
	}
	if b.participants != nil && b.participants.Exists(id) {
		reply, err := b.participants.Deliver(id, text)
		if err != nil {
			return "", fmt.Errorf("deliver %q: %w", id, err)
		}
		return reply, nil
	}
	return "", fmt.Errorf("deliver: no participant %q", id)
}

// SetParticipants attaches (or replaces) the fleet-agent lookup used by
// Deliver/PushEvent. Safe to call after New (e.g. when wiring production).
func (b *Butler) SetParticipants(p Participants) {
	b.participants = p
}

// TakeOver promotes an adopted (observe-only) thread to one jevons owns
// end to end: it launches jevons's own process resuming the session, and
// flips the thread to spawned so it becomes directable, GC-able, and
// rehydratable like any other owned thread. The session id is preserved,
// so a taken-over thread can later be decoupled and handed back.
//
// It refuses if the session is still being driven elsewhere: two claude
// processes on one session corrupt the transcript, so the owner must stop
// driving it in its terminal first (the two-writer constraint).
func (b *Butler) TakeOver(id string) (*thread.Thread, error) {
	if b.fleet == nil {
		return nil, fmt.Errorf("takeover: no fleet configured")
	}
	t, ok := b.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("takeover: no thread %q", id)
	}
	if t.Kind != thread.KindAdopted {
		return nil, fmt.Errorf("takeover: thread %q is already owned", id)
	}
	if b.externallyActive(t.SessionID) {
		return nil, fmt.Errorf("takeover %q: its session is still active in another process — stop driving it in its terminal, then take it over", id)
	}

	// Promote and persist first; then launch. On launch failure revert to
	// observe-only rather than leaving a broken half-owned thread.
	t.Kind = thread.KindSpawned
	if err := b.store.Put(t); err != nil {
		return nil, err
	}
	if err := b.fleet.Launch(t); err != nil {
		t.Kind = thread.KindAdopted
		_ = b.store.Put(t)
		return nil, fmt.Errorf("takeover %q: %w", id, err)
	}
	return t, nil
}

// ReapIdle stops the processes of spawned threads that have gone idle,
// freeing resources while keeping the threads durable and rehydratable
// (process-as-cache GC). It returns the IDs it reaped. Adopted threads
// are never reaped — the butler does not own their processes.
func (b *Butler) ReapIdle() []string {
	if b.fleet == nil {
		return nil
	}
	var reaped []string
	for _, t := range b.store.List() {
		if t.Kind != thread.KindSpawned || !b.fleet.Alive(t.ID) {
			continue
		}
		if b.deriveStatus(t).State == thread.StateIdle {
			b.fleet.Stop(t.ID)
			reaped = append(reaped, t.ID)
		}
	}
	return reaped
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

	processUp := b.fleet != nil && b.fleet.Alive(t.ID)

	return thread.DeriveStatus(thread.StatusInput{
		Entries:          entries,
		Now:              b.now(),
		ExternallyActive: b.externallyActive(t.SessionID),
		ProcessUp:        processUp,
		IdleThreshold:    b.idleThreshold,
	})
}
