// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// harness-usage prints on-demand usage reports for coding harnesses
// used by the Jevons fleet (🎯T163).
//
//	go run ./cmd/harness-usage              # all harnesses
//	go run ./cmd/harness-usage -harness grok
//	go run ./cmd/harness-usage -json
//
// Defaults read live state under $HOME (.claude, .grok, .codex, .cursor).
// Override with -root / -cursor-dashboard / -cursor-db for fixtures.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/marcelocantos/jevons/internal/harnessusage"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("harness-usage", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	harness := fs.String("harness", "all", "claude|grok|codex|cursor|all")
	jsonOut := fs.Bool("json", false, "emit JSON array of reports")
	tryLive := fs.Bool("try-live-api", false, "probe provider usage APIs when creds present (still falls back to local scrape)")
	cursorDash := fs.String("cursor-dashboard", "", "path to Cursor usage dashboard JSON export")
	cursorDB := fs.String("cursor-db", "", "path to Cursor ai-code-tracking.db")
	claudeRoot := fs.String("claude-root", "", "override Claude data root (~/.claude)")
	grokRoot := fs.String("grok-root", "", "override Grok data root (~/.grok)")
	codexRoot := fs.String("codex-root", "", "override Codex data root (~/.codex)")
	maxFiles := fs.Int("max-files", 0, "cap session files walked per harness (0=unlimited)")
	since := fs.String("since", "", "RFC3339 lower bound on event timestamps")
	spend := fs.Bool("spend", false, "fleet spend decomposition: turns x calls x context, coordinator split (🎯T392.6)")
	until := fs.String("until", "", "RFC3339 upper bound (spend mode; default now)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cargs := &harnessusage.CollectArgs{
		TryLiveAPI:          *tryLive,
		CursorDashboardJSON: *cursorDash,
		CursorTrackingDB:    *cursorDB,
		MaxFiles:            *maxFiles,
		Roots:               map[harnessusage.Harness]string{},
	}
	if *claudeRoot != "" {
		cargs.Roots[harnessusage.HarnessClaude] = *claudeRoot
	}
	if *grokRoot != "" {
		cargs.Roots[harnessusage.HarnessGrok] = *grokRoot
	}
	if *codexRoot != "" {
		cargs.Roots[harnessusage.HarnessCodex] = *codexRoot
	}
	if *since != "" {
		t, err := time.Parse(time.RFC3339, *since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harness-usage: -since: %v\n", err)
			return 2
		}
		cargs.Since = &t
	}

	h := strings.TrimSpace(strings.ToLower(*harness))
	if *spend {
		return runSpend(cargs, h, *since, *until, *jsonOut)
	}

	var reports []harnessusage.Report
	if h == "" || h == "all" || h == "*" {
		reports = harnessusage.CollectAll(cargs)
	} else {
		parsed, err := harnessusage.ParseHarness(h)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harness-usage: %v\n", err)
			return 2
		}
		r, err := harnessusage.Collect(parsed, cargs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "harness-usage: %v\n", err)
			return 1
		}
		reports = []harnessusage.Report{r}
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reports); err != nil {
			fmt.Fprintf(os.Stderr, "harness-usage: encode: %v\n", err)
			return 1
		}
		return 0
	}

	printTable(reports)
	return 0
}

func printTable(reports []harnessusage.Report) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "HARNESS\tSOURCE\tSESSIONS\tEVENTS\tINPUT\tOUTPUT\tCACHE_R\tREASON\tCOST_USD\tNOTES")
	for _, r := range reports {
		cost := "-"
		if r.CostUSD != nil {
			cost = fmt.Sprintf("%.4f", *r.CostUSD)
		}
		notes := strings.Join(r.Notes, "; ")
		if len(notes) > 80 {
			notes = notes[:77] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%s\n",
			r.Harness, r.Source, r.Sessions, r.Events,
			r.Tokens.Input, r.Tokens.Output, r.Tokens.CacheRead, r.Tokens.Reasoning,
			cost, notes)
	}
	_ = w.Flush()
}
