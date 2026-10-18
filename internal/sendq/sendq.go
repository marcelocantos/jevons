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
// WHAT IS PERSISTED: the payload, acceptance time, and any unresolved delivery
// attempt. A claim remains on disk while the provider is called; daemon death
// leaves it held for reconciliation, never silently removed or replayed.
// Not persisted: whether a turn was in flight when it was
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
	// RequestID is the immutable host-issued identity of an admitted owner question.
	// Unlike ID (queue acceptance) and AttemptID (delivery try), it survives
	// retries, daemon restarts and digest archival without being reminted.
	RequestID string `json:"request_id,omitempty"`
	// TransportRequestID and SessionID are written together before an
	// identified handoff. Empty means no durable receipt binding exists;
	// legacy holds must never infer one from the current seat or Entry.ID.
	TransportRequestID string `json:"transport_request_id,omitempty"`
	SessionID          string `json:"session_id,omitempty"`
	// Text is the payload exactly as accepted. Never truncated: this is the
	// message, not a record of it.
	Text string `json:"text"`
	// WireText freezes any transport decoration at first receipt binding.
	// Text remains the original admission used for duplicate detection.
	// The presence flag distinguishes an explicitly empty wire payload from
	// older bindings, whose wire payload is the original Text.
	WireText    string `json:"wire_text,omitempty"`
	WireTextSet bool   `json:"wire_text_set,omitempty"`
	// EnqueuedAt is when the daemon accepted it. The age of the oldest entry
	// is the number that says whether a queue is waiting or stalled.
	EnqueuedAt time.Time `json:"enqueued_at"`
	// Empty state is the legacy pending representation. Attempting/uncertain
	// entries remain on disk but cannot be claimed by an automatic retry.
	State     DeliveryState `json:"state,omitempty"`
	AttemptID string        `json:"attempt_id,omitempty"`
	Detail    string        `json:"detail,omitempty"`
	// Reconciled is the operator judgement that resolved an uncertain
	// delivery, attributed and evidenced (🎯T726). It rides an entry that
	// survived reconciliation — requeued, or kept as the authoritative
	// message of a consolidation — so the next reader of this file learns
	// who decided and why without a git archaeology expedition.
	Reconciled *Reconciliation `json:"reconciled,omitempty"`
	Directive  *Directive      `json:"directive,omitempty"`
	// Members are the comma-joined ids of the original entries folded into this digest
	// (🎯T774). The originals live in the digest archive, readable by id.
	Members string `json:"members,omitempty"`
}

type DeliveryState string

const (
	// Pending is the empty on-disk state. EntryPhase reads it as admitted,
	// digest, or requeued. Those three are the one claimable pending.
	Pending    DeliveryState = ""
	Attempting DeliveryState = "attempting"
	Uncertain  DeliveryState = "uncertain"
)

// AttemptOutcome records what the caller established, not what a transport
// return code happens to imply. Only DefinitelyNotSent permits another send.
type AttemptOutcome string

const (
	Confirmed         AttemptOutcome = "confirmed"
	DefinitelyNotSent AttemptOutcome = "not_sent"
	Unverified        AttemptOutcome = "unverified"
	// TerminalUndelivered requires a sender-visible recorded disposition.
	TerminalUndelivered AttemptOutcome = "terminal_undelivered"
)

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
	// EntryIDs freezes the observed cohort for automatic cleanup. A later
	// arrival must not replace an entry another drain has since removed.
	EntryIDs []string
	// Uncertain counts entries that must be reconciled rather than replayed:
	// an uncertain delivery, or an attempt left by a dead daemon. An attempt
	// this store owns is not one of those. It is InFlight, and a sweep must
	// not treat that queue as idle work either.
	Uncertain int
	// InFlight counts attempts this store owns. It is not an uncertain receipt.
	InFlight int
	// BlockReason is why the queue is not only waiting. Empty means every
	// entry is claimable pending. It is depth's companion, not a second queue.
	BlockReason string
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
	var msg string
	if b.Uncertain > 0 {
		msg = fmt.Sprintf("%s: %d held, %d delivery outcome(s) uncertain, oldest waiting %s",
			b.Agent, b.Depth, b.Uncertain, b.OldestAge(now).Round(time.Second))
	} else {
		msg = fmt.Sprintf("%s: %d queued, oldest waiting %s",
			b.Agent, b.Depth, b.OldestAge(now).Round(time.Second))
	}
	if b.BlockReason != "" {
		msg += "; block: " + b.BlockReason
	}
	return msg
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
	observe func(string, int)

	mu  sync.Mutex
	dir string
	// mem holds the queues when there is no directory to hold them. The
	// storeless daemon and the hermetic test share the durable store's code
	// down to the last method; only where a record lands differs.
	mem map[string][]Entry
	// Ownership exists only in this process. A reopened store has no active
	// owner for an attempting entry left by the preceding daemon.
	active map[string]string
	// memArchive is the digest archive of a memory-backed store (🎯T774).
	memArchive       map[string]archiveFile
	memSupersessions map[string]Supersession
}

// NewStore roots a store at dir (conventionally <state_dir>/sendq). An empty
// dir is a memory-backed store: same behaviour, no durability.
func NewStore(dir string) *Store { return &Store{dir: strings.TrimSpace(dir)} }

// SetObserver installs the queue's observation feed. It runs under the queue
// lock and must not call back into the store.
func (s *Store) SetObserver(observe func(string, int)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observe = observe
}

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
func (s *Store) load(agent string) (out file, err error) {
	defer func() {
		if err == nil && s.observe != nil {
			s.observe(agent, len(out.Entries))
		}
	}()
	if !s.Durable() {
		name := strings.TrimSpace(agent)
		if name == "" {
			return file{}, fmt.Errorf("sendq: agent name is required")
		}
		return file{Agent: name, Entries: append([]Entry(nil), s.mem[name]...)}, nil
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
	for _, e := range f.Entries {
		if err := e.validateReceiptBinding(); err != nil {
			return file{}, fmt.Errorf("sendq: invalid receipt binding for %q: %w", agent, err)
		}
		if e.Directive != nil {
			if err := e.Directive.Validate(); err != nil {
				return file{}, err
			}
		}
		if e.State != Pending && e.State != Attempting && e.State != Uncertain {
			return file{}, fmt.Errorf("sendq: unknown delivery state %q for %q", e.State, agent)
		}
		if e.State != Pending && (e.ID == "" || e.AttemptID == "") {
			return file{}, fmt.Errorf("sendq: incomplete delivery attempt for %q", agent)
		}
	}
	f.Agent = agent
	return f, nil
}

// save writes an agent's queue with atomic write-and-rename, removing the
// record entirely when the queue is empty so an idle fleet does not leave a
// directory of empty files for the age sweep to walk.
func (s *Store) save(f file) (err error) {
	defer func() {
		if err == nil && s.observe != nil {
			s.observe(f.Agent, len(f.Entries))
		}
	}()
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

// DuplicateRequestIDError means this host identity already has an unresolved
// queue entry. The caller must inspect/reconcile that entry, not interpret a
// second enqueue as a new delivery. Conflict also records a payload mismatch;
// question scope is owned by the host ledger, not represented by this queue.
type DuplicateRequestIDError struct {
	RequestID string
	Existing  Entry
	Conflict  bool
}

func (e *DuplicateRequestIDError) Error() string {
	if e.Conflict {
		return fmt.Sprintf("sendq: request_id %q conflicts with entry %s (different text)", e.RequestID, e.Existing.ID)
	}
	return fmt.Sprintf("sendq: request_id %q already held by entry %s (state %s)", e.RequestID, e.Existing.ID, e.Existing.State)
}

// duplicateRequestID is called while Store.mu is held after load, before any
// supersession or append. Empty IDs are legacy traffic and never deduplicated.
func duplicateRequestID(entries []Entry, text, requestID string) error {
	if requestID == "" {
		return nil
	}
	for _, e := range entries {
		if e.RequestID == requestID {
			return &DuplicateRequestIDError{RequestID: requestID, Existing: e, Conflict: e.Text != text}
		}
	}
	return nil
}

// Append adds a message to the back of an agent's queue and returns the
// entry as written together with the resulting depth.
func (s *Store) Append(agent, text string, at time.Time) (Entry, int, error) {
	return s.AppendWithRequestID(agent, text, "", at)
}

// AppendWithRequestID holds an admitted question with the ID issued at host
// intake. Empty means a legacy/non-owner message; the queue never mints one.
func (s *Store) AppendWithRequestID(agent, text, requestID string, at time.Time) (Entry, int, error) {
	if s == nil {
		return Entry{}, 0, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return Entry{}, 0, err
	}
	if err := duplicateRequestID(f.Entries, text, requestID); err != nil {
		return Entry{}, 0, err
	}
	e := Entry{ID: NewID(), RequestID: requestID, Text: text, EnqueuedAt: at.UTC()}
	f.Entries = append(f.Entries, e)
	if err := s.save(f); err != nil {
		return Entry{}, 0, err
	}
	return e, len(f.Entries), nil
}

// AppendSuperseding is Append that first removes admitted entries for which
// supersedes reports true (🎯T821). The removed payloads are archived under
// the new entry's id before the queue changes. A digest and an operator
// requeue stay: they are not the admitted phase, even though their on-disk
// state is empty. Attempting and Uncertain entries are never touched: they
// may already be in the receiver's hands. The returned count is how many
// admitted entries the new one replaced.
func (s *Store) AppendSuperseding(agent, text string, at time.Time, supersedes func(Entry) bool) (Entry, int, int, error) {
	return s.AppendSupersedingWithRequestID(agent, text, "", at, supersedes)
}

// AppendSupersedingWithRequestID retains the incoming question identity and
// never silently replaces another admitted question (even if the predicate
// would otherwise match it). A previously receipt-bound entry also keeps its
// identity when returned to Pending.
func (s *Store) AppendSupersedingWithRequestID(agent, text, requestID string, at time.Time, supersedes func(Entry) bool) (Entry, int, int, error) {
	if s == nil {
		return Entry{}, 0, 0, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return Entry{}, 0, 0, err
	}
	if err := duplicateRequestID(f.Entries, text, requestID); err != nil {
		return Entry{}, 0, 0, err
	}
	kept := make([]Entry, 0, len(f.Entries)+1)
	var removed []Entry
	for _, old := range f.Entries {
		if EntryPhase(old, s.active[agent]) == PhaseAdmitted && old.RequestID == "" && old.TransportRequestID == "" && supersedes != nil && supersedes(old) {
			removed = append(removed, old)
			continue
		}
		kept = append(kept, old)
	}
	e := Entry{ID: NewID(), RequestID: requestID, Text: text, EnqueuedAt: at.UTC()}
	if len(removed) > 0 {
		if err := s.archiveSupersession(Supersession{
			Agent:   strings.TrimSpace(agent),
			At:      at.UTC(),
			HoldID:  e.ID,
			Removed: removed,
		}); err != nil {
			return Entry{}, 0, 0, err
		}
	}
	f.Entries = append(kept, e)
	if err := s.save(f); err != nil {
		return Entry{}, 0, 0, err
	}
	return e, len(f.Entries), len(removed), nil
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
	if len(f.Entries) > 0 && f.Entries[0].State == Attempting {
		return fmt.Errorf("sendq: cannot prepend past a delivery attempt in flight for %q", agent)
	}
	f.Entries = append([]Entry{e}, f.Entries...)
	return s.save(f)
}

// PopFront removes the oldest claimable entry and returns it on a receipt.
// The caller names who took it and what they observed. A blank actor or a
// blank evidence line removes nothing. An in-flight attempt and a queue of
// only unresolved attempts stay where they are. An empty queue is ok=false
// and a nil error. Reconcile rejects ReconcilePop: taking the payload is not
// a confirmed, requeued, or dropped delivery.
func (s *Store) PopFront(agent, actor, evidence string, now time.Time) (Receipt, bool, error) {
	if s == nil {
		return Receipt{}, false, fmt.Errorf("sendq: no store")
	}
	actor, evidence, err := validAttribution(actor, evidence)
	if err != nil {
		return Receipt{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return Receipt{}, false, err
	}
	if len(f.Entries) == 0 {
		return Receipt{}, false, nil
	}
	i, _, blocked := claimable(f.Entries, s.active[agent])
	if blocked {
		return Receipt{}, false, fmt.Errorf("sendq: %q has a delivery attempt in flight", agent)
	}
	if i < 0 {
		return Receipt{}, false, fmt.Errorf("sendq: %q holds only unresolved delivery attempts", agent)
	}
	e := f.Entries[i]
	f.Entries = append(f.Entries[:i:i], f.Entries[i+1:]...)
	if err := s.save(f); err != nil {
		return Receipt{}, false, err
	}
	rec := Receipt{
		Agent:   strings.TrimSpace(agent),
		By:      Reconciliation{Actor: actor, At: now.UTC(), Outcome: ReconcilePop, Evidence: evidence},
		Removed: []Entry{e},
		Depth:   len(f.Entries),
	}
	return rec, true, nil
}

// claimable finds the entry a drain may take next (🎯T766.5).
//
// An Uncertain entry is one whose delivery nobody can establish. Until
// 2026-09-21 it blocked everything behind it, so one ambiguous send froze a
// seat's whole queue until a human reconciled it — jevons-po held 104
// messages for seventeen hours behind one. The owner chose to let the queue
// flow past it instead: the entry stays exactly where it is, held and
// pinned for reconciliation, and is never offered again, while the entries
// behind it keep delivering. What does still block is an Attempting entry,
// because that is a send in flight (or one a dead daemon left mid-submit),
// and a second concurrent attempt to one seat is how deliveries interleave.
//
// Cost, accepted deliberately: messages behind an Uncertain entry can reach
// the seat before it, so a seat's queue is FIFO among deliverable entries,
// not absolutely.
//
// i is the index of the first Pending entry, or -1. blocker is the entry that
// stops the scan, when there is one.
//
// Only an attempt this store is running blocks — active is its attempt id
// for the agent. An Attempting entry with any other id was left by a daemon
// that died mid-submit: nothing is in flight for it here, and before this
// check it froze the queue behind it exactly as an Uncertain entry used to
// (observed 2026-09-21 on jevons-po after a restart). It is held the same
// way: never offered again, and flowed past.
func claimable(entries []Entry, active string) (i int, blocker Entry, blocked bool) {
	// Reconciliation can requeue an older hold ahead of an active send.
	// Ownership serializes the entire seat, not just the prefix before the
	// first Pending entry; check it before selecting the next obligation.
	for _, e := range entries {
		if EntryPhase(e, active) == PhaseInFlight {
			return -1, e, true
		}
	}
	for idx, e := range entries {
		if claimablePhase(EntryPhase(e, active)) {
			return idx, Entry{}, false
		}
	}
	return -1, Entry{}, false
}

// ClaimFront durably retains the oldest entry while granting one attempt.
// Another drain, including one after restart, cannot repeat an unresolved send.
// ok=false with a nonempty entry means delivery needs reconciliation.
func (s *Store) ClaimFront(agent string) (Entry, bool, error) {
	return s.claimFront(agent, "", false)
}

// ClaimFrontID claims only an observed entry, so stale cleanup cannot consume
// a replacement which arrived after its backlog snapshot.
func (s *Store) ClaimFrontID(agent, id string) (Entry, bool, error) {
	return s.claimFront(agent, id, true)
}

func (s *Store) claimFront(agent, id string, matchID bool) (Entry, bool, error) {
	if s == nil {
		return Entry{}, false, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil || len(f.Entries) == 0 {
		return Entry{}, false, err
	}
	i, blocker, blocked := claimable(f.Entries, s.active[agent])
	if blocked {
		return blocker, false, nil
	}
	if i < 0 {
		return f.Entries[0], false, nil // only held entries: report, never claim
	}
	e := f.Entries[i]
	if matchID && e.ID != id {
		return e, false, nil
	}
	if e.ID == "" {
		e.ID = NewID()
	}
	e, _, err = applyDelivery(e, stepClaim, "", NewID(), nil)
	if err != nil {
		return Entry{}, false, err
	}
	f.Entries[i] = e
	if err := s.save(f); err != nil {
		return Entry{}, false, err
	}
	if s.active == nil {
		s.active = map[string]string{}
	}
	s.active[agent] = e.AttemptID
	return e, true, nil
}

// BlockedHead atomically reads an unresolved attempt and its local ownership.
// A healthy in-progress operation is not an orphan requiring reconciliation.
func (s *Store) BlockedHead(agent string) (Entry, bool, error) {
	if s == nil {
		return Entry{}, false, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil || len(f.Entries) == 0 {
		return Entry{}, false, err
	}
	for _, e := range f.Entries {
		phase := EntryPhase(e, s.active[agent])
		if claimablePhase(phase) || phase == PhaseInFlight {
			continue
		}
		return e, true, nil
	}
	return Entry{}, false, nil
}

// Resolve records one attempt's outcome. Unverified retains the payload without
// offering it again. Confirmed and TerminalUndelivered leave the queue only
// after that entry's payload is archived under its own id. DefinitelyNotSent
// puts the same entry back and archives nothing. Both entry and attempt IDs
// guard late results.
func (s *Store) Resolve(agent string, attempt Entry, outcome AttemptOutcome, detail string) error {
	if s == nil {
		return fmt.Errorf("sendq: no store")
	}
	if outcome != Confirmed && outcome != DefinitelyNotSent && outcome != Unverified && outcome != TerminalUndelivered {
		return fmt.Errorf("sendq: invalid attempt outcome %q", outcome)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Even a failed persistence attempt ends this operation's ownership. Its
	// durable claim remains unresolved and must become visible to recovery.
	if s.active[agent] == attempt.AttemptID {
		delete(s.active, agent)
	}
	f, err := s.load(agent)
	if err != nil {
		return err
	}
	at := -1
	if attempt.ID != "" && attempt.AttemptID != "" {
		for idx, e := range f.Entries {
			if e.ID == attempt.ID {
				at = idx
				break
			}
		}
	}
	if at < 0 || f.Entries[at].AttemptID != attempt.AttemptID || f.Entries[at].State == Pending {
		return fmt.Errorf("sendq: stale delivery attempt for %q", agent)
	}
	e := f.Entries[at]
	var remove bool
	switch outcome {
	case Confirmed:
		_, remove, err = applyDelivery(e, stepConfirm, detail, "", nil)
	case TerminalUndelivered:
		_, remove, err = applyDelivery(e, stepTerminal, detail, "", nil)
	case Unverified:
		e, remove, err = applyDelivery(e, stepUnverified, detail, "", nil)
	case DefinitelyNotSent:
		e, remove, err = applyDelivery(e, stepNotSent, detail, "", nil)
	}
	if err != nil {
		return err
	}
	if remove {
		// e is the on-disk entry, text included. Archive it before the
		// removal is saved. A failed archive leaves the queue unchanged.
		if err := s.archiveSupersession(Supersession{
			Agent:   strings.TrimSpace(agent),
			At:      time.Now().UTC(),
			HoldID:  e.ID,
			Removed: []Entry{e},
		}); err != nil {
			return err
		}
		f.Entries = append(f.Entries[:at:at], f.Entries[at+1:]...)
	} else {
		f.Entries[at] = e
	}
	return s.save(f)
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

// DischargePending removes copies positively identified in receiver records
// (🎯T766.5.1). Each removed payload is archived under its own id before the
// queue changes. An operator requeue shares the empty on-disk state, so a
// matched requeued copy is discharged too, and the archive is what keeps
// that text. An Attempting or Uncertain delivery may have reached the
// receiver and must be reconciled, not discharged. Pending digests are
// unfolded from their archived originals before matching, so a receipt for
// one member cannot discard unrelated accepted messages or replay that
// member inside the digest on the next drain. Receipt-bound entries retain
// their exact identity; this legacy matcher must neither settle them nor
// unfold a previously bound digest into new inputs.
func (s *Store) DischargePending(agent string, matches func(Entry) bool) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return 0, err
	}
	kept := make([]Entry, 0, len(f.Entries))
	var removedEntries []Entry
	for _, e := range f.Entries {
		if e.State != Pending || e.TransportRequestID != "" {
			kept = append(kept, e)
			continue
		}
		if e.Members != "" {
			members, ok := s.archivedMembers(e.ID)
			if !ok {
				return 0, fmt.Errorf("sendq: cannot discharge pending digest %s without its archived members", e.ID)
			}
			survivors := make([]Entry, 0, len(members))
			for _, m := range members {
				if matches(m) {
					removedEntries = append(removedEntries, m)
				} else {
					survivors = append(survivors, m)
				}
			}
			if len(survivors) == len(members) {
				kept = append(kept, e)
			} else {
				kept = append(kept, survivors...)
			}
			continue
		}
		if matches(e) {
			removedEntries = append(removedEntries, e)
			continue
		}
		kept = append(kept, e)
	}
	if len(removedEntries) == 0 {
		return 0, nil
	}
	for _, e := range removedEntries {
		if err := s.archiveSupersession(Supersession{
			Agent:   strings.TrimSpace(agent),
			At:      time.Now().UTC(),
			HoldID:  e.ID,
			Removed: []Entry{e},
		}); err != nil {
			return 0, err
		}
	}
	f.Entries = kept
	if err := s.save(f); err != nil {
		return 0, err
	}
	return len(removedEntries), nil
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
	_, err := s.Discard(agent)
	return err
}

// Discard is an explicit authority override. Each removed payload is archived
// under its own id before the queue is emptied. A failed archive leaves the
// queue unchanged. Return the exact removed entries under the same lock so
// the caller can record IDs and uncertain dispositions. Automatic
// drains/reapers must use ClaimFront/Resolve instead.
func (s *Store) Discard(agent string) ([]Entry, error) {
	if s == nil {
		return nil, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return nil, err
	}
	for _, e := range f.Entries {
		if err := s.archiveSupersession(Supersession{
			Agent:   strings.TrimSpace(agent),
			At:      time.Now().UTC(),
			HoldID:  e.ID,
			Removed: []Entry{e},
		}); err != nil {
			return nil, err
		}
	}
	if err := s.save(file{Agent: agent}); err != nil {
		return nil, err
	}
	delete(s.active, agent)
	return f.Entries, nil
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
		b, err := s.Backlog(name)
		if err != nil {
			return out, err
		}
		if b.Depth == 0 {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Oldest.Before(out[j].Oldest) })
	return out, nil
}

// Backlog reports one agent's current cohort, including an empty queue. Recovery
// uses it after receipt queries, which can race later arrivals and dispositions.
func (s *Store) Backlog(agent string) (Backlog, error) {
	if s == nil {
		return Backlog{}, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	b := Backlog{Agent: agent, Depth: len(f.Entries)}
	if err != nil || len(f.Entries) == 0 {
		return b, err
	}
	b.Oldest = f.Entries[0].EnqueuedAt
	active := s.active[agent]
	for _, e := range f.Entries {
		b.EntryIDs = append(b.EntryIDs, e.ID)
		phase := EntryPhase(e, active)
		switch phase {
		case PhaseInFlight:
			b.InFlight++
		case PhaseUncertain, PhaseHeldAttempt:
			b.Uncertain++
		default:
			if e.State != Pending {
				b.Uncertain++
			}
		}
		if b.BlockReason == "" {
			b.BlockReason = blockReason(phase)
		}
	}
	return b, nil
}
