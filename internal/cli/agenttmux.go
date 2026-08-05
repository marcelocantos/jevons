// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Claude fleet agents run as tmux windows on claudia's dedicated tmux
// server, and tmux gives a new window the SERVER's global environment —
// not the environment of the process asking for the window. The server
// outlives every agent (an anchor session holds it open), so its
// environment is a snapshot of whichever process happened to start it,
// possibly days earlier: a `go test` run, an interactive shell, or a
// Claude Code session. Observed in the wild: a claudia test run from
// 2026-08-02 left CI=true, crash-test variables, and a Claude Code
// session's feature flags on the server, and every Claude agent jevonsd
// spawned afterwards inherited them (🎯T282).
//
// Scrubbing jevonsd's own environment therefore is not enough. At boot we
// also reconcile the running server's global environment with jevonsd's
// for the variables that change how a spawned agent behaves.

// tmuxSocketEnv overrides claudia's default tmux socket path (claudia:
// internal/tmuxagent.SocketPath). Kept in sync deliberately: jevonsd must
// reach the same server claudia spawns agents on.
const tmuxSocketEnv = "CLAUDIA_TMUX_SOCKET"

// agentEnvVars are the variables whose stale value on the tmux server
// would follow every Claude agent: the session-identity set (see
// inheritedSessionEnv) plus CI, which a leftover test run leaves behind
// and which makes an agent behave as if it were non-interactive.
//
// Everything else on the server is left alone: this is a reconciliation
// of known-hostile variables, not a wholesale environment replacement.
func agentEnvVars() []string {
	vars := append(slices.Clone(inheritedSessionEnv), "CI")
	slices.Sort(vars)
	return vars
}

// TmuxEnvOp is one reconciliation step on the tmux server's global
// environment: set Name to Value, or unset it.
type TmuxEnvOp struct {
	Name  string
	Value string
	Unset bool
}

// TmuxEnvFixups is the pure oracle: given the server's current global
// environment and a lookup over jevonsd's own environment, it returns the
// ops that make the server agree with jevonsd for agentEnvVars. Variables
// already in agreement produce no op, so a healthy server reconciles to
// an empty list.
func TmuxEnvFixups(server map[string]string, daemon func(string) (string, bool)) []TmuxEnvOp {
	var ops []TmuxEnvOp
	for _, name := range agentEnvVars() {
		want, wantSet := daemon(name)
		have, haveSet := server[name]
		switch {
		case wantSet && (!haveSet || have != want):
			ops = append(ops, TmuxEnvOp{Name: name, Value: want})
		case !wantSet && haveSet:
			ops = append(ops, TmuxEnvOp{Name: name, Unset: true})
		}
	}
	return ops
}

// AgentTmuxSocket returns claudia's tmux socket path, mirroring claudia's
// own resolution (CLAUDIA_TMUX_SOCKET, else $XDG_STATE_HOME/claudia, else
// ~/.local/state/claudia).
func AgentTmuxSocket() string {
	if p := strings.TrimSpace(os.Getenv(tmuxSocketEnv)); p != "" {
		return p
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "claudia", "tmux.sock")
}

// NormalizeAgentTmuxEnv reconciles a RUNNING claudia tmux server's global
// environment with this process's, for agentEnvVars. It returns the names
// it changed. No server (the common case at boot) is not an error and
// changes nothing: the server claudia starts later inherits jevonsd's
// environment directly.
func NormalizeAgentTmuxEnv() ([]string, error) {
	sock := AgentTmuxSocket()
	if sock == "" {
		return nil, nil
	}
	if _, err := os.Stat(sock); err != nil {
		return nil, nil
	}
	current, err := exec.Command("tmux", "-S", sock, "show-environment", "-g").Output()
	if err != nil {
		// No server on that socket (or tmux missing): nothing to fix.
		return nil, nil
	}
	ops := TmuxEnvFixups(parseTmuxEnv(current), os.LookupEnv)
	var changed []string
	for _, op := range ops {
		args := []string{"-S", sock, "set-environment", "-g"}
		if op.Unset {
			args = append(args, "-u", op.Name)
		} else {
			args = append(args, op.Name, op.Value)
		}
		if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
			return changed, fmt.Errorf("tmux set-environment -g %s: %w (%s)",
				op.Name, err, strings.TrimSpace(string(out)))
		}
		changed = append(changed, op.Name)
	}
	return changed, nil
}

// parseTmuxEnv reads `tmux show-environment -g` output. Lines are
// NAME=value for set variables and -NAME for explicitly unset ones; both
// unset forms (absent, or -NAME) map to "not present".
func parseTmuxEnv(out []byte) map[string]string {
	env := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		env[name] = value
	}
	return env
}
