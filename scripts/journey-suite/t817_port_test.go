// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

// 🎯T817: two workers running `make test-journey` at once collided on the
// fixed :13715 and the loser died after a 1-2 minute build. The target now
// passes -port $(PORT) with PORT defaulting to 0 (ephemeral).
func makeJourneyLine(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("make", append([]string{"-C", "../..", "-n", "test-journey"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n test-journey %v: %v\n%s", args, err, out)
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "./scripts/journey-suite") {
			return l
		}
	}
	t.Fatalf("no journey-suite invocation in:\n%s", out)
	return ""
}

func TestT817MakeTestJourneyDefaultsToEphemeralPort(t *testing.T) {
	line := makeJourneyLine(t)
	if !strings.Contains(line, "-port 0") {
		t.Fatalf("default invocation %q lacks -port 0", line)
	}
	for _, fixed := range []string{"13715", "13705"} {
		if strings.Contains(line, fixed) {
			t.Fatalf("default invocation %q pins %s", line, fixed)
		}
	}
	if pinned := makeJourneyLine(t, "PORT=13777"); !strings.Contains(pinned, "-port 13777") {
		t.Fatalf("PORT override ignored: %q", pinned)
	}
	if only := makeJourneyLine(t, "ONLY=J32"); !strings.Contains(only, "-only J32") || !strings.Contains(only, "-port 0") {
		t.Fatalf("ONLY invocation %q", only)
	}
}

// Two isolates starting side by side: the second resolution happens while the
// first still holds its port, as it does when both workers run at once.
func TestT817TwoEphemeralIsolatesDoNotCollide(t *testing.T) {
	p1, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p1))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	p2, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	if p1 == p2 {
		t.Fatalf("both isolates resolved port %d", p1)
	}
	for _, p := range []int{p1, p2} {
		if err := portguard.RefuseDaily(p); err != nil {
			t.Fatalf("ephemeral port %d is a development port: %v", p, err)
		}
	}
	// T526 guard stays live: the held port is refused, the free one is not.
	if portguard.ErrIfPortHeld(p2) != nil {
		t.Fatalf("free port %d reported held", p2)
	}
	if err := portguard.RefuseDaily(portguard.DailyPort); err == nil {
		t.Fatal("development port no longer refused")
	}
}
