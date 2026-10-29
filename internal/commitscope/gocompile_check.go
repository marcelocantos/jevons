// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var goCompileErrorLine = regexp.MustCompile(`\.go:\d+:\d+:`)

// CheckStagedGoCompile builds every Go package touched in staged, using the
// tree git would commit (write-tree on the hook's index). Another worker's
// dirty worktree edits are not read. When disabled, or when no staged path
// is a .go file, or when the repo has no go.mod, this is a no-op.
//
// Packages that no longer exist on the index tree (a deleted directory) are
// dropped rather than handed to `go build`, which would refuse with
// `stat …/commitscope-gocompile-…/<dir>: directory not found` (🎯T1285).
// A package under a nested module (its own go.mod) is compiled inside that
// module with GOWORK=off, not as a package of the root module.
func CheckStagedGoCompile(repoRoot, indexFile string, staged []string, disabled bool) (refuse bool, msg string, err error) {
	if disabled {
		return false, "", nil
	}
	pkgs := GoPackageDirsFromStaged(staged)
	if len(pkgs) == 0 {
		return false, "", nil
	}
	if _, statErr := os.Stat(filepath.Join(repoRoot, "go.mod")); statErr != nil {
		return false, "", nil
	}
	tree, err := gitWriteTree(repoRoot, indexFile)
	if err != nil {
		return false, "", fmt.Errorf("write-tree: %w", err)
	}
	tmp, err := os.MkdirTemp("", "commitscope-gocompile-")
	if err != nil {
		return false, "", err
	}
	defer os.RemoveAll(tmp)
	if err := gitArchiveTree(repoRoot, tree, tmp); err != nil {
		return false, "", err
	}
	line, buildErr := goBuildPackages(tmp, pkgs)
	if buildErr == nil {
		return false, "", nil
	}
	if line == "" {
		line = strings.TrimSpace(buildErr.Error())
	}
	return true, GoCompileRefusal(line), nil
}

func gitWriteTree(repoRoot, indexFile string) (string, error) {
	cmd := exec.Command("git", "write-tree")
	cmd.Dir = repoRoot
	if strings.TrimSpace(indexFile) != "" {
		cmd.Env = append(os.Environ(), "GIT_INDEX_FILE="+indexFile)
	}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitArchiveTree(repoRoot, tree, dest string) error {
	archive := exec.Command("git", "archive", "--format=tar", tree)
	archive.Dir = repoRoot
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	extract := exec.Command("tar", "-x", "-C", dest)
	extract.Stdin = pipe
	var extractErr bytes.Buffer
	extract.Stderr = &extractErr
	if err := extract.Start(); err != nil {
		return fmt.Errorf("tar -x: %w", err)
	}
	if err := archive.Run(); err != nil {
		extract.Wait()
		return fmt.Errorf("git archive %s: %w", tree, err)
	}
	if err := extract.Wait(); err != nil {
		return fmt.Errorf("tar -x: %w: %s", err, extractErr.String())
	}
	return nil
}

func goBuildPackages(workdir string, pkgs []string) (string, error) {
	type group struct {
		cwd  string
		pkgs []string
	}
	byMod := map[string]*group{}
	var order []string
	for _, p := range pkgs {
		abs := filepath.Join(workdir, filepath.FromSlash(p))
		st, err := os.Stat(abs)
		if err != nil || !st.IsDir() {
			// Deleted on the index tree: nothing left to compile (🎯T1285).
			continue
		}
		modRel, pkgRel := packageInModule(workdir, p)
		g, ok := byMod[modRel]
		if !ok {
			cwd := workdir
			if modRel != "." {
				cwd = filepath.Join(workdir, filepath.FromSlash(modRel))
			}
			g = &group{cwd: cwd}
			byMod[modRel] = g
			order = append(order, modRel)
		}
		g.pkgs = append(g.pkgs, pkgRel)
	}
	sort.Strings(order)
	for _, modRel := range order {
		g := byMod[modRel]
		if line, err := goBuildPackageList(g.cwd, g.pkgs); err != nil {
			return line, err
		}
	}
	return "", nil
}

func goBuildPackageList(cwd string, pkgs []string) (string, error) {
	// A test-only package (scripts/docratchet) has nothing for `go build`,
	// which refuses it with "no non-test Go files". Compile its test binary
	// instead, without running it, so TestMain and init never execute here.
	var build, testOnly []string
	for _, rel := range pkgs {
		dir := filepath.Join(cwd, filepath.FromSlash(strings.TrimPrefix(rel, "./")))
		if hasNonTestGo(dir) {
			build = append(build, rel)
		} else if hasAnyGo(dir) {
			testOnly = append(testOnly, rel)
		}
	}
	if len(build) > 0 {
		// No -o: workdir is a throwaway extraction of the index, so a main
		// package's binary lands there and goes with it. A file -o target
		// fails for more than one package, and a directory target fails
		// when none of them is main.
		if line, err := runGo(cwd, append([]string{"build"}, build...)); err != nil {
			return line, err
		}
	}
	for _, rel := range testOnly {
		if line, err := runGo(cwd, []string{"test", "-c", "-o", os.DevNull, rel}); err != nil {
			return line, err
		}
	}
	return "", nil
}

// packageInModule reports the nearest enclosing module of pkg (a slash path
// relative to workdir) and the package path relative to that module, suitable
// for `go build`. Nested modules compile inside their own module (🎯T1285).
func packageInModule(workdir, pkg string) (modRel, pkgRel string) {
	pkg = filepath.ToSlash(strings.TrimPrefix(pkg, "./"))
	absPkg := filepath.Join(workdir, filepath.FromSlash(pkg))
	dir := absPkg
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			relMod, err := filepath.Rel(workdir, dir)
			if err != nil {
				return ".", "./" + pkg
			}
			relMod = filepath.ToSlash(relMod)
			relPkg, err := filepath.Rel(dir, absPkg)
			if err != nil {
				relPkg = "."
			}
			relPkg = filepath.ToSlash(relPkg)
			if relMod == "." {
				return ".", "./" + pkg
			}
			if relPkg == "." {
				return relMod, "./."
			}
			return relMod, "./" + relPkg
		}
		if dir == workdir {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		rel, err := filepath.Rel(workdir, parent)
		if err != nil || strings.HasPrefix(rel, "..") {
			break
		}
		dir = parent
	}
	return ".", "./" + pkg
}

func hasNonTestGo(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			return true
		}
	}
	return false
}

func hasAnyGo(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

func runGo(workdir string, args []string) (string, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if line := firstGoCompileLine(stderr.String()); line != "" {
			return line, err
		}
		if s := strings.TrimSpace(stderr.String()); s != "" {
			return firstLine(s), err
		}
		return "", err
	}
	return "", nil
}

func firstGoCompileLine(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if goCompileErrorLine.MatchString(line) {
			return line
		}
	}
	return ""
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return strings.TrimSpace(s)
}
