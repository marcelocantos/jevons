// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"
)

// This regression uses only the existing waitTurn API, so it also runs
// against the preceding implementation as a negative control.
func TestT625WaitTurnRecognizesTypedOwnerEcho(t *testing.T) {
	frames := make(chan []byte, 2)
	frames <- []byte(`{"type":"user","turn_origin":"owner","message":{"content":[{"type":"text","text":"Reply with exactly: fresh-token"}]}}`)
	frames <- []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"fresh-token"}],"stop_reason":"end_turn"}}`)
	close(frames)
	gotUser, text, terminal, err := waitTurn(context.Background(), frames, "fresh-token", true)
	if err != nil || !gotUser || !terminal || text != "fresh-token" {
		t.Fatalf("typed owner turn was not recognized: user=%v text=%q terminal=%v err=%v", gotUser, text, terminal, err)
	}
}
