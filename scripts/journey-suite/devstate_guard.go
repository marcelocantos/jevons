// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// 🎯T1082: the journey suite never opens the development daemon's state.
//
// On 2026-10-20 (gate 252cf6ac) the development jevons.db gained a fresh
// -shm and an empty -wal at 09:09 while the development daemon was stopped.
// The opener was the isolate's own overseer: a live agent with a shell, told
// that development state is ~/.jevons and never told where its own was, ran
// `sqlite3 ~/.jevons/jevons.db "SELECT name FROM sqlite_master …"` (overseer
// transcript row 40, 22:09:18Z). Journeys need the real HOME for plan
// credentials, so no sandbox stops an agent's shell from reaching the
// development tree. This guard catches it three ways:
//
//   - agent tool calls: every tool_use row in the isolate stores (the primary
//     isolate and each brokered child, read before the child is deleted) is
//     scanned for a path into the development state dir. That is the opener
//     actually seen, and it is decidable whether or not the development
//     daemon is running.
//   - a database fingerprint: size, mtime and SHA-256 of jevons.db, and the
//     presence, size and mtime of its -wal and -shm, before and after. Only
//     meaningful while no development daemon holds the database, so it is
//     taken only when none does at the start and at the end.
//   - a holder poll: while the suite runs, any process descended from the
//     suite that holds the database open. That catches daemon code that
//     resolves the default state dir (a jevonsd given a missing config), which
//     holds the file for its life. A short-lived opener can slip between
//     polls; the transcript scan is what decides those.

// devStatePollEvery is the holder poll's cadence.
const devStatePollEvery = 500 * time.Millisecond

// devStateRefPattern matches a path into home's .jevons directory, however a
// shell command spells home, and not a sibling such as .jevons-worktrees.
func devStateRefPattern(home string) *regexp.Regexp {
	homes := []string{`~`, `\$HOME`, `\$\{HOME\}`}
	if home != "" {
		homes = append(homes, regexp.QuoteMeta(home))
	}
	return regexp.MustCompile(`(?:` + strings.Join(homes, "|") + `)/\.jevons(?:[/"'\s;|&)\\]|$)`)
}

// devStateTouch is one agent tool call that reached into the development state.
type devStateTouch struct {
	store, agent string
	idx          int
	excerpt      string
}

func (t devStateTouch) String() string {
	return fmt.Sprintf("%s: agent %s tool call #%d: %s", t.store, t.agent, t.idx, t.excerpt)
}

// scanDevStateRefs reads every tool_use row of the isolate store under
// stateDir and returns those that name a path in home's .jevons. A store
// that does not exist yet has no rows; any other read failure is an error.
func scanDevStateRefs(stateDir, home string) ([]devStateTouch, error) {
	path := statedb.DefaultPath(stateDir)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT agent, idx, body FROM transcript_events WHERE typ = 'tool_use' ORDER BY agent, idx")
	if err != nil {
		return nil, fmt.Errorf("scan %s tool calls: %w", stateDir, err)
	}
	defer rows.Close()
	re := devStateRefPattern(home)
	var out []devStateTouch
	for rows.Next() {
		var agent, body string
		var idx int
		if err := rows.Scan(&agent, &idx, &body); err != nil {
			return nil, err
		}
		loc := re.FindStringIndex(body)
		if loc == nil {
			continue
		}
		from := max(0, loc[0]-80)
		to := min(len(body), loc[1]+80)
		out = append(out, devStateTouch{store: stateDir, agent: agent, idx: idx, excerpt: body[from:to]})
	}
	return out, rows.Err()
}

// devFileStamp is one file's fingerprint. Hash is set for the database only.
type devFileStamp struct {
	exists bool
	size   int64
	mtime  time.Time
	hash   string
}

func stampDevFile(path string, hash bool) (devFileStamp, error) {
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return devFileStamp{}, nil
	}
	if err != nil {
		return devFileStamp{}, err
	}
	out := devFileStamp{exists: true, size: st.Size(), mtime: st.ModTime()}
	if hash {
		f, err := os.Open(path)
		if err != nil {
			return devFileStamp{}, err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return devFileStamp{}, err
		}
		out.hash = hex.EncodeToString(h.Sum(nil))
	}
	return out, nil
}

// devDBStamp fingerprints the development database and its WAL siblings.
type devDBStamp map[string]devFileStamp

func stampDevDB(db string) (devDBStamp, error) {
	out := devDBStamp{}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		st, err := stampDevFile(db+suffix, suffix == "")
		if err != nil {
			return nil, err
		}
		out[filepath.Base(db+suffix)] = st
	}
	return out, nil
}

// diffDevDB names every way after differs from before.
func diffDevDB(before, after devDBStamp) []string {
	var names []string
	for name := range before {
		names = append(names, name)
	}
	slices.Sort(names)
	var out []string
	for _, name := range names {
		b, a := before[name], after[name]
		switch {
		case b.exists != a.exists:
			out = append(out, fmt.Sprintf("%s exists %v → %v", name, b.exists, a.exists))
		case !b.exists:
		case b.size != a.size || !b.mtime.Equal(a.mtime) || b.hash != a.hash:
			out = append(out, fmt.Sprintf("%s size %d → %d, mtime %s → %s%s", name, b.size, a.size,
				b.mtime.Format(time.RFC3339Nano), a.mtime.Format(time.RFC3339Nano),
				map[bool]string{true: ", content changed", false: ""}[b.hash != a.hash]))
		}
	}
	return out
}

// devDBHolders lists the pids that hold db or its WAL siblings open.
func devDBHolders(db string) ([]int, error) {
	out, err := exec.Command("lsof", "-t", "--", db, db+"-wal", db+"-shm").Output()
	var exit *exec.ExitError
	if err != nil && !(errors.As(err, &exit) && len(out) == 0) {
		// lsof exits 1 when nothing holds any of the files.
		return nil, fmt.Errorf("lsof %s: %w", db, err)
	}
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(f); err == nil && !slices.Contains(pids, pid) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

// descendsFrom reports whether pid's ancestry reaches root.
func descendsFrom(pid, root int) bool {
	for range 64 {
		if pid == root {
			return true
		}
		if pid <= 1 {
			return false
		}
		out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return false
		}
		next, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			return false
		}
		pid = next
	}
	return false
}

func processCommand(pid int) string {
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "(exited)"
	}
	return trim(strings.TrimSpace(string(out)), 200)
}

// devStateGuard watches the development database for the length of a run.
type devStateGuard struct {
	home, db string
	suitePID int
	// before is nil when a development daemon held the database at the start.
	before    devDBStamp
	devAtOpen []int
	stop      chan struct{}
	done      chan struct{}
	// listening reports a development daemon on its port (a test seam).
	listening func() bool
	mu        sync.Mutex
	holders   []string
	pollErr   error
	touches   []devStateTouch
}

// developmentListening reports whether something serves the development port.
func developmentListening() bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", config.DevelopmentPort), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// startDevStateGuard fingerprints the development database (when no daemon
// holds it) and starts the holder poll.
func startDevStateGuard(home string, suitePID int) (*devStateGuard, error) {
	return startDevStateGuardWith(home, suitePID, developmentListening)
}

func startDevStateGuardWith(home string, suitePID int, listening func() bool) (*devStateGuard, error) {
	g := &devStateGuard{
		home: home, db: statedb.DefaultPath(filepath.Join(home, ".jevons")), suitePID: suitePID,
		stop: make(chan struct{}), done: make(chan struct{}), listening: listening,
	}
	held, err := devDBHolders(g.db)
	if err != nil {
		return nil, err
	}
	g.devAtOpen = held
	if len(held) == 0 && !g.listening() {
		if g.before, err = stampDevDB(g.db); err != nil {
			return nil, err
		}
	}
	go g.poll()
	return g, nil
}

func (g *devStateGuard) poll() {
	defer close(g.done)
	seen := map[int]bool{}
	for {
		select {
		case <-g.stop:
			return
		case <-time.After(devStatePollEvery):
		}
		pids, err := devDBHolders(g.db)
		if err != nil {
			g.mu.Lock()
			g.pollErr = err
			g.mu.Unlock()
			continue
		}
		for _, pid := range pids {
			if seen[pid] || slices.Contains(g.devAtOpen, pid) {
				continue
			}
			seen[pid] = true
			if !descendsFrom(pid, g.suitePID) {
				continue
			}
			g.mu.Lock()
			g.holders = append(g.holders, fmt.Sprintf("pid %d (%s)", pid, processCommand(pid)))
			g.mu.Unlock()
		}
	}
}

// noteStore scans one isolate store before it is deleted.
func (g *devStateGuard) noteStore(stateDir string) error {
	if g == nil {
		return nil
	}
	touches, err := scanDevStateRefs(stateDir, g.home)
	g.mu.Lock()
	g.touches = append(g.touches, touches...)
	g.mu.Unlock()
	return err
}

// finish stops the poll and returns an error naming every way the run
// reached the development state. Call it after the isolate has stopped.
func (g *devStateGuard) finish(primaryStateDir string) error {
	if g == nil {
		return nil
	}
	close(g.stop)
	<-g.done
	errs := []error{g.noteStore(primaryStateDir)}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, t := range g.touches {
		errs = append(errs, fmt.Errorf("🎯T1082 an isolate agent reached into the development state: %s", t))
	}
	for _, h := range g.holders {
		errs = append(errs, fmt.Errorf("🎯T1082 a process started by this suite held the development %s open: %s", g.db, h))
	}
	if g.pollErr != nil {
		errs = append(errs, fmt.Errorf("🎯T1082 holder poll: %w", g.pollErr))
	}
	if g.before == nil {
		fmt.Printf("NOTE 🎯T1082: a development daemon held %s at the start (pids %v); its fingerprint is not compared, the tool-call scan and holder poll still decide\n", g.db, g.devAtOpen)
		return errors.Join(errs...)
	}
	pids, err := devDBHolders(g.db)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	// A holder this suite started is the defect, not a development daemon.
	var held []int
	for _, pid := range pids {
		if !descendsFrom(pid, g.suitePID) {
			held = append(held, pid)
		}
	}
	if len(held) > 0 || g.listening() {
		fmt.Printf("NOTE 🎯T1082: a development daemon started during the run (holders %v); %s's fingerprint is not compared\n", held, g.db)
		return errors.Join(errs...)
	}
	after, err := stampDevDB(g.db)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	if diff := diffDevDB(g.before, after); len(diff) > 0 {
		errs = append(errs, fmt.Errorf("🎯T1082 the development database was opened or modified during the suite: %s", strings.Join(diff, "; ")))
	}
	return errors.Join(errs...)
}
