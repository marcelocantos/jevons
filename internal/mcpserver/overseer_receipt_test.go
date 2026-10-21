// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import "testing"

// An agent note to the overseer whose batch receipt settled reports sent and
// is remembered as delivered (an identical replay is suppressed). One whose
// receipt did not settle stays delivered_unconfirmed, as before.
func TestOverseerNoteReportsItsSettledReceipt(t *testing.T) {
	settle := true
	var got []string
	s := &Server{}
	s.SetOverseerDeliverReceipted(func(text string, origin SendOrigin) (bool, error) {
		got = append(got, text)
		return settle && origin == OriginAgent, nil
	})

	res, err := s.deliverByName("jevons", "[Agent w answered your message mid-turn, and is still working]\nParis", OriginAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "sent" || !res.ReceiptSettled {
		t.Fatalf("settled overseer note = %+v, want sent with ReceiptSettled", res)
	}
	res, err = s.deliverByName("jevons", "[Agent w answered your message mid-turn, and is still working]\nParis", OriginAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusSuppressedReplay {
		t.Fatalf("replay of a settled note = %q, want %q", res.Status, StatusSuppressedReplay)
	}

	settle = false
	res, err = s.deliverByName("jevons", "worker done report", OriginAgent, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "delivered_unconfirmed" || res.ReceiptSettled {
		t.Fatalf("unsettled overseer note = %+v, want delivered_unconfirmed", res)
	}
	if len(got) != 2 {
		t.Fatalf("overseer handoffs = %d, want 2", len(got))
	}
}
