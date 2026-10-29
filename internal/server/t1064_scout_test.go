// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T1064 regression guard. T382 (chat_wire_owner_echo_test.go) fixed the
// case where a pasted image is represented INLINE in a single text block
// ("[image: ref]\n<words>"). It never covered the case where the provider's
// user-message content is genuinely MULTIPART — a non-text block (e.g. a
// real {"type":"image",...} block, Anthropic's actual multimodal shape)
// alongside a text block carrying userTurnPrefix.
//
// Before the fix, userTurnText's typed-block loop bailed to (""/false) the
// instant it saw ANY non-"text" block, so the owner-turn fact on the text
// block was never read, isOwnerNotifyText never ran, and chatWireLine fell
// through to the isClaudeShaped pass-through — emitting the RAW provider
// echo (marker and all) onto the wire as a second user-role line, alongside
// the clean bubble chatUserEcho already painted on send. One logical owner
// message, two wire lines.
//
// The fix: an "image" block alongside a "text" block no longer forces the
// bail; userTurnText joins the text blocks and ignores the image block, so
// the owner-turn fact is read and T382's dedup drops the echo as before.
// A structured line with NO text block (tool_result, bare tool_use) still
// bails to the pass-through, unaffected (see
// TestUserTurnTextLeavesStructuredLinesAlone).
//
// This is a candidate mechanism for the 2026-10-11 T1064 incident, not a
// confirmed cause — there is no access to that incident's actual event
// tape to confirm the real report used a true multipart block rather than
// some other shape.
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
	if !prose {
		t.Fatalf("userTurnText did not classify the multipart owner echo as prose (text=%q) — fix regressed", text)
	}
	if !isOwnerNotifyText(text) {
		t.Fatalf("isOwnerNotifyText did not recognize the owner-turn fact in %q", text)
	}

	// chatWireLine must now DROP this echo (the dedup runs), not pass it
	// through verbatim as a second bubble.
	if line, ok := chatWireLine(ev); ok {
		t.Fatalf("chatWireLine passed the multipart owner echo through to the wire (fix regressed): %s", line)
	}
}
