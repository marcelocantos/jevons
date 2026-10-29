// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitbase_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitbase"
	"github.com/marcelocantos/jevons/internal/worktreereap"
)

func linkedRepo(t *testing.T) (*repo, *repo) {
	t.Helper()
	base := newRepo(t)
	path := filepath.Join(t.TempDir(), "linked")
	if out, err := base.git(t, "worktree", "add", "-b", "linked-worker", path); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	if err := worktreereap.Mark(&worktreereap.MarkArgs{Worktree: path, Note: t.Name()}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(path, ".git")); err != nil || !st.Mode().IsRegular() {
		t.Fatalf("fixture must have a .git file: %v %v", st, err)
	}
	return base, &repo{dir: path}
}

func TestT1204LinkedWorktreeCommitsOnlyExactBlobs(t *testing.T) {
	base, linked := linkedRepo(t)
	baseHead := base.head(t)
	// A foreign path has both staged and additional unstaged content. The hot
	// file also contains foreign WIP, so it cannot be staged from the worktree.
	linked.write(t, "README", "foreign staged\n")
	if out, err := linked.git(t, "add", "README"); err != nil {
		t.Fatalf("stage foreign: %s %v", out, err)
	}
	linked.write(t, "README", "foreign staged\nforeign unstaged\n")
	hotWIP := "ours\nforeign hot-file WIP\n"
	linked.write(t, hotFile, hotWIP)
	res, err := commitbase.Commit(&commitbase.CommitArgs{Dir: linked.dir, Message: "exact blobs in linked tree", Blobs: map[string][]byte{hotFile: []byte("ours\n"), "new.txt": []byte("new exact blob\n")}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IndexWarning != "" {
		t.Fatalf("index sync: %s", res.IndexWarning)
	}
	for path, want := range map[string]string{hotFile: "ours\n", "new.txt": "new exact blob\n", "README": "base\n"} {
		got, err := linked.git(t, "show", "HEAD:"+path)
		if err != nil || got != want {
			t.Fatalf("committed %s=%q, want %q: %v", path, got, want, err)
		}
	}
	changed, err := linked.git(t, "diff-tree", "--no-commit-id", "--name-only", "-r", res.CommitSHA)
	if err != nil || strings.TrimSpace(changed) != hotFile+"\nnew.txt" {
		t.Fatalf("commit includes foreign paths: %q %v", changed, err)
	}
	staged, err := linked.git(t, "diff", "--cached", "--name-status")
	if err != nil || strings.TrimSpace(staged) != "M\tREADME" {
		t.Fatalf("foreign index changed: %q %v", staged, err)
	}
	if got, err := linked.git(t, "show", ":README"); err != nil || got != "foreign staged\n" {
		t.Fatalf("foreign staged bytes changed: %q %v", got, err)
	}
	for path, want := range map[string]string{hotFile: hotWIP, "README": "foreign staged\nforeign unstaged\n"} {
		got, err := os.ReadFile(filepath.Join(linked.dir, path))
		if err != nil || string(got) != want {
			t.Fatalf("foreign WIP changed in %s: %q %v", path, got, err)
		}
	}
	gitIndex, err := linked.git(t, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil || filepath.Dir(res.IndexFile) != filepath.Dir(strings.TrimSpace(gitIndex)) {
		t.Fatalf("private index outside worktree git directory: %s / %s (%v)", res.IndexFile, gitIndex, err)
	}
	if _, err := os.Stat(res.IndexFile); !os.IsNotExist(err) {
		t.Fatalf("successful temporary index not removed: %v", err)
	}
	if got := base.head(t); got != baseHead {
		t.Fatalf("main worktree HEAD moved: %s", got)
	}
}

func TestT1204LinkedWorktreeStillRefusesMovedHEAD(t *testing.T) {
	_, linked := linkedRepo(t)
	var interloper string
	res, err := commitbase.Commit(&commitbase.CommitArgs{Dir: linked.dir, Message: "must refuse stale blob", Blobs: map[string][]byte{hotFile: []byte("ours\n")}, AfterSeed: func() error {
		linked.write(t, "README", "interloper committed\n")
		if out, err := linked.git(t, "add", "README"); err != nil {
			return errors.New(out + err.Error())
		}
		if out, err := linked.git(t, "commit", "-m", "interloper"); err != nil {
			return errors.New(out + err.Error())
		}
		interloper = linked.head(t)
		return nil
	}})
	var refused *commitbase.RefuseError
	if res != nil || !errors.As(err, &refused) {
		t.Fatalf("stale linked-tree commit accepted: %+v %v", res, err)
	}
	if linked.head(t) != interloper || !strings.Contains(refused.Message, "README") {
		t.Fatalf("stale refusal lost interloper: %v", err)
	}
	if got, err := linked.git(t, "show", "HEAD:"+hotFile); err != nil || got != "base\n" {
		t.Fatalf("stale blob landed: %q %v", got, err)
	}
}
