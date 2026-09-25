// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatreg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestRemintSubscriptionRewritesPlans(t *testing.T) {
	dir := t.TempDir()
	claude := &claudia.AgentDef{
		Name: "po", Provider: claudia.ProviderClaude,
		Materialized: true, SessionID: "claude-sid",
		GrokConnect: true, ConnectURL: "ws://old", ConnectPID: 9,
	}
	if !RemintSubscription(claude, dir) {
		t.Fatal("claude remint reported no change")
	}
	if claude.Provider != claudia.Provider("anthropic") {
		t.Fatalf("claude = %+v", claude)
	}
	if claude.Materialized || claude.GrokConnect || claude.ConnectURL != "" || claude.ConnectPID != 0 {
		t.Fatalf("claude still carries a vendor session: %+v", claude)
	}
	if claude.SessionID != "claude-sid" {
		t.Fatalf("session id dropped: %q", claude.SessionID)
	}

	grok := &claudia.AgentDef{Name: "jevons", Provider: claudia.ProviderGrok, Materialized: true, SessionID: "g"}
	if !RemintSubscription(grok, dir) {
		t.Fatal("grok remint reported no change")
	}
	if grok.Provider != claudia.Provider("xai-oauth") {
		t.Fatalf("grok remint must store xai-oauth: %+v", grok)
	}
	if grok.Materialized {
		t.Fatal("grok without spool must not RequireResume a vendor JSONL")
	}
	if grok.SessionID != "g" {
		t.Fatalf("grok session id dropped: %q", grok.SessionID)
	}

	cursor := &claudia.AgentDef{Name: "c", Provider: claudia.ProviderCursor, GrokConnect: true}
	if !RemintSubscription(cursor, dir) || cursor.GrokConnect {
		t.Fatalf("cursor = %+v", cursor)
	}

	bedrock := &claudia.AgentDef{Name: "b", Provider: claudia.ProviderBedrock}
	if RemintSubscription(bedrock, dir) {
		t.Fatal("bedrock is not a subscription remint")
	}
}

func TestRemintKeepsSpoolHistory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-25.log"), []byte(
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"po","type":"ready"}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	def := &claudia.AgentDef{
		Name: "po", Provider: claudia.Provider("anthropic"),
		Materialized: true, SessionID: "keep",
	}
	if RemintSubscription(def, dir) {
		t.Fatalf("already-sidecar seat with spool changed: %+v", def)
	}
	if !def.Materialized || def.SessionID != "keep" {
		t.Fatalf("spool history was dropped: %+v", def)
	}
}

func TestRemintRegistryIsTheFleet(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	reg, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range []claudia.AgentDef{
		{Name: "jevons", Provider: claudia.ProviderGrok, Materialized: true, SessionID: "g"},
		{Name: "jevons-po", Provider: claudia.ProviderCursor, Materialized: true, SessionID: "c"},
		{Name: "other", Provider: claudia.ProviderBedrock, SessionID: "b"},
	} {
		if err := reg.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	n, err := RemintRegistry(reg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("reminted %d, want 2 subscription seats", n)
	}
	if got := reg.Def("jevons"); got == nil || got.Materialized || got.Provider != claudia.Provider("xai-oauth") {
		t.Fatalf("jevons = %+v", got)
	}
	if got := reg.Def("jevons-po"); got == nil || got.Provider != claudia.ProviderCursor {
		t.Fatalf("jevons-po = %+v", got)
	}
	if got := reg.Def("other"); got == nil || got.Provider != claudia.ProviderBedrock {
		t.Fatalf("bedrock touched: %+v", got)
	}
}
