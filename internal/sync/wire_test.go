// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestEncodeDecodePeerFrameRoundTrip(t *testing.T) {
	raw, err := EncodePeerFrame(RoleAsMaster, TagAck, []byte{
		// AckMsg payload: seq int64 little-endian (0)
		0, 0, 0, 0, 0, 0, 0, 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < peerFrameMin {
		t.Fatalf("frame too short: %d", len(raw))
	}

	frames, err := DecodePeerFrames(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("want 1 frame, got %d", len(frames))
	}
	if frames[0].Role != RoleAsMaster {
		t.Fatalf("role: got %d want %d", frames[0].Role, RoleAsMaster)
	}
	if frames[0].Tag != TagAck {
		t.Fatalf("tag: got 0x%02x want 0x%02x", frames[0].Tag, TagAck)
	}
	if !bytes.Equal(frames[0].Raw, raw) {
		t.Fatal("raw round-trip mismatch")
	}
}

func TestDecodePeerFramesBatch(t *testing.T) {
	a, err := EncodePeerFrame(RoleAsMaster, TagAck, make([]byte, 8))
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncodePeerFrame(RoleAsReplica, TagHello, []byte{
		// minimal-ish: protocol version u32 + empty fingerprint + 0 tables + last_seq i64
		// keep as opaque payload for framing test
		1, 0, 0, 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	batch := append(append([]byte{}, a...), b...)
	frames, err := DecodePeerFrames(batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("want 2 frames, got %d", len(frames))
	}
	if frames[0].Role != RoleAsMaster || frames[1].Role != RoleAsReplica {
		t.Fatalf("roles: %d %d", frames[0].Role, frames[1].Role)
	}
	if !bytes.Equal(EncodePeerFrames(frames), batch) {
		t.Fatal("EncodePeerFrames did not reassemble batch")
	}
}

func TestDecodePeerFramesTruncated(t *testing.T) {
	raw, err := EncodePeerFrame(RoleAsMaster, TagAck, make([]byte, 8))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePeerFrames(raw[:len(raw)-1]); err == nil {
		t.Fatal("expected truncated error")
	}
	// Length prefix only.
	if _, err := DecodePeerFrames([]byte{10, 0, 0}); err == nil {
		t.Fatal("expected truncated length error")
	}
}

func TestDecodePeerFramesInvalidRole(t *testing.T) {
	buf := make([]byte, 6)
	binary.LittleEndian.PutUint32(buf[:4], 2) // role + tag
	buf[4] = 9                                // invalid role
	buf[5] = TagAck
	if _, err := DecodePeerFrames(buf); err == nil {
		t.Fatal("expected invalid role error")
	}
}

func TestDecodePeerFramesUnknownTag(t *testing.T) {
	buf := make([]byte, 6)
	binary.LittleEndian.PutUint32(buf[:4], 2)
	buf[4] = RoleAsMaster
	buf[5] = 0xFF
	if _, err := DecodePeerFrames(buf); err == nil {
		t.Fatal("expected unknown tag error")
	}
}

func TestEncodePeerFrameRejectsBadRoleTag(t *testing.T) {
	if _, err := EncodePeerFrame(3, TagAck, nil); err == nil {
		t.Fatal("expected bad role error")
	}
	if _, err := EncodePeerFrame(RoleAsMaster, 0xEE, nil); err == nil {
		t.Fatal("expected bad tag error")
	}
}
