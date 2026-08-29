// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package panecensus

import "testing"

// TestT579AnchorPaneIsNeverReaped pins the pane the fleet's tmux server
// depends on. The live census reaped %0 "zsh" in claudia-anchor 330
// times on 2026-08-29 as "no registry entry and no in-flight turn";
// when that pane is the session's last window the session dies, the
// server with it, and the next spawn fails with "no server running on
// <claudia socket>" — which is how eight fleet-health recoveries died.
func TestT579AnchorPaneIsNeverReaped(t *testing.T) {
	panes := []Pane{
		Pane{Session: AnchorSessionName, Window: "zsh", ID: "%0"}.WithFlight(FlightIdle),
		Pane{Session: AnchorSessionName, Window: "stray", ID: "%9"}.WithFlight(FlightIdle),
	}
	r := Plan(panes, map[string]bool{}, DefaultWarmPoolMax)

	if got := r.Decisions[0].Action; got != ActionKeepAnchor {
		t.Errorf("anchor placeholder action = %q, want %q", got, ActionKeepAnchor)
	}
	if got := r.Decisions[1].Action; got != ActionReap {
		t.Errorf("unregistered idle pane action = %q, want %q (the anchor rule must not spare everything)", got, ActionReap)
	}
	for _, d := range r.Reap() {
		if d.Pane.ID == "%0" {
			t.Fatalf("census still reaps the anchor pane: %s", d.Reason)
		}
	}
}

// TestT579AnchorRuleIsNarrow: an agent window in the anchor session is
// an ordinary pane. Only the bare placeholder shell is infrastructure.
func TestT579AnchorRuleIsNarrow(t *testing.T) {
	cases := []struct {
		name string
		pane Pane
		want bool
	}{
		{"placeholder", Pane{Session: AnchorSessionName, Window: "zsh"}, true},
		{"login shell", Pane{Session: AnchorSessionName, Window: "-bash"}, true},
		{"named agent in anchor", Pane{Session: AnchorSessionName, Window: "zsh", AgentName: "jv-t579"}, false},
		{"session-bearing pane", Pane{Session: AnchorSessionName, Window: "zsh", SessionID: "abc"}, false},
		{"shell in another session", Pane{Session: "holder", Window: "zsh"}, false},
		{"agent window", Pane{Session: AnchorSessionName, Window: "claudia-1a2b3c4d"}, false},
	}
	for _, c := range cases {
		if got := c.pane.IsAnchor(); got != c.want {
			t.Errorf("%s: IsAnchor() = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestT579AnchorSurvivesParsedTmuxOutput runs the rule over the real
// `tmux list-panes -a -F` shape the daemon feeds the census.
func TestT579AnchorSurvivesParsedTmuxOutput(t *testing.T) {
	raw := "claudia-anchor\tzsh\t%0\t111\t\t\t\n" +
		"claudia-anchor\tclaudia-1a2b3c4d\t%3\t222\t\tjv-t579\t1a2b3c4d-0000-4000-8000-000000000000\n"
	panes := ParseListPanes(raw)
	if len(panes) != 2 {
		t.Fatalf("parsed %d panes, want 2", len(panes))
	}
	r := Plan(panes, map[string]bool{"jv-t579": true}, DefaultWarmPoolMax)
	if r.Decisions[0].Action != ActionKeepAnchor {
		t.Errorf("anchor action = %q", r.Decisions[0].Action)
	}
	if r.Decisions[1].Action != ActionKeep {
		t.Errorf("registered agent action = %q", r.Decisions[1].Action)
	}
	if len(r.Reap()) != 0 {
		t.Errorf("census would reap %d panes from a healthy fleet", len(r.Reap()))
	}
}
