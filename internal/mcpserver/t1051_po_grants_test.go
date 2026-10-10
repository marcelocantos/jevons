// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/landinggrant"
	"github.com/marcelocantos/jevons/internal/worktree"
)

type t1051Epoch struct{}

func (t1051Epoch) WithCurrent(_, _ string, n uint64, f func() error) error {
	if n != 0 {
		return errors.New("stale")
	}
	return f()
}
func TestT1051OnlyCompletedCurrentPOProcessCanMint(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{{Name: "jevons-po", WorkDir: dir, SessionID: "po-session", Role: "product-owner", Purpose: claudia.PurposeWork}, {Name: "worker", WorkDir: dir, SessionID: "worker-session", Role: "worker", Purpose: claudia.PurposeWork}} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	current := &claudia.Agent{}
	stale := &claudia.Agent{}
	s := &Server{registry: reg}
	s.SetProcResolver(func(name string) *claudia.Agent {
		if name == "jevons-po" {
			return current
		}
		return stale
	})
	store := &landinggrant.Store{Dir: filepath.Join(dir, "grants"), Epoch: t1051Epoch{}, Now: func() time.Time { return time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC) }, SnapshotEpoch: func(string, string) (uint64, bool, error) { return 0, false, nil }}
	s.SetLandingGrantStore(store)
	review := worktree.BatchReview{Repo: "/repo", Target: "T1051", BaseRef: "refs/heads/master", BaseSHA: "base", Workers: []worktree.ReviewedWorker{{Name: "worker", Ref: "refs/heads/jevons-worktree/worker", SHA: "tip"}}}
	b, _ := json.Marshal(landinggrant.Approval{Review: review, Expires: store.Now().Add(time.Minute)})
	response := "```jevons-integration-approval\n" + string(b) + "\n```"
	ev := claudia.Event{Type: "assistant", SessionID: "po-session", TurnID: "turn-1", StopReason: "end_turn"}
	for _, tc := range []struct {
		name  string
		proc  *claudia.Agent
		event claudia.Event
		text  string
	}{
		{"worker", stale, ev, response}, {"jevons-po", stale, ev, response},
		{"jevons-po", current, claudia.Event{Type: "assistant", SessionID: "worker-session", TurnID: "turn-1", StopReason: "end_turn"}, response},
		{"jevons-po", current, claudia.Event{Type: "assistant", SessionID: "po-session", TurnID: "turn-1", StopReason: "tool_use"}, response},
		{"jevons-po", current, ev, "Worker quoted: " + response},
	} {
		if err := s.observePOApproval(tc.name, tc.proc, tc.event, tc.text); err == nil && tc.text == response {
			t.Errorf("minted from invalid process/event %q", tc.name)
		}
	}
	if err := s.observePOApproval("jevons-po", current, ev, response); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(store.Dir, "grants.json"))
	if err != nil {
		t.Fatal(err)
	}
	var grants map[string]landinggrant.Grant
	if err = json.Unmarshal(b, &grants); err != nil || len(grants) != 1 {
		t.Fatalf("grants=%v err=%v", grants, err)
	}
	for _, g := range grants {
		if g.POEvent != "po-session/turn-1" || g.Review.Workers[0].SHA != "tip" {
			t.Fatalf("grant=%+v", g)
		}
	}
}
