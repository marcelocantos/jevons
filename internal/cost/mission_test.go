// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func TestRequestMissionFreezesFirstWriteAndDoesNotAlterUsageFacts(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordRequestMission(ctx, "worker", "owner-request", ""); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.RequestMission(ctx, "worker", "owner-request"); err != nil || ok {
		t.Fatalf("empty mission stored: ok=%v err=%v", ok, err)
	}
	if err := s.RecordRequestMission(ctx, "worker", "owner-request", "T1074"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordRequestMission(ctx, "worker", "owner-request", "T9999"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.RequestMission(ctx, "worker", "owner-request")
	if err != nil || !ok || got != "T1074" {
		t.Fatalf("mission=%q ok=%v err=%v", got, ok, err)
	}

	start, final := usagePair("measured", 1)
	if err := s.IngestUsageFact(ctx, final.Seat, final.SessionID, &final); err != nil {
		t.Fatal(err)
	}
	if err := s.IngestUsageFact(ctx, final.Seat, final.SessionID, &final); err != nil {
		t.Fatal(err)
	}
	mission, ok, err := s.UsageMission(ctx, &final)
	if err != nil || !ok || mission != "T1074" {
		t.Fatalf("usage mission=%q ok=%v err=%v", mission, ok, err)
	}
	usage, err := s.StructuredUsage(ctx, testNow, testNow.Add(time.Hour))
	if err != nil || usage.Calls != 1 || usage.Known.Input != 21 || usage.Known.Output != 4 {
		t.Fatalf("replay changed consumption: %+v %v", usage, err)
	}
	var raw string
	if err := s.db.QueryRow(`SELECT fact FROM usage_calls`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored claudia.UsageFact
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored, final) {
		t.Fatalf("usage fact gained host mission state: %+v", stored)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["mission"]; ok {
		t.Fatalf("stored fact has a mission field: %s", raw)
	}

	unknown, _ := usagePair("other", 3)
	unknown.Requests = []claudia.UsageRequestRef{{RequestID: "unbound", AttemptID: "attempt"}}
	unknown.CallID = "other"
	if mission, ok, err := s.UsageMission(ctx, &unknown); err != nil || ok || mission != "" {
		t.Fatalf("unbound request attributed: %q %v %v", mission, ok, err)
	}
	// The started row is not ingested; only the shape of its references matters.
	_ = start
	split := final
	split.Requests = []claudia.UsageRequestRef{
		{RequestID: "owner-request", AttemptID: "delivery-attempt"},
		{RequestID: "other-request", AttemptID: "other-attempt"},
	}
	if err := s.RecordRequestMission(ctx, "worker", "other-request", "T1"); err != nil {
		t.Fatal(err)
	}
	if mission, ok, err := s.UsageMission(ctx, &split); err != nil || ok || mission != "" {
		t.Fatalf("disagreeing missions attributed: %q %v %v", mission, ok, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.RequestMission(ctx, "worker", "owner-request"); err == nil || ok {
		t.Fatalf("closed store looked unknown: ok=%v err=%v", ok, err)
	}
}
