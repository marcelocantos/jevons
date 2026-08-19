// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package shaevidence

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckInRepoClassifiesAncestorRewrittenMissing(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t427", "GIT_AUTHOR_EMAIL=t427@test",
			"GIT_COMMITTER_NAME=t427", "GIT_COMMITTER_EMAIL=t427@test",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-b", "master")
	run("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "first")
	first := run("rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte("targets: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "bullseye.yaml")
	run("commit", "-m", "yaml only tip")
	amendedAway := run("rev-parse", "HEAD")

	// Amend the yaml-only tip away — the old SHA remains as an object.
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte("targets: {T1: {}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "bullseye.yaml")
	run("commit", "--amend", "--no-edit")
	head := run("rev-parse", "HEAD")

	check := CheckInRepo(dir, "HEAD")
	if k := check(first); k != Ancestor {
		t.Fatalf("first commit: got %s, want ancestor", k)
	}
	if k := check(head); k != Ancestor {
		t.Fatalf("HEAD: got %s, want ancestor", k)
	}
	if k := check(amendedAway); k != Rewritten {
		t.Fatalf("amended-away tip %s: got %s, want rewritten", amendedAway[:7], k)
	}
	if k := check("0000000000000000000000000000000000000001"); k != Missing {
		t.Fatalf("missing: got %s, want missing", k)
	}
}

func TestScanFindingsReportsOnlyNonAncestors(t *testing.T) {
	check := CheckFunc(func(sha string) Reachability {
		switch sha {
		case "aaa1111":
			return Ancestor
		case "bbb2222":
			return Rewritten
		case "ccc3333":
			return Missing
		default:
			return Missing
		}
	})
	text := "SHA aaa1111 ok\nSHA bbb2222 gone\ncommit ccc3333 never"
	got := ScanFindings(text, check)
	if len(got) != 2 {
		t.Fatalf("got %#v, want rewritten+missing", got)
	}
	if got[0].SHA != "bbb2222" || got[0].Kind != Rewritten {
		t.Fatalf("first finding %#v", got[0])
	}
	if got[1].SHA != "ccc3333" || got[1].Kind != Missing {
		t.Fatalf("second finding %#v", got[1])
	}
}
