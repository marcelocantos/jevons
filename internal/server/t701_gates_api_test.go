// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T701: a reviewer with no shell can extract cited gate ids from prose and
// resolve each through GET /api/gates/{id}. GREEN, SUSPECT, voided and absent
// ids must return four distinguishable answers; an absent id is never green.
func TestT701GateAPIFourDistinctAnswers(t *testing.T) {
	root := t.TempDir()
	t.Setenv(gate.StoreDirEnv, root)
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	start := time.Date(2026, 9, 20, 20, 0, 0, 0, time.UTC)
	green := &gate.Record{
		ID:          "701green1",
		Name:        "make-test-go",
		Command:     []string{"make", "test-go"},
		Dir:         "/tmp/jevons",
		Started:     start,
		Ended:       start.Add(12*time.Second + 300*time.Millisecond),
		ExitStatus:  0,
		StatusKnown: true,
		Verdict:     gate.VerdictGreen,
		OutputPath:  root + "/701green1.log",
		Tree:        &gate.TreeProvenance{Commit: "ac753cbbdeadbeef0123456789abcdef", Clean: true},
	}
	suspect := &gate.Record{
		ID:          "701sus01",
		Name:        "go-test",
		Command:     []string{"go", "test", "./..."},
		Dir:         "/tmp/jevons",
		Started:     start,
		Ended:       start.Add(3 * time.Second),
		ExitStatus:  0,
		StatusKnown: true,
		Verdict:     gate.VerdictSuspect,
		OutputPath:  root + "/701sus01.log",
		Anomalies:   []gate.Anomaly{{Marker: "panic", Line: "panic: test"}},
	}
	voidable := &gate.Record{
		ID:          "701void01",
		Name:        "make-test-ui",
		Command:     []string{"make", "test-ui"},
		Dir:         "/tmp/jevons",
		Started:     start,
		Ended:       start.Add(time.Second),
		ExitStatus:  0,
		StatusKnown: true,
		Verdict:     gate.VerdictGreen,
	}
	for _, rec := range []*gate.Record{green, suspect, voidable} {
		if err := store.Save(rec); err != nil {
			t.Fatalf("Save %s: %v", rec.ID, err)
		}
	}
	if err := os.WriteFile(store.LogPath(voidable.ID), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("log: %v", err)
	}
	voided, err := store.Void(voidable.ID, "allowlist sweep", func() time.Time { return start.Add(2 * time.Hour) })
	if err != nil {
		t.Fatalf("Void: %v", err)
	}

	s := New("test", t.TempDir())
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	absent := "701absent"
	report := strings.Join([]string{
		green.Attestation(),
		suspect.Attestation(),
		voided.Attestation(),
		fmt.Sprintf("GATE missing exit=0 GREEN id=%s out=deadbeef dur=0s", absent),
	}, "\n")
	cited := gate.ParseAttestations(report)
	if len(cited) != 4 {
		t.Fatalf("ParseAttestations found %d ids, want 4", len(cited))
	}

	seen := make(map[string]gate.View)
	for _, c := range cited {
		resp, err := http.Get(srv.URL + "/api/gates/" + c.ID)
		if err != nil {
			t.Fatalf("GET %s: %v", c.ID, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if c.ID == absent {
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("absent id status=%d body=%s", resp.StatusCode, body)
			}
			var miss gate.NotFoundView
			if err := json.Unmarshal(body, &miss); err != nil {
				t.Fatalf("decode absent: %v", err)
			}
			if miss.Error != gate.ErrNotFound || miss.Found {
				t.Fatalf("absent body=%+v — must not read as a pass", miss)
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", c.ID, resp.StatusCode, body)
		}
		var view gate.View
		if err := json.Unmarshal(body, &view); err != nil {
			t.Fatalf("decode %s: %v", c.ID, err)
		}
		seen[c.ID] = view
	}

	gv := seen[green.ID]
	if gv.Verdict != gate.VerdictGreen || gv.Status != "0" || !gv.Found {
		t.Fatalf("green=%+v", gv)
	}
	if gv.Duration != "12.3s" {
		t.Fatalf("green duration=%q want 12.3s", gv.Duration)
	}
	if gv.OutputPath != green.OutputPath {
		t.Fatalf("green output_path=%q", gv.OutputPath)
	}

	sv := seen[suspect.ID]
	if sv.Verdict != gate.VerdictSuspect {
		t.Fatalf("suspect verdict=%s want SUSPECT", sv.Verdict)
	}
	if sv.Duration != "3s" || sv.OutputPath != suspect.OutputPath {
		t.Fatalf("suspect duration/output=%q %q", sv.Duration, sv.OutputPath)
	}

	vv := seen[voidable.ID]
	if vv.Verdict != gate.VerdictVoid || vv.VoidReason == "" {
		t.Fatalf("voided=%+v — must read as voided, not green", vv)
	}
	if vv.OutputPath == "" || !strings.Contains(vv.OutputPath, gate.VoidedDir) {
		t.Fatalf("voided output_path=%q", vv.OutputPath)
	}

	verdicts := []gate.Verdict{gv.Verdict, sv.Verdict, vv.Verdict}
	for i := 0; i < len(verdicts); i++ {
		for j := i + 1; j < len(verdicts); j++ {
			if verdicts[i] == verdicts[j] {
				t.Fatalf("verdicts not distinct: %v", verdicts)
			}
		}
	}
}
