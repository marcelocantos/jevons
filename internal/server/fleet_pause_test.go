// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestT966FleetPauseHTTP(t *testing.T) {
	var paused bool
	s := &Server{}
	s.SetFleetPauseControl(FleetPauseControl{
		Pause: func(actor, reason string) error {
			paused = true
			return nil
		},
		Resume: func(actor, reason string) error {
			paused = false
			return nil
		},
		Paused: func() bool { return paused },
	})
	body, _ := json.Marshal(map[string]any{"pause": true})
	req := httptest.NewRequest(http.MethodPost, "/api/fleet-pause", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleFleetPause(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause: HTTP %d %s", rec.Code, rec.Body.String())
	}
	if !paused {
		t.Fatal("pause handler did not call Pause")
	}
}
