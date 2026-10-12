// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// 🎯T450 — the restart oracle's stale-binary check bites. A script that
// claims "OK: serving" while leaving the old binary up must still be caught
// by TestRestartThrashPolicy's variant check. (The load ratchet and the
// skip-wait mutant that lived here guarded the restart rate limit, removed
// 2026-10-12.)

// TestT450StaleBinaryMutationFails asserts the variant-served check still
// bites a script that claims OK without activating the new binary.
func TestT450StaleBinaryMutationFails(t *testing.T) {
	if _, err := os.Stat("/usr/sbin/lsof"); err != nil {
		t.Skip("lsof unavailable")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go unavailable")
	}

	e := newThrashEnv(t)
	e.build("a")
	out, err := e.run()
	if err != nil {
		t.Fatalf("cold start failed: %v\n%s", err, out)
	}
	if got := e.variantServed(); got != "a" {
		t.Fatalf("cold start variant %q", got)
	}

	// Mutate only after a real daemon is serving: the cold start must use
	// the honest script, otherwise there is no "stale" binary to leave up.
	body, err := os.ReadFile(e.script)
	if err != nil {
		t.Fatal(err)
	}
	mutated := string(body)
	for _, pair := range [][2]string{
		{`kill_port_listeners`, `true # 🎯T450 mutant: skip kill_port_listeners`},
		{`start_daemon_detached`, `true # 🎯T450 mutant: skip start_daemon_detached`},
		{`wait_until_serving`, `true # 🎯T450 mutant: skip wait_until_serving`},
	} {
		// Call sites in main only — never the function definitions, which
		// end in "() {" and so cannot match a line that is nothing but the
		// name.
		//
		// Indentation-tolerant on purpose. This used to require a call at
		// column zero, and when the script grew a conditional around the
		// start ("  start_daemon_detached" inside an if), the mutation
		// could no longer find it -- and a mutation test that cannot mutate
		// reports a failure to locate, not a surviving mutant. Which is at
		// least honest, but it sat red long enough to hide the packages
		// failing beside it.
		re := regexp.MustCompile(`(?m)^([ \t]*)` + regexp.QuoteMeta(pair[0]) + `[ \t]*$`)
		loc := re.FindStringSubmatchIndex(mutated)
		if loc == nil {
			t.Fatalf("cannot locate call site %q to mutate", pair[0])
		}
		indent := mutated[loc[2]:loc[3]]
		mutated = mutated[:loc[0]] + indent + pair[1] + mutated[loc[1]:]
	}
	mutated = strings.Replace(mutated,
		`log "OK: development jevonsd serving on :$PORT (workdir=$WORKDIR)"`,
		`log "OK: development jevonsd serving on :$PORT (workdir=$WORKDIR) # 🎯T450 mutant lie"`,
		1)
	if err := os.WriteFile(e.script, []byte(mutated), 0o755); err != nil {
		t.Fatal(err)
	}

	e.build("b")
	out, err = e.run()
	got := e.variantServed()
	if got == "b" {
		t.Fatalf("stale-binary mutant unexpectedly activated variant b — mutation did not take.\nout:\n%s", out)
	}
	if got != "a" {
		t.Fatalf("after mutant 'activation', :%d serves %q, want stale %q\nout:\n%s", e.port, got, "a", out)
	}
	// This is the oracle assertion that must fail on the mutant tree:
	//   if got := e.variantServed(); got != "b" { t.Fatalf(...) }
	t.Logf("stale-binary mutant bitten: still serving %q after claimed OK (run err=%v)", got, err)
}
