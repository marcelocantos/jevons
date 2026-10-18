// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// The overseer wire enters the hard-block hook from a typed spend cause.
// The same sentence, and the same sentence on a 429, do not.
func TestT7664SpendCauseEntersTheHardBlockHook(t *testing.T) {
	s := New("test", t.TempDir())
	var got []agenterr.Class
	s.SetProviderHardBlockHooks(func(c agenterr.Class, raw string) {
		got = append(got, c)
		if c == agenterr.ClassSpendWall && !agenterr.HardBlock(c, raw) {
			t.Errorf("hook class %s did not hard-block", c)
		}
		if c == agenterr.ClassRateLimit && agenterr.HardBlock(c, raw) {
			t.Errorf("hook class %s hard-blocked %q", c, raw)
		}
	}, func() {})

	spend := "You've hit your monthly spend limit"
	s.DeliverOverseerEvent(claudia.Event{Type: "assistant", Text: spend, IsError: true})
	if len(got) != 0 {
		t.Fatalf("spend prose called the hook: %v", got)
	}

	sentenceReason := claudia.Event{
		Type:    "assistant",
		Text:    spend,
		IsError: true,
		Raw:     []byte(`{"code":402,"message":"You've hit your monthly spend limit"}`),
		Reason:  spend,
	}
	s.DeliverOverseerEvent(sentenceReason)
	if len(got) != 0 {
		t.Fatalf("a spend sentence in Reason called the hook: %v", got)
	}

	s.DeliverOverseerEvent(claudia.Event{
		Type: "assistant",
		Text: spend,
		Raw:  []byte(`{"code":429,"message":"You've hit your monthly spend limit"}`),
	})
	s.DeliverOverseerEvent(claudia.Event{
		Type: "assistant",
		Text: spend,
		Raw:  []byte(`{"code":402,"message":"You've hit your monthly spend limit"}`),
	})
	s.DeliverOverseerEvent(claudia.Event{
		Type:    "assistant",
		Text:    spend,
		IsError: true,
		Reason:  agenterr.CauseSpendWall,
	})
	s.DeliverOverseerEvent(claudia.Event{
		Type:    "assistant",
		Text:    spend,
		IsError: true,
		Reason:  agenterr.CauseRateLimit,
	})

	want := []agenterr.Class{
		agenterr.ClassRateLimit,
		agenterr.ClassSpendWall,
		agenterr.ClassSpendWall,
		agenterr.ClassRateLimit,
	}
	if len(got) != len(want) {
		t.Fatalf("hook classes %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hook[%d]=%s want %s (all %v)", i, got[i], want[i], got)
		}
	}
}
