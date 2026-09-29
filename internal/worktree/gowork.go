// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package worktree

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// An isolated tree lives beside the shared clone, so the workspace file that
// makes the shared clone build — the org-level ../go.work that `use`s the
// clone and its unpublished siblings (🎯T448) — still governs it, but does not
// list it. Every go command in the tree then fails with "directory . is
// contained in a module that is not one of the workspace modules". The worker
// cannot build, test, or gate its own slice until it notices and works around
// it; the ones before this fix did so by hand.
//
// MirrorGoWork gives the tree its own go.work: the governing workspace's go
// version, the same siblings by absolute path, and the tree itself in place of
// the clone. The file is kept out of git through the repo's info/exclude, so a
// worker's `git add` cannot carry it into a commit.

// MirrorGoWork writes wt/go.work mirroring the workspace that governs base.
// It reports whether it wrote one: no governing workspace, or a go.work
// already in the tree, is not an error and leaves the tree alone.
func MirrorGoWork(base, wt string) (bool, error) {
	dst := filepath.Join(wt, "go.work")
	if _, err := os.Stat(dst); err == nil {
		return false, nil
	}
	src, err := goEnv(base, "GOWORK")
	if err != nil {
		return false, err
	}
	if src == "" || src == "off" {
		return false, nil
	}
	cmd := exec.Command("go", "work", "edit", "-json", src)
	cmd.Dir = base
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("worktree: read %s: %w", src, err)
	}
	var work struct {
		Go  string
		Use []struct{ DiskPath string }
	}
	if err := json.Unmarshal(out, &work); err != nil {
		return false, fmt.Errorf("worktree: parse %s: %w", src, err)
	}

	var b strings.Builder
	if work.Go != "" {
		fmt.Fprintf(&b, "go %s\n\n", work.Go)
	}
	b.WriteString("use (\n\t.\n")
	srcDir := filepath.Dir(src)
	cleanBase := filepath.Clean(base)
	for _, u := range work.Use {
		p := u.DiskPath
		if !filepath.IsAbs(p) {
			p = filepath.Join(srcDir, p)
		}
		p = filepath.Clean(p)
		if p == cleanBase {
			continue // the tree stands in for the clone it was made from
		}
		fmt.Fprintf(&b, "\t%s\n", p)
	}
	b.WriteString(")\n")
	if err := excludeFromGit(base, "/go.work", "/go.work.sum"); err != nil {
		return false, err
	}
	if err := os.WriteFile(dst, []byte(b.String()), 0o644); err != nil {
		return false, fmt.Errorf("worktree: write %s: %w", dst, err)
	}
	return true, nil
}

// excludeFromGit appends patterns missing from the repo's info/exclude. That
// file is shared by every worktree of the repo, which is the point: one line
// covers every isolated tree. The shared clone itself has no go.work of its
// own (its workspace is the org-level one), so the lines change nothing there.
func excludeFromGit(base string, patterns ...string) error {
	common, err := gitOut(base, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	path := filepath.Join(common, "info", "exclude")
	have, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("worktree: read %s: %w", path, err)
	}
	present := map[string]bool{}
	for line := range strings.SplitSeq(string(have), "\n") {
		present[strings.TrimSpace(line)] = true
	}
	var add strings.Builder
	if len(have) > 0 && !strings.HasSuffix(string(have), "\n") {
		add.WriteString("\n")
	}
	missing := false
	for _, p := range patterns {
		if !present[p] {
			fmt.Fprintf(&add, "%s\n", p)
			missing = true
		}
	}
	if !missing {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("worktree: mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("worktree: open %s: %w", path, err)
	}
	if _, err := f.WriteString(add.String()); err != nil {
		f.Close()
		return fmt.Errorf("worktree: append %s: %w", path, err)
	}
	return f.Close()
}

func goEnv(dir, key string) (string, error) {
	cmd := exec.Command("go", "env", key)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("worktree: go env %s: %w", key, err)
	}
	return strings.TrimSpace(string(out)), nil
}
