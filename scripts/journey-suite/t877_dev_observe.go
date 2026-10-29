// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

func t877DevelopmentHost() string {
	return fmt.Sprintf("127.0.0.1:%d", config.DevelopmentPort)
}

type t877DevHealth struct {
	Build  string `json:"build"`
	Status string `json:"status"`
}

type t877DevAgent struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Running  bool   `json:"running"`
}

type t877DevMigrateMenu struct {
	Providers []struct {
		Provider string `json:"provider"`
		Band     string `json:"band"`
		Eligible bool   `json:"eligible"`
	} `json:"providers"`
}

// t877DevObserveReport is the read-only development snapshot taken before an
// isolated plan-usage sweep (🎯T877 acceptance bullet 4).
type t877DevObserveReport struct {
	Build              string
	RunningSidecarGrok int
	MigrateProviders   []string
	GrokHot            bool
	ClaudeEligible     bool
}

func t877GET(path string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("http://" + t877DevelopmentHost() + path)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func parseT877DevObserve(healthRaw, agentsRaw, menuRaw []byte) (t877DevObserveReport, error) {
	var health t877DevHealth
	if err := json.Unmarshal(healthRaw, &health); err != nil {
		return t877DevObserveReport{}, fmt.Errorf("health decode: %w", err)
	}
	if health.Status != "ok" {
		return t877DevObserveReport{}, fmt.Errorf("development health status=%q", health.Status)
	}
	var agents []t877DevAgent
	if err := json.Unmarshal(agentsRaw, &agents); err != nil {
		return t877DevObserveReport{}, fmt.Errorf("agents decode: %w", err)
	}
	var menu t877DevMigrateMenu
	if err := json.Unmarshal(menuRaw, &menu); err != nil {
		return t877DevObserveReport{}, fmt.Errorf("migrate/options decode: %w", err)
	}
	if err := t877ValidateMigrateMenu(menu); err != nil {
		return t877DevObserveReport{}, err
	}
	rep := t877DevObserveReport{Build: health.Build}
	for _, row := range menu.Providers {
		rep.MigrateProviders = append(rep.MigrateProviders, row.Provider)
		switch strings.ToLower(row.Provider) {
		case "grok":
			rep.GrokHot = row.Band == string(planusage.BandHot)
		case "claude":
			rep.ClaudeEligible = row.Eligible
		}
	}
	for _, a := range agents {
		if !a.Running {
			continue
		}
		if seatprovider.Resolve(claudia.Provider(a.Provider)).Plan == seatprovider.Grok &&
			seatprovider.Resolve(claudia.Provider(a.Provider)).Sidecar {
			rep.RunningSidecarGrok++
		}
	}
	return rep, nil
}

func t877ValidateMigrateMenu(menu t877DevMigrateMenu) error {
	seen := map[string]int{}
	for _, row := range menu.Providers {
		p := strings.ToLower(strings.TrimSpace(row.Provider))
		seen[p]++
		if p == "xai-oauth" {
			return fmt.Errorf("migrate menu invented unpublished xai-oauth row")
		}
	}
	for p, n := range seen {
		if n > 1 {
			return fmt.Errorf("duplicate migrate menu row for %s (%d)", p, n)
		}
	}
	return nil
}

// t877ValidateDevDecisionsForHotGrok checks Claudia's read-only placement
// preview for running Grok sidecar seats when Grok is hot and Claude eligible.
func t877ValidateDevDecisionsForHotGrok(rep t877DevObserveReport, decisions []planusage.PlanAction, agents []t877DevAgent) error {
	if rep.RunningSidecarGrok == 0 || !rep.GrokHot || !rep.ClaudeEligible {
		return nil
	}
	byName := map[string]planusage.PlanAction{}
	for _, d := range decisions {
		byName[d.Name] = d
	}
	var missing []string
	for _, a := range agents {
		if !a.Running {
			continue
		}
		if seatprovider.Resolve(claudia.Provider(a.Provider)).Plan != seatprovider.Grok {
			continue
		}
		d, ok := byName[a.Name]
		if !ok || d.Action != planusage.SeatMigrate || d.To != "claude" || d.Author != claudia.DecisionAuthor {
			missing = append(missing, fmt.Sprintf("%s: %+v", a.Name, d))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("hot Grok sidecar seats missing Claude migrate preview: %s", strings.Join(missing, "; "))
	}
	return nil
}

func t877ObserveDevelopmentFleet() (t877DevObserveReport, error) {
	healthRaw, err := t877GET("/health")
	if err != nil {
		return t877DevObserveReport{}, fmt.Errorf("development fleet observe: %w", err)
	}
	agentsRaw, err := t877GET("/api/agents")
	if err != nil {
		return t877DevObserveReport{}, fmt.Errorf("development fleet observe: %w", err)
	}
	menuRaw, err := t877GET("/api/migrate/options")
	if err != nil {
		return t877DevObserveReport{}, fmt.Errorf("development fleet observe: %w", err)
	}
	rep, err := parseT877DevObserve(healthRaw, agentsRaw, menuRaw)
	if err != nil {
		return t877DevObserveReport{}, fmt.Errorf("development fleet observe: %w", err)
	}
	decRaw, err := t877GET("/api/plan-usage/decisions")
	if err != nil {
		return t877DevObserveReport{}, fmt.Errorf("development fleet observe: %w", err)
	}
	var decisions []planusage.PlanAction
	if err := json.Unmarshal(decRaw, &decisions); err != nil {
		return t877DevObserveReport{}, fmt.Errorf("development decisions decode: %w", err)
	}
	var agents []t877DevAgent
	if err := json.Unmarshal(agentsRaw, &agents); err != nil {
		return t877DevObserveReport{}, fmt.Errorf("development agents decode: %w", err)
	}
	if err := t877ValidateDevDecisionsForHotGrok(rep, decisions, agents); err != nil {
		return rep, err
	}
	fmt.Printf("J48-t877 dev observe (read-only, no sweep): build=%s running_grok_sidecar=%d menu=%v grok_hot=%v claude_eligible=%v\n",
		rep.Build, rep.RunningSidecarGrok, rep.MigrateProviders, rep.GrokHot, rep.ClaudeEligible)
	return rep, nil
}
