package main

import (
	"strings"
	"testing"
)

// 🎯T940: a journey's isolated broker must carry CLAUDIA_OMP_NO_REFRESH=1 —
// it shares the real Anthropic/Codex/Cursor/xAI plan Keychain item with the
// development broker (omp.DefaultDataPath ignores XDG_STATE_HOME), so it
// must never be allowed to contact the OAuth provider and rotate the single
// refresh token a production broker also holds.
func TestT940IsolatedBrokerEnvNeverRefreshes(t *testing.T) {
	env := isolatedBrokerEnv(nil, "/tmp/a/broker.sock", "/tmp/a/omp.sock", "/tmp/a/server.ts",
		"/tmp/a/state", "/tmp/a/spool")
	if got := findEnv(env, "CLAUDIA_OMP_NO_REFRESH"); got != "1" {
		t.Fatalf("CLAUDIA_OMP_NO_REFRESH=%q, want 1", got)
	}
}

// A prior isolate's env (or an owner shell that happens to run journeys with
// CLAUDIA_OMP_NO_REFRESH unset for the real broker) must not leave this
// broker un-isolated, and the base environment must never end up carrying
// two conflicting values for the same variable.
func TestT940IsolatedBrokerEnvStripsAnyInheritedOverride(t *testing.T) {
	base := []string{
		"HOME=/Users/x",
		"CLAUDIA_OMP_NO_REFRESH=", // an owner shell that unset it back to empty
		"XDG_STATE_HOME=/somewhere/else",
	}
	env := isolatedBrokerEnv(base, "/tmp/a/broker.sock", "/tmp/a/omp.sock", "/tmp/a/server.ts",
		"/tmp/a/state", "/tmp/a/spool")
	n := 0
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDIA_OMP_NO_REFRESH=") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("CLAUDIA_OMP_NO_REFRESH appears %d times in %v, want exactly 1", n, env)
	}
	if got := findEnv(env, "CLAUDIA_OMP_NO_REFRESH"); got != "1" {
		t.Fatalf("CLAUDIA_OMP_NO_REFRESH=%q, want 1", got)
	}
	if got := findEnv(env, "XDG_STATE_HOME"); got != "/tmp/a/state" {
		t.Fatalf("XDG_STATE_HOME=%q, want this run's own state dir, not an inherited one", got)
	}
	if got := findEnv(env, "HOME"); got != "/Users/x" {
		t.Fatalf("HOME=%q, want the inherited value preserved", got)
	}
}

func findEnv(env []string, key string) string {
	for _, e := range env {
		if name, val, ok := strings.Cut(e, "="); ok && name == key {
			return val
		}
	}
	return ""
}
