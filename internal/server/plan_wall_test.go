// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/statedb"
)

func TestTranscriptEndsOnPlanWall(t *testing.T) {
	wall := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"\n\nUpgrade your plan to continue"}],"stop_reason":"end_turn"}}`
	if !transcriptEndsOnPlanWall(wall) {
		t.Fatal("plan wall not recognised")
	}
	reply := `{"type":"assistant","message":{"content":[{"type":"text","text":"still working"}]}}`
	if transcriptEndsOnPlanWall(reply) {
		t.Fatal("ordinary reply treated as a plan wall")
	}
	user := `{"type":"user","message":{"content":[{"type":"text","text":"Upgrade your plan to continue"}]}}`
	if transcriptEndsOnPlanWall(user) {
		t.Fatal("a user turn was treated as the plan wall")
	}
}

func TestDecoratePlanWallsUsesTheTranscriptEnd(t *testing.T) {
	db, err := statedb.Open(filepath.Join(t.TempDir(), "jevons.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	wall := `{"type":"assistant","message":{"content":[{"type":"text","text":"\n\nUpgrade your plan to continue"}],"stop_reason":"end_turn"}}`
	if err := db.Upsert("jevons-po", []statedb.Event{{Index: 1, Type: "assistant", Body: wall}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Upsert("claudia-po", []statedb.Event{{Index: 1, Type: "assistant", Body: `{"type":"assistant","message":{"content":[{"type":"text","text":"on it"}]}}`}}); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetStateDB(db)
	rows := s.decoratePlanWalls([]agentInfo{
		{Name: "jevons-po", Running: true},
		{Name: "claudia-po", Running: true},
	})
	if rows[0].PlanWall != planWallText {
		t.Fatalf("jevons-po plan_wall=%q", rows[0].PlanWall)
	}
	if rows[1].PlanWall != "" {
		t.Fatalf("claudia-po plan_wall=%q", rows[1].PlanWall)
	}
}
