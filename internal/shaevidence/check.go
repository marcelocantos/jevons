// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package shaevidence

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Reachability is what `git merge-base --is-ancestor` (plus object presence)
// says about a cited SHA relative to HEAD.
type Reachability int

const (
	// Ancestor: the SHA is reachable from HEAD — stable evidence.
	Ancestor Reachability = iota
	// Rewritten: the object still exists locally but is not an ancestor of
	// HEAD — typically a tip that bullseye (or a rebase) amended away.
	Rewritten
	// Missing: no such object in this repository.
	Missing
)

func (r Reachability) String() string {
	switch r {
	case Ancestor:
		return "ancestor"
	case Rewritten:
		return "rewritten"
	case Missing:
		return "missing"
	default:
		return fmt.Sprintf("reachability(%d)", int(r))
	}
}

// CheckFunc classifies one SHA. Tests inject fakes; production uses CheckInRepo.
type CheckFunc func(sha string) Reachability

// CheckInRepo returns a CheckFunc bound to dir's git repository and headRef
// (usually "HEAD"). A SHA that is not a commit object is Missing.
func CheckInRepo(dir, headRef string) CheckFunc {
	if strings.TrimSpace(headRef) == "" {
		headRef = "HEAD"
	}
	return func(sha string) Reachability {
		sha = strings.TrimSpace(sha)
		if sha == "" {
			return Missing
		}
		if !gitOK(dir, "cat-file", "-e", sha+"^{commit}") {
			return Missing
		}
		if gitOK(dir, "merge-base", "--is-ancestor", sha, headRef) {
			return Ancestor
		}
		return Rewritten
	}
}

func gitOK(dir string, args ...string) bool {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return err == nil
}
