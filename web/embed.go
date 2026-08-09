// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package web embeds the canonical chat UI so a released jevonsd binary
// serves it standalone (🎯T53 — brew installs ship no repo checkout).
// Dev mode still serves the on-disk tree with hot reload when present.
package web

import "embed"

// FS holds the UI: index.html at the root plus the scripts/ modules.
// Test files (*_test.js) are not matched by the production list.
// Keep this list in sync with <script src="scripts/…"> in index.html
// (enforced by TestEmbeddedScriptsCoverIndex — 🎯T292).
//
//go:embed index.html
//go:embed scripts/boot_sentinel.js
//go:embed scripts/jlog.js
//go:embed scripts/decision_log.js
//go:embed scripts/transport.js
//go:embed scripts/chat_reconnect.js
//go:embed scripts/owner_ux.js
//go:embed scripts/history_loading.js
//go:embed scripts/chat_events.js
//go:embed scripts/attention_threads.js
//go:embed scripts/idea_capture.js
//go:embed scripts/aside_history.js
//go:embed scripts/fleet_row.js
//go:embed scripts/model_prefix.js
//go:embed scripts/fleet_paint.js
//go:embed scripts/fleet_cycle.js
//go:embed scripts/portfolio_group.js
//go:embed scripts/agent_transcript.js
//go:embed scripts/conversation_widget.js
//go:embed scripts/frontier_table.js
//go:embed scripts/rsi_dispositions.js
//go:embed scripts/target_context_chrome.js
//go:embed scripts/target_hotspot.js
//go:embed scripts/instant_tip.js
//go:embed scripts/virtual_list.js
//go:embed scripts/thread_route.js
//go:embed scripts/route_suggest.js
//go:embed scripts/composer_layout.js
//go:embed scripts/composer_keys.js
//go:embed scripts/composer_focus.js
//go:embed scripts/wispr_context.js
//go:embed scripts/send_queue.js
//go:embed scripts/pending_turns.js
//go:embed scripts/composer_persist.js
//go:embed scripts/rhs_layout.js
//go:embed scripts/layout_probe.js
//go:embed scripts/tool_summary.js
//go:embed scripts/working_progress.js
//go:embed scripts/mermaid_actions.js
//go:embed scripts/markdown_normalize.js
//go:embed scripts/decision_matrix.js
//go:embed scripts/link_safety.js
//go:embed scripts/image_lightbox.js
//go:embed scripts/smd.js
//go:embed scripts/streaming_markdown.js
//go:embed scripts/cost_display.js
//go:embed scripts/tool_tooltip.js
var FS embed.FS
