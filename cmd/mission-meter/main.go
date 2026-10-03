// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/marcelocantos/jevons/internal/missionmeter"
	"os"
	"time"
)

type paths []string

func (p *paths) String() string     { return fmt.Sprint([]string(*p)) }
func (p *paths) Set(s string) error { *p = append(*p, s); return nil }
func main() {
	var spool, events paths
	var from, to, workdir string
	flag.Var(&spool, "spool", "dated spool JSONL path (repeatable)")
	flag.Var(&events, "events", "lifecycle event JSONL path (repeatable)")
	flag.StringVar(&from, "from", "", "inclusive RFC3339 timestamp")
	flag.StringVar(&to, "to", "", "exclusive RFC3339 timestamp")
	flag.StringVar(&workdir, "workdir-prefix", "", "optional repository workdir prefix; requires lifecycle events")
	flag.Parse()
	if len(spool) == 0 && len(events) == 0 {
		fail("provide -spool and/or -events paths")
	}
	var w missionmeter.Window
	var err error
	if from != "" {
		w.From, err = time.Parse(time.RFC3339Nano, from)
		if err != nil {
			fail("invalid -from: " + err.Error())
		}
	}
	if to != "" {
		w.To, err = time.Parse(time.RFC3339Nano, to)
		if err != nil {
			fail("invalid -to: " + err.Error())
		}
	}
	if !w.From.IsZero() && !w.To.IsZero() && !w.From.Before(w.To) {
		fail("-from must precede -to")
	}
	if workdir != "" && len(events) == 0 {
		fail("-workdir-prefix requires -events")
	}
	report, err := missionmeter.ScanScoped(spool, events, w, workdir)
	if err != nil {
		fail(err.Error())
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fail(err.Error())
	}
}
func fail(s string) { fmt.Fprintln(os.Stderr, "mission-meter:", s); os.Exit(1) }
