// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package hoststray

import "testing"

func TestClassifyFixtureJevonsd(t *testing.T) {
	cases := []struct {
		args string
		want string
	}{
		{
			"/var/folders/T/TestRestartThrashPolicy123/001/bin/jevonsd -port 60949",
			KindFixtureJevonsd,
		},
		{
			"/tmp/jevonsd -port 13705 -workdir /Users/x/.jevons",
			"",
		},
		{
			"/var/folders/T/TestFoo/001/bin/jevonsd -port 12345",
			KindFixtureJevonsd,
		},
		{
			"/usr/local/bin/jevonsd -port 13705",
			"",
		},
		{
			"/var/folders/fn/x/T/go-build3982507924/b001/exe/journey-suite -port 0 -only J52 -bin ./bin/jevonsd",
			"",
		},
	}
	for _, c := range cases {
		if got := classifyFixtureJevonsd(c.args); got != c.want {
			t.Errorf("classifyFixtureJevonsd(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestClassifyJourneyTmux(t *testing.T) {
	cases := []struct {
		args string
		want string
	}{
		{
			"tmux -S /var/folders/T/jevons-journey-123/claudia/tmux.sock new-session -d -s claudia-anchor",
			KindJourneyTmux,
		},
		{
			"tmux -S /tmp/claudia/tmux.sock new-session -d -s claudia-anchor",
			"",
		},
	}
	for _, c := range cases {
		if got := classifyJourneyTmux(c.args); got != c.want {
			t.Errorf("classifyJourneyTmux(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

func TestClassifyOrphanLoadShell(t *testing.T) {
	busy := "/bin/sh -c while :; do :; done"
	if got := classifyOrphanLoadShell(1, busy); got != KindOrphanLoadShell {
		t.Errorf("orphan load = %q, want %q", got, KindOrphanLoadShell)
	}
	if got := classifyOrphanLoadShell(500, busy); got != "" {
		t.Errorf("non-orphan load = %q, want empty", got)
	}
}
