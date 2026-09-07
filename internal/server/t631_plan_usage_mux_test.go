// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestMuxPlanUsageOpenReturnsCurrentSnapshot(t *testing.T) {
	s := New("test", t.TempDir())
	rem := 41.0
	s.SetPlanUsageSource(func() any {
		return planusage.Snapshot{
			At: time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC),
			Backends: []planusage.Backend{{
				Provider: "claude",
				Status:   planusage.StatusAvailable,
				Windows: []planusage.Window{{
					Name:             "weekly",
					RemainingPercent: &rem,
				}},
			}},
		}
	})
	buf := &replayBuf{}
	sess := &muxSession{send: make(chan []byte, 4), transcripts: map[string]*muxWatch{}}
	s.handleMuxEnvelope(t.Context(), buf, sess, muxEnvelope{V: 1, Ch: planUsageChannel, T: "open"})
	if !sess.watchingPlanUsage() {
		t.Fatal("open must watch plan-usage")
	}
	if len(buf.frames) != 1 {
		t.Fatalf("frames=%d want 1", len(buf.frames))
	}
	env := buf.frames[0]
	if env["ch"] != planUsageChannel || env["t"] != "frame" {
		t.Fatalf("envelope=%v", env)
	}
	body, _ := json.Marshal(env["body"])
	if !bytes.Contains(body, []byte(`"remaining_percent":41`)) {
		t.Fatalf("body=%s", body)
	}
}

func TestMuxPlanUsageFanReachesWatchersOnly(t *testing.T) {
	s := New("test", t.TempDir())
	s.mux = newMuxHub()
	rem := 22.0
	s.SetPlanUsageSource(func() any {
		return planusage.Snapshot{
			Backends: []planusage.Backend{{
				Provider: "claude",
				Status:   planusage.StatusAvailable,
				Windows:  []planusage.Window{{Name: "weekly", RemainingPercent: &rem}},
			}},
		}
	})
	watch := &muxSession{send: make(chan []byte, 4), transcripts: map[string]*muxWatch{}}
	idle := &muxSession{send: make(chan []byte, 4), transcripts: map[string]*muxWatch{}}
	s.mux.add(watch)
	s.mux.add(idle)
	watch.setPlanUsage(true)
	s.FanPlanUsage()
	select {
	case raw := <-watch.send:
		var env muxEnvelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		if env.Ch != planUsageChannel || env.T != "frame" {
			t.Fatalf("env=%+v", env)
		}
	default:
		t.Fatal("watcher got no fan-out")
	}
	select {
	case <-idle.send:
		t.Fatal("idle session must not receive plan-usage")
	default:
	}
}

func TestMuxUnknownChannelStillErrors(t *testing.T) {
	s := New("test", t.TempDir())
	buf := &replayBuf{}
	sess := &muxSession{send: make(chan []byte, 1), transcripts: map[string]*muxWatch{}}
	s.handleMuxEnvelope(t.Context(), buf, sess, muxEnvelope{V: 1, Ch: "fleet", T: "open"})
	if len(buf.frames) != 1 || buf.frames[0]["t"] != "error" {
		t.Fatalf("frames=%v", buf.frames)
	}
}
