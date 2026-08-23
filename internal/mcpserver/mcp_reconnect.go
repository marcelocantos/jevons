// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// grokRunFunc runs the Grok CLI with args (excluding the binary name) and
// returns combined stdout/stderr. Tests inject a fake; production shells out.
type grokRunFunc func(ctx context.Context, args ...string) (string, error)

// registerMCPReconnect exposes mid-session MCP reconnect for the overseer
// (🎯T60). Grok owns client attach; jevons is producer-only. The control
// plane is a disable→enable cycle via `grok mcp`, which Grok documents as
// live-toggling servers without a session rotate or TUI hop.
func (s *Server) registerMCPReconnect() {
	s.mcpSrv.AddTool(
		mcp.NewTool("jevons_mcp_reconnect",
			mcp.WithDescription(
				"Reconnect dropped MCP servers mid-session without leaving chat or rotating the session. "+
					"Cycles each target via `grok mcp disable` then `grok mcp enable` so the Grok client "+
					"re-attaches tools in the live overseer conversation (not only TUI /mcps). "+
					"Omit server to reconnect all configured servers; pass server to reconnect one by name. "+
					"Grok overseers only (🎯T282): with any other provider selected this reports the "+
					"provider-appropriate remedy instead of cycling Grok's config."),
			mcp.WithString("server",
				mcp.Description("Optional MCP server name (e.g. github, gmail). Empty = reconnect all configured servers.")),
		),
		s.handleMCPReconnect,
	)
}

func (s *Server) handleMCPReconnect(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	name, _ := args["server"].(string)
	name = strings.TrimSpace(name)

	report, err := s.reconnectMCPServers(ctx, name)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(report), nil
}

// mcpReconnectUnsupported is the pure oracle for which overseer providers
// this control plane applies to (🎯T282). `grok mcp disable/enable` toggles
// servers for a Grok client only; cycling it for a Claude overseer would
// report success while re-attaching nothing the caller can see. Rather
// than that silent lie, non-Grok overseers get a message naming the real
// remedy. It returns ok=true when the cycle may proceed.
func mcpReconnectUnsupported(provider claudia.Provider) (string, bool) {
	switch provider {
	case claudia.ProviderGrok, "":
		return "", true
	case claudia.ProviderClaude:
		return "mcp reconnect is a Grok control plane (`grok mcp disable/enable`); " +
			"the overseer provider is claude, which has no live disable/enable — " +
			"re-attach with /mcp in the session, or re-mint so AgentDef.MCPServers " +
			"carries jevonsmcp (claudia 🎯T40)", false
	default:
		return fmt.Sprintf("mcp reconnect is a Grok control plane (`grok mcp disable/enable`); "+
			"the overseer provider is %s, which jevonsd cannot cycle — restart jevonsd "+
			"to re-register its MCP endpoint for that client", provider), false
	}
}

// reconnectMCPServers reconnects one named server, or all configured
// servers when name is empty. Returns a human-readable multi-line report.
// Fails closed if the CLI path is a no-op (zero actions taken).
func (s *Server) reconnectMCPServers(ctx context.Context, name string) (string, error) {
	if msg, ok := mcpReconnectUnsupported(s.resolvedDefaultProvider()); !ok {
		return "", fmt.Errorf("%s", msg)
	}
	run := s.grokRun
	if run == nil {
		run = defaultGrokRun
	}

	var targets []string
	if name != "" {
		targets = []string{name}
	} else {
		listed, err := listGrokMCPServers(ctx, run)
		if err != nil {
			return "", fmt.Errorf("list MCP servers: %w", err)
		}
		if len(listed) == 0 {
			return "", fmt.Errorf("no MCP servers configured — nothing to reconnect")
		}
		targets = listed
	}

	var lines []string
	var okN, failN int
	for _, n := range targets {
		if err := cycleGrokMCPServer(ctx, run, n); err != nil {
			failN++
			lines = append(lines, fmt.Sprintf("FAIL %s: %v", n, err))
			continue
		}
		okN++
		lines = append(lines, fmt.Sprintf("OK   %s: reconnected (disable→enable)", n))
	}

	// Oracle: mid-session reconnect must not be a silent no-op.
	if okN+failN == 0 {
		return "", fmt.Errorf("reconnect no-op: no servers targeted")
	}
	if okN == 0 {
		return "", fmt.Errorf("reconnect failed for all targets:\n%s", strings.Join(lines, "\n"))
	}

	header := fmt.Sprintf("mcp reconnect: %d ok, %d failed", okN, failN)
	return header + "\n" + strings.Join(lines, "\n"), nil
}

// cycleGrokMCPServer forces a live re-attach: disable then enable. Disable
// errors are soft (already-disabled is fine); enable must succeed.
func cycleGrokMCPServer(ctx context.Context, run grokRunFunc, name string) error {
	if name == "" {
		return fmt.Errorf("empty server name")
	}
	// Best-effort disable: already-disabled or flaky state still proceeds to enable.
	_, _ = run(ctx, "mcp", "disable", name)
	out, err := run(ctx, "mcp", "enable", name)
	if err != nil {
		msg := strings.TrimSpace(out)
		if msg != "" {
			return fmt.Errorf("enable: %w (%s)", err, msg)
		}
		return fmt.Errorf("enable: %w", err)
	}
	return nil
}

type grokMCPListEntry struct {
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled"`
}

func listGrokMCPServers(ctx context.Context, run grokRunFunc) ([]string, error) {
	out, err := run(ctx, "mcp", "list", "--json")
	if err != nil {
		return nil, err
	}
	var entries []grokMCPListEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		return nil, fmt.Errorf("parse grok mcp list --json: %w", err)
	}
	var names []string
	for _, e := range entries {
		n := strings.TrimSpace(e.Name)
		if n == "" {
			continue
		}
		// Include disabled entries too — reconnect should re-enable them.
		names = append(names, n)
	}
	return names, nil
}

func defaultGrokRun(ctx context.Context, args ...string) (string, error) {
	bin := lookPathGrok()
	// Bound hung CLI calls so a stuck enable cannot freeze the overseer tool.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func lookPathGrok() string {
	if p, err := exec.LookPath("grok"); err == nil {
		return p
	}
	for _, p := range []string{
		"/Users/marcelo/.grok/bin/grok",
		"/opt/homebrew/bin/grok",
		"/usr/local/bin/grok",
	} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return "grok"
}
