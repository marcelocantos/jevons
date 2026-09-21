// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marcelocantos/jevons/internal/muxwin"
	"github.com/marcelocantos/jevons/internal/statedb"
)

// seedThroughStore runs write (which appends seed lines to the agent's chatlog
// journal) and then installs exactly those appended lines into the statedb the
// daemon serves the React pane from (🎯T825). The daemon imports a journal
// once, at boot, and only while the agent has no canonical state; a journal
// appended after boot is never read, so a seed that only touches the JSONL
// paints an empty pane. Only the bytes write appended are folded, so rows the
// live daemon already stored are kept and the seed lands after them.
func seedThroughStore(stateDir, agent, journal string, write func() error) error {
	var before int64
	if fi, err := os.Stat(journal); err == nil {
		before = fi.Size()
	}
	if err := write(); err != nil {
		return err
	}
	f, err := os.Open(journal)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(before, io.SeekStart); err != nil {
		return err
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	var lines []string
	for _, ln := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	db, err := statedb.Open(statedb.DefaultPath(stateDir))
	if err != nil {
		return fmt.Errorf("open store for seed: %w", err)
	}
	defer db.Close()
	return installSeedRows(db, agent, lines)
}

// installSeedRows folds journal lines into transcript rows and appends them
// after the agent's existing rows in one revision-advancing transaction.
func installSeedRows(db *statedb.Store, agent string, lines []string) error {
	evs := muxwin.EventsFromLines(lines)
	if len(evs) == 0 {
		return fmt.Errorf("seed folded to no transcript events (%d lines)", len(lines))
	}
	n, err := db.N(agent)
	if err != nil {
		return err
	}
	rows := make([]statedb.Event, len(evs))
	for i, ev := range evs {
		rows[i] = statedb.Event{
			Index: n + i + 1,
			ID:    ev.ID,
			TS:    ev.TS,
			Type:  ev.Type,
			Kind:  int(ev.Kind),
			Body:  string(ev.Body),
		}
		if ev.ID == "" || strings.HasPrefix(ev.ID, "e:") {
			rows[i].ID = ""
		}
	}
	return db.Upsert(agent, rows)
}
