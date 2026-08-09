// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/marcelocantos/jevons/internal/harnessusage"
)

// runSpend prints the 🎯T392 spend decomposition for a window.
//
//	go run ./cmd/harness-usage -spend -harness grok \
//	  -since 2026-08-08T01:53:00Z -until 2026-08-09T11:53:00Z
//
// Reported in tokens, not dollars, on purpose: a subscription plan is
// decremented in tokens, and the provider's own USD field is an estimate
// against a rate card we do not control (🎯T394).
func runSpend(cargs *harnessusage.CollectArgs, harness, since, until string, jsonOut bool) int {
	args := harnessusage.SpendArgs{
		Collect:        *cargs,
		AgentBySession: agentsBySession(),
		Coordinators:   harnessusage.BaselineCoordinators,
	}
	if since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harness-usage: -since: %v\n", err)
			return 2
		}
		args.Since = t
	}
	if until != "" {
		t, err := time.Parse(time.RFC3339, until)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harness-usage: -until: %v\n", err)
			return 2
		}
		args.Until = t
	}
	if harness != "" && harness != "all" && harness != "*" {
		h, err := harnessusage.ParseHarness(harness)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harness-usage: %v\n", err)
			return 2
		}
		args.Harnesses = []harnessusage.Harness{h}
	}

	rep, err := harnessusage.CollectSpend(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness-usage: %v\n", err)
		return 1
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(os.Stderr, "harness-usage: encode: %v\n", err)
			return 1
		}
		return 0
	}

	m := func(n int64) string { return fmt.Sprintf("%.1fM", float64(n)/1e6) }
	fmt.Printf("window   %s .. %s\n", rep.Since.Format(time.RFC3339), rep.Until.Format(time.RFC3339))
	fmt.Printf("identity %s input = %d turns x %.1f calls/turn x %.0fk context\n",
		m(rep.Input), rep.Turns, float64(rep.ModelCalls)/max64(float64(rep.Turns), 1), rep.MeanContext/1000)
	fmt.Printf("output   %s (%.1f%% of input — you are billed for re-reading, not producing)\n",
		m(rep.Output), 100*float64(rep.Output)/max64(float64(rep.Input), 1))
	fmt.Printf("split    coordinator %s (%.1f%%)  implementer %s  unattributed %s\n",
		m(rep.CoordinatorInput), 100*rep.CoordinatorShare(),
		m(rep.ImplementerInput), m(rep.UnattributedInput))
	fmt.Printf("wasted   %d cancelled turns, %s (%.1f%%)\n",
		rep.CancelledTurns, m(rep.CancelledInput), 100*rep.CancelledShare())
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tROLE\tSESSIONS\tTURNS\tCALLS\tINPUT\tSHARE\tCTX/CALL")
	for _, a := range rep.ByAgent {
		name, role := a.Agent, "worker"
		if name == "" {
			name, role = "(unattributed)", "-"
		} else if a.Coordinator {
			role = "coordinator"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%s\t%.1f%%\t%.0fk\n",
			name, role, a.Sessions, a.Turns, a.ModelCalls, m(a.Input),
			100*float64(a.Input)/max64(float64(rep.Input), 1), a.MeanContext/1000)
	}
	_ = w.Flush()
	for _, n := range rep.Notes {
		fmt.Fprintln(os.Stderr, "note: "+n)
	}
	return 0
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// agentsBySession maps provider session ids to the agent that owned them.
//
// The registry only holds the session an agent is on *now*, so a window
// that predates a rotation attributes almost nothing — the first run of
// this report over the frozen baseline came back 100% unattributed for
// exactly that reason. The daemon logs are the historical record: every
// launch, migration and rehydrate writes the name beside the session id.
// Both sources are merged, registry last, so a live row wins.
//
// Attribution is best-effort by construction: logs rotate, and a window
// older than the retained logs will carry unattributed spend. That is
// reported as its own bucket rather than being spread across the named
// agents, which would fabricate a split.
func agentsBySession() map[string]string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	state := filepath.Join(home, ".jevons")
	m := map[string]string{}

	for _, log := range []string{"daily-jevonsd.log", "jevonsd.log"} {
		harvestSessionNames(filepath.Join(state, log), m)
	}

	data, err := os.ReadFile(filepath.Join(state, "agents.json"))
	if err != nil {
		return m
	}
	var defs []struct {
		Name      string `json:"name"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(data, &defs); err != nil {
		return m
	}
	for _, d := range defs {
		if strings.TrimSpace(d.SessionID) != "" {
			m[d.SessionID] = d.Name
		}
	}
	return m
}

// sessionNamePair matches the daemon's structured log in either order:
// `name=jevons session=<uuid>` and `session_id=<uuid> ... name=<agent>`.
var sessionNamePair = []*regexp.Regexp{
	regexp.MustCompile(`name=([A-Za-z0-9._-]+)[^\n]{0,200}?\bsession(?:_id)?=([0-9a-fA-F-]{36})`),
	regexp.MustCompile(`\bsession(?:_id)?=([0-9a-fA-F-]{36})[^\n]{0,200}?name=([A-Za-z0-9._-]+)`),
}

func harvestSessionNames(path string, into map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		for i, re := range sessionNamePair {
			for _, g := range re.FindAllStringSubmatch(line, -1) {
				name, sid := g[1], g[2]
				if i == 1 {
					name, sid = g[2], g[1]
				}
				// First writer wins: the launch line that created the
				// session is more trustworthy than a later mention.
				if _, seen := into[sid]; !seen {
					into[sid] = name
				}
			}
		}
	}
}
