// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package research

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/marcelocantos/jevons/internal/rsi"
)

// Claims are written so they change when the substance changes and not when
// the clock moves: raw counts inside a sliding window would supersede every
// cycle. Volumes are quantized into activity levels and the exact numbers ride
// along as evidence, which never triggers a supersession.
const (
	levelQuiet  = "quiet"
	levelLight  = "light"
	levelSteady = "steady"
	levelHeavy  = "heavy"
)

// ScanArgs bounds one context pass.
type ScanArgs struct {
	// Workdir is the primary repository (scope-level findings).
	Workdir string
	// RelatedRoots are directories whose immediate children may be repos.
	RelatedRoots []string
	// EventLogPath and SessionsDir are the fleet surfaces. Empty skips.
	EventLogPath string
	SessionsDir  string
	// BullseyePath overrides the frontier ledger (default Workdir/bullseye.yaml).
	BullseyePath string
	// Since is the window floor; MaxCommits / MaxRepos bound the mine.
	Since      time.Time
	MaxCommits int
	MaxRepos   int
	Now        time.Time
}

// ScanStats reports what one pass actually read.
type ScanStats struct {
	Repos        int
	Commits      int
	Scopes       int
	FrontierIDs  int
	EventRows    int
	SessionTurns int
}

// ScanResult is one pass's proposed note revisions plus what it read.
type ScanResult struct {
	Inputs []RevisionInput
	Stats  ScanStats
}

// Scan explores the surrounding context and proposes note revisions.
// Best-effort per surface: an unreadable surface is skipped, never fatal — one
// missing repo must not take the standing cycle down.
func Scan(args ScanArgs) (ScanResult, error) {
	now := args.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	since := args.Since
	if since.IsZero() {
		since = now.Add(-DefaultLookback)
	}
	maxCommits := args.MaxCommits
	if maxCommits <= 0 {
		maxCommits = DefaultMaxCommits
	}
	var res ScanResult

	primary := strings.TrimSpace(args.Workdir)
	if primary != "" {
		commits, err := rsi.MineGitCommits(primary, since, maxCommits)
		if err == nil && len(commits) > 0 {
			findings := RepoFindings(filepath.Base(primary), commits, true)
			res.Stats.Repos++
			res.Stats.Commits += len(commits)
			res.Stats.Scopes += len(findings) - 1
			res.Inputs = append(res.Inputs, RevisionInput{
				Topic:    "repo/" + filepath.Base(primary),
				Title:    "Repository activity: " + filepath.Base(primary),
				At:       now,
				Summary:  fmt.Sprintf("%d commits since %s", len(commits), since.UTC().Format(time.RFC3339)),
				Findings: findings,
				Sources:  []Source{{Kind: "git", Ref: primary, At: stamp(now)}},
			})
		}
	}

	for _, repo := range DiscoverRelatedRepos(args.RelatedRoots, primary, args.MaxRepos) {
		commits, err := rsi.MineGitCommits(repo, since, maxCommits)
		if err != nil || len(commits) == 0 {
			continue
		}
		name := filepath.Base(repo)
		res.Stats.Repos++
		res.Stats.Commits += len(commits)
		res.Inputs = append(res.Inputs, RevisionInput{
			Topic:    "repo/" + name,
			Title:    "Repository activity: " + name,
			At:       now,
			Summary:  fmt.Sprintf("%d commits since %s", len(commits), since.UTC().Format(time.RFC3339)),
			Findings: RepoFindings(name, commits, false),
			Sources:  []Source{{Kind: "repo", Ref: repo, At: stamp(now)}},
		})
	}

	if path := frontierPath(args); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			findings, count, err := FrontierFindings(data)
			if err == nil && len(findings) > 0 {
				res.Stats.FrontierIDs = count
				res.Inputs = append(res.Inputs, RevisionInput{
					Topic:    "context/frontier",
					Title:    "Convergence frontier",
					At:       now,
					Summary:  fmt.Sprintf("%d targets in %s", count, path),
					Findings: findings,
					Sources:  []Source{{Kind: "frontier", Ref: path, At: stamp(now)}},
				})
			}
		}
	}

	var fleet []Finding
	var fleetSources []Source
	if path := strings.TrimSpace(args.EventLogPath); path != "" {
		if rows, err := rsi.TailEventLogRows(path, 2000, since); err == nil && len(rows) > 0 {
			res.Stats.EventRows = len(rows)
			fleet = append(fleet, EventFindings(rows)...)
			fleetSources = append(fleetSources, Source{Kind: "eventlog", Ref: path, At: stamp(now)})
		}
	}
	if dir := strings.TrimSpace(args.SessionsDir); dir != "" {
		if turns, err := rsi.LoadRecentSessionTurns(dir, 12, 40); err == nil && len(turns) > 0 {
			res.Stats.SessionTurns = len(turns)
			fleet = append(fleet, SessionFindings(turns)...)
			fleetSources = append(fleetSources, Source{Kind: "session", Ref: dir, At: stamp(now)})
		}
	}
	if len(fleet) > 0 {
		res.Inputs = append(res.Inputs, RevisionInput{
			Topic:    "context/fleet",
			Title:    "Fleet and session context",
			At:       now,
			Summary:  fmt.Sprintf("%d event rows, %d session turns", res.Stats.EventRows, res.Stats.SessionTurns),
			Findings: fleet,
			Sources:  fleetSources,
		})
	}
	return res, nil
}

// RepoFindings turns mined commits (newest first) into note findings: one
// repo-level claim and, for the primary repo, one claim per touched scope.
func RepoFindings(name string, commits []rsi.GitCommit, byScope bool) []Finding {
	if len(commits) == 0 {
		return nil
	}
	head := commits[0]
	out := []Finding{{
		Key: "repo:" + name,
		Claim: fmt.Sprintf("%s activity in %s; head %s %q",
			activityLevel(len(commits)), name, shortSHA(head.SHA), head.Subject),
		Evidence: []string{
			fmt.Sprintf("%d commits mined", len(commits)),
			fmt.Sprintf("head %s at %s", shortSHA(head.SHA), head.At.UTC().Format(time.RFC3339)),
		},
		Sources: []Source{{Kind: "git", Ref: head.SHA, At: head.At.UTC().Format(time.RFC3339)}},
	}}
	if !byScope {
		return out
	}

	type scopeAgg struct {
		count int
		head  rsi.GitCommit
	}
	agg := map[string]*scopeAgg{}
	for _, c := range commits {
		for _, scope := range commitScopes(c.Files) {
			a := agg[scope]
			if a == nil {
				// Commits arrive newest first, so the first seen is the head.
				agg[scope] = &scopeAgg{count: 1, head: c}
				continue
			}
			a.count++
		}
	}
	scopes := make([]string, 0, len(agg))
	for s := range agg {
		scopes = append(scopes, s)
	}
	sort.Strings(scopes)
	for _, scope := range scopes {
		a := agg[scope]
		out = append(out, Finding{
			Key: "scope:" + scope,
			Claim: fmt.Sprintf("%s activity in %s; head %s %q",
				activityLevel(a.count), scope, shortSHA(a.head.SHA), a.head.Subject),
			Evidence: []string{fmt.Sprintf("%d commits touched %s", a.count, scope)},
			Sources:  []Source{{Kind: "git", Ref: a.head.SHA, At: a.head.At.UTC().Format(time.RFC3339)}},
		})
	}
	return out
}

// FrontierFindings parses a bullseye ledger into frontier claims. Pure: the
// hermetic test decides it from bytes, without a repository.
func FrontierFindings(data []byte) ([]Finding, int, error) {
	var doc struct {
		Targets map[string]struct {
			Name   string `yaml:"name"`
			Status string `yaml:"status"`
		} `yaml:"targets"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, 0, fmt.Errorf("research scan: parse bullseye: %w", err)
	}
	if len(doc.Targets) == 0 {
		return nil, 0, nil
	}
	counts := map[string]int{}
	var identified []string
	newestName := map[string]string{}
	for id, t := range doc.Targets {
		status := strings.TrimSpace(strings.ToLower(t.Status))
		counts[status]++
		if status == "identified" {
			identified = append(identified, id)
			newestName[id] = t.Name
		}
	}
	sort.Slice(identified, func(i, j int) bool {
		return targetOrder(identified[i]) > targetOrder(identified[j])
	})
	newest := identified
	if len(newest) > 3 {
		newest = newest[:3]
	}

	findings := []Finding{{
		Key: "frontier:status",
		Claim: fmt.Sprintf("%d identified, %d converging, %d achieved, %d set aside",
			counts["identified"], counts["converging"], counts["achieved"], counts["set_aside"]),
		Evidence: []string{fmt.Sprintf("%d targets in ledger", len(doc.Targets))},
	}}
	if len(newest) > 0 {
		var labels []string
		for _, id := range newest {
			labels = append(labels, "🎯"+id)
		}
		findings = append(findings, Finding{
			Key:      "frontier:newest",
			Claim:    "newest identified targets: " + strings.Join(labels, ", "),
			Evidence: newestEvidence(newest, newestName),
		})
	}
	return findings, len(doc.Targets), nil
}

// EventFindings clusters eventlog rows by component and outcome.
func EventFindings(rows []rsi.EventRow) []Finding {
	type key struct{ component, outcome string }
	counts := map[key]int{}
	last := map[key]rsi.EventRow{}
	for _, r := range rows {
		c := strings.TrimSpace(r.Component)
		if c == "" {
			continue
		}
		k := key{component: c, outcome: strings.TrimSpace(r.Outcome)}
		counts[k]++
		if _, seen := last[k]; !seen {
			last[k] = r
		}
	}
	keys := make([]key, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i].component < keys[j].component
	})
	if len(keys) > 8 {
		keys = keys[:8]
	}
	var out []Finding
	for _, k := range keys {
		outcome := k.outcome
		if outcome == "" {
			outcome = "unlabelled"
		}
		out = append(out, Finding{
			Key: "events:" + k.component + ":" + outcome,
			Claim: fmt.Sprintf("%s activity from %s with outcome %s (last decision %q)",
				activityLevel(counts[k]), k.component, outcome, last[k].Decision),
			Evidence: []string{fmt.Sprintf("%d rows", counts[k])},
			Sources:  []Source{{Kind: "eventlog", Ref: k.component + "/" + outcome, At: last[k].TS}},
		})
	}
	return out
}

// SessionFindings summarizes recent agent/session activity.
func SessionFindings(turns []rsi.ChatTurn) []Finding {
	if len(turns) == 0 {
		return nil
	}
	sessions := map[string]int{}
	newest := turns[0]
	for _, t := range turns {
		if id := strings.TrimSpace(t.SourceID); id != "" {
			sessions[id]++
		}
		if t.TS.After(newest.TS) {
			newest = t
		}
	}
	return []Finding{{
		Key: "sessions:recent",
		Claim: fmt.Sprintf("%s session activity across %d sessions; most recent %s",
			activityLevel(len(sessions)), len(sessions), firstNonEmpty(newest.SourceID, "unknown")),
		Evidence: []string{fmt.Sprintf("%d turns sampled", len(turns))},
		Sources:  []Source{{Kind: "session", Ref: newest.SourceID, At: newest.TS.UTC().Format(time.RFC3339)}},
	}}
}

// DiscoverRelatedRepos lists git repositories directly under the given roots,
// excluding the primary workdir, newest-modified first and bounded by max.
func DiscoverRelatedRepos(roots []string, exclude string, max int) []string {
	if max <= 0 {
		max = DefaultMaxRelatedRepos
	}
	exclude = filepath.Clean(expandHome(strings.TrimSpace(exclude)))
	type hit struct {
		path string
		mod  time.Time
	}
	var hits []hit
	seen := map[string]bool{}
	for _, root := range roots {
		root = expandHome(strings.TrimSpace(root))
		if root == "" {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(root, e.Name())
			if path == exclude || seen[path] {
				continue
			}
			st, err := os.Stat(filepath.Join(path, ".git"))
			if err != nil || st == nil {
				continue
			}
			seen[path] = true
			mod := time.Time{}
			if info, err := os.Stat(path); err == nil {
				mod = info.ModTime()
			}
			hits = append(hits, hit{path: path, mod: mod})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].mod.After(hits[j].mod) })
	if len(hits) > max {
		hits = hits[:max]
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.path)
	}
	return out
}

func frontierPath(args ScanArgs) string {
	if p := strings.TrimSpace(args.BullseyePath); p != "" {
		return p
	}
	if w := strings.TrimSpace(args.Workdir); w != "" {
		return filepath.Join(w, "bullseye.yaml")
	}
	return ""
}

// activityLevel quantizes a volume so a claim survives a cycle that merely
// counted one more of the same thing.
func activityLevel(n int) string {
	switch {
	case n <= 0:
		return levelQuiet
	case n <= 2:
		return levelLight
	case n <= 9:
		return levelSteady
	default:
		return levelHeavy
	}
}

// commitScopes maps touched files to coarse scopes (internal/x, cmd/x, web, …).
func commitScopes(files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		parts := strings.Split(strings.TrimSpace(f), "/")
		if len(parts) == 0 || parts[0] == "" {
			continue
		}
		scope := parts[0]
		if len(parts) > 1 && (scope == "internal" || scope == "cmd" || scope == "docs" || scope == "scripts") {
			scope = parts[0] + "/" + parts[1]
		}
		if seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out
}

func newestEvidence(ids []string, names map[string]string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, fmt.Sprintf("🎯%s %s", id, names[id]))
	}
	return out
}

// targetOrder sorts hierarchical target ids numerically (T356 after T99).
func targetOrder(id string) float64 {
	raw := strings.TrimPrefix(strings.TrimSpace(id), "T")
	major, minor, _ := strings.Cut(raw, ".")
	n := 0.0
	for _, r := range major {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + float64(r-'0')
	}
	m := 0.0
	scale := 0.1
	for _, r := range minor {
		if r < '0' || r > '9' {
			break
		}
		m += float64(r-'0') * scale
		scale /= 10
	}
	return n + m
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
