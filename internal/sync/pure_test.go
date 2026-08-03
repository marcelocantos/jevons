// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sync

import "testing"

func TestClassifyFrameRejectsTextJSON(t *testing.T) {
	kind, frames, err := ClassifyFrame(WSText, []byte(`{"type":"user_message","text":"hi"}`))
	if kind != FrameApplicationJSON {
		t.Fatalf("kind: got %v want FrameApplicationJSON", kind)
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if frames != nil {
		t.Fatal("frames should be nil")
	}
	if !IsPureTransportViolation(kind) {
		t.Fatal("expected pure transport violation")
	}
}

func TestClassifyFrameRejectsBinaryJSON(t *testing.T) {
	kind, _, err := ClassifyFrame(WSBinary, []byte(`{"type":"init","version":"1"}`))
	if kind != FrameApplicationJSON {
		t.Fatalf("kind: got %v want FrameApplicationJSON", kind)
	}
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClassifyFrameAcceptsPeerBinary(t *testing.T) {
	raw, err := EncodePeerFrame(RoleAsReplica, TagAck, make([]byte, 8))
	if err != nil {
		t.Fatal(err)
	}
	kind, frames, err := ClassifyFrame(WSBinary, raw)
	if err != nil {
		t.Fatal(err)
	}
	if kind != FramePeerBinary {
		t.Fatalf("kind: got %v want FramePeerBinary", kind)
	}
	if len(frames) != 1 {
		t.Fatalf("frames: got %d want 1", len(frames))
	}
	if IsPureTransportViolation(kind) {
		t.Fatal("peer binary is not a violation")
	}
}

func TestClassifyFrameEmpty(t *testing.T) {
	kind, frames, err := ClassifyFrame(WSBinary, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kind != FrameEmpty {
		t.Fatalf("kind: got %v want FrameEmpty", kind)
	}
	if frames != nil {
		t.Fatal("frames should be nil")
	}
}

func TestClassifyFrameInvalidBinary(t *testing.T) {
	// Valid-looking non-JSON garbage that fails framing.
	kind, _, err := ClassifyFrame(WSBinary, []byte{0x01, 0x02, 0x03, 0x04, 0x05})
	if kind != FrameInvalid {
		t.Fatalf("kind: got %v want FrameInvalid", kind)
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsPureTransportViolation(kind) {
		t.Fatal("expected pure transport violation")
	}
}

func TestClassifyFrameRejectsNonBinaryNonText(t *testing.T) {
	kind, _, err := ClassifyFrame(WSMessageType(99), []byte{1})
	if kind != FrameInvalid {
		t.Fatalf("kind: got %v want FrameInvalid", kind)
	}
	if err == nil {
		t.Fatal("expected error")
	}
}
