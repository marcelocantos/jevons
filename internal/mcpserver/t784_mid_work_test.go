// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
	"strings"
	"testing"
)

// Replay the T765 incident shape: a green slice oracle followed by owned
// next work. This is a synthetic regression, not the historical transcript.
func TestT784MidWorkOracleNeverReaps(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		status        envelope.Progress
	}{
		{"status", "Done. Slice oracle passed.", envelope.ProgressInProgress},
		{"next", "Slice oracle GREEN. Next step: implement the remaining call graph ratchet.", envelope.ProgressNone},
		{"remaining", "Slice oracle GREEN. Remaining scope: migrate callers and run the clean gate.", envelope.ProgressLanded},
		{"next_work", "Slice oracle GREEN. Next work: remove the adapter.", envelope.ProgressNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := envelope.Format(&envelope.Message{Kind: envelope.KindFinishReport, Target: "T765", SHA: "abcdef0123456", Status: tc.status, SilentLedger: envelope.SilentLedgerEmpty, Payload: tc.payload})
			if LooksLikeFinishedWorkReport(raw) {
				t.Fatal("mid-work report classified as finished")
			}
			s, reg := reapSinkServer(t, "jv-t765-worker")
			if ok, reason := ShouldAutoReapDoneWorkAgent(reg, "jv-t765-worker", raw, nil); ok || !strings.HasPrefix(reason, outstandingScopeReapReasonPrefix) {
				t.Fatalf("reap=%v reason=%s", ok, reason)
			}
			s.agentEventSink("jv-t765-worker")(claudia.Event{Type: "assistant", Text: raw, StopReason: "end_turn"})
			if reg.Def("jv-t765-worker") == nil {
				t.Fatal("mid-work seat removed through real sink")
			}
		})
	}
}
