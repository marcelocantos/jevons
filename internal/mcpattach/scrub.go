// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpattach

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/mcpscope"
)

// Scrub deletes Name from provider user-scope configs. Empty path
// overrides mean the production HOME files. Missing files are a no-op.
// Isolates should not call this on HOME — they pass fixture paths or skip.
func Scrub(a Args) error {
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return fmt.Errorf("mcpattach: scrub name required")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("mcpattach: home dir: %w", err)
	}
	claude := a.ClaudeJSON
	if claude == "" {
		claude = filepath.Join(home, ".claude.json")
	}
	cursor := a.CursorJSON
	if cursor == "" {
		cursor = filepath.Join(home, ".cursor", "mcp.json")
	}
	if _, err := mcpscope.WriteRemove(claude, name); err != nil {
		return fmt.Errorf("mcpattach: scrub claude: %w", err)
	}
	// Cursor is the exception: it is kept, not scrubbed. cursor-agent
	// 2026.09.18 gives the model only what ~/.cursor/mcp.json holds and
	// ignores servers passed per session over ACP (claudia 🎯T118), so
	// the per-session channel 🎯T464 relies on does not reach a Cursor
	// seat. Scrubbing this file was the reason no Cursor seat had
	// jevons_* tools on 2026-09-22; with the entry present, a throwaway
	// session listed all 55. A missing file is a no-op, as before.
	if strings.TrimSpace(a.URL) != "" {
		if _, err := os.Stat(cursor); err == nil {
			if _, err := mcpscope.WriteEnsure(cursor, name, mcpscope.HTTPEntry(a.URL)); err != nil {
				return fmt.Errorf("mcpattach: ensure cursor: %w", err)
			}
		}
	}
	grok := a.GrokTOML
	if grok == "" {
		grok = filepath.Join(home, ".grok", "config.toml")
	}
	codex := a.CodexTOML
	if codex == "" {
		codex = filepath.Join(home, ".codex", "config.toml")
	}
	if err := scrubTOMLServer(grok, name); err != nil {
		return fmt.Errorf("mcpattach: scrub grok: %w", err)
	}
	if err := scrubTOMLServer(codex, name); err != nil {
		return fmt.Errorf("mcpattach: scrub codex: %w", err)
	}
	return nil
}

func scrubTOMLServer(path, name string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	out, changed := removeTOMLHTTPServer(data, name)
	if !changed {
		return nil
	}
	return os.WriteFile(path, out, 0o600)
}

// removeTOMLHTTPServer drops `[mcp_servers.<name>]` and any dotted
// children until the next top-level or sibling table.
func removeTOMLHTTPServer(data []byte, name string) ([]byte, bool) {
	want := "[mcp_servers." + name
	lines := strings.SplitAfter(string(data), "\n")
	var b strings.Builder
	skip := false
	changed := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") {
			if strings.HasPrefix(trim, want+"]") || strings.HasPrefix(trim, want+".") {
				skip = true
				changed = true
				continue
			}
			skip = false
		}
		if skip {
			continue
		}
		b.WriteString(line)
	}
	if !changed {
		return data, false
	}
	return []byte(b.String()), true
}
