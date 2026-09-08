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

func TestT634MuxPlanUsageFrameCarriesHistory(t *testing.T) {
	s := New("test", t.TempDir())
	rem := 50.0
	at := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	s.SetPlanUsageSource(func() any {
		return planusage.Snapshot{
			Backends: []planusage.Backend{{
				Provider: "claude",
				Status:   planusage.StatusAvailable,
				Windows: []planusage.Window{{
					Name:             "weekly",
					RemainingPercent: &rem,
					History: []planusage.HistoryPoint{{
						At: at, Remaining: 80,
					}, {
						At: at.Add(time.Hour), Remaining: 50,
					}},
				}},
			}},
		}
	})
	buf := &replayBuf{}
	sess := &muxSession{send: make(chan []byte, 4), transcripts: map[string]*muxWatch{}}
	s.handleMuxEnvelope(t.Context(), buf, sess, muxEnvelope{V: 1, Ch: planUsageChannel, T: "open"})
	if len(buf.frames) != 1 {
		t.Fatalf("frames=%d", len(buf.frames))
	}
	body, _ := json.Marshal(buf.frames[0]["body"])
	if !bytes.Contains(body, []byte(`"history"`)) {
		t.Fatalf("history missing from mux frame: %s", body)
	}
	if !bytes.Contains(body, []byte(`"remaining_percent":80`)) {
		t.Fatalf("sample missing from mux frame: %s", body)
	}
}
