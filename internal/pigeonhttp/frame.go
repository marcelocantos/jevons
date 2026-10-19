// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package pigeonhttp carries the thin HTTP/WebSocket gateway that lets a
// paired iOS client reach the canonical React cockpit over pigeon's
// primary stream (🎯T628).
//
// Swift Pigeon 0.19.0 exposes only send/recv on the primary stream (no
// OpenChannel), so concurrent HTTP requests and WebSocket sessions are
// multiplexed as length-prefixed JSON frames on that one stream.
package pigeonhttp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ProtocolVersion is the only supported frame version.
const ProtocolVersion = 1

// Ops carried on the primary stream.
const (
	OpHello       = "hello"         // client → server: enter gateway mode
	OpHelloOK     = "hello-ok"      // server → client
	OpHTTP        = "http"          // client → server: full request (small bodies)
	OpHTTPResp    = "http-resp"     // server → client: status + headers
	OpHTTPBody    = "http-body"     // either direction: body chunk
	OpWSOpen      = "ws-open"       // client → server
	OpWSOpenOK    = "ws-open-ok"    // server → client
	OpWSOpenErr   = "ws-open-err"   // server → client
	OpWSMsg       = "ws-msg"        // either direction
	OpWSClose     = "ws-close"      // either direction
	OpError       = "error"         // server → client for a request id
)

// Frame is one multiplexed message on the pigeon primary stream.
type Frame struct {
	V       int               `json:"v"`
	Op      string            `json:"op"`
	ID      string            `json:"id,omitempty"`
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Status  int               `json:"status,omitempty"`
	BodyB64 string            `json:"body_b64,omitempty"`
	ChunkB64 string           `json:"chunk_b64,omitempty"`
	Fin     bool              `json:"fin,omitempty"`
	Text    bool              `json:"text,omitempty"` // ws-msg: true = text frame
	Code    int               `json:"code,omitempty"` // ws-close
	Reason  string            `json:"reason,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// Encode marshals a frame to JSON bytes.
func Encode(f Frame) ([]byte, error) {
	if f.V == 0 {
		f.V = ProtocolVersion
	}
	return json.Marshal(f)
}

// Decode unmarshals a frame. Rejects unknown versions.
func Decode(data []byte) (Frame, error) {
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		return Frame{}, fmt.Errorf("pigeonhttp: decode: %w", err)
	}
	if f.V != ProtocolVersion {
		return Frame{}, fmt.Errorf("pigeonhttp: unsupported version %d", f.V)
	}
	if f.Op == "" {
		return Frame{}, fmt.Errorf("pigeonhttp: missing op")
	}
	return f, nil
}

// Body returns the decoded body_b64 payload, or nil.
func (f Frame) Body() ([]byte, error) {
	if f.BodyB64 == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(f.BodyB64)
}

// Chunk returns the decoded chunk_b64 payload, or nil.
func (f Frame) Chunk() ([]byte, error) {
	if f.ChunkB64 == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(f.ChunkB64)
}

// WithBody sets body_b64 from raw bytes.
func WithBody(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(raw)
}
