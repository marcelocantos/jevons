// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
)

func TestT1174AgentListReportsAbsentCursorBrokerSeat(t *testing.T) {
	s, _ := t401Server(t)
	t401RegisterWork(t, s, "jevons-po")

	prevList := brokerSeatsList
	brokerSeatsList = func(context.Context) ([]claudia.BrokerSeat, error) {
		return []claudia.BrokerSeat{
			{Name: "jv-compact-1207c272", Provider: claudia.ProviderCursor, Alive: true},
			{Name: "jevons-po", Provider: claudia.ProviderCursor, Alive: true, Owned: true},
		}, nil
	}
	prevAvail := brokerSeatsAvailable
	brokerSeatsAvailable = func() bool { return true }
	t.Cleanup(func() {
		brokerSeatsList = prevList
		brokerSeatsAvailable = prevAvail
	})

	res, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	text := targetFileToolText(res)
	if !strings.Contains(text, "jv-compact-1207c272") {
		t.Fatalf("agent_list missing absent broker seat:\n%s", text)
	}
	if !strings.Contains(text, "T1174") {
		t.Fatalf("agent_list missing T1174 marker:\n%s", text)
	}
}

func TestT1174AgentListQuietWhenBrokerUnreadable(t *testing.T) {
	s, _ := t401Server(t)
	prevAvail := brokerSeatsAvailable
	brokerSeatsAvailable = func() bool { return false }
	t.Cleanup(func() { brokerSeatsAvailable = prevAvail })

	res, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	text := targetFileToolText(res)
	if strings.Contains(text, "T1174") {
		t.Fatalf("must not invent absence when broker is unknown:\n%s", text)
	}
}
