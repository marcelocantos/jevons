// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ErrLandingGateCoverage means the GREEN gate record's command does not
// unfiltered-cover every required Go package (🎯T1180).
var ErrLandingGateCoverage = errors.New("landing gate does not cover every changed Go package")

// GitCommitGoPackageDirs lists directory paths (e.g. internal/mcpserver) of
// every Go package touched by commit, using git diff-tree in repo.
func GitCommitGoPackageDirs(repo, commit string) ([]string, error) {
	out, err := exec.Command("git", "-C", repo, "diff-tree", "--no-commit-id", "--name-only", "-r", commit).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff-tree %s: %w", commit, err)
	}
	seen := map[string]bool{}
	var dirs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, ".go") {
			continue
		}
		dir := goSourcePackageDir(line)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// GitBranchGoPackageDirs lists directory paths of every Go package with a
// changed .go file on the branch range since..until (merge-base..tip).
func GitBranchGoPackageDirs(repo, since, until string) ([]string, error) {
	if repo == "" || since == "" || until == "" {
		return nil, nil
	}
	out, err := exec.Command("git", "-C", repo, "diff", "--name-only", since+".."+until).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s..%s: %w", since, until, err)
	}
	seen := map[string]bool{}
	var dirs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, ".go") {
			continue
		}
		dir := goSourcePackageDir(line)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// CheckLandingGatePackageCoverage refuses when rec's command is not a go test
// (or make test-go, or a nested-module bash -c cd-and-test form, 🎯T1271)
// whose patterns cover every required package without a -run/-skip filter.
// required empty means nothing to check.
func CheckLandingGatePackageCoverage(rec *Record, required []string) error {
	if rec == nil || len(required) == 0 {
		return nil
	}
	orig := rec.Command
	if len(orig) == 0 {
		return landingGateCoverageError(orig, required, required, false, false, true)
	}
	if gateAchieveFullSuite(orig) {
		return nil
	}
	argv := coverageGoTestArgv(orig)
	if !gateCommandIsGoTestOrKnownMake(argv) {
		return landingGateCoverageError(orig, required, required, false, false, true)
	}
	patterns, hasRun := goTestPackagePatterns(argv)
	hasSkip := goTestHasSkipFlag(argv)
	missing := packagesNotCovered(patterns, required)
	if hasRun || hasSkip || len(missing) > 0 {
		return landingGateCoverageError(orig, required, missing, hasRun, hasSkip, false)
	}
	return nil
}

// GoTestCount1Argv builds argv for an unfiltered go test -count=1 over pkgs.
// pkgs are directory paths as returned by GitBranchGoPackageDirs.
func GoTestCount1Argv(pkgs []string) []string {
	if len(pkgs) == 0 {
		return nil
	}
	argv := []string{"go", "test", "-count=1"}
	for _, p := range pkgs {
		p = strings.TrimPrefix(p, "./")
		if !strings.HasSuffix(p, "/...") {
			p = "./" + p + "/..."
		} else if !strings.HasPrefix(p, "./") {
			p = "./" + p
		}
		argv = append(argv, p)
	}
	return argv
}

// SuggestGoTestGateCommand names a bin/gate -clean go test covering pkgs.
func SuggestGoTestGateCommand(pkgs []string) string {
	argv := GoTestCount1Argv(pkgs)
	if len(argv) == 0 {
		return "bin/gate -clean -- make test-go"
	}
	return "bin/gate -clean -- " + strings.Join(argv, " ")
}

func landingGateCoverageError(argv []string, required, missing []string, hasRun, hasSkip, badCommand bool) error {
	if len(missing) == 0 && !badCommand {
		missing = required
	}
	var why []string
	if badCommand {
		why = append(why, "gate command must be go test or make test-go")
	}
	if hasRun {
		why = append(why, "gate used a -run filter")
	}
	if hasSkip {
		why = append(why, "gate used a -skip filter")
	}
	if len(missing) > 0 && !badCommand {
		why = append(why, "uncovered packages: "+strings.Join(missing, ", "))
	} else if len(missing) > 0 {
		why = append(why, "branch changed packages: "+strings.Join(required, ", "))
	}
	msg := fmt.Sprintf("landing gate ran %q (%s); run: %s",
		formatArgv(argv), strings.Join(why, "; "), SuggestGoTestGateCommand(required))
	return fmt.Errorf("%s: %w", msg, ErrLandingGateCoverage)
}

func formatArgv(argv []string) string {
	if len(argv) == 0 {
		return "(empty)"
	}
	return strings.Join(argv, " ")
}

func gateCommandIsGoTestOrKnownMake(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	if gateAchieveFullSuite(argv) {
		return true
	}
	if len(argv) >= 2 && argv[0] == "go" && argv[1] == "test" {
		return true
	}
	if filepath.Base(argv[0]) == "gotest" {
		return true
	}
	switch filepath.Base(argv[0]) {
	case "make", "gmake":
		for _, a := range argv[1:] {
			switch a {
			case "test-go", "test-go-raw", "test":
				return true
			}
		}
	}
	return false
}

func goSourcePackageDir(path string) string {
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "." {
		return "."
	}
	return strings.TrimPrefix(dir, "./")
}

func landingGoPackageDirs(repo string, commits []string, list func(string, string) ([]string, error)) ([]string, error) {
	if list == nil || repo == "" || len(commits) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range commits {
		dirs, err := list(repo, c)
		if err != nil {
			return nil, err
		}
		for _, d := range dirs {
			if seen[d] {
				continue
			}
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out, nil
}

func achieveGateCoverageFlags(rec *Record, gateCommit string, fixes []string, contains func(code, fix string) bool, repo string, list func(string, string) ([]string, error)) []Flag {
	if rec == nil || rec.Tree == nil || gateCommit == "" || list == nil || repo == "" {
		return nil
	}
	landings := landingCommits(gateCommit, fixes, contains)
	required, err := landingGoPackageDirs(repo, landings, list)
	if err != nil || len(required) == 0 {
		return nil
	}
	argv := rec.Command
	if len(argv) == 0 {
		return nil
	}
	if gateAchieveFullSuite(argv) {
		return nil
	}
	argv = coverageGoTestArgv(argv)
	patterns, hasRun := goTestPackagePatterns(argv)
	hasSkip := goTestHasSkipFlag(argv)
	var flags []Flag
	if hasRun || hasSkip {
		detail := "the cited gate ran go test with a -run filter, so its GREEN covers only a " +
			"subset of tests and cannot attest a landing — cite a full-package or make test-go gate"
		if hasSkip && !hasRun {
			detail = "the cited gate ran go test with a -skip filter, so its GREEN does not cover " +
				"every test in the named packages — cite a full-package or make test-go gate"
		}
		flags = append(flags, Flag{
			Kind:   FlagAchieveGateNarrowRun,
			Detail: detail,
		})
	}
	missing := packagesNotCovered(patterns, required)
	if len(missing) > 0 {
		flags = append(flags, Flag{
			Kind: FlagAchieveGateMissingPackages,
			Detail: fmt.Sprintf(
				"the cited gate command does not cover every Go package the landing changed; missing: %s",
				strings.Join(missing, ", ")),
		})
	}
	return flags
}

func landingCommits(gateCommit string, fixes []string, contains func(code, fix string) bool) []string {
	if contains == nil {
		return nil
	}
	var landings []string
	for _, fix := range fixes {
		if contains(gateCommit, fix) {
			landings = append(landings, fix)
		}
	}
	if len(landings) == 0 && gateCommit != "" {
		landings = []string{gateCommit}
	}
	return landings
}

func gateAchieveFullSuite(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	switch filepath.Base(argv[0]) {
	case "make", "gmake":
		for _, a := range argv[1:] {
			switch a {
			case "test-go", "test-go-raw", "test":
				return true
			}
		}
	case "gotest":
		return gotestIsFullSuite(argv[1:])
	}
	if len(argv) >= 2 && argv[0] == "go" {
		return goTestIsFullSuite(argv[1:])
	}
	return false
}

func gotestIsFullSuite(args []string) bool {
	if goTestHasSubsetFilter(args) {
		return false
	}
	pkgs := goTestPackageArgs(args)
	if len(pkgs) == 0 {
		return true
	}
	for _, p := range pkgs {
		if p == "./..." || p == "..." {
			return true
		}
	}
	return false
}

func goTestIsFullSuite(args []string) bool {
	if len(args) == 0 || args[0] != "test" {
		return false
	}
	rest := args[1:]
	if goTestHasSubsetFilter(rest) {
		return false
	}
	pkgs := goTestPackageArgs(rest)
	for _, p := range pkgs {
		if p == "./..." || p == "..." {
			return true
		}
	}
	return false
}

func goTestPackagePatterns(argv []string) (patterns []string, hasRun bool) {
	if len(argv) >= 2 && argv[0] == "go" && argv[1] == "test" {
		args := argv[2:]
		return goTestPackageArgs(args), goTestHasRunFlag(args)
	}
	if filepath.Base(argv[0]) == "gotest" {
		args := argv[1:]
		return goTestPackageArgs(args), goTestHasRunFlag(args)
	}
	return nil, false
}

func goTestHasSkipFlag(argv []string) bool {
	var args []string
	if len(argv) >= 2 && argv[0] == "go" && argv[1] == "test" {
		args = argv[2:]
	} else if filepath.Base(argv[0]) == "gotest" {
		args = argv[1:]
	} else {
		return false
	}
	return goTestHasSkipFilter(args)
}

func goTestHasSubsetFilter(args []string) bool {
	return goTestHasRunFlag(args) || goTestHasSkipFilter(args)
}

func goTestHasRunFlag(args []string) bool {
	return goTestHasTestFlag(args, "run")
}

func goTestHasSkipFilter(args []string) bool {
	return goTestHasTestFlag(args, "skip")
}

func goTestHasTestFlag(args []string, name string) bool {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-"+name || a == "-test."+name:
			return true
		case strings.HasPrefix(a, "-"+name+"=") || strings.HasPrefix(a, "-test."+name+"="):
			return true
		}
	}
	return false
}

func goTestPackageArgs(args []string) []string {
	var pkgs []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			continue
		case a == "-run" || a == "-test.run", a == "-skip" || a == "-test.skip":
			if i+1 < len(args) {
				i++
			}
		case strings.HasPrefix(a, "-run=") || strings.HasPrefix(a, "-test.run="),
			strings.HasPrefix(a, "-skip=") || strings.HasPrefix(a, "-test.skip="):
		case strings.HasPrefix(a, "-"):
			if flagTakesValue(a) && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
			}
		default:
			pkgs = append(pkgs, normalizeGoTestPattern(a))
		}
	}
	return pkgs
}

func normalizeGoTestPattern(p string) string {
	p = strings.TrimSpace(p)
	if rel := relativePackage(p); rel != p {
		return rel
	}
	return strings.TrimPrefix(p, "./")
}

func packagesNotCovered(patterns []string, required []string) []string {
	if len(patterns) == 0 {
		return required
	}
	var missing []string
	for _, req := range required {
		if !goTestPatternsCover(patterns, req) {
			missing = append(missing, req)
		}
	}
	return missing
}

func goTestPatternsCover(patterns []string, pkgDir string) bool {
	pkgDir = strings.TrimPrefix(pkgDir, "./")
	for _, p := range patterns {
		if patternCoversPackage(p, pkgDir) {
			return true
		}
	}
	return false
}

func patternCoversPackage(pattern, pkgDir string) bool {
	pattern = strings.TrimPrefix(pattern, "./")
	pkgDir = strings.TrimPrefix(pkgDir, "./")
	if pattern == "..." || pattern == "./..." {
		return true
	}
	if strings.HasSuffix(pattern, "/...") {
		prefix := strings.TrimSuffix(pattern, "/...")
		return pkgDir == prefix || strings.HasPrefix(pkgDir, prefix+"/")
	}
	if strings.HasSuffix(pattern, "...") && strings.Count(pattern, "/") > 0 {
		prefix := strings.TrimSuffix(pattern, "...")
		return pkgDir == prefix || strings.HasPrefix(pkgDir, prefix+"/")
	}
	pattern = strings.TrimSuffix(pattern, "/")
	pkgDir = strings.TrimSuffix(pkgDir, "/")
	return pattern == pkgDir
}
