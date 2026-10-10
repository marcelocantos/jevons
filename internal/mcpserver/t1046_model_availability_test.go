// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/mark3labs/mcp-go/mcp"
)

func t1046Server(t *testing.T) *Server {
	t.Helper()
	s := New(t.TempDir(), nil, nil)
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reg)
	return s
}

func TestT1046LaunchConfigUsesCatalogEconomyWhenSparkAbsent(t *testing.T) {
	s := t1046Server(t)
	def, existed, note, err := s.stitchAgentStart("ops", t.TempDir(), "", "codex", "ops_classify", "jevons-po", claudia.PurposeWork, "", "")
	if err != nil || existed {
		t.Fatalf("stitch: existed=%v err=%v", existed, err)
	}
	if def.Model != "gpt-6-luna" || startConfigFromDef(def).Model != "gpt-6-luna" || !strings.Contains(note, "Spark absent") {
		t.Fatalf("registered/launch model=%q/%q note=%q", def.Model, startConfigFromDef(def).Model, note)
	}
	if s.registry.Def("ops").Model != "gpt-6-luna" {
		t.Fatal("registry stored obsolete pin")
	}
}

func TestT1046SparkPresentAndExplicitPinPrecedence(t *testing.T) {
	s := t1046Server(t)
	s.modelCatalog = func() []claudia.CatalogModel {
		return append(claudia.ModelCatalog(), claudia.CatalogModel{Provider: claudia.ProviderCodex, Model: cost.ModelCodexSpark, Access: claudia.ModelAccessPlan, Quality: claudia.ModelQualityEconomy, Session: true})
	}
	def, _, _, err := s.stitchAgentStart("spark", t.TempDir(), "", "codex", "mechanical", "jevons-po", claudia.PurposeWork, "", "")
	if err != nil || def.Model != cost.ModelCodexSpark {
		t.Fatalf("present Spark model=%q err=%v", def.Model, err)
	}
	s.modelCatalog = nil // absent again: caller's explicit pin wins, even if catalog changes
	def, _, _, err = s.stitchAgentStart("explicit", t.TempDir(), cost.ModelCodexSpark, "codex", "mechanical", "jevons-po", claudia.PurposeWork, "", "")
	if err != nil || def.Model != cost.ModelCodexSpark {
		t.Fatalf("explicit pin model=%q err=%v", def.Model, err)
	}
}

func TestT1046FailedLaunchRowRetriesWithSupportedModel(t *testing.T) {
	s := t1046Server(t)
	workdir := t.TempDir()
	// Simulate an older installation's persisted row: the sidecar rejects
	// Spark; the failed Launch keeps the row for a same-name retry.
	s.modelCatalog = func() []claudia.CatalogModel {
		return append(claudia.ModelCatalog(), claudia.CatalogModel{Provider: claudia.ProviderCodex, Model: cost.ModelCodexSpark, Access: claudia.ModelAccessPlan, Quality: claudia.ModelQualityEconomy, Session: true})
	}
	attempts := 0
	s.launchAgentFn = func(_ context.Context, name string) (*claudia.Agent, error) {
		attempts++
		cfg := startConfigFromDef(s.registry.Def(name))
		if attempts == 1 && cfg.Model != cost.ModelCodexSpark {
			t.Errorf("first launch model=%q", cfg.Model)
		}
		if attempts == 2 && cfg.Model != "gpt-6-luna" {
			t.Errorf("retry launch model=%q", cfg.Model)
		}
		if attempts == 1 {
			return nil, errors.New("sidecar model unavailable")
		}
		return nil, nil // hermetic successful launch on the corrected config
	}
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "retry", "workdir": workdir, "provider": "codex", "task_type": "mechanical", "parent": "jevons-po", "purpose": "work"}
	result, err := s.handleAgentStart(t.Context(), req)
	if err != nil || !result.IsError || s.registry.Def("retry").Model != cost.ModelCodexSpark {
		t.Fatalf("initial launch: result=%v err=%v row=%+v", result, err, s.registry.Def("retry"))
	}
	session := s.registry.Def("retry").SessionID
	s.modelCatalog = nil
	result, err = s.handleAgentStart(t.Context(), req)
	if err != nil || result.IsError || attempts != 2 {
		t.Fatalf("retry: result=%v err=%v attempts=%d", result, err, attempts)
	}
	def := s.registry.Def("retry")
	if def.Model != "gpt-6-luna" || def.SessionID != session {
		t.Fatalf("retry row=%+v want same session and supported model", def)
	}
}
