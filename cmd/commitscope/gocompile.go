// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/commitscope"
)

// goCompileVerdict builds the packages a commit touches under scripts/ and
// cmd/ against the INDEX tree (🎯T822). Worktree dirt from other workers is
// masked via `go build -overlay`:
//
//   - tracked paths whose worktree bytes differ from the index are replaced
//     with the index blob;
//   - untracked .go files under a touched package are treated as deleted
//     (overlay value ""), so a neighbour's WIP file cannot fail the build.
//
// A clean tree with staged content already written to disk needs no overlay
// and finishes in the time of `go build` alone.
//
// Returns (message, refused). An empty package list is a no-op so commits
// that touch no watched Go path are not slowed.
func goCompileVerdict(staged []string, disabled bool) (string, bool) {
	pkgs := commitscope.TouchedGoPackages(staged)
	if len(pkgs) == 0 || disabled {
		return "", false
	}
	root, err := one("git", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Sprintf("commitscope: 🎯T822: cannot locate repo root: %v\n", err), true
	}
	out, err := buildPackagesAgainstIndex(root, pkgs)
	if err == nil {
		return "", false
	}
	return commitscope.GoCompileRefusal(pkgs, string(out)), true
}

// buildPackagesAgainstIndex runs `go build` on pkgs at root with an overlay
// that forces the build to see the index tree for the touched packages.
func buildPackagesAgainstIndex(root string, pkgs []string) ([]byte, error) {
	replace, tmpDir, err := indexOverlay(root, pkgs)
	if err != nil {
		return nil, err
	}
	var overlayPath string
	defer func() {
		if overlayPath != "" {
			_ = os.Remove(overlayPath)
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	args := []string{"build"}
	if len(replace) > 0 {
		payload, err := json.Marshal(struct {
			Replace map[string]string `json:"Replace"`
		}{Replace: replace})
		if err != nil {
			return nil, err
		}
		f, err := os.CreateTemp("", "commitscope-overlay-*.json")
		if err != nil {
			return nil, err
		}
		overlayPath = f.Name()
		if _, err := f.Write(payload); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		args = append(args, "-overlay", overlayPath)
	}
	args = append(args, pkgs...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	// Drop a hostile inherited GOFLAGS; module cache hits are the common case.
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	return cmd.CombinedOutput()
}

// indexOverlay builds the go-build Replace map for pkgs. tmpDir holds index
// blobs and must be removed by the caller; it is empty when nothing needs
// overlaying.
func indexOverlay(root string, pkgs []string) (replace map[string]string, tmpDir string, err error) {
	replace = map[string]string{}

	divergent, err := gitLines(root, "diff", "--name-only", "-z")
	if err != nil {
		return nil, "", err
	}
	untracked, err := gitLines(root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, "", err
	}

	pkgDirs := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		pkgDirs = append(pkgDirs, strings.TrimPrefix(p, "./"))
	}
	inTouchedPkg := func(path string) bool {
		path = filepath.ToSlash(path)
		for _, dir := range pkgDirs {
			if path == dir || strings.HasPrefix(path, dir+"/") {
				return true
			}
		}
		return false
	}

	needTmp := false
	for _, p := range divergent {
		if overlayRelevant(p) {
			needTmp = true
			break
		}
	}
	if !needTmp {
		for _, p := range untracked {
			if strings.HasSuffix(p, ".go") && inTouchedPkg(p) {
				needTmp = true
				break
			}
		}
	}
	if !needTmp {
		return replace, "", nil
	}

	tmpDir, err = os.MkdirTemp("", "commitscope-overlay-")
	if err != nil {
		return nil, "", err
	}

	i := 0
	for _, p := range divergent {
		if !overlayRelevant(p) {
			continue
		}
		blob, showErr := gitShowIndex(root, p)
		dst := ""
		if showErr == nil {
			safe := fmt.Sprintf("%d-%s", i, strings.ReplaceAll(p, string(os.PathSeparator), "_"))
			dst = filepath.Join(tmpDir, safe)
			if err := os.WriteFile(dst, blob, 0o644); err != nil {
				return nil, tmpDir, err
			}
		}
		// Empty dst ⇒ file treated as deleted (not in index, or deleted there).
		replace[filepath.ToSlash(p)] = dst
		i++
	}
	for _, p := range untracked {
		if !strings.HasSuffix(p, ".go") || !inTouchedPkg(p) {
			continue
		}
		// Untracked neighbour WIP: hide it from the package build.
		replace[filepath.ToSlash(p)] = ""
	}
	return replace, tmpDir, nil
}


// overlayRelevant reports paths whose worktree/index divergence can change
// a Go build. Skipping images and other dirt keeps the hook in the seconds.
func overlayRelevant(p string) bool {
	p = filepath.ToSlash(p)
	base := filepath.Base(p)
	switch {
	case strings.HasSuffix(p, ".go"), base == "go.mod", base == "go.sum":
		return true
	case strings.HasSuffix(p, ".md"): // //go:embed persona.md / help_agent.md / roles
		return true
	default:
		return false
	}
}

// gitLines runs git in root and splits NUL-terminated stdout into paths.
func gitLines(root string, args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	var paths []string
	for p := range strings.SplitSeq(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// gitShowIndex reads path's bytes from the effective index (GIT_INDEX_FILE).
func gitShowIndex(root, path string) ([]byte, error) {
	cmd := exec.Command("git", "show", ":"+path)
	cmd.Dir = root
	return cmd.Output()
}
