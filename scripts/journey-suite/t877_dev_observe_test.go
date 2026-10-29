// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

func TestT877DevObserveRejectsXaiOAuthMigrateRow(t *testing.T) {
	menu := t877DevMigrateMenu{Providers: []struct {
		Provider string `json:"provider"`
		Band     string `json:"band"`
		Eligible bool   `json:"eligible"`
	}{
		{Provider: "grok", Band: string(planusage.BandHot)},
		{Provider: "xai-oauth", Band: string(planusage.BandUnpublished)},
	}}
	if err := t877ValidateMigrateMenu(menu); err == nil {
		t.Fatal("expected xai-oauth menu row to be rejected")
	}
}

func TestT877DevObserveHotGrokSidecarPreview(t *testing.T) {
	rep := t877DevObserveReport{RunningSidecarGrok: 1, GrokHot: true, ClaudeEligible: true}
	agents := []t877DevAgent{{Name: "jv-grok", Provider: "xai-oauth", Running: true}}
	decisions := []planusage.PlanAction{{
		Name: "jv-grok", From: "grok", To: "claude", Action: planusage.SeatMigrate, Author: claudia.DecisionAuthor,
	}}
	if err := t877ValidateDevDecisionsForHotGrok(rep, decisions, agents); err != nil {
		t.Fatal(err)
	}
	if seatprovider.Resolve("xai-oauth").Plan != seatprovider.Grok {
		t.Fatal("fixture provider must map to grok plan")
	}
}

func TestT877DevObserveParseFixture(t *testing.T) {
	health := `{"build":"abc","status":"ok"}`
	agents := `[{"name":"w","provider":"xai-oauth","running":true}]`
	menu := `{"providers":[{"provider":"grok","band":"hot","eligible":false},{"provider":"claude","band":"ok","eligible":true}]}`
	rep, err := parseT877DevObserve([]byte(health), []byte(agents), []byte(menu))
	if err != nil {
		t.Fatal(err)
	}
	if rep.RunningSidecarGrok != 1 || !rep.GrokHot || !rep.ClaudeEligible {
		t.Fatalf("rep=%+v", rep)
	}
	raw, _ := json.Marshal(rep)
	if !json.Valid(raw) {
		t.Fatal("report must marshal")
	}
}
