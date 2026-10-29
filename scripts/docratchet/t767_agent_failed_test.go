// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package docratchet_test

import (
	"strings"
	"testing"
)

// TestT767DrainUsesClaudiaAgentFailed enforces 🎯T767: the send-queue drain must
// classify broker-relayed refusals through claudia.AgentFailed, not string
// prefixes on Error() text.
func TestT767DrainUsesClaudiaAgentFailed(t *testing.T) {
	body := readRepo(t, "internal/mcpserver/agent_send.go")
	for _, banned := range []string{
		"brokerAgentFailedPrefix",
		"providerRefusalText",
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("agent_send.go still contains banned %q (🎯T767)", banned)
		}
	}
	if !strings.Contains(body, "claudia.AgentFailed") {
		t.Fatal("queueSendDefinitelyNotSent must call claudia.AgentFailed (🎯T767)")
	}
}
