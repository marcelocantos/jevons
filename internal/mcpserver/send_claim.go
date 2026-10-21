// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"strings"
)

// 🎯T429 — a failed send call answers "why did the CALL fail", not "what
// happened to the PAYLOAD". 🎯T416 established the rule: the RECEIVER's own
// records decide, and where they cannot, the answer is unknown. So a send
// error is first asked a narrower question than "did it fail": does it
// DISPROVE delivery? Only ClaimNotSent does.
type SendClaim int

const (
	// ClaimNotSent: the error is positive evidence the payload never reached
	// the receiver. The process was gone, the registry refused, the paste
	// itself errored — nothing was handed over, so there is nothing for the
	// receiver's records to hold and no reason to go looking.
	ClaimNotSent SendClaim = iota
	// ClaimReplyUnverified: the payload was handed over and the wait for a
	// REPLY ran out. It says the receiver had not finished answering inside a
	// window this daemon chose; it says nothing whatever about delivery.
	// Clause 5 is the instance: a payload sat in the receiver's transcript at
	// 10:22:51Z, 3873 bytes of it, while the caller was told the operation
	// had failed. The claim is the typed deadline, not that sentence.
	ClaimReplyUnverified
)

func (c SendClaim) String() string {
	switch c {
	case ClaimReplyUnverified:
		return "reply_unverified"
	default:
		return "not_sent"
	}
}

// DisprovesDelivery reports whether this error is itself evidence that the
// receiver never got the payload. Only ClaimNotSent is: the other is a
// failure to observe, and a failure to observe cannot distinguish "not there"
// from "not there yet".
func (c SendClaim) DisprovesDelivery() bool { return c == ClaimNotSent }

// ClassifySendError decides what a send error says about the payload. Pure.
//
// A reply wait is context.DeadlineExceeded, wrapped or not. That is what
// fleet's reply assembler returns when its own window closes. A sentence
// that quotes a timeout is not that wait: the MCP client wrote "The
// operation timed out" for a push that had already landed, and matching
// the words would treat a quoted failure as a failure to observe.
// Network deadlines (os.ErrDeadlineExceeded, a net.Error timeout) stay
// ClaimNotSent — they are a transport that did not reach the provider.
func ClassifySendError(err error) SendClaim {
	if err == nil {
		return ClaimNotSent
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ClaimReplyUnverified
	}
	return ClaimNotSent
}

// describeTransportClaim renders the transport's own account for an operator,
// beside — never instead of — what the daemon observed of the receiver. The two
// disagreeing is the ordinary case this target exists to handle, so the message
// has to show both or the reader cannot tell which one they are being told.
func describeTransportClaim(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	const max = 300
	if len(text) > max {
		text = text[:max] + "…"
	}
	return text
}
