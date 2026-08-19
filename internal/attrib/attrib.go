// Package attrib records which agent touched which path in the shared clone,
// so a stopped worker's unfinished work is attributable and recoverable rather
// than an anonymous pile (🎯T466).
//
// The 2026-08-15 host RAM stand-down is the reference case. Stopping the fleet
// worked; un-stopping it did not, because the tree the twelve stopped workers
// left behind — 119 modified paths and 52 untracked files — carried no record
// of who wrote what. Resuming them races them into each other's edits and
// discarding is unthinkable, so the pile simply sat there.
//
// The feed already existed and was being thrown away. Claude Code's PreToolUse
// and PostToolUse hooks fire on every mutating tool call with the session id and
// the file path attached, and internal/treeguard has been consuming them since
// 🎯T376 — but only to police a handful of guarded hot files, discarding every
// other path it saw. This package keeps that record instead.
//
// Two properties shape the storage:
//
//   - One file per session, never a shared one. A fix for a shared-mutable-state
//     bug that stored its records in a shared mutable file would reproduce the
//     bug it exists to catch; internal/treeguard/store.go set that precedent and
//     this follows it.
//   - The agent name is stamped at record time, not resolved at read time. A
//     session id can rotate under a worker (🎯T425), and the registry only holds
//     the current one, so records keyed on session alone go anonymous the moment
//     a worker is re-minted — exactly the failure this package exists to end.
//     Read-time roster resolution stays as the fallback for records written
//     before the name was knowable.
package attrib

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Via records what evidence put a touch into the record. It travels with every
// record because the backfill of a pile that predates the feed cannot be as
// certain as the feed itself, and an index that flattens the difference invites
// its reader to trust a guess as much as an observation.
const (
	// ViaHook is the live feed: a mutating tool call observed as it happened.
	ViaHook = "hook"
	// ViaDrain is a path found staged in the shared index when an agent stopped.
	ViaDrain = "index-drain"
	// ViaTreeguard is backfilled from a treeguard session observation. Direct
	// and non-transcript, but only ever covers the guarded hot files.
	ViaTreeguard = "backfill:treeguard"
	// ViaTranscript is backfilled from a session transcript's tool calls. The
	// only complete evidence for a pile that predates the feed. Read once, by
	// the backfill, never by the operator — the operator reads the index.
	ViaTranscript = "backfill:transcript"
	// ViaMtime is backfilled from a file mtime falling inside one agent's
	// lifetime and no other's. Weakest class; a coincidence of timing, not a
	// record of authorship.
	ViaMtime = "backfill:mtime"
)

// Record is one observed touch of one repo-relative path by one session.
type Record struct {
	Session string    `json:"session"`
	Agent   string    `json:"agent,omitempty"`
	Path    string    `json:"path"`
	Tool    string    `json:"tool,omitempty"`
	At      time.Time `json:"at"`
	Via     string    `json:"via"`
}

// StoreDirEnv overrides the record root (tests, isolates).
const StoreDirEnv = "JEVONS_ATTRIB_DIR"

// DefaultRoot is ~/.jevons/attrib unless StoreDirEnv is set.
func DefaultRoot() string {
	if dir := os.Getenv(StoreDirEnv); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "jevons-attrib")
	}
	return filepath.Join(home, ".jevons", "attrib")
}

// Store is the on-disk record of touches.
type Store struct {
	Root string
}

// touchesFile is the per-session append-only log.
const touchesFile = "touches.jsonl"

func (s *Store) sessionPath(session string) string {
	return filepath.Join(s.Root, "sessions", sanitize(session), touchesFile)
}

// Append adds records for one session. Callers batch a tool call's writes into
// one Append so a single call is one fsync rather than one per path.
//
// Appending in O_APPEND mode is what keeps concurrent workers safe without a
// lock: each session writes only its own file, and even a session that somehow
// ran twice would interleave whole lines rather than corrupt each other's.
func (s *Store) Append(session string, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	path := s.sessionPath(session)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	// A crash can tear the previous append mid-line, leaving no trailing
	// newline; appending straight after would merge this record into the torn
	// line and lose BOTH. One healing newline turns that into "one torn line
	// skipped by Load", which is the degradation the format promises.
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, info.Size()-1); err == nil && last[0] != '\n' {
			if _, err := f.Write([]byte("\n")); err != nil {
				return err
			}
		}
	}
	enc := json.NewEncoder(f)
	for _, r := range records {
		if r.Session == "" {
			r.Session = session
		}
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

// Load reads every recorded touch, from every session.
//
// A malformed line is skipped rather than failing the read. The alternative —
// a hard error, as durable state elsewhere in this daemon rightly uses — would
// mean one torn write during a crash makes the whole pile unattributable again,
// which is the outage this package exists to prevent. Skipping degrades to
// "that touch is missing"; failing degrades to "every touch is missing".
func (s *Store) Load() ([]Record, error) {
	dir := filepath.Join(s.Root, "sessions")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		f, err := os.Open(filepath.Join(dir, e.Name(), touchesFile))
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var r Record
			if err := json.Unmarshal([]byte(line), &r); err != nil {
				continue
			}
			if r.Path == "" {
				continue
			}
			out = append(out, r)
		}
		f.Close()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// RosterEntry maps an agent name to the session it was running.
type RosterEntry struct {
	Name      string `json:"name"`
	SessionID string `json:"session_id"`
	Workdir   string `json:"workdir"`
	Purpose   string `json:"purpose"`
	Parent    string `json:"parent"`
	TargetID  string `json:"target_id"`
}

// Roster resolves session ids to agent names.
type Roster struct {
	bySession map[string]RosterEntry
}

// LoadRoster reads agent registries, later files losing to earlier ones on a
// conflict.
//
// More than one path is the point: ~/.jevons/agents.json holds only the CURRENT
// fleet, so an agent that was stopped and removed — precisely the agent whose
// leftovers need attributing — resolves to nothing there. The registry backups
// beside it still name it. A session absent from every roster is reported as
// unknown rather than guessed at.
func LoadRoster(paths ...string) (*Roster, error) {
	r := &Roster{bySession: map[string]RosterEntry{}}
	var firstErr error
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if firstErr == nil && !errors.Is(err, os.ErrNotExist) {
				firstErr = err
			}
			continue
		}
		var entries []RosterEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, e := range entries {
			if e.SessionID == "" || e.Name == "" {
				continue
			}
			if _, seen := r.bySession[e.SessionID]; !seen {
				r.bySession[e.SessionID] = e
			}
		}
	}
	if len(r.bySession) == 0 && firstErr != nil {
		return r, firstErr
	}
	return r, nil
}

// DefaultRosterPaths is the registry plus every backup beside it.
func DefaultRosterPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	base := filepath.Join(home, ".jevons")
	paths := []string{filepath.Join(base, "agents.json")}
	backups, _ := filepath.Glob(filepath.Join(base, "agents.json.bak*"))
	sort.Sort(sort.Reverse(sort.StringSlice(backups)))
	return append(paths, backups...)
}

// UnknownAgent names a session no roster accounts for. It is a real answer, not
// a placeholder: "some session that is not in any registry I have" is more
// useful to an operator than an invented owner.
const UnknownAgent = "unknown"

// Agent returns the agent name for a session.
func (r *Roster) Agent(session string) string {
	if r == nil {
		return UnknownAgent
	}
	if e, ok := r.bySession[session]; ok {
		return e.Name
	}
	return UnknownAgent
}

// Entry returns the full roster entry for a session.
func (r *Roster) Entry(session string) (RosterEntry, bool) {
	if r == nil {
		return RosterEntry{}, false
	}
	e, ok := r.bySession[session]
	return e, ok
}

// Sessions returns every session the roster knows, newest registry first.
func (r *Roster) Sessions() []string {
	if r == nil {
		return nil
	}
	out := make([]string, 0, len(r.bySession))
	for s := range r.bySession {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Resolve fills in the agent name on records that lack one, using the roster.
// A record that already carries a name keeps it: the name stamped when the
// touch happened outranks a session lookup made after a rotation.
func Resolve(records []Record, roster *Roster) []Record {
	out := make([]Record, len(records))
	for i, r := range records {
		if r.Agent == "" {
			r.Agent = roster.Agent(r.Session)
		}
		out[i] = r
	}
	return out
}

func sanitize(s string) string {
	if s == "" {
		return "unknown-session"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// RelPath maps an absolute path to a repo-relative slash path, reporting
// whether it lies inside the repo at all.
func RelPath(repoRoot, path string) (string, bool) {
	if path == "" || repoRoot == "" {
		return "", false
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(repoRoot, abs)
	}
	rel, err := filepath.Rel(filepath.Clean(repoRoot), filepath.Clean(abs))
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") || rel == "." {
		return "", false
	}
	return rel, true
}
