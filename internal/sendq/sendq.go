// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package sendq is the daemon's own durable backlog of messages it has
// accepted for an agent but not yet handed over (🎯T418).
//
// WHY IT IS ON DISK. 🎯T416 stopped the daemon from pasting a message into a
// pane whose turn is running, because the provider CLI's queue is a store
// jevons can neither see nor replay: it merges silently with later sends and
// dies with the pane at the next rotation or restart. The message is held in
// the daemon's own queue instead — which was a map in the daemon's memory, so
// the loss moved rather than went away. jevonsd restarted three times on the
// day 🎯T416 was written and four times in one session on 2026-08-15; every
// one of those bounces silently emptied a queue whose senders had been told
// "queued (N pending) … held by the daemon".
//
// So the queue outlives the process that accepted the message. A daemon that
// dies between accepting and delivering comes back up, reads the backlog off
// disk, and drains it at the receiver's next turn boundary. That is the whole
// of acceptance clause 4: an in-memory queue moves the loss rather than
// removing it.
//
// WHAT IS PERSISTED, and what deliberately is not. The payload and when it was
// accepted — everything needed to deliver the message later, or to say how long
// it has been waiting. Not persisted: whether a turn was in flight when it was
// accepted. That is a claim about a process that no longer exists (see
// turn_flight.go on why FlightUnknown is a first-class answer), and writing it
// to disk would resurrect a stale belief as a fact after exactly the event that
// invalidates it.
//
// A STORE WITH NO DIRECTORY KEEPS ITS ENTRIES IN MEMORY, so a daemon (or a
// hermetic test) with no state directory still has one queue implementation
// rather than two code paths that drift. It is not durable and does not pretend
// to be: Durable reports which kind it is, and the daemon says so once at
// startup rather than leaving the operator to infer it.
//
// A MALFORMED FILE IS AN ERROR, never a silent reset. The house rule for
// durable state applies with extra force here: quietly starting from an empty
// queue is indistinguishable, to every observer, from having delivered
// everything in it.
package sendq

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry is one accepted-but-undelivered message.
type Entry struct {
	// ID identifies this acceptance across a restart, so a log line, a
	// fleet-health notice and the file on disk can be talked about as one
	// thing rather than by quoting the payload back.
	ID string `json:"id"`
	// Text is the payload exactly as accepted. Never truncated: this is the
	// message, not a record of it.
	Text string `json:"text"`
	// EnqueuedAt is when the daemon accepted it. The age of the oldest entry
	// is the number that says whether a queue is waiting or stalled.
	EnqueuedAt time.Time `json:"enqueued_at"`
}

// Age is how long this entry has been waiting, as of now.
func (e Entry) Age(now time.Time) time.Duration { return now.Sub(e.EnqueuedAt) }

// Backlog is the observable state of one agent's queue: what a stall looks
// like from outside. Depth alone cannot distinguish a queue that is moving
// from one that has not moved since a rotation, which is why the age of the
// oldest entry is part of the answer and not an optional detail (clause 9 —
// a counter only the daemon reads is where "queued" goes to die).
type Backlog struct {
	Agent  string
	Depth  int
	Oldest time.Time
}

// OldestAge is how long the head of this queue has been waiting.
func (b Backlog) OldestAge(now time.Time) time.Duration {
	if b.Depth == 0 || b.Oldest.IsZero() {
		return 0
	}
	return now.Sub(b.Oldest)
}

// Describe is the operator-facing one-liner used in logs and notices.
func (b Backlog) Describe(now time.Time) string {
	if b.Depth == 0 {
		return fmt.Sprintf("%s: queue empty", b.Agent)
	}
	return fmt.Sprintf("%s: %d queued, oldest waiting %s",
		b.Agent, b.Depth, b.OldestAge(now).Round(time.Second))
}

// file is the on-disk shape. The agent name is written into the record as
// well as into the filename so a record found loose is self-describing.
type file struct {
	Agent   string  `json:"agent"`
	Entries []Entry `json:"entries"`
}

// Store is a directory of per-agent queues.
//
// One file per agent rather than one file for the fleet: the queues are
// mutated concurrently by unrelated agents' turn boundaries, and a shared
// file would serialise every drain behind every enqueue for the sake of a
// single inode.
type Store struct {
	mu  sync.Mutex
	dir string
	// mem holds the queues when there is no directory to hold them. The
	// storeless daemon and the hermetic test share the durable store's code
	// down to the last method; only where a record lands differs.
	mem map[string][]Entry
}

// NewStore roots a store at dir (conventionally <state_dir>/sendq). An empty
// dir is a memory-backed store: same behaviour, no durability.
func NewStore(dir string) *Store { return &Store{dir: strings.TrimSpace(dir)} }

// Dir is where this store keeps its records, empty when it is memory-backed.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// Durable reports whether this store survives the process that wrote it.
func (s *Store) Durable() bool { return s != nil && s.dir != "" }

// path is the per-agent record file. Agent names come from the registry, but
// a name carrying a separator would escape the directory — refuse rather than
// sanitise, on the same reasoning as handover.Store.path: a silently renamed
// queue is a queue nobody drains.
func (s *Store) path(agent string) (string, error) {
	name := strings.TrimSpace(agent)
	if name == "" {
		return "", fmt.Errorf("sendq: agent name is required")
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("sendq: unusable agent name %q", agent)
	}
	return filepath.Join(s.dir, name+".json"), nil
}

// load reads an agent's queue. A missing file is an empty queue; an
// unreadable or unparseable one is an error.
func (s *Store) load(agent string) (file, error) {
	if !s.Durable() {
		name := strings.TrimSpace(agent)
		if name == "" {
			return file{}, fmt.Errorf("sendq: agent name is required")
		}
		return file{Agent: name, Entries: s.mem[name]}, nil
	}
	path, err := s.path(agent)
	if err != nil {
		return file{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return file{Agent: agent}, nil
	}
	if err != nil {
		return file{}, fmt.Errorf("sendq: read queue for %q: %w", agent, err)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return file{}, fmt.Errorf("sendq: parse queue for %q (%s): %w", agent, path, err)
	}
	f.Agent = agent
	return f, nil
}

// save writes an agent's queue with atomic write-and-rename, removing the
// record entirely when the queue is empty so an idle fleet does not leave a
// directory of empty files for the age sweep to walk.
func (s *Store) save(f file) error {
	if !s.Durable() {
		if s.mem == nil {
			s.mem = map[string][]Entry{}
		}
		if len(f.Entries) == 0 {
			delete(s.mem, f.Agent)
		} else {
			s.mem[f.Agent] = f.Entries
		}
		return nil
	}
	path, err := s.path(f.Agent)
	if err != nil {
		return err
	}
	if len(f.Entries) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("sendq: clear queue for %q: %w", f.Agent, err)
		}
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("sendq: create %s: %w", s.dir, err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("sendq: encode queue for %q: %w", f.Agent, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("sendq: write queue for %q: %w", f.Agent, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("sendq: commit queue for %q: %w", f.Agent, err)
	}
	return nil
}

// NewID mints an entry id. Exported because the caller that accepts a message
// may want to name it in its reply before the write completes.
func NewID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A queue entry that cannot be named is still a queue entry: fall back
		// to the clock rather than refuse the message.
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// Append adds a message to the back of an agent's queue and returns the
// entry as written together with the resulting depth.
func (s *Store) Append(agent, text string, at time.Time) (Entry, int, error) {
	if s == nil {
		return Entry{}, 0, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return Entry{}, 0, err
	}
	e := Entry{ID: NewID(), Text: text, EnqueuedAt: at.UTC()}
	f.Entries = append(f.Entries, e)
	if err := s.save(f); err != nil {
		return Entry{}, 0, err
	}
	return e, len(f.Entries), nil
}

// PushFront returns an entry to the head of the queue, for the drain that
// found the agent busy after all. The entry keeps its original EnqueuedAt:
// re-stamping it would reset the age that says how long a message has been
// waiting, which is the one number a stall is visible in.
func (s *Store) PushFront(agent string, e Entry) error {
	if s == nil {
		return fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return err
	}
	f.Entries = append([]Entry{e}, f.Entries...)
	return s.save(f)
}

// PopFront removes and returns the oldest queued message.
func (s *Store) PopFront(agent string) (Entry, bool, error) {
	if s == nil {
		return Entry{}, false, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return Entry{}, false, err
	}
	if len(f.Entries) == 0 {
		return Entry{}, false, nil
	}
	e := f.Entries[0]
	f.Entries = f.Entries[1:]
	if err := s.save(f); err != nil {
		return Entry{}, false, err
	}
	return e, true, nil
}

// Snapshot returns an agent's queue in order, without mutating it.
func (s *Store) Snapshot(agent string) ([]Entry, error) {
	if s == nil {
		return nil, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return nil, err
	}
	return f.Entries, nil
}

// Depth is the number of messages waiting for an agent. An unreadable queue
// reports zero AND an error; callers that only want the number (a status
// line) may ignore the error, but none may read zero as "nothing waiting"
// while an error is set.
func (s *Store) Depth(agent string) (int, error) {
	entries, err := s.Snapshot(agent)
	return len(entries), err
}

// Clear drops an agent's whole queue. Used when a seat is removed: a backlog
// addressed to an agent that no longer exists has nowhere to be delivered,
// and is reported by the caller rather than kept forever.
func (s *Store) Clear(agent string) error {
	if s == nil {
		return fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.save(file{Agent: agent})
}

// Agents lists every agent with a queue on disk, sorted. This is what makes
// the backlog survive a restart in practice: the daemon does not have to
// remember who it was holding messages for.
func (s *Store) Agents() ([]string, error) {
	if s == nil {
		return nil, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.Durable() {
		names := make([]string, 0, len(s.mem))
		for name := range s.mem {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}
	ents, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sendq: list %s: %w", s.dir, err)
	}
	var names []string
	for _, ent := range ents {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !strings.HasSuffix(name, ".json") {
			continue // .tmp from an interrupted write
		}
		names = append(names, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(names)
	return names, nil
}

// Backlogs summarises every non-empty queue, oldest-first, so the fleet's
// held traffic is one read rather than a walk every caller reimplements.
func (s *Store) Backlogs() ([]Backlog, error) {
	names, err := s.Agents()
	if err != nil {
		return nil, err
	}
	var out []Backlog
	for _, name := range names {
		entries, err := s.Snapshot(name)
		if err != nil {
			return out, err
		}
		if len(entries) == 0 {
			continue
		}
		out = append(out, Backlog{Agent: name, Depth: len(entries), Oldest: entries[0].EnqueuedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Oldest.Before(out[j].Oldest) })
	return out, nil
}
