// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// GoCompileEnv turns the uncompilable-package rule off for one command (🎯T822).
// Separate from DisableEnv and UIBundleEnv: a deliberate whole-index commit
// must still refuse a missing import under scripts/ or cmd/.
const GoCompileEnv = "JEVONS_GO_COMPILE"

// goCompileRoots are the trees whose packages a fleet worker can break and
// cost every later clean gate multi-minute RED (the T820 missing-import
// incident). internal/ is deliberately out of scope for this slice: the
// acceptance names scripts/ and cmd/, and those are the packages journeys
// and tools load first.
var goCompileRoots = []string{
	"scripts/",
	"cmd/",
}

// IsGoCompilePath reports whether a change to p is a Go source file under a
// root the compile guard watches. Non-.go files, tests under other trees,
// and vendored paths are ignored so a docs-only or internal/-only commit is
// not slowed.
func IsGoCompilePath(p string) bool {
	p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if !strings.HasSuffix(p, ".go") {
		return false
	}
	for _, root := range goCompileRoots {
		if strings.HasPrefix(p, root) {
			return true
		}
	}
	return false
}

// TouchedGoPackages returns the unique package directories of staged Go files
// under scripts/ and cmd/, as "./scripts/foo" paths suitable for `go build`.
// Order is stable so refusal text and tests do not thrash.
func TouchedGoPackages(staged []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range staged {
		if !IsGoCompilePath(p) {
			continue
		}
		p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
		dir := path.Dir(p)
		if dir == "." || dir == "" {
			continue
		}
		pkg := "./" + dir
		if _, ok := seen[pkg]; ok {
			continue
		}
		seen[pkg] = struct{}{}
		out = append(out, pkg)
	}
	sort.Strings(out)
	return out
}

// GoCompileRefusal is the owner-facing text when a touched package fails to
// compile against the index tree. compilerOutput is `go build`'s combined
// stdout/stderr; packages are the ones we asked it to build.
func GoCompileRefusal(packages []string, compilerOutput string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "commitscope: refusing — this commit leaves %s uncompilable (🎯T822).\n\n",
		plural(len(packages), "Go package"))
	b.WriteString("A missing import (or any other compile error) under scripts/ or cmd/ costs\n")
	b.WriteString("every later clean gate multi-minute RED. Compiler output:\n\n")
	out := strings.TrimSpace(compilerOutput)
	if out == "" {
		out = "(go build failed with no output)"
	}
	for _, line := range strings.Split(out, "\n") {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	b.WriteString("\nFix the package, then commit again:\n")
	b.WriteString("  go build ")
	b.WriteString(strings.Join(packages, " "))
	b.WriteString("\n  git commit --only <your paths> -m \"…\"\n\n")
	fmt.Fprintf(&b, "Deliberate bypass (logged nowhere; prefer a fix): %s=off git commit …\n", GoCompileEnv)
	return b.String()
}
