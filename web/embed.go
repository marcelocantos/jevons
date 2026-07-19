// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package web embeds the canonical chat UI so a released jevonsd binary
// serves it standalone (🎯T53 — brew installs ship no repo checkout).
// Dev mode still serves the on-disk tree with hot reload when present.
package web

import "embed"

// FS holds the UI: index.html at the root plus the scripts/ modules.
// Test files are excluded from the embed.
//
//go:embed index.html scripts/chat_events.js scripts/transport.js scripts/jlog.js
var FS embed.FS
