// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"testing"
)

func TestParseToolsCall(t *testing.T) {
	name, args := parseToolsCall([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"jevons_agent_list","arguments":{"query":"running"}}}`))
	if name != "jevons_agent_list" {
		t.Fatalf("name=%q", name)
	}
	if args["query"] != "running" {
		t.Fatalf("args=%v", args)
	}
	if n, _ := parseToolsCall([]byte(`{"method":"tools/list"}`)); n != "" {
		t.Fatalf("tools/list must not parse as a call: %q", n)
	}
}
