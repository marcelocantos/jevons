// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestCostReportPreservesCanonicalUncertainty(t *testing.T) {
	snap := &cost.Snapshot{
		Accounting:       cost.AccountingSubscription,
		Window:           time.Minute,
		SpentTodayUSD:    9,
		WorkerUSDPerHour: map[string]float64{"worker": 3},
		StructuredUsage: cost.StructuredUsage{
			Known: cost.Usage{Input: 21, CacheRead: 7, Output: 4}, KnownInputTotal: 28,
			Calls: 2, IncompleteCalls: 1, UnknownInputTotal: 1, UnknownOutput: 1, UnknownCost: 2, ObservationFailures: 1,
		},
		Collector: &cost.CollectorHealth{HistoryEnabled: true, InventoryIncomplete: true, LastHistoryErr: "history unavailable"},
	}
	snap.StructuredUsageToday = snap.StructuredUsage
	s := &Server{costSnapshot: func() (*cost.Snapshot, error) { return snap, nil }}
	result, err := s.handleCost(context.Background(), mcp.CallToolRequest{})
	if err != nil || result.IsError || len(result.Content) != 1 {
		t.Fatalf("cost result=%+v err=%v", result, err)
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("not text: %+v", result.Content)
	}
	for _, want := range []string{
		"today: est $9.00", "do not add them", "window: at least 32 consumed tokens",
		"today: at least 32 consumed tokens", "2 observed calls; 1 incomplete",
		"unknown input total in 1, output in 1, cost in 2", "1 observation failures",
		"not subscription quota or proof of complete coverage", "history unavailable",
	} {
		if !strings.Contains(text.Text, want) {
			t.Errorf("missing %q in %s", want, text.Text)
		}
	}
	if strings.Index(text.Text, "per worker:") > strings.Index(text.Text, "Canonical provider observations") {
		t.Fatal("legacy worker estimates appear under the canonical observation section")
	}
	// The public JSON surface carries the same unknown dimensions as prose.
	json := s.CostJSON().(map[string]any)
	u := json["structured_usage"].(map[string]any)
	if u["known_input_total"] != float64(28) || u["unknown_cost_calls"] != float64(2) {
		t.Fatalf("cost JSON lost measurements: %+v", u)
	}
	snap.Collector = nil
	result, err = s.handleCost(context.Background(), mcp.CallToolRequest{})
	if err != nil || !strings.Contains(result.Content[0].(mcp.TextContent).Text, "history recovery is unavailable") {
		t.Fatalf("missing collector claimed coverage: %+v %v", result, err)
	}
}
