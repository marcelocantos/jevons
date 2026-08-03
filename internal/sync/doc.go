// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package sync implements the pure sqlpipe transport residual for 🎯T10.
//
// Target architecture: the WebSocket is a pure sqlpipe PeerMessage pipe —
// no application-level JSON (init, history, text, status, user_message, …).
// Server-owned tables (transcript, sessions, scripts, state) and client-owned
// tables (requests, preferences) replicate bidirectionally via sqlpipe Peer;
// reconnect uses hash-based diff sync.
//
// Current residual (this package):
//
//   - Wire framing for concatenated PeerMessages (outer envelope matching
//     sqlpipe SerializePeer / DeserializePeer).
//   - Pure-transport frame policy: binary PeerMessage frames only;
//     WebSocket text / application JSON is a protocol violation.
//   - Sync table DDL constants for the eventual Peer-backed store.
//
// Not yet in this package (blockers for full 🎯T10):
//
//   - Live sqlpipe.Peer / SyncManager (removed in 2d1f5b4 when sqlpipe was
//     dropped for modernc.org/sqlite; re-adding needs CGO + C++23 and a CI
//     toolchain decision).
//   - Product cutover of /ws/chat and /ws/remote off application JSON.
//   - Query-subscription render path (Lua / web).
//
// /ws/sqlpipe is the pure-transport endpoint: it only accepts binary frames
// and refuses text JSON. Without a PeerSession attached it fails closed.
package sync
