// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// supervisor-pass runs one unattended supervision tick against a fixture and
// prints the terminal pass line (🎯T700 oracle). Escalation goes through the
// development daemon's /api/supervisor/escalate, not seat Bash or blurter MCP.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/supervisormem"
)

func main() {
	os.Exit(run())
}

func run() int {
	daemon := flag.String("daemon", "http://127.0.0.1:13705", "jevonsd base URL")
	fixture := flag.String("fixture", "", "JSON wake fixture path; empty uses wake-worthy T700 default")
	quietOnly := flag.Bool("quiet-only", false, "healthy pass with nothing wake-worthy")
	flag.Parse()

	var fix supervisormem.WakeFixture
	if *quietOnly {
		fix.WakeWorthy = false
	} else if strings.TrimSpace(*fixture) == "" {
		fix = supervisormem.DefaultWakeFixture(time.Now().UTC())
	} else {
		b, err := os.ReadFile(*fixture)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read fixture: %v\n", err)
			return 1
		}
		if err := json.Unmarshal(b, &fix); err != nil {
			fmt.Fprintf(os.Stderr, "parse fixture: %v\n", err)
			return 1
		}
	}

	out, err := supervisormem.RunUnattendedPass(*daemon, fix, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pass: %v\n", err)
		return 1
	}
	fmt.Println(out.Line)
	if out.Muzzled {
		return 2
	}
	return 0
}
