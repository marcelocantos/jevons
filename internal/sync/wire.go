// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"encoding/binary"
	"fmt"
)

// Peer message outer envelope (sqlpipe SerializePeer / DeserializePeer):
//
//	[4B LE length][1B sender_role][1B tag][payload…]
//
// length covers everything after the length prefix (role + tag + payload).
// Minimum valid frame is 6 bytes (4 + role + tag).

const (
	// peerFrameMin is the shortest legal PeerMessage wire encoding.
	peerFrameMin = 6

	// RoleAsMaster / RoleAsReplica match sqlpipe.SenderRole.
	RoleAsMaster  = 0
	RoleAsReplica = 1
)

// Message tags match sqlpipe.MessageTag (keep in lockstep with the library).
const (
	TagHello        = 0x01
	TagChangeset    = 0x03
	TagAck          = 0x08
	TagError        = 0x09
	TagBucketHashes = 0x0A
	TagNeedBuckets  = 0x0B
	TagRowHashes    = 0x0C
	TagDiffReady    = 0x0D
)

// PeerFrame is one length-prefixed PeerMessage on the wire.
type PeerFrame struct {
	// Raw is the full message including the 4-byte length prefix.
	Raw []byte
	// Role is the sender role byte (RoleAsMaster or RoleAsReplica).
	Role byte
	// Tag is the sqlpipe message tag.
	Tag byte
	// Payload is tag+payload content after the role byte (excluding length).
	// Equivalent to buf[5:] of the full frame.
	Payload []byte
}

// DecodePeerFrames splits a binary WebSocket payload into PeerFrames.
// Frames are concatenated with no count prefix (sqlpipe native batching).
func DecodePeerFrames(data []byte) ([]PeerFrame, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var frames []PeerFrame
	pos := 0
	for pos < len(data) {
		if pos+4 > len(data) {
			return nil, fmt.Errorf("truncated peer frame length at offset %d", pos)
		}
		msgLen := binary.LittleEndian.Uint32(data[pos:])
		total := 4 + int(msgLen)
		if msgLen < 2 {
			return nil, fmt.Errorf("peer frame too short at offset %d: length=%d", pos, msgLen)
		}
		if pos+total > len(data) {
			return nil, fmt.Errorf("truncated peer frame at offset %d: need %d bytes, have %d", pos, total, len(data)-pos)
		}
		raw := data[pos : pos+total]
		role := raw[4]
		if role != RoleAsMaster && role != RoleAsReplica {
			return nil, fmt.Errorf("invalid sender role %d at offset %d", role, pos)
		}
		tag := raw[5]
		if !knownTag(tag) {
			return nil, fmt.Errorf("unknown message tag 0x%02x at offset %d", tag, pos)
		}
		frames = append(frames, PeerFrame{
			Raw:     append([]byte(nil), raw...),
			Role:    role,
			Tag:     tag,
			Payload: append([]byte(nil), raw[5:]...),
		})
		pos += total
	}
	return frames, nil
}

// EncodePeerFrames concatenates PeerFrames into a single binary WebSocket payload.
func EncodePeerFrames(frames []PeerFrame) []byte {
	if len(frames) == 0 {
		return nil
	}
	var out []byte
	for _, f := range frames {
		out = append(out, f.Raw...)
	}
	return out
}

// EncodePeerFrame builds a single PeerMessage wire frame from role, tag, and
// tag-payload (payload may be empty; tag is the first payload byte if payload
// already includes it — callers pass tag separately).
//
// payload is the bytes after the tag (may be empty).
func EncodePeerFrame(role, tag byte, payload []byte) ([]byte, error) {
	if role != RoleAsMaster && role != RoleAsReplica {
		return nil, fmt.Errorf("invalid sender role %d", role)
	}
	if !knownTag(tag) {
		return nil, fmt.Errorf("unknown message tag 0x%02x", tag)
	}
	// length = role(1) + tag(1) + payload
	msgLen := 1 + 1 + len(payload)
	buf := make([]byte, 4+msgLen)
	binary.LittleEndian.PutUint32(buf[:4], uint32(msgLen))
	buf[4] = role
	buf[5] = tag
	copy(buf[6:], payload)
	return buf, nil
}

func knownTag(tag byte) bool {
	switch tag {
	case TagHello, TagChangeset, TagAck, TagError,
		TagBucketHashes, TagNeedBuckets, TagRowHashes, TagDiffReady:
		return true
	default:
		return false
	}
}
