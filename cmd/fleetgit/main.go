// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Command fleetgit is a git wrapper for fleet seats: it refuses `git stash`
// in the shared checkout when stash would take another actor's dirty paths
// (🎯T1134). Prepend bin/ (or this binary's directory) ahead of real git on
// PATH for harnesses without the Claude treeguard hook.
//
// Real git is resolved from PATH with this binary removed so the wrapper
// does not recurse.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetgit"
	"github.com/marcelocantos/jevons/internal/treeguard"
)

const exitRefuse = 2

const repositoryProbeEnv = "JEVONS_FLEETGIT_REPOSITORY_PROBE"

const repositoryProbeTimeout = 5 * time.Second

func main() {
	if os.Getenv(repositoryProbeEnv) == "1" {
		fmt.Fprintln(os.Stderr, "fleetgit: recursive repository probe refused")
		os.Exit(exitRefuse)
	}
	if len(os.Args) < 2 {
		runRealGit(os.Args)
		return
	}
	gitArgv := formatGitArgv(os.Args)
	seat := seatWorkdir()
	gitCwd := gitInvocationCwd(os.Args)
	if decision, ok := treeguard.RefuseBranchCheckoutInSharedClone(seat, gitCwd, gitArgv, fleetgit.FleetSeat()); ok && decision.Verdict == treeguard.Deny {
		treeguard.LogBranchCheckoutDenial(decision)
		fmt.Fprintln(os.Stderr, decision.Message)
		os.Exit(exitRefuse)
	}
	if len(os.Args) >= 2 && os.Args[1] == "stash" && stashWouldPush(os.Args[2:]) {
		if refuse, msg, ok := fleetgit.RefuseSharedCloneGitStash(seat, gitCwd, fleetgit.FleetSeat(), gitArgv); ok && refuse {
			fmt.Fprintln(os.Stderr, msg)
			os.Exit(exitRefuse)
		}
	}
	if gitSubcommandMutates(os.Args) {
		if len(os.Args) >= 2 && os.Args[1] == "stash" {
			// Mutating stash handled above; read-only forms (list/show) fall through.
		} else if refuse, msg, ok := fleetgit.RefuseSharedCloneGitWrite(seat, gitCwd, fleetgit.FleetSeat(), gitArgv); ok && refuse {
			fmt.Fprintln(os.Stderr, msg)
			os.Exit(exitRefuse)
		}
	}
	runRealGit(os.Args)
}

func repoRoot() string {
	root := os.Getenv(treeguard.ProjectDirEnv)
	if root != "" {
		return root
	}
	out, err := repositoryRoot()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func repositoryRoot() ([]byte, error) {
	git, err := lookRealGit()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), repositoryProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, "rev-parse", "--show-toplevel")
	cmd.Env = append(os.Environ(), repositoryProbeEnv+"=1")
	cmd.WaitDelay = time.Second
	return cmd.Output()
}

func stashWouldPush(args []string) bool {
	for _, w := range treeguard.ScanCommand("git stash " + strings.Join(args, " ")) {
		if w.Form == treeguard.FormGitStashPush {
			return true
		}
	}
	return false
}

func runRealGit(argv []string) {
	git, err := lookRealGit()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cmd := exec.Command(git, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		os.Exit(1)
	}
}

func lookRealGit() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	selfDir := filepath.Dir(self)
	selfInfo, err := os.Stat(self)
	if err != nil {
		return "", err
	}
	path := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		if filepath.Clean(dir) == filepath.Clean(selfDir) {
			continue
		}
		candidate := filepath.Join(dir, "git")
		if st, err := os.Stat(candidate); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 && !os.SameFile(selfInfo, st) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("fleetgit: real git not found on PATH outside %s", selfDir)
}

func gitSubcommandMutates(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	args := argv[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "-C" && i+1 < len(args) {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "-") {
			continue
		}
		switch args[i] {
		case "stash":
			return stashWouldPush(args[i+1:])
		case "commit", "merge", "cherry-pick", "rebase", "am", "apply", "reset", "clean", "checkout", "restore", "switch", "push", "tag":
			return true
		default:
			return false
		}
	}
	return false
}

func seatWorkdir() string {
	root := os.Getenv(treeguard.ProjectDirEnv)
	if root != "" {
		return root
	}
	out, err := repositoryRoot()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func formatGitArgv(argv []string) string {
	if len(argv) <= 1 {
		return "git"
	}
	return "git " + strings.Join(argv[1:], " ")
}

func gitInvocationCwd(argv []string) string {
	if len(argv) < 2 {
		return seatWorkdir()
	}
	args := argv[1:]
	for i := 0; i < len(args); i++ {
		if args[i] == "-C" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return seatWorkdir()
}
