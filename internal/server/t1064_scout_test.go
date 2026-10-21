// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1064 scout oracle. T382 (chat_wire_owner_echo_test.go) fixed the case
// where a pasted image is represented INLINE in a single text block
// ("[image: ref]\n<words>"). It never covered the case where the provider's
// user-message content is genuinely MULTIPART — a non-text block (e.g. a
// real {"type":"image",...} block, Anthropic's actual multimodal shape)
// alongside a text block carrying userTurnPrefix. userTurnText's typed-block
// loop bails to (""/false) the instant it sees ANY non-"text" block type, so
// the owner-turn fact on the text block is never read, isOwnerNotifyText
// never runs, and chatWireLine falls through to the isClaudeShaped
// pass-through — emitting the RAW provider echo (marker and all) onto the
// wire as a second user-role line, alongside the clean bubble chatUserEcho
// already painted on send. One logical owner message, two wire lines.
//
// This is a candidate mechanism for T1064 (split bubble), not a confirmed
// cause — it demonstrates the dedup gap is real and reproducible on
// multipart content; whether the real incident used mixed content blocks is
// unconfirmed (no incident tape available).
func TestT1064MixedContentBlockEchoBypassesOwnerDedup(t *testing.T) {
	body := "Here is a screenshot"
	delivered := userTurnPrefix + body
	raw, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "image", "source": map[string]any{"type": "base64", "data": "AAAA"}},
				{"type": "text", "text": delivered},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := claudia.Event{Type: "user", Raw: raw}

	text, prose := userTurnText(ev)
	if prose {
		t.Fatalf("userTurnText unexpectedly classified mixed content as prose (text=%q) — gap may already be closed", text)
	}
	if isOwnerNotifyText(text) {
		t.Fatalf("isOwnerNotifyText unexpectedly true on empty text")
	}

	line, ok := chatWireLine(ev)
	if !ok {
		t.Fatal("chatWireLine dropped the mixed-content echo (gap closed — update this test)")
	}
	// The raw provider echo, marker included, reached the wire — this is
	// the second line a client would paint as a second bubble for the
	// SAME owner message it already painted via chatUserEcho on send.
	var decoded struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(line), &decoded); err != nil || decoded.Type != "user" {
		t.Fatalf("expected a pass-through user-role wire line, got: %s", line)
	}
	t.Logf("REPRO confirmed: mixed-content owner echo bypasses isOwnerNotifyText dedup and reaches the wire verbatim: %s", line)
}
