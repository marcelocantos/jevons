// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"io"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/gate"
)

// The live notify path calls FalseGreenFlagsForReport. Exercise that exact
// checker with a private gate store rather than publishing deliberate false
// claims from a real fleet seat into the overseer's notification stream.
func TestT1048IsolatedReportChecker(t *testing.T) {
	root := t.TempDir()
	t.Setenv("JEVONS_GATE_DIR", root)
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatal(err)
	}
	run := func(name, script string) *gate.Record {
		t.Helper()
		rec, err := gate.Run(&gate.RunArgs{
			Command: []string{"sh", "-c", script}, Name: name, Dir: t.TempDir(),
			Store: store, Stdout: io.Discard, Stderr: io.Discard,
		})
		if err != nil {
			t.Fatal(err)
		}
		return rec
	}
	green := run("positive", "exit 0")
	killed := run("host-kill", "kill -9 $$")
	red := run("unrelated-red", "exit 1")
	if green.Verdict != gate.VerdictGreen || killed.Verdict != gate.VerdictKilled || red.Verdict != gate.VerdictRed {
		t.Fatalf("bad control records: green=%s killed=%s red=%s", green.Verdict, killed.Verdict, red.Verdict)
	}
	prefix := "```jevons\njevons: kind status-ping\njevons: status in-progress\n```\n\n"
	honest := prefix + "Positive GREEN: `" + green.Attestation() + "`.\n\n" +
		"SIGKILL record: `" + killed.Attestation() + "`. Host termination observation, not a passing record or test result.\n\n" +
		"RED disclosure: `" + red.Attestation() + "`. This unrelated earlier run cannot support claiming its suite green.\n"
	probe := func(t *testing.T, report string, want gate.FlagKind) {
		t.Helper()
		s, po, inbox, _ := t690Server(t) // hermetic fake sender + private report store; no network/push sink
		sink := s.agentEventSink(t690Worker)
		sink(claudia.Event{Type: "assistant", Text: report, StopReason: "end_turn"})
		if len(inbox.texts) != 1 || len(po.sent) != 1 {
			t.Fatalf("missing actual report routes: overseer=%d parent=%d", len(inbox.texts), len(po.sent))
		}
		// Status-ping chatter can dedupe the overseer copy globally across subtests;
		// the daemon parent-report route is not chatter-deduped.
		banner := string(want)
		if want == "" {
			if strings.Contains(po.sent[0], gate.BannerHeading) {
				t.Fatalf("honest control bannered: %q", inbox.texts[0])
			}
		} else if !strings.Contains(po.sent[0], banner) {
			t.Fatalf("want %s in actual parent route: overseer=%q parent=%q", want, inbox.texts[0], po.sent[0])
		}
	}
	if flags := FalseGreenFlagsForReport(honest, ""); len(flags) != 0 {
		t.Fatalf("honest controls falsely flagged: %v", flags)
	}
	probe(t, honest, "")
	cases := []struct {
		name, report string
		kind         gate.FlagKind
	}{
		{"killed-claimed-pass", strings.Replace(honest, "not a passing record or test result", "it is green", 1), gate.FlagAttestationKilled},
		{"killed-claimed-failure", strings.Replace(honest, "not a passing record or test result", "proves a failing test assertion", 1), gate.FlagAttestationKilled},
		{"red-claimed-pass", strings.Replace(honest, "cannot support claiming its suite green", "can support claiming its suite green", 1), gate.FlagAttestationNotGreen},
		{"neighbour-fail", strings.Replace(honest, "Positive GREEN:", "    --- FAIL: TestReal (0.00s)\nPositive GREEN:", 1), gate.FlagOutputContradicts},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flags := FalseGreenFlagsForReport(tc.report, "")
			for _, f := range flags {
				if f.Kind == tc.kind {
					probe(t, tc.report, tc.kind)
					return
				}
			}
			t.Fatalf("wanted %s, got %v", tc.kind, flags)
		})
	}
}
