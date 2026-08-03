// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"bytes"
	"fmt"
)

// FrameKind classifies an incoming WebSocket frame under pure-transport policy.
type FrameKind int

const (
	// FramePeerBinary is a binary frame that decodes as PeerMessage(s).
	FramePeerBinary FrameKind = iota
	// FrameApplicationJSON is a text (or JSON-looking) application protocol frame.
	// Pure transport rejects these.
	FrameApplicationJSON
	// FrameEmpty is a zero-length payload (ignored).
	FrameEmpty
	// FrameInvalid is binary that is not valid PeerMessage framing.
	FrameInvalid
)

// WSMessageType mirrors coder/websocket message types without importing it.
type WSMessageType int

const (
	// WSText is a WebSocket text frame (application JSON lives here today).
	WSText WSMessageType = 1
	// WSBinary is a WebSocket binary frame (sqlpipe peer wire).
	WSBinary WSMessageType = 2
)

// ClassifyFrame applies pure-transport policy to one WebSocket message.
//
// Pure transport residual (🎯T10 first acceptance criterion):
// WebSocket carries only sqlpipe peer messages — no application-level JSON.
func ClassifyFrame(mt WSMessageType, data []byte) (FrameKind, []PeerFrame, error) {
	if len(data) == 0 {
		return FrameEmpty, nil, nil
	}

	// Text frames are application protocol under the current chat wire.
	// Pure sqlpipe transport never uses text frames.
	if mt == WSText {
		return FrameApplicationJSON, nil, fmt.Errorf("pure transport rejects WebSocket text frames (application JSON)")
	}

	if mt != WSBinary {
		return FrameInvalid, nil, fmt.Errorf("pure transport rejects non-binary frame type %d", mt)
	}

	// Guard: application JSON sometimes sent as binary by mistake.
	if looksLikeJSON(data) {
		return FrameApplicationJSON, nil, fmt.Errorf("pure transport rejects application JSON in binary frames")
	}

	frames, err := DecodePeerFrames(data)
	if err != nil {
		return FrameInvalid, nil, err
	}
	if len(frames) == 0 {
		return FrameEmpty, nil, nil
	}
	return FramePeerBinary, frames, nil
}

// IsPureTransportViolation reports whether err came from rejecting
// application-level traffic on a pure-transport connection.
func IsPureTransportViolation(kind FrameKind) bool {
	return kind == FrameApplicationJSON || kind == FrameInvalid
}

func looksLikeJSON(data []byte) bool {
	trim := bytes.TrimLeft(data, " \t\r\n")
	if len(trim) == 0 {
		return false
	}
	return trim[0] == '{' || trim[0] == '['
}
