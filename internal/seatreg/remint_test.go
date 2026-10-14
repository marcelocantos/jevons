// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatreg

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
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

func TestParkNonFleetAutoStartLeavesTheFleet(t *testing.T) {
	dir := t.TempDir()
	grants, err := New(filepath.Join(dir, GrantsFile))
	if err != nil {
		t.Fatal(err)
	}
	fleet, err := New(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range []claudia.AgentDef{
		{Name: "jevons", Provider: claudia.ProviderGrok, AutoStart: true, SessionID: "fleet"},
		{Name: "leftover", Provider: claudia.ProviderClaude, AutoStart: true, SessionID: "old"},
	} {
		if err := grants.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	if err := fleet.Register(claudia.AgentDef{Name: "jevons", Provider: claudia.Provider("xai-oauth"), SessionID: "fleet"}); err != nil {
		t.Fatal(err)
	}
	n, err := ParkNonFleetAutoStart(grants, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("parked %d, want leftover only", n)
	}
	if got := grants.Def("jevons"); got == nil || !got.AutoStart {
		t.Fatalf("fleet grant parked: %+v", got)
	}
	if got := grants.Def("leftover"); got == nil || got.AutoStart {
		t.Fatalf("leftover still AutoStart: %+v", got)
	}
}

func TestT8666LiveFleetIsSidecarProviders(t *testing.T) {
	if os.Getenv("JEVONS_T866_LIVE") == "" {
		t.Skip("JEVONS_T866_LIVE not set")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	reg, err := New(Path(filepath.Join(home, ".jevons")))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, row := range reg.List() {
		if !subscriptionPlan(row.Provider) {
			continue
		}
		n++
		want := cli.SidecarLaunchProvider(row.Provider)
		if row.Provider != want {
			t.Fatalf("fleet seat %s still stores %q, want sidecar id %q", row.Name, row.Provider, want)
		}
		if row.GrokConnect || row.ConnectURL != "" || row.ConnectPID != 0 {
			t.Fatalf("fleet seat %s still carries Grok CLI connect: %+v", row.Name, row)
		}
	}
	if n < 2 {
		t.Fatalf("live fleet had %d subscription seats; T866.6 is the fleet, not one smoke seat", n)
	}
}

func TestRemintRegistryExceptKeepsSurvivingCLITransport(t *testing.T) {
	reg, err := New(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "survivor", Provider: claudia.ProviderClaude, SessionID: "legacy", Materialized: true},
		{Name: "cold", Provider: claudia.ProviderClaude, SessionID: "cold", Materialized: true},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	n, err := RemintRegistryExcept(reg, t.TempDir(), map[string]bool{"survivor": true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reminted %d, want cold seat only", n)
	}
	if d := reg.Def("survivor"); d.Provider != claudia.ProviderClaude || !d.Materialized || d.SessionID != "legacy" {
		t.Fatalf("surviving CLI changed: %+v", d)
	}
	if d := reg.Def("cold"); d.Provider != "anthropic" || d.Materialized {
		t.Fatalf("cold seat not sidecar: %+v", d)
	}
}

// A syntactically reattachable handoff can still point at a dead PID. Boot
// protects that row before adoption, then remints it once adoption has failed.
func TestT1047StaleHandoffRemintsAfterReattach(t *testing.T) {
	reg, err := New(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: "dead-cli", Provider: claudia.ProviderClaude, SessionID: "old", Materialized: true}); err != nil {
		t.Fatal(err)
	}
	if n, err := RemintRegistryExcept(reg, t.TempDir(), map[string]bool{"dead-cli": true}); err != nil || n != 0 {
		t.Fatalf("before adoption: n=%d err=%v", n, err)
	}
	if got := reg.Def("dead-cli").Provider; got != claudia.ProviderClaude {
		t.Fatalf("early relabel: %s", got)
	}
	if n, err := RemintRegistryExcept(reg, t.TempDir(), nil); err != nil || n != 1 {
		t.Fatalf("after failed adoption: n=%d err=%v", n, err)
	}
	if d := reg.Def("dead-cli"); d.Provider != "anthropic" || d.Materialized {
		t.Fatalf("stale transport left behind: %+v", d)
	}
}
