// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"os"
	"strings"
)

// Live provider usage APIs are optional. When TryLiveAPI is set we probe
// for credentials and emit notes; failures never abort the local scrape
// path (🎯T163 residual: auth/rate limits → fixture/local still works).

func tryAnthropicUsageAPI() string {
	if envAny("ANTHROPIC_ADMIN_API_KEY", "ANTHROPIC_API_KEY") == "" {
		return "live API: no ANTHROPIC_ADMIN_API_KEY/ANTHROPIC_API_KEY — using local Claude JSONL scrape"
	}
	// Admin Usage API is org-scoped and not universal; document residual.
	return "live API: Anthropic Admin Usage API not wired (requires org admin key + date range); local JSONL scrape used"
}

func tryXAIUsageAPI() string {
	if envAny("XAI_API_KEY") == "" {
		return "live API: no XAI_API_KEY — using local Grok updates.jsonl scrape"
	}
	return "live API: xAI has no public usage rollup endpoint; local updates.jsonl scrape used"
}

func tryOpenAIUsageAPI() string {
	if envAny("OPENAI_API_KEY", "CODEX_API_KEY") == "" {
		return "live API: no OPENAI_API_KEY — using local Codex rollout JSONL scrape"
	}
	return "live API: OpenAI organization usage API not wired for CLI report; local rollout JSONL scrape used"
}

func tryCursorUsageAPI() string {
	return "live API: Cursor has no public usage API; use dashboard export JSON or ai-code-tracking.db"
}

func envAny(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
