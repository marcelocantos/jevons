// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

func TestT1121PanelClassHidesReapedRegistryRow(t *testing.T) {
	s, _ := t401Server(t)
	t401RegisterWork(t, s, "reaped-row")
	if err := s.intentStore().SetAgent("reaped-row", fleetintent.Reaped, "test", "finished", time.Now()); err != nil {
		t.Fatal(err)
	}
	def := s.registry.Def("reaped-row")
	if def == nil {
		t.Fatal("missing def")
	}
	row := s.panelClassForDef(*def, false, false)
	if row.Visible || row.Class != "history" {
		t.Fatalf("reaped registry row = %+v, want hidden history", row)
	}
}

func TestT1121AgentListOmitsHistoryByDefault(t *testing.T) {
	s, dir := t401Server(t)
	if err := s.registry.Register(claudia.AgentDef{
		Name: "po-dead", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	t401RegisterWork(t, s, "reaped-row")
	if err := s.intentStore().SetAgent("reaped-row", fleetintent.Reaped, "test", "finished", time.Now()); err != nil {
		t.Fatal(err)
	}
	res, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	body := targetFileToolText(res)
	if !strings.Contains(body, "po-dead") {
		t.Fatalf("default list missing fault seat:\n%s", body)
	}
	if strings.Contains(body, "reaped-row") {
		t.Fatalf("default list must omit reaped registry row:\n%s", body)
	}
	all, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"history": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(targetFileToolText(all), "reaped-row") {
		t.Fatalf("history list must include reaped row:\n%s", targetFileToolText(all))
	}
}
