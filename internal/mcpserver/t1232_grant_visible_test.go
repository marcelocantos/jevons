// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// The list and start results must describe the stored grant, including roots,
// rather than guessing a policy from the provider name.
func TestT1232GrantVisibleAtListAndStartCallSites(t *testing.T) {
	t791Steerable(t)
	s, reg := t541Server(t)
	s.launchAgentFn = func(context.Context, string) (*claudia.Agent, error) { return nil, nil }
	const name = "jv-t1232-grant"
	dir := t.TempDir()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": name, "workdir": dir, "provider": "codex", "purpose": "work", "parent": "jevons-po"}
	start, err := s.handleAgentStart(t.Context(), req)
	if err != nil || start == nil || start.IsError {
		t.Fatalf("start: %v %s", err, toolText(start))
	}
	def := reg.Def(name)
	if def == nil || def.SandboxMode == "" || len(def.SandboxWritableRoots) == 0 || !def.SandboxGitWrite {
		t.Fatalf("not a minted sandbox grant: %+v", def)
	}
	for label, body := range map[string]string{"start": toolText(start)} {
		assertT1232Grant(t, label, body, *def)
	}
	list, err := s.handleAgentList(t.Context(), mcp.CallToolRequest{})
	if err != nil || list == nil || list.IsError {
		t.Fatalf("list: %v %s", err, toolText(list))
	}
	assertT1232Grant(t, "list", toolText(list), *def)
}

func assertT1232Grant(t *testing.T, label, body string, d claudia.AgentDef) {
	t.Helper()
	for _, want := range []string{"sandbox_grant=", "mode:\"" + d.SandboxMode + "\"", "network:true", "git_write:true", d.SandboxWritableRoots[0]} {
		if !strings.Contains(body, want) {
			t.Errorf("%s missing %q: %s", label, want, body)
		}
	}
}
