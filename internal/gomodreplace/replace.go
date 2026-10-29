// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package gomodreplace detects committed replace directives for
// github.com/marcelocantos/… modules (🎯T448 / 🎯T1151.6).
package gomodreplace

import (
	"fmt"
	"strings"
)

const MarcelocantosPrefix = "github.com/marcelocantos/"

// MarcelocantosReplaceLines returns every replace directive line in body that
// names a github.com/marcelocantos/… module.
func MarcelocantosReplaceLines(body string) []string {
	var hits []string
	inBlock := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inBlock && trimmed == ")":
			inBlock = false
			continue
		case !inBlock && strings.HasPrefix(trimmed, "replace") && strings.HasSuffix(trimmed, "("):
			inBlock = true
			continue
		case !inBlock && !strings.HasPrefix(trimmed, "replace "):
			continue
		}
		if inBlock && !strings.Contains(trimmed, "=>") {
			continue
		}
		mod := replacedModulePath(trimmed)
		if mod != "" && strings.HasPrefix(mod, MarcelocantosPrefix) {
			hits = append(hits, strings.TrimSpace(line))
		}
	}
	return hits
}

func replacedModulePath(trimmed string) string {
	fields := strings.Fields(strings.TrimPrefix(trimmed, "replace "))
	if len(fields) == 0 {
		return ""
	}
	mod := fields[0]
	if i := strings.Index(mod, "=>"); i >= 0 {
		mod = strings.TrimSpace(mod[:i])
	}
	return mod
}

// NewMarcelocantosReplaceLines returns staged replace lines that are not
// already present unchanged in head (add or change).
func NewMarcelocantosReplaceLines(head, staged string) []string {
	stagedHits := MarcelocantosReplaceLines(staged)
	if len(stagedHits) == 0 {
		return nil
	}
	headSet := make(map[string]bool, len(MarcelocantosReplaceLines(head)))
	for _, line := range MarcelocantosReplaceLines(head) {
		headSet[line] = true
	}
	var newOnes []string
	for _, line := range stagedHits {
		if !headSet[line] {
			newOnes = append(newOnes, line)
		}
	}
	return newOnes
}

// CommitscopeRefusalMessage names 🎯T448 and the publish-then-pin path.
func CommitscopeRefusalMessage(line string) string {
	line = strings.TrimSpace(line)
	return fmt.Sprintf(
		"commitscope: refusing go.mod with a replace of a github.com/marcelocantos module (🎯T448 / 🎯T1151.6).\n\n"+
			"  %s\n\n"+
			"Publish the sibling change, bump the require pin, and send a needs-release request to that repo's product owner — "+
			"do not commit a local replace (../sibling or an absolute path).\n",
		line,
	)
}
