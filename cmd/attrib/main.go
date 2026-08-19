// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command attrib answers the operator question 🎯T466 exists for: the shared
// clone is dirty, the workers that dirtied it are stopped — whose is what,
// and how do I recover or discard ONE agent's slice without touching the
// other N-1?
//
//	attrib list       [-repo DIR] [-json]        who touched which dirty path
//	attrib save       -agent NAME [-shared]      copy an agent's slice aside
//	attrib discard    -agent NAME                save, then undo, sole paths only
//	attrib restore    -agent NAME [-slice DIR]   put a saved slice back
//	attrib drain      [-repo DIR]                empty the shared index, saved first
//	attrib backfill   [-transcripts GLOB ...]    attribute a pile that predates the feed
//
// The feed is scripts/hooks/treeguard post (every mutating tool call);
// the drain runs automatically when an agent stops. This command is the
// read-and-recover side: it never guesses, it reports unattributed paths as
// unattributed, and discard refuses shared paths — a path two agents touched
// is not any one agent's to throw away.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/attrib"
	"github.com/marcelocantos/jevons/internal/treeguard"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	if len(argv) == 0 {
		usage()
		return 2
	}
	cmd, rest := argv[0], argv[1:]
	switch cmd {
	case "list":
		return list(rest)
	case "save":
		return save(rest)
	case "discard":
		return discard(rest)
	case "restore":
		return restore(rest)
	case "drain":
		return drain(rest)
	case "backfill":
		return backfill(rest)
	default:
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: attrib list|save|discard|restore|drain|backfill [flags]
  list      [-repo DIR] [-json]                 attribution of the dirty tree
  save      -agent NAME [-shared] [-repo DIR]   save an agent's slice aside
  discard   -agent NAME [-repo DIR]             save, then undo, that agent's SOLE paths
  restore   -agent NAME [-slice DIR] [-repo DIR]  reapply a saved slice
  drain     [-repo DIR]                         empty the shared index (saved first)
  backfill  [-repo DIR] [-transcripts GLOB ...] attribute from treeguard + transcripts`)
}

func repoFlag(fs *flag.FlagSet) *string {
	return fs.String("repo", "", "repo root (default: the repo containing the working directory)")
}

func resolveRepo(repo string) (string, error) {
	if repo == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		repo = wd
	}
	return attrib.RepoRoot(repo)
}

// attribution loads records, resolves names, and folds them against the
// dirty tree — the one read path every subcommand shares.
func attribution(repo string) (attrib.Attribution, error) {
	store := &attrib.Store{Root: attrib.DefaultRoot()}
	records, err := store.Load()
	if err != nil {
		return attrib.Attribution{}, err
	}
	roster, _ := attrib.LoadRoster(attrib.DefaultRosterPaths()...)
	records = attrib.Resolve(records, roster)
	dirty, err := attrib.DirtyPaths(repo)
	if err != nil {
		return attrib.Attribution{}, err
	}
	return attrib.Attribute(records, dirty), nil
}

func list(argv []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	repo := repoFlag(fs)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := resolveRepo(*repo)
	if err != nil {
		return fail(err)
	}
	att, err := attribution(root)
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(att); err != nil {
			return fail(err)
		}
		return 0
	}
	if len(att.Slices) == 0 && len(att.Unattributed) == 0 {
		fmt.Println("clean tree: nothing to attribute")
		return 0
	}
	for _, s := range att.Slices {
		fmt.Printf("%s\n", s.Agent)
		for _, p := range s.Sole {
			fmt.Printf("  %-2s %s   (%s)\n", strings.TrimSpace(p.XY), p.Path, viaSummary(p))
		}
		for _, p := range s.Shared {
			fmt.Printf("  %-2s %s   SHARED with %s\n", strings.TrimSpace(p.XY), p.Path, others(p, s.Agent))
		}
	}
	if len(att.Unattributed) > 0 {
		fmt.Println("unattributed (no record claims these)")
		for _, p := range att.Unattributed {
			fmt.Printf("  %-2s %s\n", strings.TrimSpace(p.XY), p.Path)
		}
	}
	return 0
}

func viaSummary(p attrib.PathOwner) string {
	var vias []string
	for _, t := range p.Touches {
		for _, v := range t.Via {
			if !contains(vias, v) {
				vias = append(vias, v)
			}
		}
	}
	sort.Strings(vias)
	return strings.Join(vias, ",")
}

func others(p attrib.PathOwner, agent string) string {
	var out []string
	for _, t := range p.Touches {
		if t.Agent != agent {
			out = append(out, t.Agent)
		}
	}
	return strings.Join(out, ", ")
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// saveSlice is the shared save path: discard calls it first, save exposes it.
func saveSlice(root, agent string, includeShared bool) (*attrib.SavedSlice, []string, error) {
	att, err := attribution(root)
	if err != nil {
		return nil, nil, err
	}
	slice, ok := att.Slice(agent)
	if !ok {
		return nil, nil, fmt.Errorf("no dirty paths are attributed to %q (try `attrib list`)", agent)
	}
	paths := slice.Paths(includeShared)
	saved, err := attrib.Save(root, attrib.DefaultRoot(), agent, paths, time.Now())
	if err != nil {
		return nil, nil, err
	}
	return saved, slice.Paths(false), nil
}

func save(argv []string) int {
	fs := flag.NewFlagSet("save", flag.ContinueOnError)
	repo := repoFlag(fs)
	agent := fs.String("agent", "", "agent whose slice to save (required)")
	shared := fs.Bool("shared", false, "include paths other agents also touched")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *agent == "" {
		return fail(fmt.Errorf("-agent is required"))
	}
	root, err := resolveRepo(*repo)
	if err != nil {
		return fail(err)
	}
	saved, _, err := saveSlice(root, *agent, *shared)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("saved %d path(s) for %s to %s\n", len(saved.Kinds), *agent, saved.Dir)
	return 0
}

func discard(argv []string) int {
	fs := flag.NewFlagSet("discard", flag.ContinueOnError)
	repo := repoFlag(fs)
	agent := fs.String("agent", "", "agent whose sole paths to discard (required)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *agent == "" {
		return fail(fmt.Errorf("-agent is required"))
	}
	root, err := resolveRepo(*repo)
	if err != nil {
		return fail(err)
	}
	// Save everything the agent touched (shared included), discard only what
	// is theirs alone. The save is the reversibility guarantee; the sole-only
	// discard is the safety one — a shared path holds somebody else's edits,
	// and no per-agent operation may destroy those.
	saved, sole, err := saveSlice(root, *agent, true)
	if err != nil {
		return fail(err)
	}
	if len(sole) == 0 {
		fmt.Printf("nothing discarded: every path %s touched is shared; saved to %s\n", *agent, saved.Dir)
		return 0
	}
	if err := attrib.Discard(root, sole, saved); err != nil {
		return fail(err)
	}
	fmt.Printf("discarded %d sole path(s) for %s; everything saved to %s\n", len(sole), *agent, saved.Dir)
	return 0
}

func restore(argv []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	repo := repoFlag(fs)
	agent := fs.String("agent", "", "agent whose latest saved slice to restore")
	sliceDir := fs.String("slice", "", "explicit slice directory (overrides -agent latest)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := resolveRepo(*repo)
	if err != nil {
		return fail(err)
	}
	dir := *sliceDir
	if dir == "" {
		if *agent == "" {
			return fail(fmt.Errorf("one of -agent or -slice is required"))
		}
		dirs, _ := filepath.Glob(filepath.Join(attrib.DefaultRoot(), "slices", *agent, "*"))
		if len(dirs) == 0 {
			return fail(fmt.Errorf("no saved slices for %q under %s", *agent, attrib.DefaultRoot()))
		}
		sort.Strings(dirs)
		dir = dirs[len(dirs)-1]
	}
	saved, err := attrib.LoadSaved(dir)
	if err != nil {
		return fail(err)
	}
	if err := attrib.Restore(root, saved); err != nil {
		return fail(err)
	}
	fmt.Printf("restored %d path(s) from %s\n", len(saved.Kinds), dir)
	return 0
}

func drain(argv []string) int {
	fs := flag.NewFlagSet("drain", flag.ContinueOnError)
	repo := repoFlag(fs)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := resolveRepo(*repo)
	if err != nil {
		return fail(err)
	}
	d, err := attrib.DrainIndex(root, attrib.DefaultRoot(), "", "operator", "manual", time.Now())
	if err != nil {
		return fail(err)
	}
	if d == nil {
		fmt.Println("index already clean")
		return 0
	}
	fmt.Printf("drained %d staged path(s); index state saved to %s\n", len(d.Paths), d.Dir)
	return 0
}

func backfill(argv []string) int {
	fs := flag.NewFlagSet("backfill", flag.ContinueOnError)
	repo := repoFlag(fs)
	var transcripts multiFlag
	fs.Var(&transcripts, "transcripts", "transcript glob(s); session id = file base name (repeatable)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := resolveRepo(*repo)
	if err != nil {
		return fail(err)
	}
	store := &attrib.Store{Root: attrib.DefaultRoot()}
	nTG, err := attrib.BackfillTreeguard(store, treeguard.DefaultStoreRoot(), root)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("treeguard: %d record(s)\n", nTG)
	var files []string
	for _, g := range transcripts {
		m, err := filepath.Glob(g)
		if err != nil {
			return fail(err)
		}
		files = append(files, m...)
	}
	if len(files) > 0 {
		nTR, err := attrib.BackfillTranscripts(store, root, files)
		if err != nil {
			return fail(err)
		}
		fmt.Printf("transcripts: %d record(s) from %d file(s)\n", nTR, len(files))
	}
	return 0
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "attrib:", err)
	return 1
}
