// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// test-web-clean is the sanctioned clean-checkout web gate (🎯T659, for
// 🎯T398): one recipe that owns its worktree's dependencies end to end.
//
// It checks one commit (default HEAD) out into a detached worktree, runs
// `make test-web` there under the gate — which installs ui dependencies
// inside that tree with `npm ci`, never through a link into this clone —
// and removes the tree behind gate.RemoveWorktree's foreign-symlink guard.
// The shared clone's ui/node_modules is hashed before and after, and any
// difference fails the run even when the suite was green: the point of the
// target is that verifying a commit cannot cost a neighbour their deps.
//
// The GATE line it prints carries tree=clean@<sha> and is the citable
// evidence; wrap the whole thing in bin/gate to record the outer status too.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/marcelocantos/jevons/internal/gate"
)

const (
	gateName = "test-web-clean"
	// diffSampleCap bounds how many changed paths an error names; the first
	// few say whose install ran, the rest are noise.
	diffSampleCap = 10
	exitError     = 1
	exitSuspect   = 3
)

func main() {
	sha := flag.String("sha", "HEAD", "commit to verify")
	repo := flag.String("repo", ".", "any directory inside the clone to take the commit from")
	keep := flag.Bool("keep", false, "leave the worktree behind for inspection")
	flag.Parse()
	os.Exit(run(*repo, *sha, *keep))
}

func run(repo, sha string, keep bool) int {
	root, err := gitOut(repo, "rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintln(os.Stderr, "test-web-clean:", err)
		return exitError
	}
	shared := filepath.Join(root, "ui", "node_modules")
	before, err := manifest(shared)
	if err != nil {
		fmt.Fprintln(os.Stderr, "test-web-clean: hash shared ui/node_modules:", err)
		return exitError
	}
	fmt.Fprintf(os.Stderr, "test-web-clean: shared %s holds %d entries before the run\n", shared, len(before))

	store, err := gate.OpenStore("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "test-web-clean:", err)
		return exitError
	}
	res, runErr := gate.RunClean(&gate.CleanArgs{
		Command:  []string{"make", "test-web"},
		Repo:     root,
		Commit:   sha,
		Keep:     keep,
		Name:     gateName,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		Store:    store,
		Explicit: true,
	})
	if res == nil || res.Record == nil {
		fmt.Fprintln(os.Stderr, "test-web-clean:", runErr)
		return exitError
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "test-web-clean: record not saved:", runErr)
	}
	rec := res.Record
	fmt.Fprintln(os.Stderr, rec.Summary())
	if res.Kept {
		fmt.Fprintln(os.Stderr, "  worktree kept at", res.Worktree)
	}

	after, err := manifest(shared)
	if err != nil {
		fmt.Fprintln(os.Stderr, "test-web-clean: hash shared ui/node_modules after the run:", err)
		return exitError
	}
	if changed := diff(before, after); len(changed) > 0 {
		fmt.Fprintf(os.Stderr, "test-web-clean: the shared %s is NOT byte-identical after the run (%d entries differ; 🎯T659). First few:\n  %s\n",
			shared, len(changed), strings.Join(changed, "\n  "))
		return exitError
	}
	fmt.Fprintf(os.Stderr, "test-web-clean: shared ui/node_modules byte-identical after the run (%d entries)\n", len(after))

	switch {
	case rec.Verdict == gate.VerdictSuspect:
		return exitSuspect
	case !rec.StatusKnown:
		return exitError
	default:
		return rec.ExitStatus
	}
}

// manifest describes every entry under dir by relative path: a directory,
// a symlink and its target, or a file with its mode, size and content hash.
// A missing dir is an empty manifest, so a clone that never installed deps
// still gets the before/after comparison it deserves.
func manifest(dir string) (map[string]string, error) {
	m := map[string]string{}
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(dir)
		if err != nil {
			return nil, err
		}
		m["."] = "symlink " + target
		return m, nil
	}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		switch {
		case d.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			m[rel] = "symlink " + target
		case d.IsDir():
			m[rel] = "dir"
		default:
			info, err := d.Info()
			if err != nil {
				return err
			}
			sum, err := hashFile(path)
			if err != nil {
				return err
			}
			m[rel] = fmt.Sprintf("file %o %d %s", info.Mode().Perm(), info.Size(), sum)
		}
		return nil
	})
	return m, err
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// diff lists the paths whose description changed, was added or was removed,
// sorted, capped at diffSampleCap with a trailing count of the rest.
func diff(before, after map[string]string) []string {
	var changed []string
	for path, was := range before {
		if now, ok := after[path]; !ok {
			changed = append(changed, "removed "+path)
		} else if now != was {
			changed = append(changed, "changed "+path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			changed = append(changed, "added "+path)
		}
	}
	sort.Strings(changed)
	if len(changed) > diffSampleCap {
		rest := len(changed) - diffSampleCap
		changed = append(changed[:diffSampleCap], fmt.Sprintf("… and %d more", rest))
	}
	return changed
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
