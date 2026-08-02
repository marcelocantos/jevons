// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/selftest"
)

// SelfTestEnvFunc builds the pack execution environment (wired from
// jevonsd / HTTP server so MCP and POST /api/self_test/run share hooks).
type SelfTestEnvFunc func() *selftest.Env

// SetSelfTestEnv wires 🎯T110 self_test.* tools.
func (s *Server) SetSelfTestEnv(fn SelfTestEnvFunc) {
	s.selfTestEnv = fn
	s.registerSelfTestTools()
}

func (s *Server) registerSelfTestTools() {
	// Tool names match acceptance (self_test.run / list).
	s.mcpSrv.AddTool(
		mcp.NewTool("self_test.run",
			mcp.WithDescription("Run a self-test pack (🎯T110). Accepts pack id (or 'all') and site live|drill|ci. Returns the shared report schema; grade is derived from measurements only."),
			mcp.WithString("pack", mcp.Description("Pack id (health-L1, composer-growth-L2, agents-parent-L1) or 'all'. Default all.")),
			mcp.WithString("site", mcp.Description("Site class: live | drill | ci. Default ci.")),
		),
		s.handleSelfTestRun,
	)
	s.mcpSrv.AddTool(
		mcp.NewTool("self_test.list",
			mcp.WithDescription("List registered self-test packs with class and allowed sites (🎯T110)."),
		),
		s.handleSelfTestList,
	)
}

func (s *Server) handleSelfTestList(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	type row struct {
		ID    string   `json:"id"`
		Class string   `json:"class"`
		Sites []string `json:"sites"`
	}
	var rows []row
	for _, p := range selftest.ListPacks() {
		sites := make([]string, 0, len(p.AllowedSites()))
		for _, site := range p.AllowedSites() {
			sites = append(sites, string(site))
		}
		rows = append(rows, row{ID: p.ID(), Class: p.Class(), Sites: sites})
	}
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(raw)), nil
}

func (s *Server) handleSelfTestRun(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	pack, _ := args["pack"].(string)
	siteStr, _ := args["site"].(string)
	if siteStr == "" {
		siteStr = string(selftest.SiteCI)
	}
	if pack == "" {
		pack = "all"
	}

	var env *selftest.Env
	if s.selfTestEnv != nil {
		env = s.selfTestEnv()
	}
	if env == nil {
		env = &selftest.Env{}
	}

	result, err := selftest.Run(ctx, env, selftest.RunRequest{
		Pack: pack,
		Site: selftest.Site(siteStr),
	})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	// One-line grade summary for agents who only skim text.
	summary := ""
	if result.Single != nil {
		summary = fmt.Sprintf("grade=%s pack=%s site=%s\n", result.Single.Grade, result.Single.Pack, result.Single.Site)
	} else {
		p, f, sk, e := selftest.Summarize(result.Reports)
		summary = fmt.Sprintf("pass=%d fail=%d skip=%d error=%d\n", p, f, sk, e)
	}
	return mcp.NewToolResultText(summary + string(raw)), nil
}
