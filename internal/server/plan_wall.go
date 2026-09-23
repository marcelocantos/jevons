// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"strings"
)

// planWallText is Cursor's whole-turn refusal. The fleet row shows it
// when that text is the end of the seat's transcript, the same way a
// stop reason is shown, including while the process is still running.
const planWallText = "Upgrade your plan to continue"

// transcriptEndsOnPlanWall reports whether body, the newest transcript
// row, is an assistant turn whose text is the plan wall.
func transcriptEndsOnPlanWall(body string) bool {
	var line struct {
		Type    string `json:"type"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &line); err != nil {
		return false
	}
	if line.Type != "" && line.Type != "assistant" {
		return false
	}
	return strings.TrimSpace(contentText(line.Message.Content)) == planWallText
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, block := range blocks {
		b.WriteString(block.Text)
	}
	return b.String()
}

// decoratePlanWalls sets PlanWall on rows whose transcript ends on the
// plan wall. A missing store leaves the rows unchanged.
func (s *Server) decoratePlanWalls(agents []agentInfo) []agentInfo {
	db := s.stateStore()
	if db == nil {
		return agents
	}
	for i := range agents {
		ev, ok, err := db.Last(agents[i].Name)
		if err != nil || !ok || !transcriptEndsOnPlanWall(ev.Body) {
			continue
		}
		agents[i].PlanWall = planWallText
	}
	return agents
}
