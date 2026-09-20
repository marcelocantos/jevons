// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Adversarial peers for the 🎯T625 cancel-ordering and provider-observation
// steps. Each case is a way the cancel journey could report green without a
// cancel having happened on the request it sent, or without the backend it
// claims having been the one that answered.

func ownerMuxMeta(t *testing.T, body map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"v": 1, "ch": ownerMuxChannel, "t": "meta", "body": body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func phaseMeta(t *testing.T, phase string) []byte {
	return ownerMuxMeta(t, map[string]any{"phase": map[string]any{"phase": phase}})
}

// A window meta opens the replay boundary and carries no phase at all. It
// must never be read as a phase sample, or the very first frame of every
// connection would answer "the cancel settled".
func windowMeta(t *testing.T) []byte {
	return ownerMuxMeta(t, map[string]any{"n": 3, "lo": 1, "hi": 0, "following": true})
}

func TestT625CancelTargetsThisRequest(t *testing.T) {
	const prompt = "Count slowly from 1 to 40, one number per line, prefixing every line with nonce."
	owner := ownerMuxFixture(t, ownerMuxChannel, "user", 2, prompt, "", "owner", "put")
	stream := func(index int, stop string) []byte {
		return ownerMuxFixture(t, ownerMuxChannel, "assistant", index, "nonce 1", stop, "", "append")
	}

	for _, tc := range []struct {
		name   string
		frames [][]byte
		wantOK bool
	}{
		{"this request is streaming and the overseer is working",
			[][]byte{owner, phaseMeta(t, "streaming"), stream(3, "")}, true},
		{"phase may arrive after the stream begins",
			[][]byte{owner, stream(3, ""), phaseMeta(t, "thinking")}, true},

		// The defect this step exists for: a previous journey's turn is
		// still streaming, so the old "wait for any assistant frame" step
		// would interrupt — and settle — a turn this journey never sent.
		{"a row before this request's echo is a previous turn",
			[][]byte{stream(1, ""), phaseMeta(t, "streaming"), owner}, false},
		{"the echo's own index is not a reply to it",
			[][]byte{owner, phaseMeta(t, "streaming"), stream(2, "")}, false},
		{"no echo at all",
			[][]byte{phaseMeta(t, "streaming"), stream(3, "")}, false},

		// Interrupting a finished turn proves nothing about ordering.
		{"the request already ended",
			[][]byte{owner, phaseMeta(t, "streaming"), stream(3, "end_turn")}, false},
		{"the request already ended by stop_sequence",
			[][]byte{owner, phaseMeta(t, "streaming"), stream(3, "stop_sequence")}, false},

		// An idle seat has no turn to cancel.
		{"overseer never reported working",
			[][]byte{owner, phaseMeta(t, "idle"), stream(3, "")}, false},
		{"working then back to idle before the stream",
			[][]byte{owner, phaseMeta(t, "streaming"), phaseMeta(t, "idle"), stream(3, "")}, false},
		{"error phase is not working",
			[][]byte{owner, phaseMeta(t, "error"), stream(3, "")}, false},
		{"a window meta is not a phase sample",
			[][]byte{owner, windowMeta(t), stream(3, "")}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := make(chan []byte, len(tc.frames))
			for _, f := range tc.frames {
				frames <- f
			}
			close(frames)
			index, err := waitOwnerMuxTurnWorking(context.Background(), frames, prompt, time.Second)
			if tc.wantOK != (err == nil) {
				t.Fatalf("err=%v, wantOK=%v", err, tc.wantOK)
			}
			if tc.wantOK && index != 2 {
				t.Fatalf("owner echo index=%d, want 2", index)
			}
		})
	}
}

func TestT625CancelSettlesOnTheCanonicalLevel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		frames [][]byte
		wantOK bool
	}{
		{"idle phase settles", [][]byte{phaseMeta(t, "streaming"), phaseMeta(t, "idle")}, true},
		{"still working is not settled", [][]byte{phaseMeta(t, "streaming"), phaseMeta(t, "tool")}, false},
		{"a window meta is not a settle", [][]byte{windowMeta(t)}, false},
		{"a transcript frame is not a settle",
			[][]byte{ownerMuxFixture(t, ownerMuxChannel, "assistant", 3, "x", "end_turn", "", "append")}, false},
		{"another channel's idle is not ours",
			[][]byte{func() []byte {
				data, err := json.Marshal(map[string]any{
					"v": 1, "ch": "transcript:someone-else", "t": "meta",
					"body": map[string]any{"phase": map[string]any{"phase": "idle"}},
				})
				if err != nil {
					t.Fatal(err)
				}
				return data
			}()}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frames := make(chan []byte, len(tc.frames))
			for _, f := range tc.frames {
				frames <- f
			}
			close(frames)
			err := waitOwnerMuxSettled(context.Background(), frames, time.Second)
			if tc.wantOK != (err == nil) {
				t.Fatalf("err=%v, wantOK=%v", err, tc.wantOK)
			}
		})
	}
}

// 🎯T625: a journey that names a backend observes it. These are the shapes
// a run can take while the suite was asked for one provider.
func TestT625AssertLaunchedOnObservesTheNamedBackend(t *testing.T) {
	line := func(name, provider string) string {
		return fmt.Sprintf("msg=%q name=%s provider=%s\n", "agent started", name, provider)
	}
	for _, tc := range []struct {
		name   string
		log    string
		names  []string
		wantOK bool
	}{
		{"both workers launched on the selected backend",
			line("jv-a", "grok") + line("jv-b", "grok"), []string{"jv-a", "jv-b"}, true},
		{"one worker fell back to another backend",
			line("jv-a", "grok") + line("jv-b", "claude"), []string{"jv-a", "jv-b"}, false},
		{"a worker never launched at all",
			line("jv-a", "grok"), []string{"jv-a", "jv-b"}, false},
		{"no launch evidence whatsoever", "", []string{"jv-a"}, false},
		{"another agent's launch cannot stand in",
			line("jevons", "grok"), []string{"jv-a"}, false},
		{"a message quoting a launch line is not a launch",
			fmt.Sprintf("msg=%q\n", `agent started name=jv-a provider=grok`), []string{"jv-a"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			s := &suite{stateDir: dir, logPath: dir + "/jevonsd.log", provider: "grok"}
			if err := os.WriteFile(s.logPath, []byte(tc.log), 0o600); err != nil {
				t.Fatal(err)
			}
			err := s.assertLaunchedOn(tc.names...)
			if tc.wantOK != (err == nil) {
				t.Fatalf("err=%v, wantOK=%v", err, tc.wantOK)
			}
			if err != nil && !strings.Contains(err.Error(), tc.names[len(tc.names)-1]) &&
				!strings.Contains(err.Error(), tc.names[0]) {
				t.Fatalf("error does not name the agent it judged: %v", err)
			}
		})
	}
}
