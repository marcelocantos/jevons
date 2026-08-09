// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/secauditor"
	"github.com/marcelocantos/jevons/internal/writconf"
)

func TestWritExecTool_AllowAndDeny(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	a := secauditor.New()
	var msgs []string
	a.Deliverer = deliverFn(func(text string) error {
		msgs = append(msgs, text)
		return nil
	})
	s.SetSecurityAuditor(a)
	s.SetWritExecutor(writconf.PureExecutor{}, "", false)

	// Deny undeclared host under pure confinement.
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"command": "curl",
		"args":    []any{"https://evil.example/"},
		"agent":   "w1",
		"pure":    true,
	}
	res, err := s.handleWritExec(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if len(a.Recent()) == 0 {
		t.Fatal("expected auditor observation on deny")
	}
	if len(msgs) == 0 {
		t.Fatal("expected overseer alert")
	}

	// Allow default fleet host.
	req2 := mcp.CallToolRequest{}
	req2.Params.Arguments = map[string]any{
		"command": "curl",
		"args":    []any{"https://api.x.ai/v1"},
		"pure":    true,
	}
	res2, err := s.handleWritExec(context.Background(), req2)
	if err != nil {
		t.Fatal(err)
	}
	if res2 == nil {
		t.Fatal("nil allow result")
	}

	// Status tool.
	st, err := s.handleSecurityStatus(context.Background(), mcp.CallToolRequest{})
	if err != nil || st == nil {
		t.Fatalf("status: %v %v", st, err)
	}
}

func TestJworkPolicyDenyNotifiesAuditor(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	a := secauditor.New()
	s.SetSecurityAuditor(a)
	s.notifySecurityPolicyDeny("wid", "rm -rf /")
	if len(a.Recent()) != 1 {
		t.Fatalf("recent=%d", len(a.Recent()))
	}
	if a.Recent()[0].Kind != writconf.KindPolicyDeny {
		t.Fatalf("kind %v", a.Recent()[0].Kind)
	}
}

type deliverFn func(string) error

func (f deliverFn) DeliverAlert(text string) error { return f(text) }
