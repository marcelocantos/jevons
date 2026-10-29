// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T1146: a worker branch tip can pass its own pristine gate while the merge
// commit that combines it with a moved trunk does not build — each side was
// gated in isolation, the combined tree never was.
//
// 🎯T1191: the tip gate also cannot see tests trunk gained after the branch was
// cut. After Integrate creates a merge commit and fast-forwards to it, this
// file runs a pristine checkout go build ./... on that merge result, then
// go test -count=1 on every package the worker branch touched; failure rolls
// the shared branch back to the pre-landing tip.
//
// 🎯T1197: those touched packages are not enough. scripts/docratchet ratchets
// inspect the rest of the tree, so a branch that never edits them can still
// turn them red. Specimen 2026-10-23: b61bcc8a (T1040) touched envelope and
// landhold, passed its post-merge gate, and left
// TestT76641ReapDecisionsReachNoProseMatcher red on HEAD. Every merge landing
// therefore adds ./scripts/docratchet/ to the test set; a red ratchet rolls
// back like any other failure and names the failing test.

// ErrPostMergeGate is returned when the post-merge pristine gate does not pass.
var ErrPostMergeGate = errors.New("post-merge gate failed")

// defaultPostMergeBuild is the gate command when the repo has a go.mod at its
// root. It catches import cycles and compile errors the per-tip gate cannot see.
var defaultPostMergeBuild = []string{"go", "build", "./..."}

// postMergeAlwaysOnPackages are Go packages whose tests are cross-cutting:
// they inspect the rest of the tree, so a branch that never touches them can
// still turn them red (🎯T1197). Added to the touched-package set on every
// merge landing when the path exists at the merge result. internal/testhome
// ratchets that every test package runs on a private HOME; on 2026-10-28
// three new packages (restartauth, brokerintent, poidlenudge) landed past it
// because a branch that adds a package never touches the ratchet (🎯T1269).
var postMergeAlwaysOnPackages = []string{"scripts/docratchet", "internal/testhome"}

// postMergeTestTimeout: go test's default (10m) is too short once
// scripts/docratchet is always in the set: that package alone runs ~11 min
// under load (🎯T808). 45m matches make test-go.
const postMergeTestTimeout = "45m"

// postMergeGateCommand picks the argv for a merge-result gate. When
// override is non-nil it is used as-is (an empty slice means skip). When
// override is nil, a go.mod at the repo root selects defaultPostMergeBuild;
// otherwise there is nothing to run.
func postMergeGateCommand(base string, override []string) ([]string, bool) {
	if override != nil {
		if len(override) == 0 {
			return nil, false
		}
		return override, true
	}
	if _, err := os.Stat(filepath.Join(base, "go.mod")); err != nil {
		return nil, false
	}
	return defaultPostMergeBuild, true
}

// verifyMergeResultBuild runs pristine-checkout build and touched-package tests
// on mergeCommit. branchSince..branchUntil names the worker branch range used
// to pick test packages (merge-base..worker tip). It returns the last gate
// record written when one was stored.
func verifyMergeResultBuild(base, mergeCommit, gateStoreRoot, branchSince, branchUntil string, command []string) (*gate.Record, error) {
	cmd, ok := postMergeGateCommand(base, command)
	if !ok {
		return nil, nil
	}
	store, err := gate.OpenStore(gateStoreRoot)
	if err != nil {
		return nil, fmt.Errorf("integrate: post-merge gate store: %w", err)
	}
	// 🎯T1249: repo-declared prerequisite (submodule init + script) in the
	// same pristine checkout, before go build. Not a bypass — it has to
	// succeed, then the existing gate still runs.
	prereq, err := loadIntegratePrereq(base, mergeCommit)
	if err != nil {
		return nil, fmt.Errorf("integrate: merge result %s: %w: %w", short(mergeCommit), err, ErrPostMergeGate)
	}
	prepare := prereq.prepare()
	buildRes, runErr := gate.RunClean(&gate.CleanArgs{
		Repo:    base,
		Commit:  mergeCommit,
		Command: cmd,
		Name:    "integrate-post-merge",
		Store:   store,
		Prepare: prepare,
	})
	var rec *gate.Record
	if buildRes != nil {
		rec = buildRes.Record
	}
	if runErr != nil {
		return rec, wrapPostMergeRunError(mergeCommit, runErr)
	}
	if rec == nil || !rec.Verdict.IsGreen() {
		verdict := gate.VerdictVoid
		if rec != nil {
			verdict = rec.Verdict
		}
		return rec, fmt.Errorf("integrate: merge result %s: post-merge build verdict %s: %w",
			short(mergeCommit), verdict, ErrPostMergeGate)
	}

	required, err := gate.GitBranchGoPackageDirs(base, branchSince, branchUntil)
	if err != nil {
		return rec, fmt.Errorf("integrate: merge result %s: list touched packages: %w", short(mergeCommit), err)
	}
	required = withAlwaysOnPostMergePackages(base, mergeCommit, required)
	// 🎯T1272: Go does not traverse a nested go.mod from the root module.
	// Keep the root/always-on ratchets, but run touched nested packages from
	// their own module directory in the pristine merged checkout.
	groups := postMergeModulePackages(base, mergeCommit, required)
	for _, group := range groups {
		// 🎯T1255: declared test_tags apply to both root and nested tests.
		testCmd := postMergeTestArgv(group.packages, prereq.testTags())
		if group.module != "" {
			testCmd = nestedPostMergeTestArgv(group.module, testCmd)
		}
		testRes, runErr := gate.RunClean(&gate.CleanArgs{
			Repo:    base,
			Commit:  mergeCommit,
			Command: testCmd,
			Name:    "integrate-post-merge-test",
			Store:   store,
			Prepare: prepare,
		})
		if testRes != nil {
			rec = testRes.Record
		}
		if runErr != nil || rec == nil || !rec.Verdict.IsGreen() {
			return rec, wrapPostMergeTestFailure(mergeCommit, rec, runErr)
		}
	}
	return rec, nil
}

type postMergeModuleGroup struct {
	module   string   // empty means root module
	packages []string // paths relative to module
}

// postMergeModulePackages groups by the closest go.mod in the merged tree,
// not by the worker tree: trunk may have introduced a module boundary.
func postMergeModulePackages(base, commit string, pkgs []string) []postMergeModuleGroup {
	groups := []postMergeModuleGroup{}
	indices := map[string]int{}
	for _, pkg := range pkgs {
		module := ""
		for dir := filepath.ToSlash(filepath.Clean(pkg)); dir != "." && dir != ""; dir = filepath.ToSlash(filepath.Dir(dir)) {
			if treeHasPath(base, commit, dir+"/go.mod") {
				module = dir
				break
			}
		}
		i, ok := indices[module]
		if !ok {
			i = len(groups)
			indices[module] = i
			groups = append(groups, postMergeModuleGroup{module: module})
		}
		if module != "" {
			pkg = strings.TrimPrefix(pkg, module+"/")
			if pkg == module {
				pkg = "."
			}
		}
		groups[i].packages = append(groups[i].packages, pkg)
	}
	return groups
}

func nestedPostMergeTestArgv(module string, testCmd []string) []string {
	// Directory names from git can contain shell metacharacters; quote the
	// path rather than interpolating it as executable script text.
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	for i, arg := range testCmd {
		testCmd[i] = quote(arg)
	}
	return []string{"/bin/bash", "-c", "cd " + quote(module) + " && GOWORK=off " + strings.Join(testCmd, " ")}
}

// withAlwaysOnPostMergePackages appends scripts/docratchet (🎯T1197) when
// that path exists at mergeCommit, even if the worker branch never touched it.
func withAlwaysOnPostMergePackages(base, mergeCommit string, pkgs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(pkgs)+len(postMergeAlwaysOnPackages))
	for _, p := range pkgs {
		p = strings.TrimPrefix(p, "./")
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range postMergeAlwaysOnPackages {
		if seen[p] {
			continue
		}
		if !treeHasPath(base, mergeCommit, p) {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func treeHasPath(base, commit, path string) bool {
	if base == "" || commit == "" || path == "" {
		return false
	}
	_, _, err := gitRun(base, "cat-file", "-e", commit+":"+path)
	return err == nil
}

// postMergeTestArgv is go test -count=1 over pkgs, with a timeout that
// covers scripts/docratchet (🎯T808 / 🎯T1197). When tags is non-empty
// (🎯T1255), -tags <joined> is inserted so build-tagged packages run.
func postMergeTestArgv(pkgs []string, tags []string) []string {
	argv := gate.GoTestCount1Argv(pkgs)
	if len(argv) < 2 {
		return argv
	}
	out := []string{"go", "test", "-json", "-timeout", postMergeTestTimeout}
	if joined := joinGoTestTags(tags); joined != "" {
		out = append(out, "-tags", joined)
	}
	return append(out, argv[2:]...)
}

func wrapPostMergeRunError(mergeCommit string, runErr error) error {
	if errors.Is(runErr, ErrIntegratePrereq) {
		return fmt.Errorf("integrate: merge result %s: %w: %w", short(mergeCommit), runErr, ErrPostMergeGate)
	}
	return fmt.Errorf("integrate: merge result %s: %w: %v", short(mergeCommit), ErrPostMergeGate, runErr)
}

func wrapPostMergeTestFailure(mergeCommit string, rec *gate.Record, runErr error) error {
	if runErr != nil && errors.Is(runErr, ErrIntegratePrereq) {
		return wrapPostMergeRunError(mergeCommit, runErr)
	}
	verdict := gate.VerdictVoid
	if rec != nil {
		verdict = rec.Verdict
	}
	named := ""
	if names := failingTestNames(rec); len(names) > 0 {
		named = " (" + strings.Join(names, ", ") + ")"
	}
	if runErr != nil {
		return fmt.Errorf("integrate: merge result %s: post-merge test verdict %s%s: %w: %v",
			short(mergeCommit), verdict, named, ErrPostMergeGate, runErr)
	}
	return fmt.Errorf("integrate: merge result %s: post-merge test verdict %s%s: %w",
		short(mergeCommit), verdict, named, ErrPostMergeGate)
}

// failingTestNames extracts go test `--- FAIL: TestX` identifiers from the
// gate record so a red ratchet is named in the integrate refusal (🎯T1197).
func failingTestNames(rec *gate.Record) []string {
	if rec == nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	add := func(src string) {
		const p = "--- FAIL: "
		for _, line := range strings.Split(src, "\n") {
			i := strings.Index(line, p)
			if i < 0 {
				continue
			}
			rest := strings.TrimSpace(line[i+len(p):])
			name := rest
			if j := strings.IndexAny(rest, " \t("); j >= 0 {
				name = rest[:j]
			}
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	add(rec.OutputTail)
	for _, a := range rec.Anomalies {
		add(a.Line)
	}
	return names
}
