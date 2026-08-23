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
	if _, err := mcpscope.WriteRemove(cursor, name); err != nil {
		return fmt.Errorf("mcpattach: scrub cursor: %w", err)
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
