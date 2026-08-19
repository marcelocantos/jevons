// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitbase

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CommitArgs is everything the blessed recipe needs to write one scoped
// commit against the HEAD it was seeded from.
type CommitArgs struct {
	// Dir is the repository work tree. Empty means the current directory.
	Dir string
	// IndexFile is the worker-owned GIT_INDEX_FILE. Empty creates one under
	// Dir/.git/ and removes it after a successful commit.
	IndexFile string
	// Message is the commit message. Required.
	Message string
	// Paths are staged from the work tree as-is. Use these for files no
	// other worker has uncommitted hunks in.
	Paths []string
	// Blobs stage exact content at a path, ignoring the work tree. This is
	// the case `git commit --only` cannot cover: a shared hot file (Makefile,
	// AGENTS.md) that still holds another worker's uncommitted hunks.
	Blobs map[string][]byte
	// Disabled turns the HEAD-staleness check off (DisableEnv).
	Disabled bool
	// AfterSeed runs after read-tree and before the HEAD re-check. Tests use
	// it to land an interloper commit in the race window; production leaves
	// it nil.
	AfterSeed func() error
	// AuthorName / AuthorEmail override committer identity when set. Tests
	// use them so a hermetic repo needs no global git config.
	AuthorName  string
	AuthorEmail string
}

// Result is what a successful Commit wrote.
type Result struct {
	CommitSHA string
	BaseSHA   string
	IndexFile string
}

// Commit runs the blessed recipe:
//
//  1. record base = HEAD
//  2. read-tree base into the private index
//  3. stage Paths from the work tree and Blobs as exact content
//  4. re-read HEAD; if it moved, refuse (naming the paths that would be lost)
//  5. write-tree → commit-tree -p base → update-ref with expected old = base
//
// Step 4 is the load-bearing check. update-ref's old/new CAS alone is not
// enough: a worker who re-reads HEAD for both the parent and the CAS still
// writes a tree derived from the stale seed.
func Commit(args *CommitArgs) (*Result, error) {
	if args == nil {
		return nil, fmt.Errorf("commitbase: nil args")
	}
	if strings.TrimSpace(args.Message) == "" {
		return nil, fmt.Errorf("commitbase: commit message is required")
	}
	if len(args.Paths) == 0 && len(args.Blobs) == 0 {
		return nil, fmt.Errorf("commitbase: nothing to commit — pass Paths and/or Blobs")
	}

	dir := args.Dir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}

	index := args.IndexFile
	tempIndex := false
	if index == "" {
		index = filepath.Join(dir, ".git", "index-commitbase-"+fmt.Sprintf("%d", os.Getpid()))
		tempIndex = true
	}

	git := func(extraEnv []string, argv ...string) (string, error) {
		return runGit(dir, append([]string{"GIT_INDEX_FILE=" + index}, extraEnv...), argv...)
	}

	base, err := runGit(dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("commitbase: rev-parse HEAD: %w", err)
	}
	base = strings.TrimSpace(base)

	if _, err := git(nil, "read-tree", base); err != nil {
		return nil, fmt.Errorf("commitbase: read-tree %s: %w", short(base), err)
	}

	for _, p := range args.Paths {
		if _, err := git(nil, "update-index", "--add", "--", p); err != nil {
			return nil, fmt.Errorf("commitbase: stage %s from work tree: %w", p, err)
		}
	}
	for path, content := range args.Blobs {
		if err := stageBlob(dir, index, path, content); err != nil {
			return nil, fmt.Errorf("commitbase: stage blob %s: %w", path, err)
		}
	}

	if args.AfterSeed != nil {
		if err := args.AfterSeed(); err != nil {
			return nil, fmt.Errorf("commitbase: AfterSeed: %w", err)
		}
	}

	head, err := runGit(dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("commitbase: re-check rev-parse HEAD: %w", err)
	}
	head = strings.TrimSpace(head)

	lost, err := pathsChanged(dir, base, head)
	if err != nil {
		return nil, err
	}
	v := Decide(Check{
		BaseSHA:   base,
		HeadSHA:   head,
		LostPaths: lost,
		Disabled:  args.Disabled,
	})
	if v.Refused {
		return nil, &RefuseError{Message: v.Message}
	}

	tree, err := git(nil, "write-tree")
	if err != nil {
		return nil, fmt.Errorf("commitbase: write-tree: %w", err)
	}
	tree = strings.TrimSpace(tree)

	env := commitEnv(args)
	commit, err := git(env, "commit-tree", tree, "-p", base, "-m", args.Message)
	if err != nil {
		return nil, fmt.Errorf("commitbase: commit-tree: %w", err)
	}
	commit = strings.TrimSpace(commit)

	// CAS the ref against the seed, not against a freshly re-read HEAD. If
	// something landed between the re-check and here, this fails closed.
	if _, err := runGit(dir, nil, "update-ref", "HEAD", commit, base); err != nil {
		return nil, fmt.Errorf("commitbase: update-ref HEAD %s %s: %w\n"+
			"HEAD moved after the staleness check; refuse rather than force.",
			short(commit), short(base), err)
	}

	if tempIndex {
		_ = os.Remove(index)
	}
	return &Result{CommitSHA: commit, BaseSHA: base, IndexFile: index}, nil
}

// RefuseError is a deliberate 🎯T432 refusal (HEAD moved since seed).
type RefuseError struct {
	Message string
}

func (e *RefuseError) Error() string { return strings.TrimRight(e.Message, "\n") }

// StaleCommit is the RED-control recipe: seed from HEAD, stage, then write a
// commit whose parent is *current* HEAD while the tree is still the stale
// seed. This is the e66e934 shape — update-ref CAS against current HEAD
// succeeds and the interloper's paths disappear. Tests assert the blessed
// recipe does not do this.
func StaleCommit(args *CommitArgs) (*Result, error) {
	if args == nil {
		return nil, fmt.Errorf("commitbase: nil args")
	}
	dir := args.Dir
	index := args.IndexFile
	if index == "" {
		index = filepath.Join(dir, ".git", "index-stale-"+fmt.Sprintf("%d", os.Getpid()))
	}
	git := func(extraEnv []string, argv ...string) (string, error) {
		return runGit(dir, append([]string{"GIT_INDEX_FILE=" + index}, extraEnv...), argv...)
	}

	base, err := runGit(dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	base = strings.TrimSpace(base)
	if _, err := git(nil, "read-tree", base); err != nil {
		return nil, err
	}
	for _, p := range args.Paths {
		if _, err := git(nil, "update-index", "--add", "--", p); err != nil {
			return nil, err
		}
	}
	for path, content := range args.Blobs {
		if err := stageBlob(dir, index, path, content); err != nil {
			return nil, err
		}
	}
	if args.AfterSeed != nil {
		if err := args.AfterSeed(); err != nil {
			return nil, err
		}
	}
	// Re-read HEAD for parent + CAS — the move that looks safe and is not.
	head, err := runGit(dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	head = strings.TrimSpace(head)
	tree, err := git(nil, "write-tree")
	if err != nil {
		return nil, err
	}
	tree = strings.TrimSpace(tree)
	commit, err := git(commitEnv(args), "commit-tree", tree, "-p", head, "-m", args.Message)
	if err != nil {
		return nil, err
	}
	commit = strings.TrimSpace(commit)
	if _, err := runGit(dir, nil, "update-ref", "HEAD", commit, head); err != nil {
		return nil, err
	}
	return &Result{CommitSHA: commit, BaseSHA: base, IndexFile: index}, nil
}

func stageBlob(dir, index, path string, content []byte) error {
	hash, err := hashObject(dir, content)
	if err != nil {
		return err
	}
	_, err = runGit(dir, []string{"GIT_INDEX_FILE=" + index},
		"update-index", "--add", "--cacheinfo", "100644,"+hash+","+path)
	return err
}

func hashObject(dir string, content []byte) (string, error) {
	cmd := exec.Command("git", "hash-object", "-w", "--stdin")
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(content)
	cmd.Env = hermeticEnv(nil)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("hash-object: %w\n%s", err, out)
	}
	return strings.TrimSpace(string(out)), nil
}

func pathsChanged(dir, from, to string) ([]string, error) {
	if from == to {
		return nil, nil
	}
	out, err := runGit(dir, nil, "diff", "--name-only", from, to)
	if err != nil {
		return nil, fmt.Errorf("commitbase: diff --name-only %s %s: %w", short(from), short(to), err)
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}

func commitEnv(args *CommitArgs) []string {
	var env []string
	name := args.AuthorName
	email := args.AuthorEmail
	if name == "" {
		name = "commitbase"
	}
	if email == "" {
		email = "commitbase@example.invalid"
	}
	env = append(env,
		"GIT_AUTHOR_NAME="+name,
		"GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME="+name,
		"GIT_COMMITTER_EMAIL="+email,
	)
	return env
}

func runGit(dir string, extraEnv []string, argv ...string) (string, error) {
	cmd := exec.Command("git", argv...)
	cmd.Dir = dir
	cmd.Env = hermeticEnv(extraEnv)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", strings.Join(argv, " "), err, out)
	}
	return string(out), nil
}

func hermeticEnv(extra []string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	}
	return append(env, extra...)
}
