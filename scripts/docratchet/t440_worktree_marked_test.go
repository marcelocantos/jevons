// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// 🎯T440: a worktree created without recording its owner is a leak waiting to
// happen. On 2026-08-15 `git worktree list` in the shared clone had seventeen
// entries holding 289MB, fourteen of them belonging to sessions that were over
// — including three from the ratchets in this very directory, which do defer a
// `worktree remove --force`. Cleanup inside the dying process does not run when
// the process is killed, so the surviving evidence of ownership has to be
// written at creation and read from outside afterwards.
//
// This is the general form of that fix rather than a check on the two tests
// that leaked: the next `git worktree add` anyone writes is the next leak, and
// a ratchet that named only today's sites would not see it.
package docratchet_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exemption is what a creator writes when marking is genuinely wrong for it —
// followed, in the same comment, by why. The build snapshot is the standing
// case: it is a permanent worktree the daemon protects by path, and stamping
// it with the pid of a daemon that restarts several times a day would record
// an owner that is dead by design.
const exemption = "//worktreereap:exempt"

// TestT440WorktreeCreatorsRecordTheirOwner asserts that every Go file which
// runs `git worktree add` either marks the result or declares an exemption.
func TestT440WorktreeCreatorsRecordTheirOwner(t *testing.T) {
	root := repoRoot(t)

	skipDir := map[string]bool{
		".git": true, "bin": true, "build": true, ".build": true,
		"node_modules": true, "vendor": true, "ios": true,
	}

	var offenders []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDir[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src := string(body)
		// The call is spelled as git arguments, so match the argument pair
		// rather than a function name: `"worktree", "add"`.
		if !strings.Contains(src, `"worktree", "add"`) {
			return nil
		}
		if strings.Contains(src, "worktreereap.Mark(") || strings.Contains(src, "Mark(&MarkArgs{") || strings.Contains(src, exemption) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		offenders = append(offenders, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(offenders) > 0 {
		t.Fatalf("these files create a git worktree without recording its owner:\n  %s\n\n"+
			"Call worktreereap.Mark right after `git worktree add`, so the 🎯T440 sweeper can\n"+
			"reap the tree when this process dies without running its cleanup — a timeout panic,\n"+
			"a SIGKILL and a dropped session all skip defer and t.Cleanup, and `git worktree prune`\n"+
			"cannot help because the leaked directory still exists.\n"+
			"If marking is genuinely wrong here, write %s in the same file with the reason.",
			strings.Join(offenders, "\n  "), exemption)
	}
}
