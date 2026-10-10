// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/cost"
	"github.com/marcelocantos/jevons/internal/spool"
)

// unsupportedPinEvidence is diagnostic only. It is deliberately NOT a proof
// of emptiness: Codex may have accepted a prompt not yet flushed to rollout,
// and the sidecar may have an in-flight turn or an independently configured
// store. A missing file must never authorize a model or session rotation.
func unsupportedPinEvidence(d *claudia.AgentDef) string {
	var findings []string
	home := codexHomeDir(d.SessionID)
	root := filepath.Join(home, "sessions")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == root {
				return nil
			}
			findings = append(findings, fmt.Sprintf("native rollout unreadable at %s: %v", path, walkErr))
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".jsonl") && !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		if !strings.Contains(path, d.SessionID) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			findings = append(findings, fmt.Sprintf("native rollout unreadable at %s: %v", path, err))
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 4096), 16*1024*1024)
		rows, prompts, replies := 0, 0, 0
		for sc.Scan() {
			rows++
			var row struct {
				Type    string `json:"type"`
				Payload struct {
					Type string `json:"type"`
					Role string `json:"role"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
				findings = append(findings, fmt.Sprintf("native rollout partial/unreadable at %s row %d: %v", path, rows, err))
				return nil
			}
			if row.Type == "event_msg" && row.Payload.Type == "user_message" || row.Type == "response_item" && row.Payload.Role == "user" {
				prompts++
			}
			if row.Type == "response_item" && row.Payload.Role == "assistant" {
				replies++
			}
		}
		if err := sc.Err(); err != nil {
			findings = append(findings, fmt.Sprintf("native rollout unreadable at %s: %v", path, err))
			return nil
		}
		findings = append(findings, fmt.Sprintf("native rollout %s: %d records, %d submitted user messages, %d assistant messages", path, rows, prompts, replies))
		return nil
	})
	if err != nil {
		findings = append(findings, fmt.Sprintf("native rollout scan failed: %v", err))
	}
	records, err := spool.ReadSeat(spool.Dir(), d.Name)
	if err != nil {
		findings = append(findings, fmt.Sprintf("sidecar spool unreadable: %v", err))
	} else if len(records) > 0 {
		findings = append(findings, fmt.Sprintf("sidecar spool has %d seat records (including possible assistant history)", len(records)))
	}
	if len(findings) == 0 {
		return "native rollout and sidecar history not found (absence is not complete evidence)"
	}
	return strings.Join(findings, "; ")
}

// diagnoseUnsupportedStoredPin is called BEFORE EnsureAgentWithParent and
// Register. A persisted row is a session, even if Materialized is false:
// that bit cannot attest that no prompts were submitted. Until Claudia can
// atomically prove empty provider + sidecar history and no in-flight turn,
// refuse automatic rotation and leave the row and its session ID untouched.
func (s *Server) diagnoseUnsupportedStoredPin(d *claudia.AgentDef) error {
	if d == nil || cli.PlanProvider(d.Provider) != claudia.ProviderCodex || d.Model != cost.ModelCodexSpark || s.codexCatalogHasModel(d.Model) {
		return nil
	}
	live := ""
	if p := s.registry.Get(d.Name); p != nil && p.Alive() {
		live = " live process present;"
	}
	return fmt.Errorf("unsupported stored Codex model %q on session %s: refusing automatic model fallback or resume;%s %s. Recovery: preserve this session and its submitted prompts; choose a supported model only after an explicit history-preservation/consent decision (no automatic replay or session rotation)", d.Model, d.SessionID, live, unsupportedPinEvidence(d))
}
