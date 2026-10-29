// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The boot-time toolless oracle reads two facts: whether any client has made
// an MCP request since boot (tools/call counts, not only tools/list, because
// broker-held seats survive a restart without re-listing), and when the
// overseer was first given work. Both are driven through their real paths.
func TestToollessOracleEvidenceCountsCallsAndOverseerWork(t *testing.T) {
	s := New(t.TempDir(), nil, nil)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	if s.MCPRequestsSinceBoot() != 0 || !s.FirstOverseerDelivery().IsZero() {
		t.Fatal("fresh server already has evidence")
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"jevons_agent_list","arguments":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	mux.ServeHTTP(httptest.NewRecorder(), req)
	if got := s.MCPRequestsSinceBoot(); got != 1 {
		t.Fatalf("tools/call not counted as attach evidence: %d", got)
	}

	s.SetOverseerDeliver(func(string, SendOrigin) error { return nil })
	_, _ = s.deliverToOverseer("jevons", "sentinel note", OriginAgent)
	if s.FirstOverseerDelivery().IsZero() {
		t.Fatal("overseer delivery not recorded")
	}
}
