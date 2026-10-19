// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
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

// TestT7664ProviderCodeCauseEntersTheHardBlockHook drives DeliverOverseerEvent.
// A provider error code on Raw, with Reason empty, enters the hard-block hook.
// A stamped Reason does too. A sentence in the error field, the code written
// only in the text, and a sentence Reason do not. The owner-visible wire line
// for the code names spend_wall.
func TestT7664ProviderCodeCauseEntersTheHardBlockHook(t *testing.T) {
	s := New("test", t.TempDir())
	var got []agenterr.Class
	s.SetProviderHardBlockHooks(func(c agenterr.Class, _ string) {
		got = append(got, c)
	}, func() {})

	spend := "You've hit your monthly spend limit"
	bill := claudia.Event{
		Type:    "assistant",
		Text:    spend,
		IsError: true,
		Raw:     []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"You've hit your monthly spend limit"}]},"error":"billing_error"}`),
	}
	s.DeliverOverseerEvent(bill)
	line, ok := chatWireLine(bill)
	if !ok || !strings.Contains(line, "spend_wall") {
		t.Fatalf("billing_error wire line ok=%v line=%s", ok, line)
	}

	prose := bill
	prose.Raw = []byte(`{"type":"assistant","error":"You've hit your monthly spend limit"}`)
	s.DeliverOverseerEvent(prose)
	proseLine, proseOK := chatWireLine(prose)
	if !proseOK || strings.Contains(proseLine, "spend_wall") {
		t.Fatalf("sentence error field wire ok=%v line=%s", proseOK, proseLine)
	}

	textOnly := bill
	textOnly.Raw = []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"billing_error"}]}}`)
	s.DeliverOverseerEvent(textOnly)

	held := bill
	held.Reason = spend
	s.DeliverOverseerEvent(held)

	stamped := bill
	stamped.Reason = agenterr.CauseSpendWall
	s.DeliverOverseerEvent(stamped)

	rate := bill
	rate.Text = "you are rate limited"
	rate.Raw = []byte(`{"type":"assistant","error":"rate_limit"}`)
	s.DeliverOverseerEvent(rate)

	auth := bill
	auth.Raw = []byte(`{"type":"assistant","error":"authentication_failed"}`)
	s.DeliverOverseerEvent(auth)
	auth.Raw = []byte(`{"type":"assistant","error":"authentication_error"}`)
	s.DeliverOverseerEvent(auth)

	bug := bill
	bug.Raw = []byte(`{"type":"assistant","error":"model_not_found"}`)
	s.DeliverOverseerEvent(bug)

	backend := bill
	backend.Raw = []byte(`{"type":"assistant","error":"overloaded_error"}`)
	s.DeliverOverseerEvent(backend)
	backend.Raw = []byte(`{"type":"assistant","error":"internal_error"}`)
	s.DeliverOverseerEvent(backend)

	unknown := bill
	unknown.Raw = []byte(`{"type":"assistant","error":"something_else"}`)
	s.DeliverOverseerEvent(unknown)

	want := []agenterr.Class{
		agenterr.ClassSpendWall,
		agenterr.ClassSpendWall,
		agenterr.ClassRateLimit,
		agenterr.ClassAuth,
		agenterr.ClassAuth,
		agenterr.ClassClientBug,
		agenterr.ClassBackendUnavailable,
		agenterr.ClassBackendUnavailable,
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
