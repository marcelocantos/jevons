package attrib

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// DirtyPath is one path git reports as not matching HEAD.
type DirtyPath struct {
	Path string
	// XY is git's two-letter porcelain status: index state, worktree state.
	XY string
}

// Untracked reports whether git has never recorded this path.
func (d DirtyPath) Untracked() bool { return d.XY == "??" }

// Staged reports whether the path has content sitting in the shared index —
// the 🎯T457 hazard, since the next `git commit` by any worker sweeps it in.
func (d DirtyPath) Staged() bool {
	return len(d.XY) == 2 && d.XY[0] != ' ' && d.XY[0] != '?'
}

// Git runs a git command in repoRoot and returns stdout.
//
// Errors carry stderr because every failure here is something the operator has
// to act on by hand — a path git will not restore, an index it will not touch —
// and "exit status 1" alone tells them nothing about which.
func Git(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), errors.New("git " + strings.Join(args, " ") + ": " + msg)
	}
	return stdout.String(), nil
}

// DirtyPaths lists every path in the working tree that differs from HEAD,
// tracked or not.
//
// -z and NUL splitting are not fastidiousness: a rename entry embeds a second
// path, and any path with a space or quote in it comes back quoted and escaped
// in the default format. A tool whose job is to not lose somebody's work cannot
// afford to mis-parse the name of the file it is about to restore.
func DirtyPaths(repoRoot string) ([]DirtyPath, error) {
	out, err := Git(repoRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	fields := strings.Split(out, "\x00")
	var paths []DirtyPath
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		xy := entry[:2]
		path := entry[3:]
		// A rename's origin path follows in its own NUL-separated field.
		if xy[0] == 'R' || xy[1] == 'R' {
			i++
		}
		paths = append(paths, DirtyPath{Path: path, XY: xy})
	}
	return paths, nil
}

// StagedPaths lists what is currently sitting in the shared index.
func StagedPaths(repoRoot string) ([]string, error) {
	out, err := Git(repoRoot, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// InHEAD reports whether the path exists in the HEAD commit. A staged brand-new
// file does not, so `git restore --source=HEAD` cannot undo it and the caller
// has to unstage-and-remove instead.
func InHEAD(repoRoot, path string) bool {
	_, err := Git(repoRoot, "cat-file", "-e", "HEAD:"+path)
	return err == nil
}

// RepoRoot resolves the working tree root containing dir.
func RepoRoot(dir string) (string, error) {
	out, err := Git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
