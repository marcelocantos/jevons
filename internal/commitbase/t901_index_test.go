// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitbase_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/commitbase"
)

// 🎯T901: after a commit, the shared index agrees with it for the committed
// paths. Before, a new file read as a staged deletion and a changed file as a
// staged reversal, so the next commit of the index undid this one. Another
// worker's staged edit to an uncommitted path is left exactly as it was.
func TestT901SharedIndexFollowsTheCommit(t *testing.T) {
	r := newRepo(t)
	r.write(t, "other.txt", "theirs\n")
	if out, err := r.git(t, "add", "other.txt"); err != nil {
		t.Fatalf("stage the other worker's file: %v\n%s", err, out)
	}
	r.write(t, "new.txt", "mine\n")
	res, err := commitbase.Commit(&commitbase.CommitArgs{
		Dir:         r.dir,
		Message:     "mine",
		Paths:       []string{"new.txt"},
		Blobs:       map[string][]byte{hotFile: []byte("mine\n")},
		AuthorName:  "t",
		AuthorEmail: "t@example.invalid",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IndexWarning != "" {
		t.Fatalf("index warning: %s", res.IndexWarning)
	}
	staged, err := r.git(t, "diff", "--cached", "--name-status")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(staged), "\n") {
		if strings.HasSuffix(line, "new.txt") || strings.HasSuffix(line, hotFile) {
			t.Fatalf("committed path still staged against the new HEAD: %q (all: %q)", line, staged)
		}
	}
	if !strings.Contains(staged, "A\tother.txt") {
		t.Fatalf("the other worker's staged file was disturbed: %q", staged)
	}
}
