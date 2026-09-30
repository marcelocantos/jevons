// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"slices"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T934: of the servers a seat launched without, only the fleet-critical
// ones it is configured with are named at mint; an optional server that is
// down (context7 here) is not this target's business.
func TestT934MintNamesOnlyMissingRequiredServers(t *testing.T) {
	d := claudia.AgentDef{Name: "jv-t934-probe", SessionID: "sid-1", Parent: "jevons-po", Provider: "anthropic",
		MCPServers: []claudia.MCPServer{{Name: "jevonsmcp"}, {Name: "bullseye"}, {Name: "context7"}}}

	if got := mintMCPMissing(d, []string{"bullseye", "context7"}); !slices.Equal(got, []string{"bullseye"}) {
		t.Fatalf("missing = %v, want [bullseye]", got)
	}
	if got := mintMCPMissing(d, []string{"context7"}); len(got) != 0 {
		t.Fatalf("an optional server down named %v", got)
	}
	if got := mintMCPMissing(d, nil); len(got) != 0 {
		t.Fatalf("every server answered, yet %v", got)
	}
	unconfigured := claudia.AgentDef{Name: "x", Provider: "anthropic", MCPServers: []claudia.MCPServer{{Name: "context7"}}}
	if got := mintMCPMissing(unconfigured, []string{"bullseye"}); slices.Contains(got, "bullseye") && !configuredFor(unconfigured, "bullseye") {
		t.Fatalf("a server the seat is not configured with was named: %v", got)
	}
	note := FormatMintMCPNotice(d, []string{"bullseye"})
	for _, want := range []string{"jv-t934-probe", "sid-1", "bullseye", "re-mint"} {
		if !strings.Contains(note, want) {
			t.Fatalf("notice %q lacks %q", note, want)
		}
	}
}

// configuredFor mirrors requiredSeatServers' configured set for the check.
func configuredFor(d claudia.AgentDef, name string) bool {
	return slices.Contains(requiredSeatServers(d), name)
}
