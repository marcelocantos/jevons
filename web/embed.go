// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package web embeds the canonical chat UI so a released jevonsd binary
// serves it standalone (🎯T53 — brew installs ship no repo checkout).
// Dev mode still serves the on-disk tree with hot reload when present.
package web

import "embed"

// FS holds the UI: index.html at the root plus the scripts/ modules.
// Test files (*_test.js) are not matched by the production list.
// Keep this list in sync with <script src="scripts/…"> in index.html.
//
//go:embed index.html
//go:embed scripts/jlog.js scripts/decision_log.js scripts/transport.js
//go:embed scripts/chat_reconnect.js scripts/chat_events.js scripts/attention_threads.js
//go:embed scripts/fleet_row.js scripts/agent_transcript.js scripts/virtual_list.js
//go:embed scripts/thread_route.js scripts/route_suggest.js scripts/composer_layout.js
//go:embed scripts/composer_keys.js scripts/composer_focus.js scripts/wispr_context.js
//go:embed scripts/send_queue.js scripts/composer_persist.js scripts/layout_probe.js scripts/tool_summary.js
//go:embed scripts/mermaid_actions.js scripts/markdown_normalize.js
//go:embed scripts/smd.js scripts/streaming_markdown.js
//go:embed scripts/tool_tooltip.js
var FS embed.FS
