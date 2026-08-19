// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package treeguard

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestT466RepoWrites pins the attribution feed's extractor: mutating tool
// calls yield repo-relative paths, reads and out-of-repo writes yield none.
func TestT466RepoWrites(t *testing.T) {
	repo := t.TempDir()
	env := &Env{RepoRoot: repo}

	p := &Payload{ToolName: "Write"}
	p.ToolInput.FilePath = filepath.Join(repo, "internal", "x.go")
	if got := env.RepoWrites(p); !reflect.DeepEqual(got, []string{"internal/x.go"}) {
		t.Fatalf("Write: %v", got)
	}

	p = &Payload{ToolName: "Read"}
	p.ToolInput.FilePath = filepath.Join(repo, "internal", "x.go")
	if got := env.RepoWrites(p); got != nil {
		t.Fatalf("Read must not feed attribution: %v", got)
	}

	p = &Payload{ToolName: "Edit"}
	p.ToolInput.FilePath = "/etc/hosts"
	if got := env.RepoWrites(p); got != nil {
		t.Fatalf("outside-repo write must be dropped: %v", got)
	}

	p = &Payload{ToolName: ToolBash}
	p.ToolInput.Command = "echo hi > " + filepath.Join(repo, "notes.txt")
	if got := env.RepoWrites(p); !reflect.DeepEqual(got, []string{"notes.txt"}) {
		t.Fatalf("Bash redirect: %v", got)
	}
}
