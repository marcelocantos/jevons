// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package desktop

// Signal is one checkable inventory entry: a mnemo T85/T86/T89 surface
// (or Windows-tray twin) and the Jevons-head path that supersedes it.
// Acceptance (2): mnemo T85/T86/T89 are set aside; equivalent signals
// are reachable from the Jevons head via this inventory.
type Signal struct {
	// ID is a stable inventory key, e.g. "t85.thread_list".
	ID string `json:"id"`
	// Source names the mnemo target/surface being superseded.
	Source string `json:"source"`
	// Description is what the signal did on mnemo's head.
	Description string `json:"description"`
	// Reachability is the Jevons-head path that carries the equivalent
	// signal (provider section, feed, API, or head chrome itself).
	Reachability string `json:"reachability"`
}

// SupersededMnemoTargets lists the mnemo target ids this head retires.
var SupersededMnemoTargets = []string{"T85", "T86", "T89"}

// MnemoSupersessionInventory is the checkable catalog of signals that
// mnemo's menu-bar/tray head (T85 shim, T86 popup expansion, T89 Windows
// tray) used to own, and how each is reached from the Jevons head.
//
// Reachability is declarative: mnemo as a 🎯T27 provider (T27.8) ships
// its UI surfaces and feeds over the generic contract; the head renders
// them as one section without per-provider code. Head-chrome signals
// (status item presence) are owned by this process itself.
func MnemoSupersessionInventory() []Signal {
	return []Signal{
		// ── T85: Threads menu-bar navigator (macOS) ──────────────────
		{
			ID:          "t85.status_item",
			Source:      "mnemo T85 / T85.4",
			Description: "Menu-bar status item presence (LSUIElement shim)",
			Reachability: "jevons-head process: platform status item / tray " +
				"chrome over MenuModel (this package); launches + connects to jevonsd",
		},
		{
			ID:          "t85.thread_list",
			Source:      "mnemo T85 /api/thread/list",
			Description: "List of initiative threads with activity markers",
			Reachability: "provider section for mnemo (T27.8): composed ViewNode " +
				"surface driven by mnemo feed; head section appears when mnemo enabled",
		},
		{
			ID:          "t85.thread_preview",
			Source:      "mnemo T85 /api/thread/preview",
			Description: "Hover/preview of thread CLAUDE.md content",
			Reachability: "mnemo provider UI surface (ViewNode subtree) under the " +
				"mnemo section; actions namespaced provider/mnemo/…",
		},
		{
			ID:          "t85.thread_search",
			Source:      "mnemo T85 /api/thread/search",
			Description: "Instant + FTS search across threads",
			Reachability: "mnemo provider surface + MCP tool re-export " +
				"(provider__search via T27.4 /mcp) reachable from head actions",
		},
		{
			ID:          "t85.thread_go",
			Source:      "mnemo T85 /api/thread/go",
			Description: "Open/resume thread in terminal (iTerm2)",
			Reachability: "mnemo provider action (provider/mnemo/…/go) relayed " +
				"over /ws/provider; head button in section",
		},
		{
			ID:          "t85.thread_new",
			Source:      "mnemo T85 /api/thread/new",
			Description: "Create a new initiative thread",
			Reachability: "mnemo provider action / MCP tool under mnemo section",
		},
		{
			ID:          "t85.thread_archive",
			Source:      "mnemo T85 /api/thread/archive",
			Description: "Archive a thread into _archived/",
			Reachability: "mnemo provider action under mnemo section",
		},
		{
			ID:          "t85.markers",
			Source:      "mnemo T85.6 importance markers",
			Description: "Pin/important marker column and sort",
			Reachability: "mnemo feed event → aggregated model → surface re-render " +
				"in mnemo head section (T27.5/T27.6 path)",
		},

		// ── T86: Curated popup signals beyond basics ─────────────────
		{
			ID:          "t86.health",
			Source:      "mnemo T86 / health SSE",
			Description: "Daemon health / doctor severity on status glyph",
			Reachability: "GET /api/providers + provider_status frames on " +
				"/ws/remote; head chrome may tint from FeedStatus; " +
				"mnemo health surface in section when declared",
		},
		{
			ID:          "t86.activity_staleness",
			Source:      "mnemo T86 activity / staleness indicators",
			Description: "Per-thread activity age and staleness nudges",
			Reachability: "mnemo feed (activity) folded into aggregated model; " +
				"section text driven by last-known feed snapshot",
		},
		{
			ID:          "t86.session_signals",
			Source:      "mnemo T86 candidates (summary, decisions, PRs/CI, TODOs)",
			Description: "Curated high-value mnemo-backed popup signals",
			Reachability: "declarative mnemo UI surfaces + feeds; head renders " +
				"whatever mnemo declares — no jevonsd per-signal code",
		},
		{
			ID:          "t86.spend_throttle",
			Source:      "mnemo T86/T140 spend + throttle popup surface",
			Description: "Token/cost and throttle state in the popup",
			Reachability: "provider feed (budget/throttle) → model → mnemo " +
				"section; same generic path as any feed-bound surface",
		},

		// ── T89: Windows system-tray native head ─────────────────────
		{
			ID:          "t89.tray_presence",
			Source:      "mnemo T89 system tray",
			Description: "Windows system-tray icon presence",
			Reachability: "jevons-head platform tray chrome (same MenuModel); " +
				"Windows residual is class-3 packaging, not a second inventory",
		},
		{
			ID:          "t89.thread_list_preview",
			Source:      "mnemo T89 list + WebView2 preview",
			Description: "Thread list and preview over daemon API on Windows",
			Reachability: "same mnemo provider section as t85.thread_*; " +
				"head is medium-independent over /ws/remote ViewNode",
		},
		{
			ID:          "t89.health_sse",
			Source:      "mnemo T89 /api/events health",
			Description: "Health stream consumer on Windows tray",
			Reachability: "provider_status + /api/providers on jevonsd; " +
				"head applies the same frames as macOS",
		},
		{
			ID:          "t89.search_markers_go",
			Source:      "mnemo T89 search / markers / go (wt.exe)",
			Description: "Search, markers, and open-in-terminal on Windows",
			Reachability: "mnemo provider actions under the mnemo section; " +
				"terminal verb is provider-side (not head-hardcoded)",
		},
	}
}

// InventoryComplete reports whether every required signal id is present
// with a non-empty reachability string. The required set is the union of
// T85/T86/T89 load-bearing surfaces listed above — the oracle for
// acceptance (2).
func InventoryComplete(signals []Signal) (missing []string) {
	required := map[string]bool{
		"t85.status_item": true, "t85.thread_list": true, "t85.thread_preview": true,
		"t85.thread_search": true, "t85.thread_go": true, "t85.thread_new": true,
		"t85.thread_archive": true, "t85.markers": true,
		"t86.health": true, "t86.activity_staleness": true, "t86.session_signals": true,
		"t86.spend_throttle": true,
		"t89.tray_presence": true, "t89.thread_list_preview": true,
		"t89.health_sse": true, "t89.search_markers_go": true,
	}
	seen := make(map[string]bool, len(signals))
	for _, s := range signals {
		if s.ID == "" || s.Reachability == "" {
			missing = append(missing, s.ID+"(empty)")
			continue
		}
		seen[s.ID] = true
	}
	for id := range required {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	return missing
}
