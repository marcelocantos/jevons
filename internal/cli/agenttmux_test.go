// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cli_test

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/cli"
)

// daemonEnv builds a lookup over a fixed map, standing in for os.LookupEnv.
func daemonEnv(env map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := env[name]
		return v, ok
	}
}

func opsByName(ops []cli.TmuxEnvOp) map[string]cli.TmuxEnvOp {
	m := map[string]cli.TmuxEnvOp{}
	for _, op := range ops {
		m[op.Name] = op
	}
	return m
}

// TestTmuxEnvFixupsClearsStaleServerEnv is the 🎯T282 regression: the
// claudia tmux server outlives the process that started it, so a Claude
// fleet agent can inherit a days-old test run's environment. Reconciling
// against jevonsd's environment must unset what jevonsd does not have.
func TestTmuxEnvFixupsClearsStaleServerEnv(t *testing.T) {
	// Server env as observed in the wild: started by a claudia `go test`
	// run inside a Claude Code session.
	server := map[string]string{
		"CI":                                   "true",
		"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1",
		"CLAUDE_CODE_SESSION_ID":               "stale-session",
		"PATH":                                 "/usr/bin",
	}
	ops := opsByName(cli.TmuxEnvFixups(server, daemonEnv(map[string]string{})))

	for _, name := range []string{"CI", "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS", "CLAUDE_CODE_SESSION_ID"} {
		op, ok := ops[name]
		if !ok {
			t.Errorf("no fixup for stale %s", name)
			continue
		}
		if !op.Unset {
			t.Errorf("fixup for %s = set %q, want unset", name, op.Value)
		}
	}
	if _, ok := ops["PATH"]; ok {
		t.Error("PATH was reconciled; only known-hostile variables should be")
	}
}

// TestTmuxEnvFixupsAdoptsDaemonValue: when jevonsd itself has one of these
// set (e.g. a deliberate CI=true), the server is made to match rather than
// cleared.
func TestTmuxEnvFixupsAdoptsDaemonValue(t *testing.T) {
	server := map[string]string{"CI": "false"}
	ops := opsByName(cli.TmuxEnvFixups(server, daemonEnv(map[string]string{"CI": "true"})))

	op, ok := ops["CI"]
	if !ok {
		t.Fatal("no fixup for a disagreeing CI")
	}
	if op.Unset || op.Value != "true" {
		t.Fatalf("fixup = %+v, want set CI=true", op)
	}
}

// TestTmuxEnvFixupsQuietWhenAgreed: a healthy server needs no ops, so boot
// stays silent and idempotent.
func TestTmuxEnvFixupsQuietWhenAgreed(t *testing.T) {
	server := map[string]string{"PATH": "/usr/bin", "HOME": "/Users/x"}
	if ops := cli.TmuxEnvFixups(server, daemonEnv(map[string]string{})); len(ops) != 0 {
		t.Fatalf("ops = %+v, want none", ops)
	}
}

// TestNormalizeAgentTmuxEnvWithoutServer: no claudia tmux socket means
// nothing to reconcile — never an error at boot.
func TestNormalizeAgentTmuxEnvWithoutServer(t *testing.T) {
	t.Setenv("CLAUDIA_TMUX_SOCKET", t.TempDir()+"/absent.sock")
	changed, err := cli.NormalizeAgentTmuxEnv()
	if err != nil {
		t.Fatalf("NormalizeAgentTmuxEnv: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("changed = %v, want none", changed)
	}
}

// TestAgentTmuxSocketHonoursOverride keeps jevonsd pointed at the same
// socket claudia uses.
func TestAgentTmuxSocketHonoursOverride(t *testing.T) {
	t.Setenv("CLAUDIA_TMUX_SOCKET", "/tmp/custom-claudia.sock")
	if got := cli.AgentTmuxSocket(); got != "/tmp/custom-claudia.sock" {
		t.Fatalf("AgentTmuxSocket = %q, want the override", got)
	}
}
