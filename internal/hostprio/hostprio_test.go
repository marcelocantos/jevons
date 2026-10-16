package hostprio

import (
	"strings"
	"testing"
)

func TestClassifyCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want Kind
	}{
		{"/opt/homebrew/opt/supervisor/bin/supervisord -c /opt/homebrew/etc/supervisord.conf --nodaemon", KindControl},
		{"/Users/x/work/jevons/bin/jevonsd -port 13705", KindControl},
		{"/Users/x/work/claudia/bin/claudia broker serve", KindControl},
		{"/opt/homebrew/bin/bullseye serve", KindControl},
		{"/opt/homebrew/bin/mnemo serve --addr 127.0.0.1:19419", KindControl},
		{"/bin/zsh -c source snapshot && eval go test", KindFleet},
		{"/bin/bash -lc claudia agent run", KindFleet},
		{"/Users/x/Library/Application Support/Claude/claude-code/2.1/claude.app/Contents/MacOS/claude --model x", KindFleet},
		{"/Applications/Safari.app/Contents/MacOS/Safari", KindOther},
		{"", KindOther},
	}
	for _, tc := range cases {
		if got := ClassifyCommand(tc.cmd); got != tc.want {
			t.Errorf("ClassifyCommand(%q)=%s want %s", tc.cmd, got, tc.want)
		}
	}
}

func TestAssessStarvedVsRaised(t *testing.T) {
	starved := `
  100  1 20 0 /opt/homebrew/opt/supervisor/bin/supervisord -c conf --nodaemon
  101 100 20 0 /Users/x/jevons/bin/jevonsd -port 13705
  102 100 20 0 /Users/x/claudia/bin/claudia broker serve
  200  1 31 0 /bin/zsh -c source snap && eval go test ./...
`
	rows, err := ParsePSLines(starved)
	if err != nil {
		t.Fatal(err)
	}
	rep := Assess(rows, 0)
	if rep.OK {
		t.Fatalf("expected starved, got OK: %+v", rep)
	}
	if rep.Reason != "starved" {
		t.Fatalf("reason=%q want starved", rep.Reason)
	}
	if rep.ControlMin != 20 || rep.Baseline != 31 {
		t.Fatalf("control_min=%d baseline=%d", rep.ControlMin, rep.Baseline)
	}
	if !strings.Contains(Summarize(rep), "FAIL") {
		t.Fatalf("summary=%q", Summarize(rep))
	}

	raised := `
  100  1 31 0 /opt/homebrew/opt/supervisor/bin/supervisord -c conf --nodaemon
  101 100 31 0 /Users/x/jevons/bin/jevonsd -port 13705
  102 100 31 0 /Users/x/claudia/bin/claudia broker serve
  103 100 31 0 /opt/homebrew/bin/bullseye serve
  104 100 31 0 /opt/homebrew/bin/mnemo serve
  200  1 31 0 /bin/zsh -c source snap && eval go test ./...
`
	rows, err = ParsePSLines(raised)
	if err != nil {
		t.Fatal(err)
	}
	rep = Assess(rows, 0)
	if !rep.OK {
		t.Fatalf("expected OK after Interactive raise: %+v", rep)
	}
	if rep.ControlMin < rep.Baseline {
		t.Fatalf("control_min=%d baseline=%d", rep.ControlMin, rep.Baseline)
	}
}

func TestAssessNoControlRows(t *testing.T) {
	text := "  200  1 31 0 /bin/zsh -c echo hi\n"
	rows, err := ParsePSLines(text)
	if err != nil {
		t.Fatal(err)
	}
	rep := Assess(rows, 0)
	if rep.OK || rep.Reason != "no_control_plane_rows" {
		t.Fatalf("%+v", rep)
	}
}

func TestAssessBaselineForce(t *testing.T) {
	text := `
  100  1 25 0 /Users/x/jevons/bin/jevonsd -port 13705
`
	rows, err := ParsePSLines(text)
	if err != nil {
		t.Fatal(err)
	}
	if rep := Assess(rows, 20); !rep.OK {
		t.Fatalf("force 20 should pass: %+v", rep)
	}
	if rep := Assess(rows, 31); rep.OK || rep.Reason != "starved" {
		t.Fatalf("force 31 should starve: %+v", rep)
	}
}
