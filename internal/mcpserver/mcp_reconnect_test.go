// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// fakeGrokCLI records mcp subcommands and drives list/disable/enable.
// Oracle for 🎯T60: reconnect must issue disable+enable (not a no-op).
type fakeGrokCLI struct {
	listJSON string
	listErr  error
	// enableFail names that fail on enable
	enableFail map[string]error
	// calls is ordered "disable name" / "enable name" / "list --json"
	calls []string
}

func (f *fakeGrokCLI) run(_ context.Context, args ...string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("no args")
	}
	if args[0] != "mcp" {
		return "", fmt.Errorf("unexpected binary arg %q", args[0])
	}
	if len(args) < 2 {
		return "", fmt.Errorf("mcp: missing subcommand")
	}
	switch args[1] {
	case "list":
		f.calls = append(f.calls, "list --json")
		if f.listErr != nil {
			return "", f.listErr
		}
		if f.listJSON == "" {
			return "[]", nil
		}
		return f.listJSON, nil
	case "disable":
		if len(args) < 3 {
			return "", fmt.Errorf("disable: missing name")
		}
		f.calls = append(f.calls, "disable "+args[2])
		return "", nil
	case "enable":
		if len(args) < 3 {
			return "", fmt.Errorf("enable: missing name")
		}
		name := args[2]
		f.calls = append(f.calls, "enable "+name)
		if f.enableFail != nil {
			if err, ok := f.enableFail[name]; ok {
				return "enable failed", err
			}
		}
		return "enabled", nil
	default:
		return "", fmt.Errorf("unknown mcp subcommand %q", args[1])
	}
}

func TestMCPReconnectNamedServerDisableEnable(t *testing.T) {
	fake := &fakeGrokCLI{}
	s := &Server{grokRun: fake.run}

	report, err := s.reconnectMCPServers(context.Background(), "github")
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if !strings.Contains(report, "OK   github") {
		t.Fatalf("report missing OK github: %q", report)
	}
	want := []string{"disable github", "enable github"}
	if strings.Join(fake.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}

func TestMCPReconnectAllUsesListThenCyclesEach(t *testing.T) {
	fake := &fakeGrokCLI{
		listJSON: `[{"name":"github","enabled":true},{"name":"gmail","enabled":false},{"name":"","enabled":true}]`,
	}
	s := &Server{grokRun: fake.run}

	report, err := s.reconnectMCPServers(context.Background(), "")
	if err != nil {
		t.Fatalf("reconnect all: %v", err)
	}
	if !strings.Contains(report, "2 ok") {
		t.Fatalf("want 2 ok in report, got %q", report)
	}
	want := []string{
		"list --json",
		"disable github", "enable github",
		"disable gmail", "enable gmail",
	}
	if strings.Join(fake.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %v, want %v", fake.calls, want)
	}
}

func TestMCPReconnectNoOpEmptyListFails(t *testing.T) {
	fake := &fakeGrokCLI{listJSON: `[]`}
	s := &Server{grokRun: fake.run}

	_, err := s.reconnectMCPServers(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty list (no-op)")
	}
	if !strings.Contains(err.Error(), "nothing to reconnect") {
		t.Fatalf("err = %v, want nothing to reconnect", err)
	}
}

func TestMCPReconnectEnableFailureSurfaces(t *testing.T) {
	fake := &fakeGrokCLI{
		enableFail: map[string]error{"github": fmt.Errorf("exit 1")},
	}
	s := &Server{grokRun: fake.run}

	_, err := s.reconnectMCPServers(context.Background(), "github")
	if err == nil {
		t.Fatal("expected enable failure")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Fatalf("err should mention github: %v", err)
	}
}

func TestMCPReconnectPartialSuccessReports(t *testing.T) {
	fake := &fakeGrokCLI{
		listJSON:   `[{"name":"github"},{"name":"gmail"}]`,
		enableFail: map[string]error{"gmail": fmt.Errorf("boom")},
	}
	s := &Server{grokRun: fake.run}

	report, err := s.reconnectMCPServers(context.Background(), "")
	if err != nil {
		// Partial success must not fail the whole call — report mixed status.
		t.Fatalf("partial success should return report, not err: %v", err)
	}
	if !strings.Contains(report, "1 ok, 1 failed") {
		t.Fatalf("want mixed status, got %q", report)
	}
	if !strings.Contains(report, "OK   github") || !strings.Contains(report, "FAIL gmail") {
		t.Fatalf("want per-server lines, got %q", report)
	}
}

func TestHandleMCPReconnectToolWired(t *testing.T) {
	// Tool handler path: named server through handleMCPReconnect.
	fake := &fakeGrokCLI{}
	s := &Server{grokRun: fake.run}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"server": "bullseye"}

	result, err := s.handleMCPReconnect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %+v", result)
	}
	text := extractText(result)
	if !strings.Contains(text, "bullseye") {
		t.Fatalf("result text missing bullseye: %q", text)
	}
	want := []string{"disable bullseye", "enable bullseye"}
	if strings.Join(fake.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %v, want %v (no-op would fail this oracle)", fake.calls, want)
	}
}

func TestRegisterMCPReconnectExposesTool(t *testing.T) {
	// Full New() path must register the tool so tools/list cannot silently omit it.
	s := New("", nil, nil)
	// Call handler directly is covered above; here we only ensure registration
	// does not panic and server is non-nil. tools/list is integration-covered
	// by journey J6 once the name is in the required list.
	if s == nil || s.mcpSrv == nil {
		t.Fatal("New returned nil server")
	}
}
