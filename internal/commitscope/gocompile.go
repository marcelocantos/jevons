// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// GoCompileEnv turns the staged-Go compile rule off for one command (🎯T822).
const GoCompileEnv = "JEVONS_GO_COMPILE"

// GoPackageDirsFromStaged lists directory paths (e.g. scripts/journey-suite) of
// every Go package touched by a staged path. Non-.go paths and empty input are
// ignored. The result is sorted and deduplicated.
func GoPackageDirsFromStaged(staged []string) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, p := range staged {
		p = strings.TrimSpace(p)
		if p == "" || !strings.HasSuffix(p, ".go") {
			continue
		}
		dir := goSourcePackageDir(p)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

func goSourcePackageDir(path string) string {
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "." {
		return "."
	}
	return strings.TrimPrefix(dir, "./")
}

// GoCompileRefusal formats the stderr message when staged Go does not compile.
func GoCompileRefusal(compilerLine string) string {
	var b strings.Builder
	b.WriteString("commitscope: refusing — staged Go does not compile (🎯T822).\n\n")
	b.WriteString(compilerLine)
	if compilerLine != "" && !strings.HasSuffix(compilerLine, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("\nThe commit would leave a Go package uncompilable on the index tree.\n")
	b.WriteString("Fix the compiler error, then commit again. Bypass deliberately only when you\n")
	b.WriteString("accept landing a broken tree:\n")
	fmt.Fprintf(&b, "  %s=off git commit …\n", GoCompileEnv)
	return b.String()
}
