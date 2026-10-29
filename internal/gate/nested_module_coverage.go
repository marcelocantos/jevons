// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"path/filepath"
	"strings"
	"unicode"
)

// coverageGoTestArgv returns argv for coverage analysis. A nested-Go-module
// bash -c form (🎯T1271) is rewritten to a root-relative `go test` whose
// package patterns are prefixed with the cd directory; anything else is
// returned unchanged.
func coverageGoTestArgv(argv []string) []string {
	if unwrapped, ok := nestedModuleGoTestArgv(argv); ok {
		return unwrapped
	}
	return argv
}

// nestedModuleGoTestArgv rewrites a nested-Go-module gate command
//
//	/bin/bash -c 'cd <dir> && GOWORK=off go test ... && go vet ...'
//
// into a root-relative `go test` argv whose package patterns are prefixed
// with <dir>. Nested `./...` therefore covers only that subdirectory, never
// the root module (🎯T1271). ok is false when argv is not that form.
func nestedModuleGoTestArgv(argv []string) (unwrapped []string, ok bool) {
	script, ok := bashCScript(argv)
	if !ok {
		return nil, false
	}
	segs := splitShellAnd(script)
	if len(segs) < 2 {
		return nil, false
	}
	dir, ok := parseCdSegment(segs[0])
	if !ok {
		return nil, false
	}
	var pkgs []string
	var anyRun, anySkip, sawTest bool
	for _, seg := range segs[1:] {
		words := stripEnvAssigns(strings.Fields(seg))
		if len(words) < 2 || words[0] != "go" || words[1] != "test" {
			continue
		}
		sawTest = true
		rest := words[2:]
		if goTestHasRunFlag(rest) {
			anyRun = true
		}
		if goTestHasSkipFilter(rest) {
			anySkip = true
		}
		got := goTestPackageArgs(rest)
		if len(got) == 0 {
			pkgs = append(pkgs, ".")
		} else {
			pkgs = append(pkgs, got...)
		}
	}
	if !sawTest {
		return nil, false
	}
	out := []string{"go", "test"}
	if anyRun {
		out = append(out, "-run", "_")
	}
	if anySkip {
		out = append(out, "-skip", "_")
	}
	for _, p := range pkgs {
		out = append(out, prefixNestedPattern(dir, p))
	}
	return out, true
}

func bashCScript(argv []string) (string, bool) {
	if len(argv) < 3 {
		return "", false
	}
	switch filepath.Base(argv[0]) {
	case "bash", "sh":
	default:
		return "", false
	}
	for i := 1; i < len(argv)-1; i++ {
		a := argv[i]
		if a == "-c" || isClusteredBashC(a) {
			script := strings.TrimSpace(argv[i+1])
			if script == "" {
				return "", false
			}
			return script, true
		}
	}
	return "", false
}

func isClusteredBashC(a string) bool {
	if len(a) < 2 || a[0] != '-' || strings.HasPrefix(a, "--") {
		return false
	}
	for _, c := range a[1:] {
		if c < 'A' || (c > 'Z' && c < 'a') || c > 'z' {
			return false
		}
	}
	return strings.ContainsRune(a, 'c')
}

func splitShellAnd(script string) []string {
	var segs []string
	var b strings.Builder
	inSingle, inDouble := false, false
	flush := func() {
		s := strings.TrimSpace(b.String())
		b.Reset()
		if s != "" {
			segs = append(segs, s)
		}
	}
	for i := 0; i < len(script); i++ {
		c := script[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			b.WriteByte(c)
		case c == '"' && !inSingle:
			inDouble = !inDouble
			b.WriteByte(c)
		case !inSingle && !inDouble && c == '&' && i+1 < len(script) && script[i+1] == '&':
			flush()
			i++
		case !inSingle && !inDouble && c == ';':
			flush()
		default:
			b.WriteByte(c)
		}
	}
	flush()
	return segs
}

func parseCdSegment(seg string) (string, bool) {
	words := strings.Fields(seg)
	if len(words) != 2 || words[0] != "cd" {
		return "", false
	}
	return safeRelDir(words[1])
}

func safeRelDir(p string) (string, bool) {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"'`)
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimSuffix(p, "/")
	if p == "" || p == "." || strings.HasPrefix(p, "/") {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
		for _, r := range seg {
			if unicode.IsSpace(r) || strings.ContainsRune("$`;&|<>(){}!*?~", r) {
				return "", false
			}
		}
	}
	return p, true
}

func stripEnvAssigns(words []string) []string {
	i := 0
	for i < len(words) && isEnvAssign(words[i]) {
		i++
	}
	return words[i:]
}

func isEnvAssign(w string) bool {
	eq := strings.IndexByte(w, '=')
	if eq <= 0 {
		return false
	}
	for i, r := range w[:eq] {
		if i == 0 && !unicode.IsLetter(r) && r != '_' {
			return false
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func prefixNestedPattern(dir, p string) string {
	p = strings.TrimPrefix(strings.TrimSpace(p), "./")
	switch {
	case p == "" || p == ".":
		return "./" + dir
	case p == "...":
		return "./" + dir + "/..."
	default:
		return "./" + dir + "/" + p
	}
}
